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
Within each transaction, observation inserts use at most 5,000 rows per SQL
statement (20,000 bound parameters). Search and immutability triggers still run
for every row; committed progress still advances only after the whole transaction.
SQL insertion errors identify the affected batch's first and last paths.
Sizes are staged only for content whose size is not yet known, including enrichment
of existing content. Repeated observations must agree with either the known or
staged size. Publication
atomically applies those sizes, records counters, removes staging/ownership rows,
and changes the snapshot to `complete`. Completed snapshots and observations are
immutable.

Newly inserted content uses the insert result's row ID instead of a separate
lookup. Its size is NULL by construction, and foreign keys guarantee it cannot
already have staged metadata. Existing content still requires both identity/size
lookup and, when necessary, staged-size conflict validation.

On the 10,000-file import-phase fixture (7,500 unique contents, known sizes,
nested paths), paired five-iteration runs measured 0.975 s/import before observation
batching and 0.638 s/import afterward on an AMD Ryzen 5 9600X. Ingestion fell from
0.598 s to 0.262 s; directory building and publication were essentially unchanged.
The 50,000-file indexed-import fixture (distinct contents, shallow paths, unknown
sizes) fell from 2.931 s/import to 1.834 s/import in paired five-iteration runs.
These warm synthetic measurements do not predict an arbitrary inventory's speedup.

After the directory-build checkpoint, larger observation statements and new-content
lookup elision brought the nested fixture to 0.452 s/import and the shallow fixture
to 1.327 s/import. Paired ten-iteration runs against the initial observation-batching
checkpoint measured 0.619 s and 1.632 s respectively: a further 27% and 19% reduction
in elapsed time. The shallow fixture's sampled Go heap increased from 3.8 MB to
4.6 MB with the larger SQL buffers; sampled heap is not RSS or a peak-memory bound.

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
membership; directory identities are deduplicated before walking their parents, and immediate
file parents are extracted directly instead of expanding their ancestors again. Size/count
summaries use one snapshot-wide update. Go streams directory rows and fingerprint entries
rather than retaining whole inventories or manifests. Fingerprints and maximum known mtimes
are built bottom-up from immediate files and completed child directories. Mtimes are not part
of the fingerprint encoding. Observation batches remain
bounded at 5,000; the derived-index transaction can be larger and its database/journal cost
must be distinguished from streaming Go memory. Membership storage grows with file depth.

After observation batching, paired five-iteration import runs measured directory construction
at 0.294 s before these build changes and 0.185 s afterward on the 10,000-file nested fixture.
Total import time fell from 0.620 s to 0.515 s. The shallow 50,000-file fixture fell from
1.643 s to 1.505 s. These changes do not alter the persisted schema or fingerprint version.

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
latency budget has yet been agreed. The five-disk filtered comparison measurements below
extend this baseline; the bounded history-heavy comparison measurements follow them.
Broader distributions remain pending.

#### Five-disk filtered comparisons

`BenchmarkDirectoryFilteredComparisons` uses a source tree, an exact copy under a renamed
root, a copy with changed and extra logs, and two complementary partial-content disks.
The partial disks store contents under unrelated names: together they cover the source,
but neither alone is complete. Twenty percent of source files are root-level or nested
logs. All sizes are known (four bytes), and content identities repeat across source files.
The fixture has 10,000 distinct source contents at both benchmark scales.

Counts in benchmark names denote **source observations**, not total catalog observations:
50,000 source files produce 160,001 catalog observations; 1,000,000 produce 3,010,001.
Search triggers are disabled for fixture construction, as in the other isolated directory
benchmarks. These catalogs are for directory-query measurements only, not search tests or
production import-throughput measurements. `TestDirectoryComparisonBenchmarkFixture`
checks the same fixture at small scale, including replica identities, whole-tree versus
filtered equality, selection counts, byte totals, complementary partial coverage, and
explicit empty comparisons. Expected counts are calculated independently of glob selection.

```sh
go test ./internal/database -run '^$' \
  -bench '^BenchmarkDirectoryFilteredComparisons/50000/' -benchtime=3x -benchmem -v
go test ./internal/database -run '^$' \
  -bench '^BenchmarkDirectoryFilteredComparisons/1000000/' -benchtime=1x -benchmem -v
```

Measured on Linux amd64 / AMD Ryzen 5 9600X, Go 1.27.1, with benchmark GOMAXPROCS=4.
Each case performs an untimed validation/warm-up call before timing. Fixture construction
and catalog closure are excluded; timed calls include result assertions. The 50,000-file
numbers are means over three iterations; test/vet checks briefly overlapped the start of
that run. The million-file run uses one iteration per case and no concurrent project checks.
An initial million-file attempt hit a 120-second command timeout during its first replica
case and produced no timings; the retry removes that command timeout.
Neither run establishes filesystem-cold latency, HTTP latency, p95, peak live heap, or RSS.
`B/op` measures cumulative Go allocation per call, not retained memory. These measurements
are not an approved interactive budget or a backend acceptance decision.

| Source files | Selection | Replicas | Coverage | Replicas B/op | Coverage B/op |
| --- | --- | --- | --- | --- | --- |
| 50,000 | Unfiltered | 571 ms | 423 ms | 81,582,858 | 42,567,637 |
| 50,000 | Block `**/*.log` | 2,043 ms | 370 ms | 279,200,421 | 37,491,125 |
| 50,000 | Allow `**/*.txt` | 2,041 ms | 369 ms | 277,995,696 | 37,491,104 |
| 50,000 | Block `**` (empty) | 137 ms | 141 ms | 17,013,650 | 17,014,330 |
| 1,000,000 | Unfiltered | 151.60 s | 43.43 s | 1,624,640,616 | 849,369,424 |
| 1,000,000 | Block `**/*.log` | 462.72 s | 42.68 s | 5,398,246,520 | 747,851,760 |
| 1,000,000 | Allow `**/*.txt` | 461.80 s | 42.43 s | 5,397,055,304 | 747,845,376 |
| 1,000,000 | Block `**` (empty) | 37.61 s | 37.88 s | 338,557,080 | 338,558,672 |

Filtered replica queries cannot eliminate candidates using the unfiltered fingerprint;
this fixture exercises that cost and verifies both the exact and filtered-only copies.
Coverage retains repeated-file multiplicity while probing destination content membership.
Even an empty selection scans the source to apply filters. Slow cases remain visible;
larger candidate-directory counts, broader history-heavy workloads, cold measurements, and agreed
latency limits are still required before closing performance acceptance.
The million-source-file command took 2,600.6 seconds overall, including fixture creation
and untimed warm-up queries. Its filtered replica calls took roughly 7.7 minutes each,
with about 5.4 GB cumulatively allocated per call. Even unfiltered and empty comparisons
are slow at this scale. The increase is much larger than the 20-fold increase in source
files; these single-iteration results do not establish why. Profiling and explicitly
approved limitations or improvements are needed before treating this scale as interactive.

#### Committed directory comparison optimizations

The preceding comparison tables are historical pre-optimization baselines, not the
current implementation's timings. `5e0dcd9` gives SQLite one effective manifest-page
lower bound: the inclusive subtree prefix initially, then the exclusive cursor.
Previously SQLite sought to the prefix and repeatedly filtered out prior pages.
The upper subtree bound and 1,000-row source batches are unchanged.

`bc29c28` then reduces materialization and repeated SQL preparation. Candidate hashing
and exact equality use reusable streaming row records and canonical encoding buffers;
`sql.RawBytes` are consumed before their owning cursor advances. Source batches scan
directly into their final slots, and a temporary-selection upsert is prepared once per
request. Cancellation is checked before every stream advance, including short streams
where asynchronous cancellation could otherwise race with EOF. Canonical v1 encoding,
exact verification, symmetric filters, multiplicity, historical sources and current-only
destinations are preserved; no schema, API or persistent fingerprint changes are needed.
Whole-tree fingerprints still prune only unfiltered candidates.

On Linux amd64 / AMD Ryzen 5 9600X, Go 1.27.1, GOMAXPROCS=4, the existing five-disk
fixture produced the following warm measurements. Each cell is replicas / coverage.
50,000-source results use three timed iterations; million-source results use one.
Setup and validation/warm-up are untimed; result assertions are timed. Other session
work and some focused checks overlapped these runs, so they are not CPU-isolated.

