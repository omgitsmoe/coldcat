# Backend foundation

## Catalog sessions

Application code opens a catalog through `database.OpenContext` (or `Open`).
Opening acquires an exclusive advisory lock, initializes the schema transactionally,
verifies foreign keys, and recovers abandoned imports before returning
a usable catalog. All CLI commands use this path. The lock is held until `Close`.

Locking uses `github.com/gofrs/flock`, which supports Windows and Unix. Another
catalog process fails immediately with an actionable busy error. Process exit
releases ownership, including exit without normal cleanup. The adjacent `.lock`
file persists intentionally: deleting it can allow two processes to lock different
files for the same catalog. Ordinary symlink aliases are canonicalized; hard-link
aliases and network-filesystem locking are outside this path-based contract.

Within a session, importing also acquires an exclusive application gate. Queries,
disk creation, and a second import fail while that gate is held.

## Schema initialization and migrations

The schema version is `PRAGMA user_version`. An empty database receives the initial
schema transactionally. Ordered migrations and version checks remain available for
future schema changes, with tests covering rollback, retry, ordering, and reopening.

During pre-0.1 development, schema changes update the initial definition directly;
no upgrade migrations are added. The migration infrastructure is retained for future use.
Recreate development catalogs instead of converting old schemas. Existing inventories
are assumed complete; there is no persistent trust or visibility state and no
confirmation workflow. The initial schema enforces unique `(snapshot_id, path)`
observations and retains content identity `(hash_type, hash)`.

Missing sizes and mtimes are NULL. Supplied size zero means a known empty file.
Capture and import timestamps are required, with explicit or source-mtime capture
provenance. Newer unsupported schema versions and nonempty unversioned catalogs fail
explicitly.

The schema version advances only when the entire migration commits. Timestamp
storage uses fixed-width UTC nanosecond precision so indexed TEXT ordering agrees
with chronological ordering.

## Import publication and recovery

Each input represents a complete inventory of one disk. Imports commit bounded
batches of 1,000 records while their snapshot is internally `importing`.
Known sizes are staged, including enrichment of existing content. Publication
atomically applies those sizes, records counters, removes staging/ownership rows,
and changes the snapshot to `complete`. Completed snapshots and observations are
immutable.

The importer computes a streaming SHA-256 semantic digest over parsed records in
input order. The version-1 encoding starts with a length-prefixed
`coldcat-semantic-inventory-v1` domain string. Each record encodes the full relative
path, canonical hash algorithm, hash bytes, size-known marker and optional size,
then mtime-known marker and optional fixed-width UTC timestamp. Lengths, markers,
and sizes are unsigned 64-bit big-endian integers; strings and byte arrays are
length-prefixed. Unknown metadata has no encoded value. The digest depends on the
supplied records, not existing or enriched catalog metadata.

Publication rejects a completed snapshot with the same disk, input format,
capture instant, and semantic digest before applying staged size enrichment.
The error identifies the existing snapshot. Input filenames, comments, line
endings, and equivalent parsed representations do not affect identity; record
order does. The snapshot stores the 32-byte digest atomically with completion.
Duplicate lookup uses a non-unique partial index over completed snapshots.

Use `import --allow-repeat` to deliberately record another snapshot of the same
inventory. This creates normal historical observations, participates in the
existing capture-time/ID tie-break, and invalidates previous pagination cursors.
Rejected repeats use ordinary failed-import cleanup and preserve cursor validity.

Failure or cancellation immediately runs transactional cleanup with a separate
30-second context. Cleanup removes observations, staging, and the failed snapshot;
it removes import-owned content only if no remaining observations or staging
reference it. Shared content and previously completed metadata are preserved.

If cleanup fails, both the original and cleanup errors are reported. The catalog
session rejects application access until recovery succeeds. A crash may leave an
internal importing snapshot; recovery runs before opening a usable session and
before another import. Recovery failure prevents startup.

`importing` is only a transient lifecycle/crash-recovery marker. Completed snapshots
are queryable, and failed or interrupted imports are removed. Application access is
blocked throughout importing and recovery; queries also exclude import remnants
defensively. Content lookup requires an observation in a completed inventory.

## Query semantics

- Current scope selects the latest complete snapshot of each disk by
  `(captured_at, snapshot_id)`. Later ingestion of an older inventory does not
  replace a newer inventory.
- History scope uses all complete snapshots.
- Locations are distinct `(disk_id, path)` pairs in the requested scope.
- Disk counts are distinct disks, independently of same-disk copies.
- Observation count always counts all complete historical observations.
- Content summaries also expose current location/disk counts, including zero for
  historical-only content.
- Observation detail reports other current locations/disks. It excludes its own
  location or disk only if that identity is currently present for the same content.
  A historical-only content therefore has zero other replicas.

