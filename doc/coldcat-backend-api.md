# Coldcat backend, import CLI, and query API plan

## Goal and completion criteria

Build a tested backend for an offline disk catalog. The primary workflow is:

1. Search a remembered filename or disk-relative path.
2. Open the matching content.
3. See its known locations, disks, replica counts, and observation details.

Also support directory browsing/sizing, directory replication, and redundancy filters. Implement and exercise the application and HTTP APIs through tests before starting a frontend. No web UI or Wails integration in this work.

## Existing implementation

- Go, SQLite (`modernc.org/sqlite`), and `urfave/cli/v3`.
- `internal/base`: disk, snapshot, content, observation, scope, and query-result types with nullable unknown metadata.
- `internal/database`: transactional schema initialization/migration infrastructure, Windows/Unix exclusive catalog locking, import staging/publication/cleanup/recovery, and focused snapshot/content/observation queries.
- `internal/importer`: streams `.cshd` records in batches of 5,000, validates records, propagates scanner/read errors and cancellation, and cleans up failed imports.
- `internal/app`: context-aware application methods for current catalog statistics, disk creation/editing/detail, importing, snapshot reads, content/hash lookup, observation summaries, and cursor-paginated disk/content lists, observations, disk snapshots, and exact/substring name/path search. Search and content lists support catalog-wide redundancy counts and membership filters.
- `internal/httpapi`: readiness, disk management and read-only content/observation/snapshot routes, explicit wire DTOs, structured errors, and HTTP contract tests; implemented routes are described in [openapi.json](openapi.json).
- `cmd/coldcat`: testable `create`, grouped `disk create/list`, `snapshot list`, and `import` commands with configurable `--db`, JSON list output, explicit capture-time selection, and signal cancellation.
- Tests cover schema initialization and migration infrastructure, constraints, cross-process locking, import failure/recovery, metadata staging, current/history selection, replica counts, and lookup query plans.
- Project-local `AGENTS.md` and backend documentation are present. No root `justfile` exists. `serve` runs the HTTP API on loopback by default.

### Implementation status

Completed tasks use `[x]`; unfinished tasks use `[ ]`. **Partial** identifies a task with completed subparts and lists its remaining work. Completion describes the implemented Go backend unless HTTP/CLI contract coverage is explicitly stated.

| Milestone | Status |
| --- | --- |
| A — fixtures, semantics, migration safety | Core schema and semantic tests complete, including the distributed partial-copy fixture. |
| B — reliable streaming imports and CLI | Cleanup, recovery, locking, duplicate-input detection, algorithm-specific hash-length validation, CLI output/progress/signal contracts, and process interruption tests complete; import measurements remain. |
| C — primary workflow | Case-insensitive exact/substring search → content → locations HTTP workflow, disk/content management and pagination, replica filters, and snapshot/detail routes complete. Fuzzy search removed by ADR 0001. |
| D — directories | Derived indexes, browsing/sizing, redundancy histograms, exact replicas, content coverage, filtering, and all five directory HTTP routes complete. |
| E — performance and handoff | Active milestone: close performance acceptance and the backend handoff gate. Historical fuzzy measurements led to ADR 0001; remeasure the simplified search schema and streaming imports before acceptance. |

Resolved foundation gaps:

- [x] Immediately clean failed imports, recover interrupted imports before catalog access, and fail access/startup when cleanup/recovery fails.
- [x] Return `ParseCshd` scanner/read errors rather than accepting truncated input.
- [x] Distinguish unknown sizes/mtimes from known zero-byte files and known timestamps.
- [x] Enforce unique snapshot/path observations and reject contradictory known sizes.
- [x] Separate capture/import time and require explicit capture-time selection/provenance.
- [x] Index snapshot selection and observation content/path lookups, with `EXPLAIN QUERY PLAN` tests.
- [x] Add folded basename/path exact indexes and FTS5 substring search; require three code points for substring queries, while short exact queries remain supported.
- [x] Add derived-directory indexes alongside their implementation.

Implementation details are documented in [backend-foundation.md](backend-foundation.md). Development database recreation was skipped at the user's request.

## 1. Settle and test the domain semantics first

### Snapshots and visibility

- Each checksum file is a **complete inventory of one disk**, not a delta or a selected subtree.
- Snapshots are immutable once completed. Keep history.
- Distinguish `captured_at` (inventory time) from `imported_at` (catalog ingestion time).
- Imports run only while the app is offline, with exclusive catalog access. Only one import can run at once, and the server cannot run during an import.
- Snapshots have `importing` and `complete` states. The completion marker supports crash recovery, not concurrent browsing during import. Remove failed imports immediately; remove abandoned imports before serving queries. All catalog queries use only complete snapshots.
- Default query scope is the latest complete snapshot per disk, selected by `(captured_at, id)` with a deterministic tie-break. A later import of an older inventory must not replace a newer inventory.
- Support `scope=current|history` where meaningful; explicit snapshot endpoints always use the requested complete snapshot.
- Return the applied scope and snapshot/capture context. "Current" means latest recorded state, not a claim that a disconnected disk has been verified recently.

### Content, locations, and replicas