| Source files / selection | Cursor-seek slice | Reusable-stream slice | Stream-slice replicas / coverage B/op |
| --- | ---: | ---: | ---: |
| 50,000 / unfiltered | 198.10 / 328.55 ms | 141.41 / 125.79 ms | 25,475,901 / 34,613,141 |
| 50,000 / block logs | 945.13 / 277.60 ms | 598.50 / 115.52 ms | 107,426,248 / 30,297,162 |
| 50,000 / allow text | 934.70 / 300.23 ms | 595.42 / 113.92 ms | 107,425,064 / 30,296,608 |
| 50,000 / empty | 50.83 / 46.30 ms | 46.25 / 44.98 ms | 13,018,733 / 13,019,520 |
| 1,000,000 / unfiltered | 4.198 / 6.325 s | 2.808 / 2.360 s | 508,838,824 / 690,287,344 |
| 1,000,000 / block logs | 19.407 / 5.354 s | 11.551 / 2.137 s | 2,096,958,776 / 603,944,400 |
| 1,000,000 / allow text | 18.246 / 6.421 s | 11.503 / 2.126 s | 2,096,957,976 / 603,942,976 |
| 1,000,000 / empty | 0.948 / 1.051 s | 0.900 / 0.907 s | 258,654,072 / 258,656,176 |

The same-session pre-seek 50,000-source blocked-log replica mean was 2055.80 ms.
The million-source pre-seek values above were previously recorded baselines, not a
fresh paired run. The reusable-stream million command completed in 104.509 seconds,
including fixture creation and warm-up. Filtered replicas still take about 11.5 seconds
and cumulatively allocate about 2.10 GB per call. `B/op` is not peak live heap or RSS;
bounded streaming does not make driver allocation traffic constant. Arbitrary filtered
candidates remain exhaustively scanned and matching manifests exactly verified.
Cold/HTTP acceptance scope, latency metrics and budgets need agreement; these runs do not
measure HTTP p95. Additional candidate distributions depend on the supported limits selected.
Use the existing filtered-comparison commands above with `-timeout=30m`.

#### History-heavy directory comparisons

`BenchmarkDirectoryHistoryComparisons` reuses the five-disk roles above, with
50,000 source files and 10,000 distinct source contents per snapshot. It retains
one or five complete snapshots per disk, captured on successive days. Each generation
has unchanged paths/content membership to isolate retained-history costs while keeping
the current inventory fixed. The catalogs contain 160,001 current observations and
160,001/800,005 total historical observations, respectively, across 5/25 snapshots.
The single-snapshot current and oldest source cases intentionally query the same source.

Both source choices compare against current destination inventories only. With five
generations, the oldest source additionally matches the current source disk's `tree`
as a same-disk replica; coverage still excludes that disk. Thus historical-source
replica calls have one more verified match than current-source calls, and should not
be interpreted as a pure history-overhead comparison between source choices.

`TestDirectoryHistoryComparisonBenchmarkFixture` checks the shared fixture at 100
source files, independently verifies observation/snapshot counts and latest snapshot
IDs, and checks replica identities, selection totals and per-disk coverage for all
four existing selections. `TestDirectoryHistoryComparisonOldOnlyDestination` adds
two older complete inventories imported later, containing a source and matching tree
absent from every current disk. Both unfiltered and blocked-log queries must return
no replicas and zero coverage, proving that retained destination history is not counted.

```sh
go test ./internal/database -run '^$' \
  -bench '^BenchmarkDirectoryHistoryComparisons/50000/' \
  -benchtime=3x -benchmem -v -timeout=30m
```

Fixture creation, directory construction, closure, one validation/warm-up call per
case, and per-iteration result assertions are untimed. Timed calls include database
query/result assembly. Search triggers remain disabled, so this is a directory-only
fixture, not production import-throughput or end-to-end HTTP evidence. Allocation
metrics are cumulative Go allocations per query, not retained heap or RSS.

Measured on Linux amd64 / AMD Ryzen 5 9600X, Go 1.27.1, benchmark GOMAXPROCS=4,
with three iterations per case. Full test/vet checks briefly overlapped the start
of the run; the final focused race check ran afterward. The complete command took
70.46 seconds, including untimed fixture creation and warm-up.

| Snapshots per disk | Source | Selection | Replicas | Coverage | Replicas B/op | Coverage B/op |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| 1 | Current | Unfiltered | 574.50 ms | 424.16 ms | 81,575,581 | 42,567,541 |
| 1 | Current | Block logs | 2056.37 ms | 370.90 ms | 279,194,245 | 37,491,429 |
| 1 | Oldest | Unfiltered | 597.82 ms | 417.52 ms | 81,577,864 | 42,566,976 |
| 1 | Oldest | Block logs | 2038.04 ms | 366.22 ms | 279,198,725 | 37,491,098 |
| 5 | Current | Unfiltered | 575.61 ms | 429.46 ms | 81,575,874 | 42,566,970 |
| 5 | Current | Block logs | 2003.22 ms | 371.59 ms | 279,197,333 | 37,491,477 |
| 5 | Oldest | Unfiltered | 998.66 ms | 427.66 ms | 143,357,826 | 42,567,141 |
| 5 | Oldest | Block logs | 2458.97 ms | 380.95 ms | 340,463,957 | 37,493,338 |

At this bounded scale, current-source means remain similar with five times the
retained observations; this is not proof that history overhead is negligible at
other distributions. Historical-source replica calls are more expensive and allocate
more because they also verify the current same-disk tree. Blocked-log replicas still
take about 2.0–2.46 seconds, so slow cases remain explicit. These warm serial means
are not filesystem-cold latency, p95, statistical significance or approved budgets.
The larger bounded history slice below is complete. Larger candidate-directory counts or
changed/deleted-content history scenarios require selecting supported limits; the original
plan does not mandate million-source retained-history measurements. The final gate remains open.

`go test ./...`, `go vet ./...`, and the final focused
`go test -race -p 1 ./internal/database -run '^TestDirectory(History)?Comparison' -timeout=30m`
passed after the harness changes. Production behavior and schema are unchanged.

#### Larger bounded history comparison slice

`c927983` adds a 200,000-source-file scale to the same history matrix. Five disks with
five snapshots each contain 610,001 current and 3,050,005 historical observations,
25 snapshots and 10,000 distinct source contents per snapshot. This avoids accidentally
selecting a million-source history fixture with roughly 16 million observations.

```sh
go test ./internal/database -run '^$' \
  -bench '^BenchmarkDirectoryHistoryComparisons/200000/snapshots_5/' \
  -benchtime=1x -benchmem -v -timeout=30m
```

All eight cases passed; the command took 61.523 seconds including setup and warm-up.
These are exploratory warm single-iteration samples from the active working code,
not a pinned final-implementation comparison; concurrent container work may affect them.
The environment is Linux amd64 / Ryzen 5 9600X, Go 1.27.1, GOMAXPROCS=4.

| Source / selection | Replicas (ms) | Coverage (ms) | Replicas B/op | Coverage B/op |
| --- | ---: | ---: | ---: | ---: |
| Current / unfiltered | 663.404 | 481.741 | 114,439,192 | 138,135,872 |
| Current / block logs | 2634.665 | 443.106 | 458,871,928 | 120,872,592 |
| Oldest / unfiltered | 1202.074 | 504.124 | 200,240,200 | 138,135,520 |
| Oldest / block logs | 3385.628 | 470.759 | 544,678,824 | 120,871,712 |

The original history fixture semantics, current-only destinations and extra same-disk
replica of an oldest source still apply. Search indexing is excluded. This completes
the larger bounded warm slice, not performance acceptance. Larger-than-measured history and
broader/deeper candidates depend on selected supported limits. Cold acceptance scope and
latency metrics need agreement; these samples are not HTTP p95 evidence.

#### Initial directory build and browsing measurements

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

### Warm HTTP workflow measurement harness

`BenchmarkHTTPWorkflow` in `internal/httpapi/workflow_benchmark_test.go` sends serial
requests through a real loopback `httptest.NewServer` using a reusable HTTP client.
Each scale denotes total current observations: 50,000 or 1,000,000, across three
complete disk inventories with no retained history. There are N distinct paths and
N-3 distinct contents. The source disk holds N-2 observations, including two copies
of the target content; each other disk holds one copy under a different root.
All files have a known size of 4096 bytes and a known mtime. Background filenames
are `report-XXXXXXX.txt`; the target is `keepsake-report.txt` at four locations.
This is a deliberately skewed search fixture, not a representative multi-disk load.

Fixture files are streamed to disk and imported through the application API before
the HTTP server starts. Setup, imports, server startup, cursor preparation, and
explicit warm-up calls are untimed. Timed operations include HTTP routing, database
queries, DTO conversion, JSON encoding, loopback transfer, client body reads and
JSON decoding. The complete workflow also checks target identities/counts and
location uniqueness; its allocation/timing totals include those assertions.
`TestHTTPWorkflowBenchmarkFixture` checks the same harness at small scale, including
case-insensitive search, pagination and replica-filter no-match behavior. Fixture
creation checks the catalog's total file/content/disk counts at every scale.

