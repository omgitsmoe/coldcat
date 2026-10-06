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
disk creation/editing, and a second import fail while that gate is held.

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

### CLI output and progress

`import` writes progress to stderr and a final success summary to stdout. Add
`--json` for exactly one newline-terminated object with `snapshot` and `elapsed_ms`.
The snapshot has the same fields as `snapshot list --json`; IDs, counts, and elapsed
milliseconds are decimal strings, and timestamps are UTC RFC3339Nano. Elapsed time
uses a monotonic clock from catalog opening through successful application import,
including recovery and label lookup, but excluding final output serialization.

Progress reports committed file observations in the still-unpublished inventory,
not visible files or distinct contents. The first batch is reported immediately;
intermediate messages are throttled to one per 250 milliseconds. A final partial
batch and the pre-publication phase are always reported. No percentage is claimed
because the total input record count is unknown. Progress also uses stderr in JSON
mode, leaving stdout suitable for scripts.

Application imports accept an optional synchronous typed progress callback. Events
run after batch commits, outside transactions, and immediately before publication.
Callback errors and cancellation use normal failed-import cleanup. No fallible
callback runs after publication. The CLI fails an import if progress output fails.

Successful imports exit zero. Import errors and SIGINT/SIGTERM cancellation before
publication exit one with diagnostics on stderr and no success result on stdout.
Abrupt termination can leave committed import batches; normal catalog opening
recovers them before exposing queries. Process tests cover both graceful signals
and abrupt termination, including cursor preservation after recovery and cursor
invalidation after a subsequent successful import.

Final stdout write failures exit one but do not undo an already published snapshot;
output can be partial in that case. Before retrying, inspect snapshot history or use
the existing duplicate-import diagnostic to identify the completed inventory.

### Publication and cleanup

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
coldcat --db /path/to/catalog.sqlite disk create --label backup --capacity 2TB
coldcat --db /path/to/catalog.sqlite disk list --json
coldcat --db /path/to/catalog.sqlite snapshot list --disk-id 1 --json
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

`create` and `disk create` share flags and output. `disk list` and `snapshot list`
iterate all application pages; `--json` emits one JSON array on stdout, with decimal
strings for IDs, byte counts and counters, and UTC timestamps. Optional disk metadata
and absent latest inventories are null. Human-readable lists emit one row per item;
empty lists emit no rows. Stop the server before running these catalog commands.

CSHD v0 has unknown size; v1 distinguishes an empty size field from supplied zero.
Missing mtime is unknown; supplied Unix epoch zero is known. Paths are disk-relative,
case-sensitive Unicode strings with `/` separators. Backslashes are literal filename
characters, independently of the host OS. Absolute paths, drive-qualified paths,
empty/dot/parent segments, NULs, unsupported declared versions, invalid hex, numeric
overflow, and conflicting known sizes are rejected. Scanner/read failures propagate.

## Disk management

The application supports typed disk creation, listing, detail and partial-update
requests. HTTP exposes `GET/POST /api/v1/disks` and `GET/PATCH /api/v1/disks/{id}`.
Creation requires a nonempty case-sensitive unique label and nonnegative capacity.
Capacity is a decimal string on the wire, bounded by signed 64-bit storage. Labels
are preserved without trimming or normalization. Serial and notes are optional;
empty strings and null clear them. Omitted PATCH fields remain unchanged. Null labels
or capacities, empty PATCH objects, unknown fields and trailing JSON values are rejected.
Write requests require `application/json` and have a 65,536-byte body limit.

Metadata updates are atomic and preserve inventory/observation identities. Reads
return currently stored disk metadata, including for historical observations.
Disk list/detail responses include the latest complete snapshot by `(captured_at,id)`,
or null for disks without a complete inventory. Detail aggregates known content sizes
per path, including repeated contents, and reports `unknown_size_file_count` and
`size_complete`. These are cataloged subtotals, not measured disk usage/free space.
A disk without an inventory has null aggregates; a complete empty inventory has zero
counts and complete size information. Integer overflow fails explicitly. Each disk
page/detail is assembled in a transaction for a coherent read.