- Retain current content identity: `(hash_type, hash)`. Different algorithms do not establish equivalence, even when sizes or names match.
- An observation is a file at a disk-relative path in one snapshot. A location in the current scope is `(disk_id, path)` from that disk's latest complete snapshot.
- Return separately: `location_count`, `disk_count`, and historical `observation_count`. Repeated snapshots do not add current replicas.
- For a selected observation, return `other_location_count` (exclude its own location) and `other_disk_count` (exclude its disk).
- Content-level redundancy filtering uses `other_location_count = location_count - 1` or `other_disk_count = disk_count - 1`, and includes only contents present in the requested current scope. Historical-only contents have zero current locations, not a negative replica count.
- Filters explicitly declare their metric: `replica_metric=locations|disks`. Default to disks for redundancy tasks, while exposing locations for the user's observation-based view.
- A search result links both an observation ID and a content ID. Same-name files with different hashes remain separate; identical contents at different names remain searchable by each name.

### Directories

- Identify a directory by `(snapshot_id, path)`. Root is `""`; paths use `/`, remain case-sensitive, and are relative to the disk root.
- Infer directories from file paths initially; empty directories and filesystem allocation sizes are unavailable in the current input format.
- Directory size is the sum of descendant file sizes, including repeated content at different paths. Also expose unique-content bytes/counts separately.
- When sizes are missing, return `known_bytes`, `unknown_size_file_count`, and `size_complete`; do not present the subtotal as an exact size.
- Two separate replication questions:
  1. **Exact tree replicas:** same descendant relative file paths and content identities; root directory name may differ. Ignore mtime. Extra or missing files make the trees different.
  2. **Content coverage:** for every distinct content beneath a directory, identify other disks containing it anywhere. Return per-disk coverage and a histogram of descendant files by other-disk count. Coverage distributed across disks is not an exact directory copy.
- For coverage, exclude the source disk. For exact-tree matching, report both same-disk other roots and distinct other disks.
- Use a versioned canonical directory manifest/fingerprint as a derived index. Encode path and hash identity unambiguously; preserve file multiplicity. Verify candidate manifest equality before declaring a match. Never match directory copies merely by total size or unordered content set.

### Filtered directory comparisons

- Exact-tree comparison supports optional allow/block glob patterns applied symmetrically to descendant file paths relative to each compared root.
- With no allow patterns, all files are eligible. Otherwise, a file must match at least one allow pattern. Any matching block pattern excludes it; block rules take precedence.
- Define glob syntax explicitly: `/` separates path segments, `*` matches within a segment, and `**` spans segments. `**/*.log` excludes `.log` files at any depth, including directly under the compared root. Patterns are case-sensitive and support escaping literal glob characters.
- Two directories are exact copies **under the supplied filters** when retained relative paths and content identities match. Extra or missing excluded files do not prevent filtered equality.
- Responses include the applied filters and retained/excluded file counts, distinguishing filtered equality from whole-tree equality. A comparison retaining no files returns an explicit empty-comparison result rather than a replica list or misleading coverage percentage.
- Coverage uses the same source-file selection rules; destination lookup remains content-based regardless of destination filenames.
- Whole-tree fingerprints accelerate unfiltered comparisons. Arbitrary filters require query-specific manifests/fingerprints; an unfiltered fingerprint mismatch must not exclude a candidate that could match after filtering. Cache keys, if needed, include normalized filters and the comparison algorithm version.

### Observation dates

- Distinguish source `mtime`, inventory `captured_at`, and catalog `imported_at`.
- Missing mtime is null. Mtime is reported metadata, not proof of a content change.

## 2. Backend boundaries and future metadata

Use the existing application layer as the shared backend:

`CLI / HTTP handlers / future Wails bindings -> internal/app -> database queries and importer`

- Application methods take `context.Context`, typed requests, and typed results; SQL stays in the database layer, parsing in the importer, HTTP details in a new transport package.
- Keep domain/query results separate from HTTP DTOs where wire formatting differs. Centralize error classification and validation.
- Start with concrete SQLite-backed services and focused query files; introduce interfaces only at real substitution boundaries.
- Content owns byte-derived properties; observations own location/snapshot-specific properties.
- Future duration, dimensions, MIME information, and thumbnails attach to content unless their semantics are observation-specific. Store derived metadata with extractor/version/provenance and artifacts outside the main content rows.
- Do not implement extractors, thumbnails, a generic metadata/EAV subsystem, or job infrastructure now. Keep result composition and migrations extensible so these additions do not alter content identity or duplicate metadata per location.
- Propose short ADRs for offline/exclusive-access imports, snapshot/replica semantics, filtered directory equivalence, and the shared application boundary. Include context, the decision, alternatives, and consequences; create repository ADRs only after approval or if a repository convention is established.

## 3. Storage and import correctness

### Migrations and constraints

- [x] Replace `CREATE TABLE IF NOT EXISTS` as the sole schema mechanism with ordered transactional migrations and a schema version.
- [x] Add snapshot completion state, capture/import timestamps, input path/format and capture-time provenance, and file/content counters. Report failed-import diagnostics through CLI errors rather than retaining failed inventories.
- [x] Represent unknown size and mtime explicitly; reject conflicting known sizes, integer overflow, invalid paths/hex/metadata, and unsupported format versions.
- [x] Adopt the pre-0.1 policy of updating the initial schema instead of writing compatibility migrations. Keep the transactional migration runner and its tests for future upgrades. Recreate development databases when needed; this session's recreation was skipped.
- [x] Enforce unique `(snapshot_id, path)` observations. Unknown size is null; a supplied zero means a known empty file.
- [x] Remove persistent trust/visibility states. Inventories are assumed complete; `importing` exists only until publication or cleanup. Recover interrupted imports before catalog access.
- [x] Index observation content/snapshot and snapshot/path lookups and latest-snapshot selection. Validate these lookup shapes with `EXPLAIN QUERY PLAN`.
- [x] Add basename/path search indexes with query-plan tests: distinct-path lexicon, folded exact B-trees, and one FTS5 trigram index. No fuzzy or one-/two-rune posting tables remain.
- [x] Add directory parent/fingerprint indexes, with query-plan tests for those services.
- [x] Add a derived directory table keyed by `(snapshot_id, path)` with parent, counts, known-byte aggregates, and manifest fingerprint; build it bottom-up before snapshot publication. Per-directory content occurrences support unique-content summaries and atomic enrichment of historical directory totals.

