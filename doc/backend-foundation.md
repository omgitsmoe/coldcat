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
batch and the `directories` and `publishing` phase starts are always reported.
The difference between their elapsed times measures directory construction; the
final summary follows atomic publication. No percentage is claimed
because the total input record count is unknown. Progress also uses stderr in JSON
mode, leaving stdout suitable for scripts.

Application imports accept an optional synchronous typed progress callback. Events
run after batch commits, outside transactions, before directory construction, and
immediately before publication.
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

Import records require a supported algorithm and its exact decoded digest length:
MD4/MD5 use 16 bytes, SHA-1 uses 20, SHA3-224 uses 28, SHA-256/SHA3-256 use 32,
SHA-384/SHA3-384 use 48, and SHA-512/SHA3-512 use 64. Hexadecimal spelling is
case-insensitive; leading zero bytes are preserved. Parsing and batch insertion
share this validation. Wrong-length records fail with line/path context and
expected/actual byte counts, triggering normal failed-import cleanup even after
earlier batches committed. Digest validation does not require a linked hashing
implementation because imports consume recorded hashes rather than computing them.

Each input represents a complete inventory of one disk. Imports commit bounded
batches of 5,000 records while their snapshot is internally `importing`.
Within each transaction, observation inserts use at most 1,000 rows per SQL
statement (4,000 bound parameters). Search and immutability triggers still run
for every row; committed progress still advances only after the whole transaction.
SQL insertion errors identify the affected batch's first and last paths.
Sizes are staged only for content whose size is not yet known, including enrichment
of existing content. Repeated observations must agree with either the known or
staged size. Publication
atomically applies those sizes, records counters, removes staging/ownership rows,
and changes the snapshot to `complete`. Completed snapshots and observations are
immutable.

On the 10,000-file import-phase fixture (7,500 unique contents, known sizes,
nested paths), paired five-iteration runs measured 0.975 s/import before observation
batching and 0.638 s/import afterward on an AMD Ryzen 5 9600X. Ingestion fell from
0.598 s to 0.262 s; directory building and publication were essentially unchanged.
The 50,000-file indexed-import fixture (distinct contents, shallow paths, unknown
sizes) fell from 2.931 s/import to 1.834 s/import in paired five-iteration runs.
These warm synthetic measurements do not predict an arbitrary inventory's speedup.

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

- `GET /api/v1/catalog` exposes `scope=current`, inventory `revision`, and decimal-string
  `disk_count`, `file_count`, and `content_count` for dashboard/sidebar statistics.
  Disks include those without inventories. Files count observations in the latest complete
  snapshot per disk; content counts distinct identities across those snapshots, not summed
  per-disk counts. Empty catalogs return zeros. One SQL statement provides a consistent view.
  The revision is the maximum complete snapshot ID (or zero), not a catalog identity or a
  general response version: disk creation/editing can change the response without advancing it.
  No parameters, historical totals, or persistent identity table are provided.
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

## Directory indexes and browsing

Directories are inferred from observation ancestors and identified by `(snapshot_id, path)`.
The root is `""` and exists even in a zero-file inventory. Empty filesystem directories
cannot be inferred. Paths preserve case and Unicode; `/` separates segments, while
backslashes, SQL wildcard characters, and glob characters remain literal.

`directory` stores parent relationships, recursive summaries, maximum reported mtime,
and a versioned fingerprint. `directory_content` records each distinct descendant content
and its occurrence count. `directory_file` associates observations with their immediate
parent for indexed file pages. Parent, fingerprint, reverse-content, and snapshot/path
lookups have query-plan tests.

After observation batches finish, the database builds this index in a separate transaction
while the snapshot is still importing. Recursive SQL enumerates ancestors and groups content
membership; Go streams directory rows and fingerprint entries rather than retaining whole
inventories or manifests. Fingerprints are built bottom-up. Observation batches remain
bounded at 5,000; the derived-index transaction can be larger and its database/journal cost
must be distinguished from streaming Go memory. Membership storage grows with file depth.

