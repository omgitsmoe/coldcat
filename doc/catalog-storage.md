# Catalog size and startup

Normal catalog opens no longer run a full foreign-key scan when the schema is
already current. Foreign-key enforcement remains enabled, and abandoned imports
are still recovered before serving or importing. Upgrading a version-1 catalog
to version 2 only updates its reader-version marker, without scanning or rewriting
existing indexes. Version 2 prevents older readers from opening compact FTS indexes
with incompatible positional phrase queries. Actual schema migrations still
validate foreign keys before committing.

Version 3 introduced directory-ID ancestor memberships. That reader advanced
versions 1 or 2 with a marker-only update, without rewriting memberships or
scanning all foreign keys. Both legacy path-keyed memberships and the new layout
remain supported. Older readers reject version 3. Existing catalogs need the
explicit compact-copy command below to obtain normalized memberships.

Version 4 also supports directory-ID direct-file memberships with retained full
file-path seek indexes. That reader advanced versions 1, 2, or 3 to version
4 with a metadata-only update: neither membership layout is rewritten at startup.
Older readers reject version 4. New catalogs use both normalized layouts; explicit
compact-copy converts existing catalogs, including original version-1 layouts.

Version 5 stores only the basename in direct-file memberships. The current reader
advances versions 1–4 to version 5 with a metadata-only marker update and detects
the entry layout from column metadata. Existing paths/memberships are not rewritten
or scanned at startup. All four old layouts remain readable until explicit
compact-copy; older readers reject version 5. New catalogs use basename entries.

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
column-level rather than positional detail and no token-count table, rebuilds
direct-file memberships using directory IDs and indexed basenames,
reclaims free pages in the copy, and validates it. It does not delete history or rewrite
the source as part of compaction. New catalogs already use these compact layouts.
The logical directory keys and file/search paths remain unchanged.
`directory_content` becomes a compatibility view joining `directory_membership`
to `directory`; query predicates retain indexed directory and membership seeks.
Directory IDs are existing immutable directory row IDs, not a new global path
dictionary. Memberships cascade when abandoned directories are recovered;
completed normalized memberships reject insertion, updates, and deletion.
`directory_file` likewise becomes a compatibility view over `directory_entry`
joined to `directory`. Its insertion trigger requires a matching directory;
missing mappings fail rather than discard records. Observation IDs remain unique
primary keys, and completed entries reject insertion, updates, and deletion.

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
Direct-child listings resolve the directory ID before seeking the ordered basename
index. Canonical paths come from selected observations; compatibility-view paths
are reconstructed with the literal parent prefix. Observation and search path
indexes are unchanged.

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

After that pass the synthetic catalog was still above the 50% target. The actual large catalog was
unavailable and the workspace catalog was not opened. Million-input execution
remains permission-blocked. The next measured candidate is `directory_file`:
its repeated directory/file strings and parent index consume 29,306,880 bytes
here. Any replacement must preserve fast direct-child seek pagination rather than
assume that arbitrary path-to-ID normalization will be cheap.

### Full-catalog pass: directory-ID direct-file memberships

This pass replaces only the repeated snapshot/parent-directory keys in
`directory_file` with an existing immutable directory ID. `directory_entry` retains
the observation-ID primary key and the full file path; `directory_file_parent`
orders `(directory_id,path,observation_id)`. The compatibility view resolves the
public snapshot/path key through the unique directory index. Direct-child seeks
with an anchor and observation join use the ordered index without a temporary
sort, enforced by query-plan tests. Current/history selection, replica counts,
fingerprints, canonical paths, observation IDs, and all required indexes remain.

A path-keyed `WITHOUT ROWID` file table was considered but not implemented:
its unique observation-ID secondary index would also carry the long primary key.
Removing indexed full paths was rejected because it would require joining and
sorting observations for direct-child pagination. This retained-path directory-ID
choice is a proposed ADR topic; no ADR was created without owner approval.

The **unchanged** fixed `BenchmarkCatalogStorage` fixture measured:

| Revision/layout | Before VACUUM | After VACUUM | After / original |
| --- | ---: | ---: | ---: |
| Original `e768b14` | 166,932,480 | 154,939,392 | 100% |
| Previous `aa6fe87` | 90,456,064 | 83,476,480 | 53.88% |
| Directory-ID direct-file memberships | 76,939,264 | 70,475,776 | 45.49% |