### Import lifecycle

1. **Done:** acquire exclusive catalog access, recover abandoned imports, and validate disk, format, explicit inventory time, and input path.
2. **Done:** create an `importing` snapshot.
3. **Partial:** stream records in bounded batches of 5,000 with typed progress and throttled CLI stderr reporting. Measured memory bounds remain.
4. **Done:** validate paths/metadata/hash encoding and algorithm-specific digest lengths, deduplicate content, and reject duplicate snapshot paths.
5. **Done:** return scanner/read errors and cancellation, with line/path context in record failures.
6. **Done:** search indexes are built transactionally with observation batches; directory aggregates/fingerprints are built in a separate derived-index transaction before publication, streaming rows without whole-inventory Go collections.
7. **Done:** atomically publish the snapshot and staged sizes after parsing and all current-schema writes succeed, including search-path and directory-build prerequisites. Shared-size enrichment updates affected historical directory summaries in the publication transaction.
8. **Done:** immediately and transactionally delete failed snapshots, observations, directories and their memberships, staged sizes, search entries, and unreferenced import-owned content while preserving shared content and completed indexes.
9. **Done at shared initialization:** retain the incomplete marker if cleanup fails, recover abandoned imports before another import or catalog access, and fail startup on recovery failure. Server initialization uses this same path.

- [x] Stage shared-content size enrichment until successful publication and track content introduced by each import for targeted orphan cleanup.
- [x] Run failure cleanup with a separate 30-second context and report both the original import error and any cleanup failure.
- [x] Store a streaming, order-sensitive semantic digest of parsed file records plus disk, format, and capture time for duplicate-import detection. Reject accidental repeats with the existing snapshot ID; deliberate repeats require `--allow-repeat`. Comments and equivalent parsed representations do not affect identity.
- [x] Require explicit `--captured-at` or `--use-source-mtime` and record the capture-time provenance.
- [x] Define CSHD paths independently of host `filepath`: preserve case/Unicode, use `/` separators, reject absolute paths and parent traversal, and treat backslashes as literal filename characters.
- [x] Configure and test per-connection SQLite foreign keys. Acquire Windows/Unix exclusive catalog ownership before opening/migration/recovery and hold it for the session; reject competing sessions and simultaneous in-session imports/catalog queries.
- [x] Exercise server/import overlap through the server's shared initialization path, including competing server/import child-process nonzero exits. SQLite write locking alone does not enforce this contract because reads may still be allowed.
- [x] Keep bounded batch commits initially.
- [ ] **Partial:** sampled journal growth and transaction API boundary durations measured for full-index successful imports and late-failure cleanup at 50,000/1,000,000 files; true peak journal storage and broader import distributions remain open. These durations include begin through completed commit/rollback, not SQLite lock-hold time. Do not change to a single import-wide transaction on these samples alone; WAL is not a requirement for concurrent import/read access in this scope.

## 4. CLI surface

Retain compatibility for existing `create` while introducing grouped commands as appropriate:

```text
coldcat --db <database> disk create --label <label> --capacity <capacity> [--serial ...] [--notes ...]
coldcat --db <database> disk list [--json]
coldcat --db <database> import --label <label> <file.cshd> --captured-at <RFC3339>
coldcat --db <database> import --disk-id <id> <file.cshd> --use-source-mtime
coldcat --db <database> snapshot list --disk-id <id> [--json]
coldcat --db <database> serve --listen 127.0.0.1:8080
```

- [x] Retain `create` and provide local `import` with explicit capture-time selection. Successful import prints snapshot ID, complete state, and file/content counters to stdout.
- [x] Add elapsed time, stderr progress, and machine-readable final output (`import --json`).
- [x] Propagate SIGINT/SIGTERM cancellation and return nonzero exit codes on command errors.
- [x] Implement graceful server shutdown and process-level SIGINT/SIGTERM success-exit contract tests. Import signals before publication clean up and exit nonzero, with process-level contracts.
- [x] Open/migrate/recover through one shared initialization path, using configurable `--db` rather than a hard-coded path in handlers.
- [x] Automatically recover interrupted imports; no manual failed-snapshot cleanup command is required.
- [x] Implement `serve` with exclusive catalog ownership, grouped `disk create`, `disk list`, and `snapshot list`. List commands support `--json` and traverse all pages. Stop the server before importing or running other catalog CLI commands.
- [x] Expose disk creation/editing over HTTP for future catalog management.

## 5. HTTP API contract

**Status: implemented; performance acceptance remains.** Readiness, current catalog statistics, disk list/detail/create/edit, case-insensitive exact/substring name/path search, hash lookup, content detail, paginated content lists with redundancy/membership filters, paginated content observations, paginated disk snapshots, observation detail, complete snapshot detail, and all directory browsing/comparison routes are implemented with contract tests and [OpenAPI](openapi.json).