Publication requires a successful directory-build marker, a root, completed fingerprints,
and indexed file membership. Observation changes and staged-size mutations (including deletion
while importing) invalidate the marker. Publication clears staging after marking complete,
within the same transaction, so successful cleanup preserves the build prerequisite.
Completed directory identity, counts, mtimes, and fingerprints are immutable;
size aggregates remain enrichable. Failed imports delete directory rows before orphan-content
cleanup. Snapshot ownership cascades remove membership, file-parent rows, and build markers.
Reopening recovers abandoned directory indexes before application access.

### Summaries and size enrichment

File counts and known bytes count each descendant observation. Content counts and
unique-content known bytes count each `(hash_type, hash)` once within that directory,
including when shared across siblings. Unknown-size file/content counts distinguish incomplete
subtotals from exact sizes. Known zero remains complete. The maximum known descendant mtime
is null when no mtime is available; it does not establish a change date.

Content sizes can become known through later inventories. Successful publication updates
affected directory summaries across all snapshots in the same transaction as shared-content
enrichment. Each new known size contributes `size × occurrences` to file bytes and `size`
to unique-content bytes, removing the corresponding unknown counts. Failed publication
rolls these changes back. Integer overflow in construction or enrichment rejects the import
instead of rounding or silently returning an incomplete total.

### Fingerprint version 1

SHA-256 hashes a stream beginning with the length-prefixed UTF-8 domain
`coldcat-directory-v1`. Immediate entries are sorted by binary path, then kind. Each entry
encodes four length-prefixed fields: kind (`file` or `directory`), name relative to the parent,
algorithm, and digest bytes. Lengths are unsigned 64-bit big-endian integers. Files encode
their canonical content hash algorithm/digest; directories encode `directory-v1` and the
already-computed child fingerprint. Root names, sizes, mtimes, and input-record order do not
participate. Distinct file paths preserve multiplicity. File and inferred directory entries
at the same path are distinguished by kind.

These fingerprints are candidate indexes for exact-tree comparisons. Unfiltered requests use
the index to select candidates, then stream and compare canonical descendant manifests before
declaring replicas. Filtered requests build query-specific manifests and never reject a
candidate merely because its whole-tree fingerprint differs.

### Exact replicas and content coverage

Directory comparison destinations use each disk's latest complete snapshot. The requested
source may be historical. Exact replicas compare relative file paths and `(hash_type, hash)`;
root names and mtimes do not participate. The source directory itself is excluded, while other
roots on the source disk remain eligible. Responses distinguish same-disk matches and whether
a filtered match is also equal as a whole tree.

Allow and block patterns apply symmetrically to paths relative to each root. Matching uses
`github.com/bmatcuk/doublestar/v4` with `/` separators on every host, case-sensitive matching,
and its `*`, `**`, `?`, character-class, alternative, and backslash-escape syntax. No allow
patterns means all files; otherwise any allow match retains a file, and any block match then
excludes it. Patterns are validated once and normalized by sorting and deduplication. Requests
accept at most 100 patterns in each list, 1,024 UTF-8 bytes per pattern, and 16,384 pattern bytes
in total. A selection retaining no files returns `empty_comparison=true`, no replica or coverage
items, and no misleading complete result.

Coverage is content-based and excludes the source disk. A selected source content is covered
when it occurs anywhere in another disk's current snapshot, regardless of destination name or
directory. Responses report both source file occurrences and distinct contents, preserving
repeated-content multiplicity. Known-byte and unknown-size occurrence subtotals remain separate.
Selected source membership is streamed in 1,000-row batches into a transaction-local SQLite
table, avoiding a whole-directory Go collection.

### HTTP and pagination

The five directory routes operate on the requested complete snapshot:

- `/api/v1/snapshots/{id}/directories?parent=...`: immediate child directories.
- `/api/v1/snapshots/{id}/directory?path=...`: recursive summary and redundancy histogram.
- `/api/v1/snapshots/{id}/directory/entries?path=...`: immediate files/directories;
  `recursive=true` selects descendant files only.