Application methods take contexts and return typed domain results; SQL remains in
the database package. Snapshot results carry capture/import time and provenance.
Unknown content sizes and observation mtimes are pointers with nil meaning unknown.

## CLI

```sh
coldcat --db /path/to/catalog.sqlite create --label archive --capacity 2TB
coldcat --db /path/to/catalog.sqlite import --label archive \
  --captured-at 2023-01-01T00:00:00Z inventory.cshd
coldcat --db /path/to/catalog.sqlite import --disk-id 1 \
  --use-source-mtime inventory.cshd
coldcat --db /path/to/catalog.sqlite serve --listen 127.0.0.1:8080
```

`--db` defaults to `coldcat.sqlite`. Import requires exactly one disk selector and
one capture-time choice. The mtime choice must explicitly be true. Successful
import prints the complete snapshot ID and file/content counts. SIGINT/SIGTERM
cancel through the import context.

CSHD v0 has unknown size; v1 distinguishes an empty size field from supplied zero.
Missing mtime is unknown; supplied Unix epoch zero is known. Paths are disk-relative,
case-sensitive Unicode strings with `/` separators. Backslashes are literal filename
characters, independently of the host OS. Absolute paths, drive-qualified paths,
empty/dot/parent segments, NULs, unsupported declared versions, invalid hex, numeric
overflow, and conflicting known sizes are rejected. Scanner/read failures propagate.

## HTTP content workflow

The server opens through the same lock/migration/recovery path as CLI commands and
holds ownership until requests have drained and the database closes. Stop the server
before importing. `serve` defaults to `127.0.0.1:8080`; startup errors fail the command,
and SIGINT/SIGTERM request graceful shutdown with a ten-second drain deadline.

`internal/httpapi` implements readiness, exact hash lookup, content detail, content
observation pages, observation detail, and complete snapshot detail. See
[openapi.json](openapi.json) for the implemented wire contract. IDs, byte counts and
aggregate counts are decimal strings; timestamps are UTC RFC3339 with optional
fractional seconds, and unknown sizes/mtimes are null. CORS is not enabled.

```sh
curl 'http://127.0.0.1:8080/api/v1/contents/lookup?hash_type=sha256&hash=ab'
curl 'http://127.0.0.1:8080/api/v1/contents/1/observations?scope=current&limit=50'
```

Observation pages sort by immutable observation ID ascending and use indexed
keyset pagination. The default limit is 50 and the maximum is 200. Current lists
contain locations from the latest complete inventory of each disk. History lists
contain every complete observation, including repeated disk/path observations;
this can exceed the historical distinct-location count. `is_current` indicates
whether the observation belongs to its disk's selected current inventory.

The inventory revision is `MAX(id)` over complete snapshots, or zero when none
exist. Completed snapshots cannot be deleted, so every successful publication
advances it, including empty or older-dated inventories. Cursors bind the revision,
content ID, scope, page size, ordering version and last observation ID. Keep scope
and limit unchanged when following `next_cursor`. Changed inventory revisions
return `409 stale_cursor`; restart pagination. Restarts, empty-disk creation and
failed-import cleanup leave cursors valid. Cursors are scoped to the issuing
catalog; detecting reuse against another/recreated database is not implemented.

The revision is derived rather than stored: no catalog identity table or mutation
counter is needed for these read-only endpoints. Cursor revision validation and
page retrieval hold the same application read gate, preventing an in-session
import from publishing between them. Hash lookup uses the importer's supported
algorithms and accepts its current hash lengths; algorithm-specific length
validation remains follow-on work.

## Verification and follow-on work

Run `go test ./...`, `go test -race ./...`, and `go vet ./...` with the configured
Go toolchain. Tests use temporary catalogs and cover migration rollback/reopen,
cross-process locking, recovery failure, multi-batch import cleanup, metadata
staging, timestamp ordering, and current/history replica semantics. Focused
`EXPLAIN QUERY PLAN` checks verify indexes for the implemented lookup shapes.

Focused warm-query benchmarks are available with:

```sh
go test ./internal/database -run '^$' -bench BenchmarkContentQueries -benchmem
```

An initial Linux amd64 run on an AMD Ryzen 5 9600X, with 50,000 observations of
one high-replica content, measured warm hash lookup plus its summary counts at
17.8 ms/op, first observation pages at 0.226 ms/op, and deep pages at 0.254 ms/op.
These are focused query measurements, not the pending catalog-wide latency gate.

This foundation precedes source-digest duplicate detection, full CLI progress and
machine-readable contracts, directory-derived publication data, search, the
remaining HTTP routes, and directory/history endpoints. Short ADRs for catalog ownership and
snapshot/replica semantics remain proposed pending approval.