Version under `/api/v1`. GETs are read-only. Keep result lists bounded and paginated with deterministic sorting. Use structured errors such as `{error: {code, message, details}}`; map validation/not-found/conflict failures consistently. Emit UTC RFC3339 timestamps, nullable unknown metadata, and hashes as algorithm + hex. Define byte counts/IDs safely for JavaScript clients (decimal strings for potentially unsafe integers).

| Method and route | Purpose and principal result |
| --- | --- |
| `GET /healthz` | Server/database readiness; failure is explicit. |
| `GET /api/v1/catalog` | Implemented: current disk/file/distinct-content totals, scope and inventory revision. One catalog per backend; no persistent catalog identity or history totals. |
| `GET /api/v1/disks` | Implemented: paginated disk metadata and latest complete snapshot summary. |
| `POST /api/v1/disks` | Implemented: create disk, returning 201 and Location. |
| `GET /api/v1/disks/{id}` | Implemented: metadata, cataloged bytes/counts and size completeness, latest snapshot and capture time. Cataloged bytes are not measured disk usage/free space. |
| `PATCH /api/v1/disks/{id}` | Implemented: atomically edit label/notes/serial/capacity; omitted fields remain unchanged, null/empty notes and serial clear them. |
| `GET /api/v1/disks/{id}/snapshots` | Paginated complete inventories. |
| `GET /api/v1/snapshots/{id}` | Complete inventory provenance, dates, counts, and metadata completeness. |
| `GET /api/v1/search` | Implemented: ranked case-insensitive exact/substring observation search with content IDs, scoped/current replica summaries, membership/replica filters, and revision-bound cursors. |
| `GET /api/v1/contents` | Implemented: paginated distinct contents, current/history scope, disk/directory membership and current redundancy bounds using both metrics. Counts remain catalog-wide. |
| `GET /api/v1/contents/lookup?hash_type=...&hash=...` | Exact known-hash lookup without requiring a catalog ID. |
| `GET /api/v1/contents/{id}` | Hash, size/completeness, scoped location/disk counts, historical observation count. |
| `GET /api/v1/contents/{id}/observations` | Paginated disks/paths/snapshots/mtimes; current or history scope. |
| `GET /api/v1/observations/{id}` | Observation, content, disk, inventory context, other-location/disk counts. |
| `GET /api/v1/snapshots/{id}/directories?parent=...` | Implemented: paginated immediate child directories and their summaries. |
| `GET /api/v1/snapshots/{id}/directory?path=...` | Implemented: recursive file/content counts, size completeness, maximum known mtime, current other-disk redundancy histogram. |
| `GET /api/v1/snapshots/{id}/directory/entries?path=...` | Implemented: paginated immediate files/subdirectories; optional recursive file listing and catalog-wide current replica filters. Child directories remain navigation entries under file filters. |
| `GET /api/v1/snapshots/{id}/directory/replicas?path=...` | Paginated exact-tree matches with optional allow/block filters and disk/root/snapshot context. |
| `GET /api/v1/snapshots/{id}/directory/coverage?path=...` | Per-other-disk coverage for optionally filtered source files, completeness, and byte subtotals. |

### Search and redundancy parameters

- Search: `q`, `field=name|path`, `match=exact|substring`, `scope=current|history`, optional disk/snapshot/directory filters, replica filters, and `limit/cursor`. Substring queries require at least three Unicode code points; short exact queries remain supported. Unsupported fuzzy mode and short substrings return 400.
- A full relative path means the entire path within its disk, not a host mount path.
- [ADR 0001](adr/0001-search-without-fuzzy.md) removes typo matching and short-character postings to reduce storage/import amplification. Use a single FTS5 trigram index over explicitly folded text and B-tree exact lookups. Rank folded exact matches before prefixes before other substrings; preserve original spelling. Unicode simple folding preserves punctuation, does not canonically normalize Unicode, and does not equate `ß` with `ss`.
- Do not load every observation into Go or silently truncate candidate sets while claiming complete ranked results. Retrieval and keyset pagination remain exhaustive. Current-schema import/storage and end-to-end HTTP performance acceptance remain; see [backend-foundation.md](backend-foundation.md).
- Replica filters: `replica_metric=locations|disks`, `other_replicas=0|1|2|...` and/or min/max bounds. For directory listings, evaluate counts against the whole current catalog, not just the selected subtree.
- Cursor state includes sort/filter context and the inventory revision (`MAX(id)` over complete snapshots, or zero). A successful offline import invalidates existing cursors; unchanged restarts preserve them. Cursors are for their issuing catalog; cross-catalog/recreated-database cursor detection is outside scope. No catalog identity table or stored revision counter is needed. Imports cannot change the inventory while the server is running; live-import invalidation is outside the initial scope.
- Keep historical search matches visibly historical; include whether that content still has current known locations.
- Directory replicas and coverage accept repeatable `allow` and `block` glob parameters using the shared comparison semantics. Include these filters in cursor context and response metadata.

### Main workflow payloads

Search results should contain enough data for one useful result list: observation/content IDs, basename, relative path, disk ID/label, snapshot/capture time, nullable size/mtime, relevance, and scoped location/disk counts. Content detail and its paginated observations then provide the replica view without per-result follow-up requests.

Create an OpenAPI description alongside the HTTP implementation, with real request/response examples and tested error/pagination behavior. Serve loopback by default; explicitly configure CORS if a later development UI needs another origin.