- `/api/v1/snapshots/{id}/directory/replicas?path=...`: verified exact-tree matches, optionally
  under repeatable `allow` and `block` filters.
- `/api/v1/snapshots/{id}/directory/coverage?path=...`: optionally filtered source-content
  coverage on every other disk with a current snapshot.

Omitted/empty `parent` or `path` selects root. Malformed paths return 400; absent directories
and incomplete/missing snapshots return 404. No host-path normalization or wildcard expansion
is applied. Entries sort by binary path and kind, with directory before file for equal paths.
Pages default to 50 entries and are limited to 200. Cursors bind normalized filters, source
snapshot/path, listing mode, replica metric/bounds, limit, sort version, and inventory revision.
The normalized filter digest and stable observation/directory IDs keep cursors compact even
for long imported paths. Sort paths are resolved from those IDs within the query transaction.
Directory browsing accepts canonical paths at the lengths supported by importing, including
long names and nested paths exceeding the separate search/content membership-filter limits.
Unchanged restarts and failed-import recovery preserve cursors; successful imports invalidate
them, including when enriching historical sizes.

Replica information always uses the latest complete inventories catalog-wide, independently
of source tree membership. The histogram groups descendant file occurrences by distinct current
disks other than the source disk. File entries also expose other current locations, excluding
the source `(disk_id, path)` only when currently occupied by the same content. Historical-only
contents therefore report zero, not negative values. Responses include the source inventory,
capture context, current status, revision, and `replica_scope=current`.

Replica bounds filter file entries; immediate child directories remain navigation entries.
The default metric is disks. Unfiltered file queries page before replica expansion. Filtered
queries aggregate current replicas once per qualifying distinct content rather than once per
file occurrence. Both forms use keyset seeking; subtree prefix bounds preserve segment
boundaries and do not interpret SQL wildcards. OpenAPI describes the wire types and examples.

Directory query/build, recovery, and comparison benchmarks run at 50,000 and 1,000,000
observations:

```sh
go test ./internal/database -run '^$' \
  -bench 'BenchmarkDirectory(Queries|Recovery|Enrichment|Comparisons)' -benchtime=1x -benchmem -v
```

The isolated directory benchmarks disable observation search-index triggers during fixture
construction, distinguishing directory cost from existing FTS/short-posting costs. Query
fixtures cover shallow-wide, eight-extra-level deep, and high-duplicate-content trees. Build
time and added catalog bytes are logged; recovery includes catalog opening and deletion of
an abandoned built inventory. These are warm, single-iteration measurements, not filesystem-cold
latencies or a production latency guarantee. `BenchmarkSearchIndexedImport` exercises the
full streaming import with both search and directory indexes.

The initial 50,000-file warm comparison measurement on the same Linux/Ryzen system took
547 ms for an unfiltered verified replica and 401 ms for coverage across one other disk.
These costs include source selection and complete manifest/content verification; no interactive
latency budget has yet been agreed. Filtered comparisons and broader multi-disk distributions
remain important follow-up measurements.

On Linux amd64 / Ryzen 5 9600X, the warm single-iteration measurements were:

| Observations / shape | Directory build | Added index bytes | Root detail | Recursive first / deep page |
| --- | --- | --- | --- | --- |
| 50,000 / wide | 0.67 s | 6,238,208 | 11.9 ms | 1.3 / 1.8 ms |
| 50,000 / deep | 2.28 s | 34,500,608 | 12.2 ms | 1.2 / 2.2 ms |
| 50,000 / duplicate | 0.56 s | 4,673,536 | 4.2 ms | 1.2 / 2.0 ms |
| 1,000,000 / wide | 14.52 s | 95,887,360 | 85.3 ms | 3.3 / 5.4 ms |
| 1,000,000 / deep | 46.91 s | 383,746,048 | 86.3 ms | 3.1 / 5.3 ms |
| 1,000,000 / duplicate | 13.54 s | 94,322,688 | 76.0 ms | 4.8 / 9.8 ms |