Cases measure exact filename search and selective `KEEPSAKE` substring search with
limit 1, broad `REPORT` search with limit 50, its second page, and a broad no-match
filter requiring three other disks. Content detail and the first two-location page
are measured separately. The complete workflow follows a search result's content
ID, reads its detail, and traverses both two-location pages: four requests total.

Reproduce with:

```sh
go test ./internal/httpapi -run '^$' -bench '^BenchmarkHTTPWorkflow/50000/' -benchtime=20x -benchmem -v
go test ./internal/httpapi -run '^$' -bench '^BenchmarkHTTPWorkflow/1000000/' -benchtime=20x -benchmem -v -timeout=30m
```

On 2026-10-09, Linux amd64 / AMD Ryzen 5 9600X, Go 1.27.1 with GOMAXPROCS=4,
one 20-iteration run per scale produced these means. The 50,000-observation run
overlapped correctness checks; the million-observation run had no overlapping
test/check commands. Treat the small differences between narrow-query means as
noise, not a scaling improvement.

| Case | 50,000 (ms/op) | 1,000,000 (ms/op) |
| --- | ---: | ---: |
| Exact search | 0.885 | 0.782 |
| Selective substring | 0.901 | 0.881 |
| Broad first page | 102.781 | 1931.100 |
| Broad second page | 165.562 | 2979.083 |
| Replica-filter no-match | 131.362 | 2582.104 |
| Content detail | 0.114 | 0.118 |
| First location page | 0.205 | 0.190 |
| Complete four-request workflow | 1.425 | 1.438 |

At million scale, complete workflows allocated about 96 KB and 1,138 allocations
per operation; broad pages allocated about 507–515 KB and 3,213–3,275 allocations.
Broad queries and replica-filter no-match cases remain explicit multi-second
performance limits; this measurement task does not optimize or accept them.
`go test ./...`, `go vet ./...`, and the final focused
`go test -race -p 1 ./internal/httpapi -timeout=30m` passed after harness changes.

These are warm serial means, not p95, filesystem-cold results, concurrent-load
measurements, or an acceptance decision. Allocation counts cover both the HTTP
client and server in the same process, not server-only memory or RSS. No cache
eviction or catalog reopening is used to imply filesystem-cold behavior. Approved
budgets, broader history-heavy workloads, and the final backend handoff gate remain open.

### Warm history-heavy HTTP workflow harness

`BenchmarkHTTPHistoryWorkflow` in `internal/httpapi/history_workflow_benchmark_test.go`
extends the warm HTTP measurements with five retained complete snapshots per disk
across three disks. Its scale is **total historical observations**, not current
observations or distinct paths. Each generation contributes one fifth of the total;
the latest generation alone supplies current catalog totals.

| Fixture | Historical observations | Current observations | Distinct paths | Historical distinct contents |
| --- | ---: | ---: | ---: | ---: |
| Correctness test | 520 | 104 | 105 | 178 |
| Small benchmark | 50,000 | 10,000 | 10,001 | 17,994 |
| Large benchmark | 1,000,000 | 200,000 | 200,001 | 359,994 |

Capture times increase by one second per generation. As in the current-only fixture,
disk 0 holds all background files and two target copies; each other disk holds one
target copy. Every generation has four target observations, so the target has four
current locations on three disks and 20 historical observations. Historical scoped
location counts also remain four because the same disk/path pairs recur.

For M current observations, disk 0 has M-4 background files. Background index i
normally uses `archive/report-XXXXXXX.txt` and hash identity i+1. Every fifth index
(i divisible by five) changes identity by adding generation*M, while other identities
remain stable. Index zero is a special deletion/replacement: the first four generations
use `archive/retired-report.txt` with shared identity 5*M+1; the latest generation
uses `archive/report-0000000.txt` with identity 4*M+1. Thus retained history includes
unchanged files, changed contents at stable paths, and a historical-only content.
All hashes are full-length SHA-256 identities; sizes are 4096 bytes and mtimes are known.
This remains a skewed synthetic distribution, not a representative disk/history mix.

`TestHTTPHistoryWorkflowBenchmarkFixture` exhausts small-fixture broad and selective
search pages in both scopes, verifies snapshot/path/hash sets and observation-ID
uniqueness, checks independent stable/changed/retired identity examples, and follows
all target content-observation pages. It asserts the independent distribution totals
above, current/history flags, capture context, scope, current replica counts, and zero
current locations for retired content. The same imported fixture builder checks current
catalog totals, the primary workflow, and target/retired summaries at benchmark scales.

Cases measure current/history exact and selective substring searches (limit 1), broad
first/second pages (limit 50), content detail, and first observation pages (limit 2).
Additional cases cover current replica-filter no-match, history's second observation
page and historical-only filename search, and the current four-request workflow.
The no-match filter requires three other disks in a catalog containing only three.
No history-scope redundancy filter is issued because replica bounds require current scope.

Fixture construction, real application imports, loopback server startup, cursor discovery,
and explicit warm-up calls are untimed. Requests are serial and timed end-to-end through
the existing reusable HTTP client, including routing, SQL, DTO/JSON work, transfer,
body reads, decoding, and bounded assertions. Exhaustive checks run outside timing.
Allocations include client and server in one process, not server-only memory or RSS.

Reproduce with:

```sh
go test ./internal/httpapi -run 'TestHTTP(History)?WorkflowBenchmarkFixture' -count=1
go test ./internal/httpapi -run '^$' -bench '^BenchmarkHTTPHistoryWorkflow/50000/' -benchtime=20x -benchmem -v -timeout=30m
go test ./internal/httpapi -run '^$' -bench '^BenchmarkHTTPHistoryWorkflow/1000000/' -benchtime=20x -benchmem -v -timeout=30m
```

On 2026-10-09, Linux amd64 / AMD Ryzen 5 9600X, Go 1.27.1 with GOMAXPROCS=4,
one 20-iteration run per scale produced these means. The scales ran separately,
with no overlapping benchmark or correctness-check commands.

| Case | 50,000 historical (ms/op) | 1,000,000 historical (ms/op) |
| --- | ---: | ---: |
| Current exact search | 0.866 | 0.831 |
| Current selective substring | 0.955 | 0.951 |
| Current broad first page | 40.753 | 742.747 |
| Current broad second page | 54.266 | 978.137 |
| Current replica-filter no-match | 125.825 | 2468.997 |
| Current content detail | 0.123 | 0.120 |
| Current first observation page | 0.228 | 0.225 |
| History exact search | 0.870 | 0.978 |
| History selective substring | 0.983 | 1.005 |
| History broad first page | 23.393 | 406.332 |
| History broad second page | 37.449 | 633.567 |
| History content detail | 0.144 | 0.170 |
| History first observation page | 0.208 | 0.230 |
| History second observation page | 0.217 | 0.248 |
| Historical-only exact search | 0.754 | 0.908 |
| Current complete four-request workflow | 1.482 | 1.502 |

At million historical observations, the complete workflow allocated 97,496 bytes
and 1,153 allocations per operation. Current broad pages allocated 474,486–488,181
bytes and 3,382–3,445 allocations. The no-match mean remains multi-second despite
only 200,000 current observations; current broad pagination and historical broad
search also remain explicit latency concerns. Narrow-query differences between
scales are not evidence of an improvement. No production queries were changed.

`go test ./...`, `go vet ./...`, and the final focused
`go test -race -p 1 ./internal/httpapi -timeout=30m` passed after harness changes.

These are warm serial means, not p95, filesystem-cold results, concurrent-load
measurements, or an acceptance decision. Comparing equal total-observation scales
against `BenchmarkHTTPWorkflow` does not isolate history overhead: this fixture has
one fifth as many current observations and substantially fewer distinct paths.
The bounded history-heavy directory comparisons are described above; other disk/history
distributions, cold measurements,
approved budgets, transaction/journal measurements, and the final backend gate remain open.

### Committed search optimizations and final warm HTTP measurements

The earlier HTTP tables are historical baselines. `201534d` inlines retrieval/ranking,
uses constant exact rank to preserve index order, seeks to the anchor path before
observation expansion, and validates cursor eligibility from its unique path rather
than rebuilding all search candidates. Directional 50,000-path warm database means
fell from 86/109 ms to about 1.2 ms for broad exact first/deep pages, and from
107/150 ms to 88/59 ms for substring pages. Container contention limits comparisons.