## 6. Test-first implementation sequence

For each milestone, first write behavioral acceptance tests, then implement the shared application functionality and its HTTP/CLI adapter. Use real temporary SQLite databases for query and import integration tests, and `httptest` for API contracts.

### Milestone A — fixtures, semantics, migration safety

- [x] Establish the three-disk semantic fixture with repeated snapshots, same-disk copies, later deletion, a changed hash at the same path, older inventories imported later, and equal-capture-time tie-breaking (`internal/app/app_test.go`). Exercise failed inventories through import failure/recovery fixtures (`internal/importer/importer_test.go`).
- [x] Include directory-shaped fixture data for identical trees under renamed roots, rearranged paths, an extra file, unknown size/mtime, and real empty files (`TestDirectoryShapedFixtureAndMetadata`). This establishes input fixtures, not directory-comparison functionality.
- [x] Add a distributed partial-copy fixture where source contents are split across other disks and no single other disk has the full set (`TestDirectoryBrowsingAndEnrichment`). The root histogram confirms every file has another disk while both other disks contain only two of the source's three content identities.
- [x] Test schema initialization, constraints, migration ordering, rollback/retry, version handling, and reopen/idempotence (`internal/database/foundation_test.go`, `db_test.go`).
- [x] Keep import failure/recovery tests and distinguish unknown sizes from known zero sizes; verify failed enrichment preserves completed metadata.
- [x] Assert current/history selection and location/disk/observation counts explicitly. Replace tests that permitted duplicate snapshot paths or contradictory known sizes.

### Milestone B — reliable streaming imports and CLI

- [x] Test successful multi-batch imports, late parse failure, injected reader errors, scanner token failure, cancellation, simulated interrupted-import state, and cleanup failure (`internal/importer`, `TestRecoveryIsRequiredBeforeUse`).
- [x] Test semantic duplicate-input detection, explicit repeats, multi-batch cleanup, metadata enrichment independence, reopen/recovery, CLI exit/output contracts, and cursor preservation/invalidation.
- [x] Test malformed/unsupported versions, invalid paths/hash encoding, numeric overflow, conflicting sizes, and duplicate paths across batch boundaries.
- [x] Validate and test algorithm-specific hash lengths in parsing and batch insertion. Import-backed fixtures use full-length identities; tests cover late-failure cleanup, metadata/cursor preservation, and CLI errors/nonzero exits.
- [x] Assert cleanup removes failed snapshot/observation/staging rows and import-owned orphans while preserving shared content and completed metadata. Verify recovery precedes catalog access/another import and that recovery failure prevents startup.
- [x] Extend cleanup assertions to search entries, including shared-path preservation, index-write failure after committed batches, cancellation, FTS integrity, and interrupted-import recovery.
- [x] Extend cleanup assertions to derived directories, memberships, immediate-file indexes, and build markers. Test build/publication/enrichment failures after committed batches, abandoned built indexes on reopen, and overflow rollback while preserving completed metadata.
- [x] Test cross-process catalog exclusion and OS lock release on abrupt process exit (`TestCatalogLockAcrossProcesses`). Test in-session query/disk-creation exclusion and second-import rejection (`TestImportBlocksCatalogQueriesAndSecondImport`).
- [x] Test server/import overlap and server startup recovery of seeded interrupted state, plus child-process interrupted imports with committed batches, graceful signal cleanup, and abrupt-exit recovery.
- [x] Exercise basic CLI argument validation, capture-time provenance, configurable database path, and successful human-readable output through `newCommand` (`cmd/coldcat/command_test.go`).
- [x] Complete stdout/stderr, progress, machine-readable output, elapsed-time, process signal, and exit-code contract tests.

### Milestone C — primary search → content → replicas workflow

- [x] Expose minimal current catalog statistics through application and `GET /api/v1/catalog`, with decimal-string counters, complete-snapshot selection, import/recovery semantics, and HTTP/OpenAPI contract tests. Disk count includes disks without inventories; file/content totals use the latest complete snapshot per disk. Inventory revision does not advance for disk metadata edits.
- [x] Implement context-aware content summaries, observation detail with disk/snapshot context, explicit snapshot detail, and latest-complete-snapshot selection (`internal/app/app.go`, `internal/database/queries.go`).
- [x] Implement hash lookup, paginated current/history content lists and observation lists, paginated disk snapshot lists, and disk detail/list/create/edit through application and HTTP APIs. Test disk metadata updates, latest-complete summaries, unknown-size completeness, revision-bound pagination, and grouped CLI list/create contracts.
- [x] List distinct contents with current/history scope, disk/directory membership filters, and current redundancy bounds using both metrics. Test catalog-wide counts, literal path/segment semantics, nullable sizes, revision-bound pagination, reopen/recovery and HTTP/OpenAPI contracts.
- [x] List a disk's complete snapshots through the application and `GET /api/v1/disks/{id}/snapshots`, ordered by capture time and ID descending, with revision-bound keyset cursors. Test empty/missing disks, capture-time ties, indexed first/deep pages, restart/recovery preservation, failed-import preservation, and successful-import invalidation.
- [x] Exercise hash lookup → content → paginated observations → observation/snapshot detail through HTTP integration tests, including three current disks and two other disks.
- [x] Write HTTP integration tests that import fixtures, search a remembered filename, follow `content_id`, list locations, and verify counts and dates end-to-end.
- [x] Test case-insensitive exact/substring basename/path searches, Unicode/punctuation, historical-only matches, replica bounds, stable pagination, long-path cursors, revision changes, restart/recovery, and HTTP errors. Import-backed HTTP tests exercise uppercase search → content → locations → observation/snapshot detail; reject fuzzy mode and substrings shorter than three code points.
- [x] Test zero/one/two other-location counts and zero/one other-disk counts, historical-only zero counts, same-disk copies, and repeated historical observations (`TestSnapshotAndReplicaSemantics`).
- [x] Add a content present on three current disks to exercise two other disks; implement and test content-list redundancy filters using both metrics, including same-disk copies and inclusive bounds.

