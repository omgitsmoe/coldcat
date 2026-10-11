# Catalog size and startup

Normal catalog opens no longer run a full foreign-key scan when the schema is
already current. Foreign-key enforcement remains enabled, and abandoned imports
are still recovered before serving or importing. Upgrading a version-1 catalog
to version 2 only updates its reader-version marker, without scanning or rewriting
existing indexes. Version 2 prevents older readers from opening compact FTS indexes
with incompatible positional phrase queries. Actual schema migrations still
validate foreign keys before committing.

Version 3 supports directory-ID ancestor memberships. Opening versions 1 or 2
updates the reader-version marker only; it does not rewrite memberships or scan
all foreign keys. This reader supports both legacy path-keyed memberships and the
new layout. Older readers reject version 3. New catalogs use the new layout;
existing catalogs need the explicit compact-copy command below to obtain it.

## Measure before rewriting

Stop the server and other catalog commands. Build the current binary using the
root `just build` recipe, or build an API-only CLI:

```sh
go build -o build/coldcat ./cmd/coldcat
./build/coldcat --db /path/to/catalog.sqlite catalog stats
./build/coldcat --db /path/to/catalog.sqlite catalog stats --json > catalog-storage.json
```

The report lists allocated bytes per SQLite table/index, including FTS shadow
tables, plus freelist pages and total files across complete historical snapshots.
Payload and unused bytes per object are also available in JSON. These are main
database page measurements, not WAL/journal sizes. The object scan can take time
on a large catalog and runs only when explicitly requested.

All maintenance commands acquire normal exclusive catalog ownership and use
normal schema migration and abandoned-import recovery. Back up the stopped
catalog first if you need a completely unchanged pre-recovery copy.

Full validation is explicit:

```sh
./build/coldcat --db /path/to/catalog.sqlite catalog check
```

This checks SQLite integrity, FTS consistency against its external content table,
and foreign keys. It is intentionally not a normal-startup requirement.

## Create a compacted copy

```sh
./build/coldcat --db /path/to/catalog.sqlite catalog compact \
  --output /path/to/catalog-compact.sqlite
./build/coldcat --db /path/to/catalog-compact.sqlite catalog stats
```

The output must not exist. Compaction copies the catalog, rebuilds
ancestor memberships as a `WITHOUT ROWID` table keyed by directory ID and content
ID, rebuilds the trigram index with
column-level rather than positional detail and no token-count table, reclaims
free pages in the copy, and validates it. It does not delete history or rewrite
the source as part of compaction. New catalogs already use these compact layouts.
The logical directory keys and file/search path indexes remain unchanged.
`directory_content` becomes a compatibility view joining `directory_membership`
to `directory`; query predicates retain indexed directory and membership seeks.
Directory IDs are existing immutable directory row IDs, not a new global path
dictionary. Memberships cascade when abandoned directories are recovered;
completed normalized memberships reject insertion, updates, and deletion.

Allow substantial additional disk space: the source and destination coexist,
and rebuilding plus `VACUUM` can require another large temporary database and
journals. Space requirements are not simply the final destination size. Stop all
catalog commands before manually switching files; keep the original until you
have verified application workflows on the copy. Do not use output from an
interrupted process: handled failures remove the destination, but a killed process
can leave an incomplete file.

## Tradeoffs and measurements

Directory memberships no longer repeat ancestor strings in either their primary
storage or reverse-content index. Resolving the public snapshot/path key adds a
directory lookup; reverse-content queries join directories to recover those keys.
Direct-child file listings and file/search path seek indexes are unchanged.

Compact FTS stores which columns contain a trigram, not its positions. Substring
queries intersect distinct Unicode trigrams and verify the complete folded
substring against canonical search text. That verification is necessary to reject
grams occurring in the wrong order or at separated positions. Search ranking and
pagination retain their existing semantics. Very broad candidate sets can still
be expensive; compactness does not guarantee faster searches.

Focused, warm synthetic measurements use:

```sh
go test ./internal/database -run '^$' \
  -bench '^Benchmark(DirectoryMembershipLayout|SearchTrigramLayout)/' \
  -benchtime=20x -benchmem -v
```

The fixtures cover 50,000 ancestor-content memberships with long directory keys
and 50,000 distinct nested file paths. Candidate-baseline cases compare the old
full-detail phrase retrieval with compact trigram intersection; workflow cases
exercise the current complete search on both physical layouts. These results
are warm serial means, not cold-cache latency, p95, or a prediction for a real
30 GB catalog. Use the actual object report to estimate remaining savings before
changing paths to integer references or removing indexes.

### Full-catalog pass: directory-ID memberships

