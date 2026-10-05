# Backend foundation

## Catalog sessions

Application code opens a catalog through `database.OpenContext` (or `Open`).
Opening acquires an exclusive advisory lock, applies ordered transactional
migrations, verifies foreign keys, and recovers abandoned imports before returning
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

During pre-0.1 development, schema changes update the initial definition directly.
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

## Verification and follow-on work

Run `go test ./...`, `go test -race ./...`, and `go vet ./...` with the configured
Go toolchain. Tests use temporary catalogs and cover migration rollback/reopen,
cross-process locking, recovery failure, multi-batch import cleanup, metadata
staging, timestamp ordering, and current/history replica semantics. Focused
`EXPLAIN QUERY PLAN` checks verify indexes for the implemented lookup shapes.

This foundation precedes source-digest duplicate detection, full CLI progress and
machine-readable contracts, directory-derived publication data, search, HTTP and
OpenAPI, and directory/history endpoints. Short ADRs for catalog ownership and
snapshot/replica semantics remain proposed pending approval.