### Milestone D — directories

- [x] Test root/nested browsing, segment boundaries (`foo` must not include `foobar`), wildcard characters in paths, recursive counts, duplicate-content bytes, and unknown-size completeness. Include zero-file roots, Unicode boundaries, long imported names/directories with compact ID-based cursors, historical redundancy, shared-size enrichment, staging-mutation invalidation, cursor binding/restart/recovery, and HTTP/OpenAPI contracts for all three browsing routes.
- [x] Test exact-tree equality despite root renaming/mtime changes; reject rearranged paths, missing files, and extra files.
- [x] Test filtered equality with differing excluded `.log` files, retained extra files, root/nested doublestar matches, allow/block precedence, escaped glob characters, different roots, and all files excluded. Filtered candidates are not eliminated by whole-tree fingerprint mismatches. Patterns use `github.com/bmatcuk/doublestar/v4` with documented request bounds.
- [x] Test per-disk coverage when source files are spread over unrelated directories and when no single disk has the full source set.
- [x] Test filtered source coverage, destination-name independence, and retained/excluded counts.

### Milestone E — performance, recovery, and backend handoff

- [ ] Agree a target catalog size and interactive latency budget; a suggested initial benchmark is one million observations, with results tracked for both cold and warm queries.
- [ ] **Partial:** exact/substring search, pagination, directory query/build/enrichment costs, directory-only recovery, and full-index standalone recovery measured at 50,000/1,000,000 observations; search-and-directory-indexed streaming import and late-failure cleanup measured at 50,000 files; test-only fuzzy candidate/reranking latency measured for 50,000 distinct names and persisted fuzzy latency/storage measured for 50,000/1,000,000 distinct paths. Filesystem-cold queries and broader multi-disk distributions remain. Full-index recovery covers interruption before directory construction and after completed directory construction, preserving a completed snapshot and shared content/search paths. Directory measurements cover wide/deep/high-duplicate trees; see `backend-foundation.md`.
- [x] Check query plans for latest-snapshot selection and observation content/snapshot/path lookups (`TestQueryIndexes`).
- [x] Check exact hash lookup and content observation-page indexes; add focused 50,000-observation warm-query benchmarks for hash lookup and first/deep pages.
- [x] Check content-list keyset and observation query indexes. Benchmark current/history first/deep pages, disk/directory membership and both redundancy metrics at 50,000 and 1,000,000 observations; document the catalog-wide cost of no-match bounds in `backend-foundation.md`. An agreed latency budget and cold-query measurements remain pending.
- [x] Check search query plans and remove full-catalog observation scans from normal exact/substring retrieval. Page qualifying paths before expanding observations; document measured broad-query costs.
- [x] Test application/import cancellation propagation and actionable catalog-lock errors; verify database-session recovery on reopen after seeded interruption or cleanup failure.
- [x] Test HTTP pagination across unchanged reopen/restart and successful offline imports; failed imports preserve cursors, and server startup recovers seeded interrupted state before readiness. Child-process interrupted imports additionally verify application pagination after recovery and invalidation after subsequent publication.
- [x] Run `go test ./...`, `go test -race ./...`, and `go vet ./...` with the configured Go 1.27.1 toolchain; all passed for the completed foundation. Packages/tests also cross-compiled for Windows amd64 and macOS arm64; runtime tests ran on Linux.
- [ ] **Partial:** focused content, exact/substring search, directory queries/build/recovery/comparisons, initial fuzzy spike, and search-indexed import benchmarks have run. Initial exact-replica and coverage measurements use 50,000 files. A five-disk fixture now measures filtered replicas and coverage with exact/filtered-only copies and complementary partial-content disks; bounded history-heavy comparisons also cover one/five snapshots per disk at 50,000 source files. Results and scale definitions are in `backend-foundation.md`. Broader distributions, larger history-heavy comparisons, and cold measurements remain. No `justfile` currently exists; adopt its commands if one is introduced.
- [x] Document schema initialization, pre-0.1 reset policy, import publication/cleanup/recovery, locking, and query semantics in `doc/backend-foundation.md`.
- [ ] Final backend gate: the principal workflow and every planned endpoint are exercised by contract tests; measured search behavior is acceptable; OpenAPI examples are usable by a future frontend.

### Active acceptance sequence

Backend acceptance is the next work, not another feature milestone. No frontend implementation
or frontend UX planning starts during this sequence. Snapshot diffing, UI-driven importing,
extractors, thumbnails, and Wails remain deferred.