The current before/after runs each have 4,096 free bytes before `VACUUM` and zero
after. This pass saves **13,000,704 total compacted bytes (15.57% versus previous)**.
Relative to the fixed original, it saves **84,463,616 bytes (54.51%)**. The
77,469,696-byte fixture target is reached. This is a **fixture-only achievement**;
real-catalog savings remain unverified and are not the chosen stopping criterion.
The workspace catalog was never opened. Million-input execution remains blocked.
Direct-file objects alone fall from 29,306,880 to 16,302,080 bytes; this component
number is not substituted for the full-catalog total above.

Warm serial means, 100 iterations on the identically VACUUMed full fixture:

| Workflow | Previous | New | Delta |
| --- | ---: | ---: | ---: |
| Root detail | 7.828 ms | 8.074 ms | +3.1% |
| Nested detail | 0.177 ms | 0.185 ms | +4.3% |
| Direct entries | 0.960 ms | 0.976 ms | +1.6% |
| Unicode-containing filename substring search | 2.236 ms | 2.315 ms | +3.5% |

Full-fixture construction increased from 3.045 to 3.254 seconds (+6.9%); the
insertion trigger resolves parent IDs and enforces completed-entry immutability.
The production `BenchmarkImportPhases` fixture (10,000 files, 7,500 contents,
nested paths, 20 iterations) also measured total import time:

| Batch size | Previous | New | Delta |
| --- | ---: | ---: | ---: |
| 1,000 | 524.878 ms | 537.657 ms | +2.4% |
| 5,000 | 502.000 ms | 483.870 ms | -3.6% |
| 10,000 | 470.467 ms | 479.425 ms | +1.9% |

For batch 1,000, directory construction increased 207.0 to 214.6 ms (+3.7%),
and publishing increased 78.17 to 84.91 ms (+8.6%, including the new view join
for index-completeness validation). Across batches, construction deltas ranged
-1.0% to +3.7%; publishing ranged +1.8% to +8.6%. These are single-run warm
means, not confidence intervals, cold latency, p95, or large-catalog predictions.
The preceding pass's isolated size-enrichment +21.9% tradeoff remains documented
above; this pass does not change that query or the ancestor-membership layout.

Explicit compact-copy of the full prior path-keyed layouts produced a
**70,471,680-byte validated output from a 135,335,936-byte source in 3.111 seconds**.
It retained four complete snapshots and 50,000 historical observations. The copy
is one page smaller than the fresh new-layout fixture, a different physical build
of identical logical records. Regression tests cover original v1 rowid memberships
and full-detail FTS, v2 path-keyed memberships, and v3 normalized ancestor
memberships, all with legacy direct-file tables. Startup changes only the reader
marker; explicit compaction converts both membership layouts, installs guards
after copying completed history, rebuilds FTS, vacuums, and validates. Tests also
compare every logical record and source bytes, exercise repeat compaction,
abandoned-import cleanup, immutable entries, missing-directory failure with
destination cleanup, cancellation, locking, and old-reader rejection.

Reproduction against the immediately previous revision:

```sh
git worktree add --detach /tmp/opencode/coldcat-entry-baseline-aa6fe87 aa6fe8756cfe542635d67572359d27e507364c88
go -C /tmp/opencode/coldcat-entry-baseline-aa6fe87 test ./internal/database \
  -run '^$' -bench '^BenchmarkCatalogStorage' -benchtime=100x -benchmem -v
go test ./internal/database -run '^$' -bench '^BenchmarkCatalogStorage' \
  -benchtime=100x -benchmem -v
go test ./internal/database -run '^$' -bench '^BenchmarkCatalogCompactUpgrade$' \
  -benchtime=1x -benchmem -v
# Run in both revisions, sequentially to avoid CPU contention:
go test ./internal/importer -run '^$' -bench '^BenchmarkImportPhases' \
  -benchtime=20x -benchmem -v
```

Measurement logs: `/tmp/opencode/catalog-entry-{before,after,compact}-final.txt`
and `/tmp/opencode/catalog-entry-import-{before,after}-20x-final.txt`.
Full normal tests with an owned `/home/agent` temporary directory, `go vet ./...`,
and focused serial race tests for compact-copy, legacy opens, and directory seek
plans passed. The full race suite is reserved for the parent's once-at-end check.

### Full-catalog pass: basename direct-file entries (reader version 5)

Within an immediate parent, the full-path prefix is constant. Storing the basename
in both `directory_entry` and `(directory_id,name,observation_id)` preserves binary
Unicode ordering while avoiding two copies of that prefix. Public cursors still
identify canonical observations/directories. Direct-file queries remove the parent
prefix from the anchor, seek the basename index, and retrieve canonical full paths
only for selected observations. Recursive queries retain the original observation
path index. The compatibility view reconstructs literal paths for existing index
builders; insertion and v4 conversion reject mismatched parent/path mappings rather
than silently change them. Completed-entry guards and all required indexes remain.