Disk lists use primary-key keyset pagination in ID-ascending order, with default 50
and maximum 200 items per page. A cursor includes its kind, version, page size, last
disk ID, initial maximum disk ID, and complete-inventory revision. Newly created
disks do not enter an ongoing traversal. Metadata edits appear as stored when a page
is fetched and do not invalidate cursors; a traversal is not a historical metadata
snapshot. Successful imports invalidate cursors, while unchanged restarts, failed
imports and recovery of abandoned imports preserve them. No metadata revision table
is introduced. List pages use indexed per-disk latest-snapshot lookups; size aggregation
is restricted to detail queries for one selected snapshot.

## HTTP content workflow

The server opens through the same lock/migration/recovery path as CLI commands and
holds ownership until requests have drained and the database closes. Stop the server
before importing. `serve` defaults to `127.0.0.1:8080`; startup errors fail the command,
and SIGINT/SIGTERM request graceful shutdown with a ten-second drain deadline.

`internal/httpapi` implements readiness, disk management, exact hash lookup, content
lists/detail, content observation pages, disk snapshot pages, observation detail,
and complete snapshot detail. See
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

### Content lists and redundancy

`GET /api/v1/contents` lists each eligible content once, ordered by content ID
ascending. Scope defaults to current; history includes contents observed in any
complete inventory. Optional `disk_id` and `directory` select membership while
all summary counts remain catalog-wide, matching content detail for the same scope.
Directories require a disk selector over HTTP. Root is empty (or omit the directory
parameter); non-root paths use case-sensitive `/` segments without absolute,
drive-qualified, empty, dot or parent segments. Directory paths are bounded to
1024 UTF-8 bytes. Wildcard characters and backslashes are literal.

`replica_metric=disks|locations` defaults to disks. `other_replicas` selects an exact
count; `min_other_replicas` and `max_other_replicas` provide inclusive bounds.
Counts are the corresponding current catalog count minus one. Bounds must be
nonnegative signed 64-bit integers, cannot mix exact with range selection, and
require current scope. Historical-only content is excluded from current lists;
unfiltered history lists expose zero current counts for that content.

Content pages use the same 50-default/200-maximum limits and revision lifecycle as
observation pages. Cursors bind normalized filters, page size, endpoint/sort version,
revision and last content ID. Changed filters or invalid anchors return 400;
changed inventory revisions return 409. Empty lists serialize as `[]` and the last
page has `next_cursor: null`. Applied filters are included in every page.

```sh
curl 'http://127.0.0.1:8080/api/v1/contents?other_replicas=0'
curl 'http://127.0.0.1:8080/api/v1/contents?replica_metric=locations&min_other_replicas=2'
curl 'http://127.0.0.1:8080/api/v1/contents?disk_id=1&directory=photos'
```

The query seeks content IDs and uses indexed, correlated observation lookups for
membership and replica predicates. Historical summary fields are computed only
for the bounded page. No new storage or derived-import prerequisites are needed.
First/deep-page query-plan tests check content keyset seeks, observation indexes
and latest-snapshot selection across both scopes and metrics.

The warm benchmark suite now includes 50,000 and 1,000,000 observations with four
locations per content and one/two/three-disk distributions. On Linux amd64 / Ryzen
5 9600X, the million-observation run measured approximately:

| Query | Warm time/op |
| --- | ---: |
| First content page | 0.79 ms |
| Deep content page (including anchor validation) | 0.94 ms |
| History content page | 0.96 ms |
| Disk/directory content page | 0.90 ms |
| Nonselective disk-replica lower bound | 0.90 ms |
| Location-replica bound with no matches | 751 ms |

A no-match replica filter must check all eligible contents, so it scales with
catalog size despite bounded result memory. The 50,000-observation equivalent
measured 35 ms. These are warm focused measurements, not an agreed interactive
latency gate; cold measurements and broader real-world replica distributions
remain performance follow-on work.

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

This foundation precedes full CLI progress and
machine-readable contracts, directory-derived publication data, search, the
remaining HTTP routes, and directory/history endpoints. Short ADRs for catalog ownership and
snapshot/replica semantics remain proposed pending approval.