1. **Done — measure persisted fuzzy scale:** the million-distinct-path benchmark produced
   a 5.09 GB catalog with 41.8 million signatures, built in 11m12.5s. Warm mean typo lookup
   was 0.798 ms; broad folded-prefix search was 1669.8 ms. Results are recorded in
   `backend-foundation.md`; storage and broad-query latency remain acceptance concerns.
   Distinguish distinct paths from repeated observations; raw fixture construction is not
   streaming-import throughput, and warm means are not p95.
   **Approved response:** ADR 0001 removes fuzzy and short-character indexes, retaining
   case-insensitive exact/substring search with a three-code-point substring minimum.
   Measure the equivalent current-schema fixture and streaming import/late-failure benchmark
   before treating the storage or import concern as resolved.
   **Measured simplified schema:** the million-path fixture is 615 MB (about 88% smaller)
   and builds in 60.5s. The 50,000-file streaming import runs at 15,960 files/s, with
   late-failure cleanup at 17,313 input files/s. Broad `REPORT` search still averages
   1874 ms; measurements are not an acceptance decision or a guarantee for user inventories.
2. **Agree acceptance limits:** select supported observation/distinct-path counts and budgets
   for ordinary search, broad queries, replica-filter no-match cases, directory comparisons,
   storage, import, and recovery. Keep limits proposed until explicitly approved. Existing slow
   cases must be accepted as documented limitations or improved, not silently omitted.
3. **Measure remaining operational costs:** full-index streaming import and late-failure cleanup
   memory, transaction duration/journal growth, and representative multi-disk/history-heavy
   and filtered-comparison workloads. **Filtered multi-disk comparisons measured:**
   the five-disk directory-only benchmark covers unfiltered, blocked-log, allowed-text,
   and empty selections, checking counts and identities against a small correctness fixture.
   Runs cover 50,000 and 1,000,000 source observations (160,001 and 3,010,001 catalog
   observations). At the larger scale, filtered replicas take about 7.7 minutes, coverage
   about 42–43 seconds, and empty comparisons about 38 seconds. These slow cases remain
   unresolved performance limits, not an acceptance decision.
   This does not close the history-heavy or filesystem-cold workload gaps or approve an
   interactive latency budget. See `backend-foundation.md`.
   **Full-index streaming-import measurement harness extended:**
   `BenchmarkSearchIndexedImport` now covers 50,000/1,000,000 distinct files with successful
   publication and late parse failure after committed batches. It reports phase durations,
   total input throughput, final catalog bytes, and Go heap sampled throughout the operation,
   including directory building, publication, and cleanup. Shared small-test/benchmark
   assertions check publication/cleanup, search, directory summaries, and index integrity.
   One-iteration million-file samples took 33.62 seconds for success and 31.70 seconds
   for failure plus cleanup, with maximum sampled Go heap of 6.23/6.32 MB and final
   catalog files of 878.8/672.1 MB (cleanup leaves reusable SQLite pages).
   Sampled Go heap is not RSS or a proven memory bound; phase timing is not transaction
   timing. Transaction API boundary durations are measured separately below; broader
   import distributions remain open.
   **Sampled journal growth measured:** the same success/late-failure cases now poll
   rollback-journal, WAL and SHM apparent file sizes every 10 ms through import return,
   including failure cleanup. Serial one-iteration million-file runs observed maximum
   rollback-journal sizes of 134.4/534.9 MB, with no nonzero WAL/SHM samples; total
   durations were 32.71/31.54 seconds. Polling can miss brief peaks, file sizes are not
   allocated storage or cumulative writes, and these runs do not isolate instrumentation
   overhead or approve a transaction-policy change. True peak journal storage remains
   open. Results and reproduction commands are in `backend-foundation.md`.
   **Full-index transaction boundary timing measured:** a generated test-only Go overlay
   wraps the unchanged transaction body, including completed commit/rollback and deferred
   failure cleanup. `BenchmarkImportTransactions` verifies transaction counts and phase
   boundaries with the same success/late-failure fixtures. One-iteration million-file runs
   measured 203/202 transactions, 32.37/42.82 seconds in transaction bodies, and
   32.73/43.20 seconds total import time. Successful directory/publication transactions
   took 10.41/4.954 seconds; failure cleanup took 17.61 seconds. These are API wall-clock
   durations, not engine lock-hold measurements, p95, cold performance or acceptance.
   Production behavior is unchanged; overhead is not isolated. True peak storage and
   broader import distributions remain open. See `backend-foundation.md`.
   **Standalone full-index recovery measured:**
   `BenchmarkFullIndexRecovery` covers 50,000/1,000,000 abandoned observations before
   and after directory construction, with completed-data preservation and index-integrity
   assertions. Measurements include catalog opening and use one iteration per case;
   they are not filesystem-cold results or an acceptance decision. See `backend-foundation.md`.