Publication validation uses separate indexed observation/entry/directory joins on
this layout. A left join to the compatibility view can materialize all entries and
reconstruct every path, including unrelated history. Plan tests prohibit scans and
temporary sorts in direct-child seeks and the new publication validation query.
Both parent and basename must match the canonical observation; missing entries and
wrong snapshot/directory/name mappings still prevent publication.

This is a proposed ADR topic: constant-prefix elimination instead of removing the
ordered index or sorting joined observations. Alternatives are retained full paths
(v4), or observation-only membership with expensive child-order retrieval. The
consequences are a new reader marker, two supported entry query layouts, and explicit
copy conversion. No ADR was created without owner approval.

The **unchanged** fixed full-index fixture, including all four snapshots, measured:

| Layout | Before VACUUM | After VACUUM | After / original |
| --- | ---: | ---: | ---: |
| Original `e768b14` | 166,932,480 | 154,939,392 | 100% |
| Fixed v4 `ed2e792` | 76,939,264 | 70,475,776 | 45.49% |
| Basename entries | 62,554,112 | 57,982,976 | 37.42% |

All matched v4/candidate builds had 4,096 free bytes before VACUUM and zero after.
This saves **12,492,800 whole-catalog bytes (17.73% versus fixed v4)**, or **62.58%**
versus the original fixture. Entry table/index objects fall from 16,302,080 to
3,809,280 bytes; this component measurement does not replace the catalog total.
Five explicit v4-copy upgrades each produced a validated **57,978,880-byte** output
from a **76,939,264-byte** source, retaining all history; median copy/rebuild/VACUUM/
validation time was **1.157 s**. The copy is one page smaller than a fresh catalog,
as in previous passes.

Acceptance is cumulative versus **fixed v4 `ed2e792`**, not versus an intermediate
candidate. Five matched isolated samples per workflow ran sequentially, v4 then
candidate, with `-benchtime=1s` on the identical VACUUMed full-index fixture. These
are medians of warm serial run means, **not p95 or cold-cache measurements**.

| Workflow | v4 median ms | Basename median ms | Cumulative delta |
| --- | ---: | ---: | ---: |
| Root detail | 7.784945 | 7.912216 | +1.63% |
| Nested detail | 0.166863 | 0.166923 | +0.04% |
| Direct children, first page | 0.976275 | 0.955723 | -2.11% |
| Direct children, deep anchor (100th of 125 files) | 1.050244 | 1.028712 | -2.05% |
| Historical direct children | 0.970604 | 0.949758 | -2.15% |
| Replica-filtered direct children | 1.220137 | 1.207195 | -1.06% |
| Recursive entries | 0.936437 | 0.946539 | +1.08% |
| Root entries | 0.357988 | 0.352310 | -1.59% |
| Exact Unicode name, hit | 0.561430 | 0.559909 | -0.27% |
| Exact Unicode name, miss | 0.510749 | 0.512670 | +0.38% |
| Exact Unicode path, hit | 0.561969 | 0.562734 | +0.14% |
| Exact Unicode path, miss | 0.507828 | 0.515040 | +1.42% |
| Name substring, common | 30.800549 | 30.915718 | +0.37% |
| Name substring, rare | 2.273901 | 2.283379 | +0.42% |
| Name substring, Unicode | 29.291583 | 29.335735 | +0.15% |
| Path substring, common | 33.331995 | 33.136976 | -0.59% |
| Path substring, rare | 3.264194 | 3.272318 | +0.25% |
| Path substring, Unicode | 30.731232 | 30.969990 | +0.78% |

All measured lookup/search medians satisfy the +5% cap. The benchmark source is
copied unchanged to the v4 worktree; fixture records are unchanged. Query counts
range from 30–40 broad substring searches to thousands of small lookups per sample.
The later publication-only change does not alter these read queries.

Operational costs were measured separately with five matched samples, ten imports
per sample, on the existing 10,000-file / 7,500-content import-phase fixture:

| Batch | Total v4/new ms | Ingestion v4/new ms | Directory build v4/new ms | Publication v4/new ms |
| --- | ---: | ---: | ---: | ---: |
| 1,000 | 828.898 / 818.605 (-1.24%) | 479.1 / 481.3 (+0.46%) | 239.9 / 231.8 (-3.38%) | 110.0 / 107.0 (-2.73%) |
| 5,000 | 601.606 / 585.116 (-2.74%) | 251.0 / 244.7 (-2.51%) | 240.6 / 230.5 (-4.20%) | 110.5 / 109.7 (-0.72%) |
| 10,000 | 568.017 / 556.531 (-2.02%) | 216.6 / 217.4 (+0.37%) | 239.1 / 230.8 (-3.47%) | 113.8 / 108.4 (-4.75%) |