`1383276` materializes current snapshots once per query and rejects historical
observations before replica counting. Positive exact/minimum **disk** bounds at least
equal to the number of current snapshots are impossible (other disks cannot exceed
that count minus one), so they return empty after resource/revision/cursor validation.
This shortcut does not apply to locations or maximum-only bounds. Membership filters
do not narrow replica counts; historical-only content still has zero current counts.
Valid, non-impossible history-heavy no-match searches remain real work: exploratory
50,000-current balanced database means were about 126–137 ms after this change,
versus roughly 550–600 ms before. Do not generalize the impossible-bound shortcut
to all no-match workloads.

The final serial million-current HTTP run after these commits used the existing skewed
three-disk fixture and 20 iterations per case. It passed in 104.835 seconds. Linux
amd64 / AMD Ryzen 5 9600X, Go 1.27.1, GOMAXPROCS=4; no other benchmark was active.

| Case | Final mean ms/op |
| --- | ---: |
| Exact search | 0.741665 |
| Selective substring | 0.924235 |
| Broad first page | 1392.964227 |
| Broad second page | 1434.524452 |
| Impossible disk-replica bound | 0.076425 |
| Content detail | 0.107627 |
| First location page | 0.177192 |
| Complete four-request workflow | 1.364552 |

Use the existing `BenchmarkHTTPWorkflow/1000000/` command above. These are warm
end-to-end serial means, not p95, server-only allocation, general no-match performance
or approved budgets. Broad search remains a visible latency concern.

### Balanced disk/history HTTP distribution measurements

`372797e` adds `BenchmarkHTTPDistributionWorkflow`: balanced 3/12-disk inventories
with one/five snapshots per disk at 50,000/200,000 **total current observations**.
Five generations at the larger scale retain one million historical observations.
Stable, changed and deleted paths/content are retained, with two same-disk target
copies on every disk. The independent small-fixture oracle exhausts search and target
observation pages and checks both replica metrics and current/history metadata.
Temporary catalogs are built sequentially, reusing one inventory file. Real application
imports, server startup, cursor preparation and warm-up are untimed; serial loopback
requests include client/server HTTP, SQL, JSON work and bounded assertions.

The original 50,000-current matrix ran at three iterations per case before the final
search changes; retain it as exploratory coverage, not a final latency baseline.
The final 200,000-current matrix passed at 20 iterations in 200.249 seconds on the
same Linux/Ryzen/Go/GOMAXPROCS environment, with no other benchmark active.

```sh
go test ./internal/httpapi -run '^$' \
  -bench '^BenchmarkHTTPDistributionWorkflow/200000/' \
  -benchtime=20x -benchmem -v -timeout=30m
```

| Disks / snapshots per disk | Current broad / next ms | History broad / next ms | Impossible disk-bound ms |
| --- | ---: | ---: | ---: |
| 3 / 1 | 267.904753 / 276.586023 | 267.462660 / 275.033510 | 0.078410 |
| 3 / 5 | 446.599399 / 474.607159 | 277.067400 / 285.407123 | 0.081288 |
| 12 / 1 | 270.116071 / 278.050621 | 269.524175 / 276.797721 | 0.079236 |
| 12 / 5 | 463.590851 / 489.937366 | 286.481110 / 294.606491 | 0.079286 |

Current exact means span 0.726490–0.784230 ms and current primary workflows
1.198736–1.512690 ms; history exact spans 0.778201–0.926798 ms and history primary
workflows 1.251517–2.919731 ms. The no-match row deliberately uses impossible disk
bounds, not valid bound predicates that happen to find nothing. These warm serial
means complete this bounded distribution matrix, not representative user inventories or
acceptance approval. Additional cold/history scenarios depend on selected supported limits;
HTTP p95 is unmeasured and the acceptance latency metric must be agreed.

### Verified catalog-page-cache-cold HTTP open plus query

`84a27ca` adds Linux-only `BenchmarkHTTPCatalogColdOpen`. Before **every** timed
sample it closes SQLite, syncs the temporary catalog, applies `POSIX_FADV_DONTNEED`,
and requires `mincore` to report zero resident catalog pages. Surviving journal/WAL/SHM
sidecars and eviction/verification failures are rejected, not treated as warm fallbacks.
Cold responses are compared with validated warm baseline responses, including cursor
follow-ups. Tests cover residency validation, invalid files, eviction/data preservation,
sidecars and HTTP open failures.

Catalog opening/recovery occurs **inside** the timed real HTTP request: opening before
timing would warm schema/recovery pages. The runtime, HTTP client and server remain
warm; eviction and residency verification are untimed. This verifies catalog-file
page-cache-cold **open plus query**, not independently cold standalone query latency,
whole-filesystem/device-cache/hardware coldness, or p95.

Initial three-sample 50,000/million runs at `84a27ca` preceded the later replica-search
optimization and are historical exploratory evidence, not final-code samples. The final
million-current run after `1383276` passed in 194.164 seconds at three iterations per
case, on the same Linux/Ryzen/Go/GOMAXPROCS environment with no other benchmark active.
Every sample reported zero pre-open resident pages out of 242,233 catalog pages.

```sh
go test ./internal/httpapi -run '^$' \
  -bench '^BenchmarkHTTPCatalogColdOpen/1000000/' \
  -benchtime=3x -benchmem -v -timeout=30m
```

| Cold open + request | Final mean seconds/op |
| --- | ---: |
| Exact search | 4.530065720 |
| Selective substring | 4.533323759 |
| Broad first page | 8.275947632 |
| Impossible disk-replica bound | 4.516135305 |
| Exact next page | 4.525805919 |
| Broad next page | 8.291433701 |
| Content detail | 4.518971756 |
| Locations | 4.507581923 |
| Locations next page | 4.513421868 |

Opening/recovery dominates narrow requests. Do not subtract a warm open estimate and
call the remainder measured standalone cold latency. Required additional history-heavy cold
runs and standalone query/HTTP evidence depend on the agreed supported scope and cold boundary.
HTTP p95 is unmeasured; the latency metric and budgets need approval, not an unconditional
new percentile task. An ADR defining accepted cold/physical-storage measurement semantics is
proposed, not created or approved here.

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
inventory throughput. Actual import memory/journal and HTTP acceptance remain. Standalone
full-index recovery measurements are described next.

### Full-index interrupted-import recovery

Run startup recovery with production search triggers and directory indexing enabled:

```sh
go test ./internal/database -run '^TestFullIndexRecovery$' -count=1
go test ./internal/database -run '^$' \
  -bench '^BenchmarkFullIndexRecovery/' -benchtime=1x -benchmem -v
```

The fixture retains one completed snapshot with one unknown-size file and abandons
50,000 or 1,000,000 observations in a newer snapshot on the same disk. The abandoned
snapshot shares the completed file's path once and its content every tenth record;
all other paths and contents are import-owned. Paths have 100 nested directory buckets.
Observations are committed in batches of 5,000, with all production search triggers
enabled. A pending size for the shared content must never enrich completed metadata
during recovery. Two interruption stages cover ingestion before directory construction
and fully built directories before publication.

Fixture construction, verification, and closing are untimed. The timed operation is
`OpenContext`, including exclusive locking, SQLite opening, schema/foreign-key checks,
and abandoned-import cleanup. Verification checks completed data, directory summaries,
inventory revision, staging/orphan removal, exact/substring searches, and FTS
external-content integrity. The small `TestFullIndexRecovery` exercises the same fixture
and assertions without requiring benchmark-scale data in the normal test suite.

Measurements use Go 1.27.1 on Linux amd64, an AMD Ryzen 5 9600X host, with a
four-CPU container quota and an 8 GiB memory limit. Each case uses one measured
iteration (`-benchtime=1x`), not a latency distribution. The ordinary tests and vet
check ran concurrently near the start of the benchmark run; the race suite runs
separately afterward. Results are diagnostic samples, not isolated acceptance runs.

| Abandoned observations | Interruption stage | Open/recovery seconds | Observations/s | Catalog bytes before/after | Go allocated bytes/op |
| --- | --- | ---: | ---: | ---: | ---: |
| 50,000 | Before directories | 2.810 | 17,791 | 36,569,088 / 36,569,088 | 18,424 |
| 50,000 | Directories built | 1.687 | 29,640 | 52,473,856 / 52,473,856 | 13,256 |
| 1,000,000 | Before directories | 21.176 | 47,223 | 736,456,704 / 736,456,704 | 45,192 |
| 1,000,000 | Directories built | 33.646 | 29,721 | 1,060,134,912 / 1,060,139,008 | 13,128 |

