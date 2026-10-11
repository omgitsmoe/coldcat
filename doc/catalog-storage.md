# Catalog size and startup

Normal catalog opens no longer run a full foreign-key scan when the schema is
already current. Foreign-key enforcement remains enabled, and abandoned imports
are still recovered before serving or importing. Upgrading a version-1 catalog
to version 2 only updates its reader-version marker, without scanning or rewriting
existing indexes. Version 2 prevents older readers from opening compact FTS indexes
with incompatible positional phrase queries. Actual schema migrations still
validate foreign keys before committing.

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
`directory_content` as a `WITHOUT ROWID` table, rebuilds the trigram index with
column-level rather than positional detail and no token-count table, reclaims
free pages in the copy, and validates it. It does not delete history or rewrite
the source as part of compaction. New catalogs already use these compact layouts.
The logical directory keys and path indexes remain unchanged.

Allow substantial additional disk space: the source and destination coexist,
and rebuilding plus `VACUUM` can require another large temporary database and
journals. Space requirements are not simply the final destination size. Stop all
catalog commands before manually switching files; keep the original until you
have verified application workflows on the copy. Do not use output from an
interrupted process: handled failures remove the destination, but a killed process
can leave an incomplete file.

## Tradeoffs and measurements

Directory memberships no longer store both a rowid record and a separate full
composite primary-key record. This preserves the indexed snapshot/path and
reverse-content access patterns without adding joins.

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