Full-fixture construction medians were 3.138 s v4 / 3.107 s new (-0.99%), from
five matched builds. This SQL fixture builds production indexes but bypasses the
importer and completes snapshots directly; it is not importer throughput.

Before changing publication validation, the separate 50,000-file late enrichment
fixture showed +10.71% in five samples and +1.0% in five reverse-order samples;
the combined ten-sample medians were 33.579 / 35.944 ms (+7.04%). Those exploratory
results prompted the indexed validation change, rather than ignoring this cost.
Five fresh matched samples of the final code (20 iterations each) measured
**46.114 / 24.235 ms (-47.45%)**. This fixture shares one initially size-unknown
content across history, excludes search indexing, and times publication, not
standalone lookup latency. Timing drift is substantial between run groups; only
within-group matched comparisons are used for acceptance. No severe operational
regression was evidenced in the final candidate.

Correctness checks include full normal Go tests with an owned `/home/agent`
temporary directory, `go vet ./...`, and focused serial race tests. Legacy tests
cover all v1–v4 physical layouts, metadata-only opens, old-reader rejection,
explicit conversion, immutable completed entries, failed output cleanup, and
source-byte preservation. V4 conversion tests compare every paginated result and
directory detail across current/history, direct/recursive/directory-only/replica
filters, root/nested paths, file/directory name collisions, punctuation, spaces,
backslashes, dot components, emoji, and composed/decomposed Unicode. Existing
tests cover fingerprints, recovery, size enrichment, locking, and cancellation.
The full race suite remains reserved for the parent at the end of all passes.

Reproduction (never use an application catalog):

```sh
git worktree add --detach /tmp/opencode/coldcat-basename-baseline-v4 ed2e7921713db966d88841ce8f57a3e58832edeb
cp internal/database/catalog_basename_benchmark_test.go \
  /tmp/opencode/coldcat-basename-baseline-v4/internal/database/
# Run each pair sequentially, five times, recording each output separately:
go -C /tmp/opencode/coldcat-basename-baseline-v4 test ./internal/database \
  -run '^$' -bench '^BenchmarkCatalogBasenameWorkflows$' -benchtime=1s -benchmem -v
go test ./internal/database -run '^$' \
  -bench '^BenchmarkCatalogBasenameWorkflows$' -benchtime=1s -benchmem -v
# In both revisions, five matched samples of each:
go test ./internal/database -run '^$' -bench '^BenchmarkCatalogStorage$' -benchtime=1x -v
go test ./internal/importer -run '^$' -bench '^BenchmarkImportPhases$' -benchtime=10x -benchmem -v
go test ./internal/database -run '^$' -bench '^BenchmarkDirectoryEnrichment/50000$' \
  -benchtime=20x -benchmem -v
# Candidate-only explicit v4 conversion, five samples:
go test ./internal/database -run '^$' -bench '^BenchmarkCatalogBasenameCompactUpgrade$' \
  -benchtime=1x -count=5 -benchmem -v
```

Raw logs are `/tmp/opencode/catalog-basename-{v4,candidate}-{1..5}.txt`,
`catalog-basename-build-{v4,candidate}-{1..5}.txt`,
`catalog-basename-import-final-{v4,candidate}-{1..5}.txt`,
`catalog-basename-enrichment-final-{v4,candidate}-{1..5}.txt`, and
`catalog-basename-compact.txt`. Earlier enrichment/import candidate logs remain
available under the same prefix without `final`.

The actual 30 GB catalog was unavailable and never opened. Owner-provided totals
are original **29,555,052,544** and v4 compact **16,607,576,064** bytes, with 20
snapshots / 11,388,564 observations and no free pages (43.81% reduction). Those are
owner measurements, not this fixture's 54.51% v4 reduction; version-5 real savings
are **unverified**. The workspace catalog was not touched and million-input runs
remain blocked. Only one pass was accepted here; further passes require fresh
evidence against the same fixed v4 baseline. Ranked candidates for investigation:

1. Derive the non-folded `search_path.name` from its canonical path rather than
   store a second copy of the basename. Retain stored folded search text and both
   ordered exact-search indexes; measure the added basename computation across
   broad candidate sets before accepting even this smaller saving.
2. Share canonical path strings between observation and search storage while
   retaining both observation path/snapshot seek indexes. This is potentially
   larger, but joins and ordering are materially riskier.
3. Further FTS posting representation changes; `search_trigram_data` is the
   owner's largest object, but broad/rare/Unicode substring retrieval must remain
   within the cap and no search function or index can be removed.