All four cases passed recovery verification. The full run, including untimed fixture
construction and validation, took 153.4 seconds. Do not infer relative stage costs from
the 50,000-observation samples: the earlier case also competed with correctness checks.

This models persisted incomplete snapshots after committed work, not abrupt-process
termination or hot-journal crash recovery; those correctness contracts have separate
process tests. Reopening does not evict the OS filesystem cache. Reported allocations
are cumulative Go allocations during opening/recovery, not peak heap or RSS. SQLite
reuses freed pages without shrinking the catalog; the last case even grew by one
4 KiB page. Before/after bytes describe file length, not live index storage.
These measurements do not establish an acceptance
budget, filesystem-cold latency, or behavior for large retained multi-disk histories.

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

### Full-index streaming import scale and memory sampling

`BenchmarkSearchIndexedImport` now covers 50,000 and 1,000,000 files, each with
successful publication and a malformed final record after every valid file has
committed. It uses the production 5,000-record batches and search/directory indexes.
The disk-backed fixture has distinct SHA-256 contents and paths, shallow `archive/`
directories, and unknown sizes/mtimes. Fixture generation is incremental, not a
whole-inventory Go allocation. The late parse failure exercises ingestion and cleanup,
not cleanup after completed directory construction; standalone recovery covers that
separate interruption point.

Input generation, catalog initialization, disk creation, correctness checks, and
catalog close are outside the timed section. Success reports ingestion, directory
construction, publication, total duration, and input files/second. Failure reports
the complete parse-and-cleanup duration, not successful import throughput or an
isolated cleanup duration. Phase durations are not individual transaction durations.

A test-only sampler reads `runtime.MemStats.HeapAlloc` every 10 ms, plus initial/final
samples, throughout the import call, including directory building, publication, and
failure cleanup. The reported maximum is absolute process-wide sampled Go heap:
it includes the harness and retained allocations, is not peak RSS, can miss brief
peaks, and does not establish a memory bound. Sampling adds overhead to timings and
allocation totals. Sample counts are reported so short runs remain interpretable.
Final catalog bytes are measured after close; failed-import cleanup can leave free
SQLite pages in that file, so file size is not the size of surviving live records.

Both the benchmark and a small multi-batch test assert publication/cleanup outcomes,
inventory revision, root summaries, exact/substring search, foreign keys, and FTS
integrity. Assertions and integrity checks run after import ownership is released
and outside the measured operation. Failure checks all import-owned tables are empty.

Reproduce instrumented one-iteration measurements with:

```sh
go test ./internal/importer -run '^$' -bench '^BenchmarkSearchIndexedImport/50000/' -benchtime=1x -benchmem -v
go test ./internal/importer -run '^$' -bench '^BenchmarkSearchIndexedImport/1000000/' -benchtime=1x -benchmem -v -timeout=30m
```

Measured on 2026-10-09, Linux amd64, Go 1.27.1, AMD Ryzen 5 9600X, benchmark
parallelism 4, one iteration per case. The million-file run briefly overlapped
correctness/vet checks and the start of the focused race check; treat these as
instrumented operational samples, not isolated-machine comparisons or latency p95.

| Files / outcome | Total seconds | Input files/s | Sampled heap bytes | Heap samples | Final catalog bytes |
| --- | ---: | ---: | ---: | ---: | ---: |
| 50,000 / success | 1.364 | 36,669 | 5,603,240 | 138 | 42,717,184 |
| 50,000 / late failure + cleanup | 1.242 | 40,261 | 4,514,656 | 126 | 32,681,984 |
| 1,000,000 / success | 33.62 | 29,743 | 6,225,928 | 3,364 | 878,825,472 |
| 1,000,000 / late failure + cleanup | 31.70 | 31,542 | 6,320,280 | 3,172 | 672,092,160 |

Successful-import phase seconds (ingestion / directories / publication) were
0.6965 / 0.4653 / 0.2017 at 50,000 files and 19.29 / 9.778 / 4.557 at one million.
Cumulative timed Go allocations were 123,294,680 / 90,357,448 bytes for the small
success/failure cases and 2,574,837,272 / 1,861,021,520 bytes for the large cases;
these are allocation traffic, not simultaneously resident memory. The failure
cases preserve an empty accessible catalog despite the large remaining file.
The approximately 6.3 MB sampled heap at the larger scale is encouraging for this
fixture but neither proves bounded peak memory nor predicts arbitrary inventories.

These heap measurements do not close transaction/journal growth, peak RSS, representative
multi-disk/history-heavy imports, filesystem-cold workloads, or acceptance budgets.

### Sampled import journal growth

The same `BenchmarkSearchIndexedImport` cases now sample the catalog's `-journal`,
`-wal`, and `-shm` sidecars every 10 ms, with initial/final samples. Sampling starts
after catalog initialization and disk creation and stops after `Import` returns,
including deferred cleanup on failure. It excludes post-import integrity checks and
catalog close. Production batch sizes, journal mode, and transaction boundaries are
unchanged; all instrumentation is test-only.

Metrics report the maximum sampled apparent file size (`os.Stat().Size()`) of each
sidecar across benchmark iterations, not allocated disk blocks, cumulative write
traffic, or a combined simultaneous disk-space peak. Missing sidecars are normal
and contribute zero for that sample; other stat errors fail the benchmark. Polling
can miss short-lived journals or brief peaks, so these are sampled maxima, not proven
peak storage bounds. No journal size is attributed to a specific transaction or
phase. The `journal-samples/op` metric counts sampling rounds over the three paths.
Both samplers stop and join on benchmark cleanup as well as normal completion.

Reproduce with the commands in the preceding section. The following runs used
Linux amd64, Go 1.27.1, AMD Ryzen 5 9600X, benchmark parallelism 4, and one iteration
per case on 2026-10-09. The two scales ran serially without concurrent project checks.
The fixture and correctness assertions are unchanged.

| Files / outcome | Total seconds | Sampled journal bytes | Journal samples | Sampled WAL bytes | Sampled SHM bytes |
| --- | ---: | ---: | ---: | ---: | ---: |
| 50,000 / success | 1.335 | 6,458,248 | 135 | 0 | 0 |
| 50,000 / late failure + cleanup | 1.221 | 25,641,288 | 124 | 0 | 0 |
| 1,000,000 / success | 32.71 | 134,397,136 | 3,273 | 0 | 0 |
| 1,000,000 / late failure + cleanup | 31.54 | 534,899,248 | 3,156 | 0 | 0 |

Sampled heap maxima were 4,491,504 / 5,376,016 bytes for small success/failure cases
and 6,148,296 / 6,408,216 bytes for large cases. Final catalog sizes were unchanged
from the earlier measurements: 42,717,184 / 32,681,984 bytes at 50,000 files and
878,825,472 / 672,092,160 bytes at one million. Failed-import cleanup leaves reusable
SQLite pages; those catalog sizes are not surviving live-data sizes.

Total durations are close to the earlier heap-only samples (1.364 / 1.242 and
33.62 / 31.70 seconds), but separate runs with different contention do not isolate
sampler overhead. Sampling adds work to the operation, and these one-iteration
results are not latency p95 or an acceptance decision. Zero WAL/SHM maxima mean no
nonzero sizes were observed, not that polling proves those files never existed.
Transaction API durations are measured separately below. True peak journal storage,
broader import distributions, filesystem-cold workloads, and approved budgets remain
open. Child-process RSS measurements for this fixture are recorded below.

### Full-index import transaction durations

`BenchmarkImportTransactions` reuses the indexed streaming-import fixture and integrity
assertions above: fresh temporary catalogs, unique unknown-size SHA-256 contents and
distinct `archive/report-*.txt` paths, default 5,000-file batches, with either successful
publication or a malformed record after all files have committed. Search and directory
indexes use production code. Setup/catalog opening, fixture writing, post-import integrity
checks and catalog close are excluded from import and transaction metrics.

The harness generates a Go `-overlay` replacement for `internal/database/db.go`; no
tracked production source or transaction semantics change. The replacement wraps the
unchanged `TransactionContext` body with a per-DB observer, enabled only during `Import`.
This also observes deferred cleanup, which uses a separate background context. Each
duration starts immediately before calling the original body and ends after it returns,
including `BeginTx`, SQL work, completed `Commit` or deferred `Rollback`, and the
post-commit no-op rollback. These are exact boundary measurements of transaction API
wall-clock duration, not SQLite write-lock hold time or individual engine/fsync timings.
Modernc's exposed commit/rollback hooks were not used: there is no corresponding exposed
begin hook, and the commit hook runs before commit completion.