4. **Validate interactive behavior:** measure end-to-end HTTP search and follow-up reads,
   including warm and filesystem-cold behavior. Closing/reopening SQLite alone does not
   establish filesystem-cold performance. Compare results against the agreed limits.
   **Warm HTTP measurements complete:** `BenchmarkHTTPWorkflow` exercises real loopback
   requests at 50,000/1,000,000 total current observations across three disks, including
   exact/selective/broad search, second-page search, replica-filter no-match, content
   detail, location pages, and search → content → all locations. The small shared fixture
   verifies counts, identities and pagination. Twenty-iteration million-scale means were
   1.438 ms for the complete four-request workflow, 1931 ms for broad first-page search,
   2979 ms for its second page, and 2582 ms for replica-filter no-match. These are warm
   serial means, not p95 or an acceptance decision. Filesystem-cold measurements,
   broader history-heavy workloads, approved budgets and optimization/acceptance of slow cases
   remain open. See `backend-foundation.md` for fixture details and reproduction commands.
   **History-heavy warm HTTP measurements complete:** `BenchmarkHTTPHistoryWorkflow` retains five
   snapshots per disk across three disks, with 50,000/1,000,000 total historical
   observations and 10,000/200,000 current observations. It measures current/history
   exact/selective/broad searches and second pages, content and observation reads,
   current replica-filter no-match, historical-only search, and the current primary
   workflow. The small correctness fixture exhausts search and observation pagination,
   checks stable/changed/deleted identities, and confirms history never inflates current
   replicas. Twenty-iteration million-historical-observation means were 1.502 ms for
   the current four-request workflow, 743/978 ms for current broad first/second pages,
   406/634 ms for history broad first/second pages, and 2469 ms for current replica-filter
   no-match. These are warm serial measurements, not cold performance, p95 or budget
   approval; equal total-observation scales do not mean equal current result-set sizes.
   **Bounded history-heavy directory comparisons measured:**
   `BenchmarkDirectoryHistoryComparisons` holds 160,001 current observations fixed
   across five disks, retaining one/five snapshots per disk (160,001/800,005 total
   historical observations). Sixteen cases measure current/oldest source inventories,
   unfiltered/blocked-log selection and replicas/coverage. Shared small fixtures verify
   current-only destination selection, historical-only copies/content exclusion and
   the current same-disk replica of an older source. Three-iteration five-snapshot
   means are 576/999 ms for current/oldest unfiltered replicas, 2003/2459 ms for
   blocked-log replicas, and 372–429 ms for coverage. Fixture construction, warm-up
   and assertions are untimed; search indexing is excluded. These are warm serial
   means, not cold results, p95 or acceptance approval. Broader disk/history
   distributions, larger history-heavy comparisons and the other acceptance gaps
   remain open. See `backend-foundation.md`.
5. **Close handoff:** audit endpoint/primary-workflow contract coverage and OpenAPI examples,
   document integration setup (including same-origin serving or explicit development CORS),
   and rerun required correctness checks after any implementation changes. Record unresolved
   limits explicitly; do not mark the gate complete solely because benchmarks ran.
   **Contract/integration handoff slice complete:** `TestOpenAPIEndpointHandoff` exercises
   all 19 documented operations with a small imported two-disk fixture, checks success
   payloads against their declared schemas, and verifies common 400/405 errors and Allow
   headers. Success examples are required for every operation; missing examples/statuses
   and unusable short SHA-256 examples were corrected. [Backend integration](backend-integration.md)
   documents setup, primary reads, pagination, errors, ownership and same-origin/CORS
   constraints, and maps specialized semantic contract tests. No frontend or CORS feature
   was added. Performance approval, remaining measurements and the final gate remain open.

## 7. Future snapshot comparison and UI-driven imports

### Snapshot diffing (deferred)

- Later support comparing two complete snapshots to report files added, modified, deleted, and moved/renamed. Initially compare snapshots of the same disk; cross-disk comparison may be added later.
- Added: path exists only in the target snapshot. Deleted: path exists only in the source snapshot. Modified: the same path has a different content identity. Report metadata-only changes separately when content identity is unchanged.
- A disappeared path and newly appearing path with the same content identity are move/rename candidates. Report unique pairs as inferred moves, not proven filesystem operations. Preserve ambiguous many-to-many matches as candidates without arbitrary pairing.
- If the source path remains, report the new path as an addition/copy candidate rather than a move. Different hash algorithms do not establish unchanged content or a move.
- These are endpoint differences; snapshots cannot establish the exact operation sequence or change times between inventories.
- Support directory scope and the same allow/block file-selection semantics. Apply filters to each endpoint's relative paths. Only infer moves where both endpoints are in scope; a move across a filter boundary appears as an addition or deletion within the filtered view.
- A future endpoint could be `GET /api/v1/snapshots/{from_id}/diff/{to_id}`, returning summary counts and paginated changes with old/new paths, observation/content references, metadata differences, and move-inference status.
- Preserve immutable snapshots, stable path/content identities, and indexed `(snapshot_id, path)` and content lookups now. Do not implement the diff endpoint in the initial milestones.
- When implemented, exercise additions/deletions, content versus metadata changes, unique/ambiguous moves, copies, hash-algorithm differences, and filters through application and API tests before adding diff UI.

### UI-driven importing (deferred)

- Initial imports are local CLI-only operations with the server stopped.
- A future UI-supplied local path means a path accessible to the backend process, not necessarily the browser's filesystem.
- At that point choose explicitly between maintenance-mode importing (suspend catalog access until import/recovery finishes) and background importing (keep reads available using hidden in-progress snapshots and atomic publication). Background import/read concurrency is not an initial requirement.

## Confirmed decisions

1. Each import is a complete inventory of one disk.
2. Expose both replica metrics; default redundancy filters to distinct other disks, while supporting the requested other-location counts.
3. Directory replication includes both exact same-relative-path trees and independent file-content coverage, with allow/block filters for comparison.
4. All previously recorded observations remain searchable via explicit history scope; the normal search uses latest complete inventories.
5. Imports are CLI-only and offline, with exclusive catalog access. Automatically delete failed imports and recover interrupted imports before database use.
6. Support snapshot diffing later; preserve the necessary snapshot/path/content model now.

The backend functionality and primary HTTP workflows are implemented. Continue with the active
Milestone E acceptance sequence above; begin frontend work only after the final backend gate.