`BenchmarkCatalogStorage` measures complete catalogs, not selected objects. The
fixed fixture has two disks with two snapshots each, 12,500 files per snapshot
(50,000 historical observations), 25,000 distinct contents, 15,000 distinct
Unicode paths, and 328,000 ancestor-content memberships. Initial snapshots have
10,000 distinct contents each; 20% of each disk's second snapshot changes both
path and content, yielding 10,500 distinct contents per second snapshot. Remaining
history and duplicates are retained. Files have seven directory ancestors plus
the root, including long components and Japanese characters. Transactional SQL
seeds canonical records with search triggers enabled; production directory build
and snapshot completion build/seal the full indexes. This is not an importer
throughput measurement.

To compare revisions reproducibly, copy the **same** benchmark source to isolated
worktrees; never run these measurements against an application catalog:

```sh
scratch=$(mktemp -d /tmp/opencode/coldcat-storage-XXXXXXXX) || exit 1
git worktree add --detach "$scratch/original" e768b145b4fe494d716c406beebaa2387b89ca6d
git worktree add --detach "$scratch/prior" ec2b170
for tree in "$scratch/original" "$scratch/prior"; do
  cp internal/database/catalog_storage_benchmark_test.go "$tree/internal/database/"
  go -C "$tree" test ./internal/database -run '^$' \
    -bench '^BenchmarkCatalogStorage' -benchtime=100x -benchmem -v
done
go test ./internal/database -run '^$' \
  -bench '^BenchmarkCatalogStorage' -benchtime=100x -benchmem -v
```

The benchmark owns disposable catalogs and checks record/index counts. It reports
total main-database allocated bytes before and after equal `VACUUM` treatment,
including all search/directory indexes and free pages, followed by largest objects.

Measured on Linux/amd64, Go 1.27.1, Ryzen 5 9600X, SQLite's default 4,096-byte pages:

| Revision/layout | Before VACUUM | After VACUUM | After / original |
| --- | ---: | ---: | ---: |
| Original `e768b14` | 166,932,480 | 154,939,392 | 100% |
| Prior `ec2b170` | 135,335,936 | 123,719,680 | 79.85% |
| Directory-ID memberships | 90,456,064 | 83,476,480 | 53.88% |

All three had 4,096 free bytes before and zero after `VACUUM`. This pass reduces
the whole compacted catalog by 40,243,200 bytes (32.53% versus prior), not by the
percentage measured for a single component. Membership objects fall from
47,042,560 to 6,799,360 bytes. No required index or historical record is removed.

Warm serial query means, 100 iterations on the same compacted fixture:

| Workflow | Original | Prior | Directory-ID memberships |
| --- | ---: | ---: | ---: |
| Root detail | 8.663 ms | 8.100 ms | 7.966 ms |
| Nested detail | 0.187 ms | 0.182 ms | 0.181 ms |
| Direct entries | 0.949 ms | 0.977 ms | 0.963 ms |
| Unicode-containing filename substring search | 2.436 ms | 2.257 ms | 2.237 ms |

Fixture construction took 2.909 / 2.853 / 3.021 seconds respectively. Normalized
insertion resolves directory IDs and enforces completed-membership immutability;
this run was 5.9% slower to construct than prior. An earlier 100-iteration run
showed nested detail 6.7% slower instead of 0.8% faster; this small-query timing is
noisy. These single-run warm means are not confidence
intervals, cold latency, p95, or an extrapolation to a real 30 GB catalog.

The existing isolated 50,000-file size-enrichment benchmark (20 iterations,
directory-only fixture with one shared content and 100 leaf directories) measured
8.673 ms prior versus 10.574 ms normalized: a 21.9% / 1.901 ms increase during
publication. This measures a different fixture, not the full-catalog workflows:

```sh
go test ./internal/database -run '^$' \
  -bench '^BenchmarkDirectoryEnrichment/50000$' -benchtime=20x -benchmem -v
```

Compact-copy was also measured end-to-end on the same full-index fixture seeded
with the prior path-keyed membership layout:

```sh
go test ./internal/database -run '^$' \
  -bench '^BenchmarkCatalogCompactUpgrade$' -benchtime=1x -benchmem -v
```

The 135,335,936-byte source produced a validated 83,472,384-byte copy in 1.952
seconds, retaining four complete snapshots and 50,000 historical observations.
The copy is one page smaller than the fresh normalized catalog; these are separate
physical builds of the same records. Small correctness tests compare every logical
table/view record, source bytes, search results, and directory details; they also
exercise abandoned-import cleanup, repeated compaction, locks, cancellation,
completed-membership immutability, reader-version rejection, and failed mapping
without silently discarding memberships.

The synthetic catalog is still above the 50% target. The actual large catalog was
unavailable and the workspace catalog was not opened. Million-input execution
remains permission-blocked. The next measured candidate is `directory_file`:
its repeated directory/file strings and parent index consume 29,306,880 bytes
here. Any replacement must preserve fast direct-child seek pagination rather than
assume that arbitrary path-to-ID normalization will be cheap.