The observer records duration/error pairs after timing stops. Assertions require the
expected transaction count and successful completion of every observed transaction;
progress callbacks verify the batch/directory/publication boundaries before assigning
stage labels. The late parse error itself is not a failed transaction: all batches and
its cleanup transaction commit successfully. Small tagged tests exercise both outcomes
and explicit rollback. Observer changes and import execution are serial on each DB.
The overlay rejects changed source anchors rather than silently measuring another path.

Reproduce from the repository root using the Go toolchain:

```sh
go run ./scripts/measure-import-transactions -run 'Test(ImportTransactionTimingFixture|TransactionTimingRollback)$' -count=1 -v
go run ./scripts/measure-import-transactions -run '^$' -bench '^BenchmarkImportTransactions/' -benchtime=1x -benchmem -v -timeout=30m
```

Temporary overlays are created under `/tmp/opencode` (this directory must exist).
The `transactiontiming` build tag requires this generated overlay; it is deliberately
absent from ordinary `go test ./...` builds. To select one scale, replace the benchmark
pattern with `^BenchmarkImportTransactions/50000/` or `/1000000/`. The benchmark timer
and allocation metrics surround `Import`, not fixture construction or validation.

Serial one-iteration samples on 2026-10-09 used Linux amd64, Go 1.27.1, AMD Ryzen 5
9600X and benchmark parallelism 4. This slice ran no concurrent project checks or
heap/journal samplers; unrelated container activity was not controlled. Values below
are rounded benchmark outputs, not a latency distribution.

| Files / outcome | Import s | Transactions | Transaction total s | Setup s | Batch total s | Maximum batch s | Directory s | Publication s | Cleanup s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 50,000 / success | 1.350 | 13 | 1.332 | 0.002263 | 0.6721 | 0.07497 | 0.4512 | 0.2068 | — |
| 50,000 / late failure + cleanup | 1.212 | 12 | 1.195 | 0.001979 | 0.6640 | 0.07457 | — | — | 0.5291 |
| 1,000,000 / success | 32.73 | 203 | 32.37 | 0.001919 | 17.01 | 0.1112 | 10.41 | 4.954 | — |
| 1,000,000 / late failure + cleanup | 43.20 | 202 | 42.82 | 0.002049 | 25.21 | 0.2372 | — | — | 17.61 |

Batch totals cover 10/200 separate transactions, not one import-wide transaction.
Setup, directory construction, publication and cleanup each represent one transaction
where present. The largest measured transaction in each case was directory construction
or cleanup, not a batch. Total import time additionally includes parsing/digest work,
ownership/progress handling and observer overhead outside these boundaries.

Timing and recording add overhead; these samples do not isolate it against an uninstrumented
control. The million-file late-failure run was slower than the earlier journal-sampled
run; separate executions cannot attribute that difference to instrumentation or establish
a regression. Unknown sizes, one disk and one shallow directory do not represent shared
size enrichment, long/deep paths, repeated contents or history-heavy imports. These
measurements close the transaction-boundary timing slice for this fixture only, not true
peak storage, cold performance, approved limits or the backend acceptance gate. They do
not justify changing transaction policy.

### Isolated Linux import peak RSS

`BenchmarkImportPeakRSS` runs each import in a fresh instance of the importer test
executable, invoking only `TestImportRSSChild`. No production instrumentation or Go
overlay is involved. The parent generates the input before launch and performs all
catalog/index integrity checks after the child has exited. Each child opens a fresh
temporary catalog through `database.OpenContext`, creates one disk, runs production
`Import`, closes the catalog and writes a versioned completion record. Success and late
parse failure use the same 5,000-file batches, unknown-size unique SHA-256 contents and
short, shallow `archive/report-*.txt` paths as the earlier full-index import fixture.

The primary metric, `child-max-rss-bytes`, is Linux `/proc/self/status` `VmHWM`, read
once by the child after catalog close. Linux reports this in KiB (`kB` in the file);
the harness converts it to bytes with a factor of 1,024. The kernel maintains this
address-space RSS high-water counter throughout execution: this is not periodic RSS
polling and is not sampled Go heap. It covers the child after exec through that probe,
including the Go/test runtime, database driver, schema/catalog opening, disk creation,
parsing and indexed batches, directory construction/publication or failure cleanup,
catalog close and the small completion validation. It excludes fixture generation,
parent validation, compilation, and child work after the probe (JSON emission and test
teardown). It cannot attribute the maximum to one phase or isolate import-only memory.

The parent also records the exited child's Linux resource-usage `ru_maxrss` through
`exec.Cmd.ProcessState.SysUsage()` as `child-lifetime-max-rss-bytes`. This broader lifetime
counter includes launch/exit boundaries and teardown; it is kept separate rather than
assumed identical to the post-close address-space counter. Both counters matched for
every sample below. `child-mean-max-rss-bytes` averages per-child high-water marks;
it is not average resident memory over time. These are Linux kernel-accounted RSS
maxima, not a byte-exact instantaneous physical-memory peak or a universal memory bound.
They do not include uncharged filesystem cache, kernel memory, or combined parent/child
and container memory. No heap/journal sampler runs in the measured child.

Completion requires a successful child exit and a strict JSON result after catalog
close, with expected snapshot/file/content counts, all input files committed, a positive
import duration, and either no import error or the expected final-line parse error
without a cleanup error. Missing/malformed completion, invalid requests, missing input,
or missing/invalid kernel RSS fields fail rather than supply fallback measurements.
Before application reopening can recover anything, the parent inspects the exited
child's catalog read-only and rejects incomplete publication or surviving failed-import
rows. `60adce8` corrects this inspection to use an escaped absolute SQLite `file:` URI
with `mode=ro`; appending that query to a plain filename did not enforce read-only
opening with modernc. Regressions check write rejection, no creation of a missing catalog,
and URI-special characters in paths. The child imports and RSS accounting protocol are
unchanged; the numeric samples below predate this inspection fix, not a remeasurement
or a claim of measured performance equivalence. The parent then runs the shared search,
directory, foreign-key and FTS integrity checks.
Small tests cover both outcomes, protocol/child errors, KiB parsing and rejection of an
unclean snapshot without silently recovering it.

Reproduce on Linux from the repository root, without race instrumentation:

```sh
go test ./internal/importer -run '^TestImport(PeakRSSFixture|RSSChildErrors|RSSProtocol|RSSHWM|RSSUncleanCatalog)$' -count=1 -v
go test ./internal/importer -run '^$' -bench '^BenchmarkImportPeakRSS/' -benchtime=3x -v -timeout=30m
```

Use `^BenchmarkImportPeakRSS/50000/` or `/1000000/` to select one scale. Fixed three-iteration
runs launch three independent children per outcome, serially (not `RunParallel`). Fresh
catalogs and parent checks are outside the benchmark timer; `ns/op` times the child
launcher, including result decoding, rather than just import. `import-s/op` is measured
inside the child from `Import` entry through return, including deferred cleanup but
excluding opening/close. `child-s/op` covers process launch through exit; user/system CPU
metrics cover the child's lifetime. `B/op` with `-benchmem` would be parent allocation
traffic, not child allocations or RSS. Running this benchmark with `-race` changes the
child executable and cannot reproduce the uninstrumented memory values below.

Three serial repetitions per case on 2026-10-10 used Linux amd64, Go 1.27.1, AMD Ryzen 5
9600X and benchmark parallelism 4. The measurement command ran no concurrent checks of
its own; other agents/container activity and filesystem cache state were not controlled.
All maxima are bytes; each row lists raw child `VmHWM` values in launch order.

| Files / outcome | RSS repetitions (bytes) | Maximum bytes | Mean high-water bytes | Import seconds, repetitions |
| --- | --- | ---: | ---: | --- |
| 50,000 / success | 35,000,320; 33,308,672; 34,951,168 | 35,000,320 | 34,420,053 | 1.413100; 1.406355; 1.384772 |
| 50,000 / late failure + cleanup | 31,936,512; 31,535,104; 31,424,512 | 31,936,512 | 31,632,043 | 1.226363; 1.219723; 1.226789 |
| 1,000,000 / success | 55,169,024; 54,927,360; 55,377,920 | 55,377,920 | 55,158,101 | 33.245328; 34.065789; 38.341540 |
| 1,000,000 / late failure + cleanup | 59,883,520; 58,712,064; 58,675,200 | 59,883,520 | 59,090,261 | 45.235428; 35.739763; 35.763282 |