Child-directory pages were 0.5–0.7 ms; immediate-file pages were 1.3–4.8 ms.
Filtered nested file pages were 2.0–3.4 ms at 50,000 observations and 29.0–36.2 ms
at one million. Directory-only recovery took 0.68 s / 16.65 s at those scales, including
deletion of import-owned content and catalog opening. Root detail includes the dynamic
redundancy histogram; stored size-summary lookup alone is cheaper. Numeric latency/storage
budgets, filesystem-cold queries, and broader multi-disk distributions remain to be agreed
or measured.

Publishing one newly known content size across 50,000 / 1,000,000 historical file occurrences
took about 6.0 / 6.0 ms with 100 source subdirectories, showing that enrichment follows
content/directory memberships rather than individual observations. Full 50,000-file imports
with search and directory indexes took 16.85 s; a late parse failure plus cleanup took 25.43 s.
Progress-sampled Go heap was approximately 3.4–3.5 MiB; these samples are neither RSS nor
a measurement of peak memory during the derived build.

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

`internal/httpapi` implements readiness, disk management, case-insensitive exact/substring search,
exact hash lookup, content lists/detail, content observation pages, disk snapshot pages, observation detail,
and complete snapshot detail. See
[openapi.json](openapi.json) for the implemented wire contract. IDs, byte counts and
aggregate counts are decimal strings; timestamps are UTC RFC3339 with optional
fractional seconds, and unknown sizes/mtimes are null. CORS is not enabled.

```sh
curl 'http://127.0.0.1:8080/api/v1/search?q=report&field=name&match=substring&limit=50'
curl 'http://127.0.0.1:8080/api/v1/contents/lookup?hash_type=sha256&hash=00000000000000000000000000000000000000000000000000000000000000ab'
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
import from publishing between them. Hash lookup accepts supported algorithms and
nonempty decoded hexadecimal identities, including abbreviated identities in
existing catalogs. Exact algorithm-specific digest lengths are enforced on imports;
lookup validation and existing catalog rows are unchanged.

## Filename and path search

`GET /api/v1/search` returns observations with content, disk, complete-snapshot context,
basename, relevance, `is_current`, and catalog-wide scoped/current replica summaries.
The HTTP workflow tests import three disks, search a remembered filename, open content,
traverse locations, and verify other-replica counts plus source/capture/import dates.

Defaults are `field=name`, `match=substring`, `scope=current`, `replica_metric=disks`,
and limit 50 (maximum 200). Names are final `/`-separated segments; paths are whole
disk-relative paths. Matching is case-insensitive and literal, preserving whitespace,
Unicode spelling and punctuation. No Unicode normalization or wildcard interpretation
is applied. Queries must be valid UTF-8, nonempty, at most 1024 bytes, and contain no
NUL. This query limit does not impose a new imported-path length limit.

Exact selected-field matches rank before prefixes before other substrings. Ties use
original path in binary order, then immutable observation ID ascending. Every matching
historical observation remains a separate history result. `is_current` describes the
observation's selected snapshot; current counts also distinguish content still present
somewhere from historical-only content. Sizes and mtimes retain null/known-zero semantics.

Disk, complete-snapshot and directory filters select membership without narrowing
replica counts. Directory paths use the content-list segment/literal semantics and
require a disk or snapshot selector over HTTP. An explicit snapshot restricts the
chosen scope: an older snapshot returns an empty current page, and is searchable under
history. Disk/snapshot disagreement is invalid. Both replica metrics and inclusive
bounds follow the content-list contract and require current scope.

Search cursors bind filters, limit, ranking version and complete-inventory revision.
They carry relevance and observation ID; the immutable path is reconstructed under
the read gate, so unusually long imported paths do not produce oversized cursors.
Malformed/context-changed/invalid-anchor cursors return 400; a changed revision returns
409. Unchanged restart and failed-import recovery preserve cursors. SQL cancellation
propagates, and an active in-session import blocks search.

### Case-insensitive search without fuzzy matching

`match=exact|substring` uses version-1 Unicode simple case-folding, preserving punctuation
and whitespace. It does not normalize composed/decomposed Unicode or equate `ß` with `ss`.
Original names and paths remain unchanged in responses. Substring queries require at
least three Unicode code points; exact queries may be shorter. `match=fuzzy` and short
substring queries return validation errors, not scan fallbacks.

Relevance is folded `exact`, `prefix`, or `substring`, in that order; ties use original
binary path then observation ID. Directory identity and membership remain case-sensitive.
The distinct-path lexicon stores folded names/paths with B-tree exact indexes and one
external-content FTS5 trigram index. The trigram tokenizer is case-sensitive over already
folded text, preserving the explicit folding contract. Query phrases are literal and
candidate matches are verified against folded text. Retrieval remains exhaustive.

Fuzzy signatures, literal/folded short postings, and the separate folded FTS index have
been removed. Publication atomically seals paths referenced by completed snapshots;
cleanup deletes import-only paths and updates FTS while preserving shared completed paths.
Search cursor version 3 binds folding/ranking and rejects earlier cursor formats.

See [ADR 0001](adr/0001-search-without-fuzzy.md). This updates the pre-0.1 initial schema;
opening an obsolete fuzzy/short-index catalog fails with an explicit recreation/reimport
instruction. The workspace catalog has not been recreated or modified.

Reproduce the current equivalent distinct-path fixture with:

```sh
go test ./internal/database -run '^$' \
  -bench BenchmarkSearchPersisted -benchtime=20x -benchmem -v
```

### Current simplified-schema measurements

On 2026-10-07, Linux amd64 / Go 1.27.1 / Ryzen 5 9600X, four Go CPUs, the equivalent
million-distinct-path raw fixture built in 60.5 seconds and occupied 614,731,776 bytes
(615 MB / 586 MiB). Compared with the removed fuzzy fixture below, total catalog bytes
fell about 88% and fixture construction was about 11 times faster. Both fixtures omit
directory aggregates; these are not standalone fuzzy-index byte measurements or actual
CSHD input sizes. Warm means over 20 database calls were:

| Case-insensitive basename substring query | Time/op |
| --- | ---: |
| `report-0000005.txt` (targeted literal) | 4.49 ms |
| `REPORT` (broad folded prefix) | 1874.0 ms |
| `absent` (no-match) | 0.649 ms |

The targeted query is not equivalent to the old typo query. Broad-prefix latency has
not improved and remains an acceptance concern; removing fuzzy postings is not a
broad-query optimization. These means do not establish p95, HTTP, or filesystem-cold costs.

The 50,000-file `BenchmarkSearchIndexedImport` with the simplified schema completed a
successful streaming import at 15,960 files/s (about 3.13 seconds inside the import),
including directory construction/publication. Parsing the late-failure fixture and
finishing cleanup ran at 17,313 input files/s (about 2.89 seconds); the benchmark verifies
catalog access with revision zero afterward. Sampled Go heap was about 3.81 / 3.71 MB;
this is neither peak RSS nor a bound on SQLite memory. Inputs use synthetic short paths
and unique SHA-256 contents, so these results do not predict the user's 84,000-line
inventory throughput. Actual import memory/journal, recovery, and HTTP acceptance remain.

### Historical fuzzy measurements (removed implementation)

The following measurements explain the decision to remove fuzzy search. They are not
measurements of the current schema; the old fuzzy benchmark is no longer present.

The fixture uses 50,000 or 1,000,000 distinct paths and contents; construction can
be expensive. On 2026-10-07, Linux amd64 / Go 1.27.1 / Ryzen 5 9600X in a container
limited to four CPUs and 8 GiB, the 50,000-path fixture produced 1,990,000 signature
postings and a 239,276,032-byte catalog in 30.5 s. The prior duplicate-folded-index
layout used 366,702,592 bytes and took 51 s with the same fixture. These paths are all
lowercase; mixed-case/Unicode distributions can require more folded postings.
It uses raw transactional fixture insertion and does not build directory aggregates;
these are total catalog bytes, not incremental fuzzy-index bytes or streaming-import
throughput. Warm means over 20 database calls were:

| Fuzzy basename query | Time/op |
| --- | ---: |
| `reprot-0000005.txt` (adjacent-swap typo) | 0.77 ms |
| `REPORT` (broad folded prefix) | 86.7 ms |
| `a` (one-rune no-match) | 0.65 ms |
| `xy` (two-rune no-match) | 0.64 ms |

These do not establish p95, worst-case short-query fan-out, or a latency guarantee.
The million-distinct-path case ran on 2026-10-07 on the same CPU with four Go CPUs.
It produced 41,800,000 signature postings and a 5,093,818,368-byte catalog
(5.09 GB / 4.74 GiB), with fixture construction taking 11m12.5s. Warm means over
20 database calls were:

| Fuzzy basename query | Time/op |
| --- | ---: |
| `reprot-0000005.txt` (adjacent-swap typo) | 0.798 ms |
| `REPORT` (broad folded prefix) | 1669.8 ms |
| `a` (one-rune no-match) | 0.629 ms |
| `xy` (two-rune no-match) | 0.621 ms |

The targeted typo lookup remains fast, but broad folded-prefix latency and catalog
storage are acceptance concerns. These catalog bytes include all fixture tables and
indexes, not incremental fuzzy-index storage. The raw fixture omits directory aggregates;
its build time is not streaming-import throughput. The short no-match probes do not
measure short-query fan-out when many paths match. This successful benchmark run does
not establish acceptable performance, p95, HTTP latency, or filesystem-cold behavior.

Current-schema before/after measurements, varied distributions, HTTP latency, filesystem-cold behavior, indexed
streaming-import overhead, and standalone recovery still require measurements and
an agreed acceptance budget. Functionality is implemented; the final performance
and frontend-handoff gate remains open.

The same container had no memory-limit/OOM events during race-test investigation.
An isolated 1,000-file progress/import race test took 16.2 s before these write-layout
changes and 9.9 s after them. CPU profiling showed mostly instrumented SQLite
execution, using roughly one CPU core; more memory or parallel CPUs do not resolve
that serial cost. The CLI interruption test allows one minute for the first committed
batch and signal cleanup, preserving its handshake, exit, recovery, and cursor
assertions without treating a short wall-clock timeout as an import throughput budget.

### Literal index publication and cleanup

The initial schema contains a distinct-path lexicon (`search_path`) with indexed folded
basename/path equality and one external-content FTS5 trigram index over folded text.
Paths are shared across disks and snapshots. FTS queries use quoted literal phrases,
followed by folded literal verification; short substring queries are rejected because
FTS5 trigrams cannot retrieve them.
No search service loads the catalog's observations into Go.

Observation insert/update triggers build lexicon and trigram data in
the same bounded batch transaction. Index-write failures therefore use normal import
cleanup; publication additionally checks that every observation has a search path.
Deleting a failed observation removes its lexicon row only after its last reference
disappears and updates FTS. Completed paths remain protected. Tests cover committed
batch failure, cancellation, shared-path preservation, interrupted-state recovery,
publication prerequisites and FTS integrity.

Large failure measurements exposed unindexed content-reference lookups in existing
import ownership/staging tables. `import_content_content` and `pending_size_content`
now index those reverse references, including ownership cascades, with query-plan tests.
This allows the measured 50,000-file late failure to clean up within the existing
30-second cleanup deadline and leave the catalog usable.

### Historical search measurements and fuzzy spike

These measurements precede ADR 0001 and removal of fuzzy/short indexes. Re-run current
search and import benchmarks for acceptance; the old fuzzy spike benchmark was removed.

Reproduce the measurements with:

```sh
go test ./internal/database -run '^$' -bench BenchmarkSearchQueries -benchtime=20x -benchmem -v
go test ./internal/importer -run '^$' -bench BenchmarkSearchIndexedImport -benchtime=1x -benchmem
```

Measured on 2026-10-06, Linux amd64, Go 1.27.1, AMD Ryzen 5 9600X. The query fixture
has 50,000 distinct contents/names, mixed numbered report/item names and Unicode path
prefixes. The 50,000-observation case has 50,000 distinct paths; the million-observation
case repeats contents across 20 current disks and approximately 100,000 distinct paths.
These are current-inventory replication fixtures; history queries exercise that query
shape, rather than a separate history-heavy dataset. Total catalog sizes, including
observations and all indexes, were 131,821,568 and 402,841,600 bytes respectively.
Raw-SQL fixture construction took 10.9 and 26.8 seconds; these are not streaming-import
throughput measurements.

Warm database-method p95 samples over 20 calls, milliseconds:

| Query | 50,000 observations | 1,000,000 observations |
| --- | ---: | ---: |
| Exact basename | 0.87 | 1.18 |
| Exact path | 0.65 | 0.90 |
| Rare substring | 0.91 | 0.90 |
| Common basename substring | 22.2 | 48.5 |
| One-rune path substring | 32.2 | 99.4 |
| Two-rune path substring | 31.6 | 98.5 |
| No matching substring | 0.73 | 0.87 |
| Directory membership | 20.7 | 52.5 |
| Rare substring + disk/location bounds | 1.22 / 1.43 | 1.56 / 1.60 |
| Common substring + disk/location bounds | 36.1 / 33.6 | 266.7 / 220.9 |
| Common substring + no-match disk bound | 27.9 | 2117.3 |
| Deep common-substring page | 31.1 | 66.9 |
| Connection-cold rare substring | 1.23 | 2.03 |

The query first pages qualifying paths, each guaranteed to have an eligible observation
after the anchor, then expands only enough paths to fill the next observation page.
This is exhaustive keyset pagination, not a candidate-recall cap. It reduced initial
million-observation common/short-query means from 254/400 ms to approximately 47/97 ms.
Replica eligibility can still require examining every matching path and catalog-wide
content replicas, particularly to prove that no result satisfies a bound. The expensive
no-match case above asks for 99 other disks. An agreed interactive budget, larger
distinct-path and history-heavy distributions, and filesystem-cold measurements remain
pending. Connection-cold closes SQLite connections between operations; it does not
evict the OS filesystem cache. These p95 samples measure database calls, not HTTP load.

The 50,000-file streaming import took 17.3 seconds (about 2,897 files/s). Parsing a late
failure and completing cleanup took 26.8 seconds total (about 1,865 input files/s).
The failure benchmark asserts that catalog access succeeds with revision zero afterward.
Batch-callback Go heap samples peaked around 3.7–3.8 MB; cumulative Go allocations were
about 856 MB per run. Those samples are not peak RSS or proof of a process memory bound.
Larger imports, journal growth and standalone recovery latency still need measurement.

The initial fuzzy spike establishes:

- FTS5 trigrams support literal, case-sensitive Unicode/punctuation retrieval, but
  miss one-/two-rune queries and do not provide typo tolerance.
- A test-only deletion-signature lexicon plus exact adjacent-swap probes retains all
  generated single insertion/deletion/substitution/transposition cases for short,
  Unicode and punctuated terms. Restricted Damerau/optimal-string-alignment distance
  reranking retains these at distance one. Candidate retrieval has no truncation cap.
- The prototype applies Unicode simple case-fold orbits, preserves punctuation, and
  does not canonically normalize composed/decomposed Unicode or equate `ß` with `ss`.
  This is an explicit prototype normalization, not a shipped fuzzy HTTP contract.
- For 50,003 terms, it built 610,007 signatures in about 179 ms, allocating about
  188 MB cumulatively. Tested typo retrieval plus reranking took 4.4–4.9 microseconds;
  a short-term probe took 1.3 microseconds. These exclude SQLite postings, observation
  expansion, HTTP, index persistence and exhaustive sorting of large candidate sets.

The persisted implementation was subsequently removed because its storage/write costs
were disproportionate. FTS5 alone does not provide typo tolerance; the current contract
explicitly drops that capability rather than claiming equivalent recall. ADR 0001 records
the approved replacement, normalization, short-query restriction, and consequences.

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