At one million files, successful children used 31.28–31.71 seconds of combined user/system
CPU and failed-import children used 29.30–29.62 seconds. Wall times vary more than these
CPU totals; scheduling overlap, I/O and other container activity were not isolated.
Do not infer an inherent importer speed regression, a latency distribution, or a
contention-independent RSS guarantee from this overlap. The kernel maxima are valid
for these executions under that environment, but not proof that all inventories or
memory-pressure conditions behave identically. This closes the isolated process-RSS
measurement slice for this fixture only. Long/deep paths, known/shared-size enrichment,
repeated contents, history-heavy imports, cold behavior, approved budgets and optimization
of slow cases remain separate work. No backend gate or acceptance limit is approved.

**Blocked — true physical peak journal storage:** the requested physical requirement
has not been replaced by logical VFS growth, apparent lengths or faster polling.
It requires a suitable filesystem and validated allocation/free/reservation instrumentation;
that environment/evidence is not available in this slice. Agree the accounting boundary
(journal alone or simultaneous catalog/sidecar/temporary storage), without weakening the
physical requirement. An event-complete trace must preserve file identity
across creation, growth, truncation, unlink and close (including open-but-unlinked files)
and reconcile the simultaneously live allocations with no dropped events. SQLite VFS
write/truncate/delete instrumentation can explain logical file growth and transaction
ownership, but does not prove allocated physical blocks under delayed allocation,
sparse extents, compression or copy-on-write. A physical-storage claim needs
filesystem-aware allocation/free/reservation evidence or an instrumented filesystem,
with the chosen accounting scope and trace overhead validated. Faster `stat` polling
does not provide that evidence. This RSS slice makes no true peak journal claim and
does not change transaction policy.

### Fixed operational import distributions

`bf9f873` adds `BenchmarkImportDistributions` with production search and directory
indexing enabled. Each scale denotes **new input files** (50,000 or 1,000,000), not
the baseline-plus-import catalog's total observations. Every measured import runs on
one disk with completed baseline inventories and generation-prefixed paths:

| Profile | Baseline snapshots | Distinct shared contents | Input metadata/shape |
| --- | ---: | ---: | --- |
| `deep_known` | 1 | N | Known 4096-byte sizes; nested disk/year/month/week/day/hour/minute/second/leaf paths |
| `duplicate_shared` | 1 | N/100 | Known sizes; repeated shared identities |
| `history_enrichment` | 3 | N | Baselines have unknown sizes; new import supplies 4096-byte sizes |

Success publishes N additional observations; late parse failure commits all ingestion
batches before a malformed final record and then cleans up, preserving baseline data.
Shared small fixtures independently assert counts, content sizes, root/deep directory
summaries, current search and foreign-key/FTS integrity. Publication-trigger failures
also verify rollback preserves historical unknown sizes and completed metadata; those
publication-failure timings are **not measured**.

Fixture generation, baseline imports and integrity checks are untimed. Metrics include
import/ingestion/directory/publication durations, input throughput, continuously sampled
Go heap and final catalog bytes including baselines and reusable failed-import space.
Samples are not RSS or proven memory peaks; phase durations are not transaction durations.
All six 50,000-input success/late-failure cases passed exploratory runs. Earlier guarded
million-input commands were rejected twice by tool permission without running. After the
container restart and renewed authorization, the million-input slice ran on 2026-10-10
at `72531bd`, with unchanged benchmark/production code, Go 1.27.1, Linux amd64 and an
AMD Ryzen 5 9600X. The benchmark reported four Go CPUs; `GOMAXPROCS` was not explicitly
set. Cases ran serially, one iteration each, without overlapping project checks. Other
container activity was not controlled.

**The million-input matrix did not pass:** all three success cases passed their full
assertions; deep-known and duplicate/shared late-failure cases failed cleanup assertions;
history-enrichment late failure was interrupted before reporting a result. Successful
metrics are individual samples, not extrapolations, p95, cold measurements or acceptance.

| Million-input success profile | Import s | Ingestion s | Directories s | Publication s | Input files/s | Sampled heap bytes | Heap samples | Final catalog bytes | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `deep_known` | 313.7 | 105.8 | 195.7 | 12.16 | 3,188 | 7,764,416 | 31,373 | 7,723,622,400 | 4,917,834,104 | 124,059,105 |
| `duplicate_shared` | 95.03 | 83.90 | 6.088 | 5.045 | 10,523 | 6,209,240 | 9,504 | 1,487,233,024 | 3,620,015,072 | 121,971,736 |
| `history_enrichment` | 80.09 | 39.28 | 16.42 | 24.40 | 12,485 | 6,412,112 | 8,011 | 3,683,500,032 | 5,292,504,168 | 161,022,441 |

Exact standard `ns/op` values were 313,711,227,558 / 95,028,953,420 / 80,093,237,340
in the same order. Final catalogs contain two million observations for deep/shared
success and four million for history success; history baselines contain three million.
The assertions validated snapshot/observation/content/index counts, staged-row removal,
known-size enrichment of historical directory totals, root/deep summaries, current
revision/search, foreign keys and FTS integrity for each successful case.

Both completed late-failure attempts reported at `distribution_benchmark_test.go:372`:
`observation: 2000000, want 1000000`. The initial expected parse-error check passed,
but failed-import observations remained when cleanup counts were checked. No validated
failure metrics were emitted; subsequent preservation/integrity checks were not completed.
The importer has a separate 30-second cleanup context and joins cleanup errors with parse
errors, but this benchmark does not print the joined error when the later count assertion
fails. A cleanup timeout is a hypothesis, not a diagnosed cause. These failures require
investigation before operational acceptance; the earlier full correctness-suite pass does
not establish million-distribution cleanup correctness.

Fixtures used the authorized working filesystem through unique
`TMPDIR=/workspace/.million-distributions-3388537839`, never the workspace catalog.
A one-second `statfs` watchdog monitored available space, with a 2,000,000,000-byte abort
threshold. Initial/minimum sampled/final available bytes were
36,062,552,064 / 26,935,201,792 / 34,078,691,328; the space guard never triggered.
The outer deadline was 29 minutes, leaving signal/shutdown headroom inside the 30-minute
harness limit; the Go test command itself used `-timeout=30m`. At 1,740.028 seconds the
watchdog sent SIGINT to the isolated benchmark process group, interrupting the history
late-failure case. Go test reported `signal: interrupt`, package duration 1,721.183 seconds
and nonzero exit. No claim is made about how far that case progressed.

Normal test cleanup removed earlier fixtures. The interrupted temporary root was nonempty,
so its empty-root removal failed and artifacts were preserved (about 646 MiB at inspection),
not recursively deleted or reopened for recovery. Logs and guard metadata are under
`/tmp/opencode/million-distributions-2596164939/`; the wrapper source is
`/tmp/opencode/run-million-distributions.go`. Those paths are local run artifacts, not
repository deliverables. Signals cannot guarantee test `TempDir` or importer cleanup.

```sh
go test ./internal/importer -run '^TestImportDistributionFixtures$' -count=1
go test ./internal/importer -run '^$' \
  -bench '^BenchmarkImportDistributions/50000/' -benchtime=1x -benchmem -v -timeout=30m
# Million-input run above used the guarded wrapper and a unique workspace TMPDIR:
go test ./internal/importer -run '^$' \
  -bench '^BenchmarkImportDistributions/1000000/' -benchtime=1x -benchmem -v -timeout=30m
```

Execution permission is no longer the blocker. Two observed cleanup failures and one
interrupted case leave the million-input slice incomplete. The original matrix did not
change production code. Publication-failure timing, broader import RSS/journal/transaction
distributions, physical peak storage, cold measurements and approved budgets remain open.
Logical journal accounting is not an approved substitute for the blocked physical-peak requirement.

#### Bounded cleanup diagnosis

The follow-up on 2026-10-10 first added diagnostic context without weakening assertions:
failed distribution imports log the entire joined error before checking table counts, and
`CleanupImport` wraps each DELETE error with its stage while preserving the underlying error.
Cleanup still uses one transaction and the separate 30-second context. No schema, cleanup
algorithm, timeout, or catalog-access policy changed.

Only the million-input `duplicate_shared/late_failure_true` case was rerun:

```sh
go test ./internal/importer -run '^$' \
  -bench '^BenchmarkImportDistributions/1000000/duplicate_shared/late_failure_true$' \
  -benchtime=1x -benchmem -v -timeout=30m
```

The unique workspace `TMPDIR`, isolated process group and one-second free-space watchdog
were retained, with a 2,000,000,000-byte threshold and a 30-minute outer deadline. The
command finished normally with a failing test after 180.866 package seconds (181.478
wrapper seconds), without triggering either guard. Initial/minimum/final available bytes
were 33,790,889,984 / 31,555,260,416 / 33,556,045,824. No successful million case was repeated.
The benchmark printed both errors before the unchanged row-count failure:

```text
invalid request: line 1000002: expected a space separating fields and path: "broken"
cleanup snapshot 2 failed: delete import observations: context deadline exceeded
observation: 2000000, want 1000000
```

This confirms that the observation DELETE exceeds the cleanup deadline in the duplicate/shared
reproduction, causing transactional cleanup rollback. It does **not** diagnose the separate
deep-known failure or assign the time to any individual trigger, FTS operation, index update,
or journal I/O. The command emits no validated failure throughput or import metrics.
Logs/guard metadata are in `/tmp/opencode/million-cleanup-3948105496/`; the diagnostic wrapper
is `/tmp/opencode/run-million-cleanup.go`. Its root `/workspace/.million-cleanup-577680217`
was retained, though normal test `TempDir` cleanup removed the case's catalog.

`TestImportCleanupReferenceIndexes` now checks the observation DELETE, shared-path lookup,
search-path lookup and directory-file observation reference in addition to staged/owned
content references. All checked plans use indexes: the snapshot/path unique index,
`observation_path_snapshot`, the search-path unique index, directory-file integer primary
key, `import_content_content` and `pending_size_content`. There is no confirmed missing-index
fix in these plans. Observation deletion still invokes per-row immutability, search-path/FTS
maintenance and directory-build invalidation; their relative costs are unmeasured.
`TestImportCleanupErrorStages` injects failures at all five DELETE stages and checks the
stage/original error and rollback preservation of snapshot, observation, staging, content,
search and directory rows.

The earlier interrupted history artifact was inspected through `mode=ro&immutable=1`, not
application open or recovery. Its unrecovered main file contains snapshot 1 in `importing`
state, 320,000 observation/content/search-path rows, and no directories; its 300,104-byte
journal was not replayed. These are **not recovered or transaction-consistent counts** and
do not establish how far the interrupted process progressed. In particular, the artifacts
do not prove that the timed late-failure import began; baseline setup is untimed. All original
fixtures, the workspace catalog and its lock remain untouched.

No bounded cleanup optimization was established, so the million-input gate remains Partial.
The next bounded task is to profile the duplicate/shared observation DELETE and its triggers
on an owned temporary fixture, preserving the completed baseline and FTS integrity checks.
Use per-case guarded runs rather than another whole-matrix deadline. If a concrete optimization
is found, first test rollback, shared-path preservation, cancellation, recovery and joined
errors at small scale, then rerun only the affected million failure. Deep-known late failure
is the next undiagnosed completed failure; history late failure remains unmeasured. A timeout
or transactional-cleanup policy change requires a proposed ADR covering the decision, context,
alternatives and consequences, and user approval before implementation; none was made here.

#### Cleanup profiling without a policy change

The next bounded investigation adds two test-only profiling fixtures. Neither changes
production cleanup, triggers, constraints, migrations, or the 30-second deadline:

- `BenchmarkObservationCleanupRollback` seeds two generation-prefixed inventories in
  5,000-row transactions with 500 shared contents, builds/publishes the first snapshot,
  then times the actual observation DELETE and transaction rollback for the second.
  The second snapshot has no directories, matching the late-parse-failure stage.
  Post-timing checks verify observation/search counts and external-content FTS integrity.
- `BenchmarkDistributionCleanup` uses the actual duplicate/shared distribution inputs
  and production importer. A test-only abort trigger retains the failed snapshot;
  after dropping it, the benchmark profiles `CleanupImport`, reopens normally to clear
  the intentionally poisoned handle, and runs the existing completed-data, current-search,
  directory, staging, foreign-key and FTS assertions. Fixture setup and validation are
  untimed. Its context is the benchmark context, not the importer's cleanup deadline:
  this diagnostic measures completion cost and cannot establish deadline compliance.

Both label timed work `phase=cleanup`, so CPU profiles can exclude expensive setup:

```sh
go test ./internal/importer -run '^$' \
  -bench '^BenchmarkDistributionCleanup/50000$' -benchtime=1x \
  -cpuprofile=/tmp/opencode/cleanup.cpu -outputdir=/tmp/opencode -timeout=30m
go tool pprof -top -relative_percentages -tagfocus=phase=cleanup \
  /tmp/opencode/cleanup.cpu
```

On 2026-10-10, the isolated 50,000-row observation DELETE/rollback took 0.476 seconds.
Exploratory million-row DELETE/rollback measurements took 14.437 seconds with one seed
transaction per snapshot and 15.184 seconds with 5,000-row seed transactions. These
synthetic runs used ordinary Go temporary directories, not the production-shaped guarded
million distribution command; they had no free-space watchdog. They do not reproduce the
original deadline failure or cover its 10,000 distinct contents and production insertion
details. The batched run sampled 11.91 cleanup CPU seconds: B-tree index positioning
accounted for 4.50 cumulative seconds (37.8%), FTS deletion for 4.06 (34.1%), including
2.16 seconds of FTS merge-level work; syscall samples accounted for 2.04 flat seconds.
Cumulative percentages can overlap and must not be summed. Wall time includes I/O and
rollback. This supports substantial indexed/FTS maintenance cost, not a demonstrated
unindexed or quadratic scan.

A disposable SQL experiment predeleted unsealed search paths owned exclusively by the
failed snapshot, preserving shared paths, before deleting observations. It took 14.294
seconds on the same batched synthetic shape versus 15.184 seconds for normal ordering
(one sample each). The extra query/ordering was not retained: the small unpaired difference
does not demonstrate a reliable solution to a 30-second failure, and no production-shaped
million measurement validates it. Production already deletes directories first. Expanded
plan tests also cover the exact search-delete-trigger lookup and both directory-parent
membership cascades; all are indexed.

The production-shaped 50,000-input cleanup benchmark passed full preservation and integrity
checks at 0.505 seconds of isolated cleanup (3.050 seconds including setup/verification).
Its first development run exposed the expected poisoned-handle read rejection; reopening
after cleanup fixes the benchmark, not production. A guarded production-shaped million
profile command was denied by tool permission before execution. No attempt bypassed that
denial, no successful million matrix case was repeated, and no deep/history failure rerun
was made. The original duplicate/shared observation deadline failure therefore remains
unresolved; this investigation does not claim an optimization or deadline pass.

Next: obtain execution approval for the guarded production-shaped cleanup profile and
compare its trigger/FTS/journal costs with this synthetic result before selecting a SQL
change. If meeting the deadline instead requires deferred indexing or non-atomic cleanup,
propose an ADR with context, alternatives and consequences for approval; those are policy
changes, not safe local optimizations. No ADR or new production subsystem was created.

Focused normal cleanup/index/publication tests and distribution-fixture tests passed;
the latter took 12.380 seconds. The final 50,000-row rollback benchmark, including its
new rollback/FTS assertions, passed at 0.475 seconds. `go vet ./...` and `git diff --check`
passed. One final serial focused race command covering cleanup/index/publication and
distribution fixtures passed (database 1.567 seconds, importer 439.734 seconds); the
full race suite was not repeated. CPU profiles remain local under `/tmp/opencode/`
(`cleanup-million-batched.cpu`, `cleanup-million-bulk.cpu`,
`production-cleanup-small.cpu`) and are not repository deliverables.

## Verification and follow-on work

### Final implementation checks

After the latest implementation, the parent reported all required checks passed:

- `go test ./...`: importer 19.081 seconds; other tested packages cached.
- `go vet ./...`: passed with no output.
- `go test -race -p 1 ./... -timeout=30m`: cmd/coldcat 54.532 seconds, app 8.673 seconds,
  database 43.200 seconds, httpapi 33.556 seconds, importer 653.412 seconds;
  scripts package has no tests. This was the only end-of-work full race run.

The original matrix follow-up ran benchmarks and documentation diff checks only. The bounded
diagnostic follow-up passed `go test ./...` (importer 22.022 seconds) and `go vet ./...`, plus
one focused serial race check of cleanup-stage/index, distribution-fixture and cleanup-poisoning
tests: database 2.047 seconds, importer 454.939 seconds. The full race suite was not repeated.
The observed million-distribution cleanup failures prevent treating that operational slice
as correct. Operational acceptance remains Partial. The concrete
[remaining decisions and blockers](coldcat-backend-api.md#remaining-decisions-and-blockers)
are supported limits/budgets, cold boundary/latency metric, million-input cleanup failures
and the interrupted history case, true physical journal-peak instrumentation and final gate approval.

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
