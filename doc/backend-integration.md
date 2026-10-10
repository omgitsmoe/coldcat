# Backend integration handoff

This is integration documentation, not frontend implementation or approval of the final
backend gate. The wire contract is [openapi.json](openapi.json). Domain/import semantics
and benchmark results are in [backend-foundation.md](backend-foundation.md).

## Scope and implementation plan

This bounded Milestone E task covers endpoint contract coverage, success examples and local
integration setup. It does not change storage, add endpoints, approve performance limits,
or configure CORS.

1. Import a two-disk fixture into a temporary catalog, with identical directory trees,
   known zero-byte content, unknown size and missing mtime. Search a remembered uppercase
   filename and use returned content/observation IDs for subsequent reads.
2. Exercise every documented GET, POST and PATCH. Validate real response payloads against
   the operation's declared response schema, including resolved local references. Fail
   when a documented operation has no test case. Check unknown-parameter rejection and
   unsupported-method errors, including the `Allow` header.
3. Require at least one success example for every operation, and validate all documented
   examples with the existing schema assertions. Repair missing examples/statuses and use
   full-length SHA-256 values that can actually be passed to hash lookup.
4. Document startup/import ownership, the primary read workflow, numeric/null handling,
   cursor rules and browser origin setup. Run normal tests, vet and a focused HTTP race check.

These steps are implemented in `internal/httpapi/handoff_test.go` and the linked contract.
The schema assertions cover the types, references, required fields, enums, patterns and
formats used by these payloads; they are not a general-purpose OpenAPI validator. Examples
illustrate individual responses, not one shared catalog with reusable IDs/cursors.

## Local setup

Use Go 1.27.1 as specified by `go.mod`. From the repository root, create a separate catalog;
do not use or recreate the workspace catalog for experiments:

```sh
go run ./cmd/coldcat --db /tmp/opencode/demo.sqlite disk create --label archive --capacity 1000000
go run ./cmd/coldcat --db /tmp/opencode/demo.sqlite disk list --json
go run ./cmd/coldcat --db /tmp/opencode/demo.sqlite import --label archive --captured-at 2026-10-01T12:00:00Z /path/to/inventory.cshd
go run ./cmd/coldcat --db /tmp/opencode/demo.sqlite serve --listen 127.0.0.1:8080
```

Supply your actual CSHD inventory and its inventory capture time, not an arbitrary current
timestamp. `--use-source-mtime` is an explicit alternative to `--captured-at`. Each inventory
must describe the complete disk. A missing file in a newer inventory is no longer a current
location; previous observations remain available under history scope.

The server exclusively owns the catalog until stopped. Stop it before importing, running
catalog CLI commands, or opening that catalog in another process. Startup migrates and
recovers abandoned imports before readiness. A competing owner fails explicitly; do not
delete the lock file to bypass ownership. SIGINT/SIGTERM gracefully stop the server.

## Primary read workflow

```sh
curl --fail-with-body http://127.0.0.1:8080/healthz
curl --fail-with-body --get http://127.0.0.1:8080/api/v1/search --data-urlencode 'q=REPORT' --data-urlencode 'limit=20'
```

Use a search item's `content.id` in `/api/v1/contents/{id}`, then request
`/api/v1/contents/{id}/observations?scope=current&limit=50` for known locations. The search
item's `observation.id` links to `/api/v1/observations/{id}`; its snapshot ID links to
`/api/v1/snapshots/{id}`. Use returned IDs rather than assuming sequential values.

Search defaults to case-insensitive basename substring matching and current scope.
Substring input needs at least three Unicode code points. Use `match=exact` for shorter
names, and `field=path` for a complete disk-relative path. Fuzzy matching is not supported.
Use `scope=history` explicitly for historical observations; do not treat them as verified
current replicas. Current means latest recorded inventory, not live filesystem state.

- IDs, counts and byte totals are decimal **strings**. Keep IDs opaque; use `BigInt` or an
  appropriate decimal representation for arithmetic, not JavaScript `Number` coercion.
- Unknown size/mtime is `null`; known empty content has size `"0"`. Directory known-byte
  subtotals are not exact totals when `size_complete` is false.
- Timestamps are UTC RFC3339, potentially with fractional seconds. Capture time, import
  time and source mtime have different meanings.
- Replica counts are catalog-wide even when membership is filtered to a disk/directory.
  Default redundancy metric is other disks; same-disk copies add locations, not disks.
- Directory root is `""`; paths are case-sensitive and `/`-separated. URL-encode paths,
  searches and glob parameters. Do not interpret them as backend host filesystem paths.

## Pagination and errors

Read `items` and follow `next_cursor` until it is `null`. Keep the same endpoint/resource,
scope, filters and limit; append the opaque cursor using a URL builder. Never construct or
decode cursors as an application contract. They belong to the issuing catalog.

Successful offline imports invalidate cursors with HTTP 409 / `stale_cursor`: restart the
traversal. Unchanged restarts and failed-import cleanup preserve cursors. Disk metadata edits
do not change the inventory revision, and disk listing is not a frozen metadata snapshot.

Errors use `{"error":{"code":"...","message":"..."}}`. Branch on code/status, not message
text. Invalid parameters return 400, missing resources 404, unsupported methods 405 with
`Allow`, and unavailable catalog/readiness 503. Disk writes additionally enforce conflicts,
JSON content type and a 65,536-byte body limit. Unknown query parameters and repeated scalar
parameters are rejected; directory comparison `allow`/`block` patterns are repeatable.
An empty list is not a missing resource. See OpenAPI for endpoint-specific distinctions.

## Browser origin and deployment constraints

The backend serves JSON only: it does not serve static assets, expose OpenAPI as an HTTP
route, or enable CORS. Keep loopback binding; it has no authentication and is not an
Internet-facing service.

For a later local browser client, a same-origin development proxy can forward `/api/v1/*`
and `/healthz` to the loopback backend while serving assets itself. Same-origin includes
scheme, hostname and port: another localhost port is a different origin. Without a proxy,
cross-origin browser access requires a separately implemented, explicit CORS configuration
with approved origins/methods/headers. There is currently no CORS CLI option, and browser
security must not be disabled as a workaround. No frontend or proxy is implemented here.

## Contract coverage and remaining gate

`TestOpenAPIEndpointHandoff` checks all 19 documented operations against actual HTTP
payloads and checks common 400/405 behavior. `TestOpenAPISuccessExamples` requires success
examples; `TestOpenAPIReferencesAndExamples` validates their shapes and local references.
Existing specialized acceptance tests retain the semantic and pagination coverage:

| Area | Tests in `internal/httpapi` |
| --- | --- |
| Search → content → locations and observation/snapshot detail | `TestSearchHTTPWorkflow`, `TestContentWorkflow` |
| Catalog statistics and safe integer encoding | `TestCatalogHTTPContract`, `TestCatalogDecimalEncoding`, `TestWireIntegers` |
| Disk creation/editing, lists and snapshots | `TestHTTPDiskManagement`, `TestHTTPSnapshotListing` |
| Content membership/redundancy filters | `TestContentListHTTPContract` |
| Directory browsing, comparisons and coverage | `TestDirectoryHTTPWorkflowAndPagination`, `TestDirectoryComparisonHTTPContract` |
| Cursor binding, restart/import/recovery behavior | `TestDirectoryCursorRequestBinding`, `TestHTTPPaginationAfterRestartAndImports`, `TestHTTPIncompleteVisibilityRecoveryAndCancellation`, `TestDuplicateImportPreservesCursorAcrossReopen` |

The contract/integration handoff slice is **complete**, but the final backend gate remains
**open**. Completed warm evidence includes the three-disk
[current HTTP workflow](backend-foundation.md#warm-http-workflow-measurement-harness),
[five-snapshot HTTP workflow](backend-foundation.md#warm-history-heavy-http-workflow-harness),
and bounded five-disk [history comparisons](backend-foundation.md#history-heavy-directory-comparisons).
The balanced 3/12-disk, one/five-snapshot
[distribution harness](../internal/httpapi/distribution_workflow_benchmark_test.go) and its
small-fixture correctness audit are committed; the final 200,000-current-observation matrix
was measured at 20 iterations after the search optimizations (up to one million historical
observations). Larger bounded directory history also covers 200,000 source files and
3,050,005 historical observations across five disks / 25 snapshots. Consult canonical results and
remaining acceptance work in [backend-foundation.md](backend-foundation.md) and the
[active acceptance sequence](coldcat-backend-api.md#active-acceptance-sequence), rather than
treating every history-heavy measurement as unfinished. Warm serial means are not p95.

Additional completed slices are distinct from acceptance:

- Cursor-seek and reusable-stream directory optimizations preserve exact filtered verification;
  warm million-source filtered replicas now take about 11.5 seconds, not the historical
  7.7-minute baseline. Cumulative allocation is about 2.10 GB/call, not peak memory.
- Exact search/anchor validation and query-local current snapshot reuse are implemented.
  Final million-current warm HTTP primary workflow is 1.365 ms; broad pages are 1393/1435 ms.
  Impossible disk bounds return in 0.076 ms, but valid no-match workloads are not generally cheap.
- Linux cold harness verifies zero resident catalog pages before each sample. Final million
  catalog-page-cache-cold **open plus HTTP query** means are about 4.53 seconds for exact
  and 8.28/8.29 seconds for broad first/next pages. Catalog opening/recovery is timed;
  runtime/client/server remain warm. This is not standalone cold-query latency, hardware or
  whole-filesystem coldness, broader history-heavy cold evidence, or p95.
- Flat full-index imports have transaction API boundary timing, sampled journal growth and
  isolated Linux child RSS evidence. Million-file child maxima are 55,377,920 / 59,883,520
  bytes for success / late failure. The lifecycle includes runtime and catalog initialization
  through close, not import-only memory or universal bounds. The read-only inspection URI
  fix is tested; existing numeric samples predate it, rather than constituting a rerun.
- Deep/shared/history-enrichment import harnesses and all six 50,000-input cases are measured;
  publication rollback is tested separately, not timed.

Remaining gates are explicit: million-input operational distribution runs were rejected twice
by tool-level permission despite user authorization and sufficient reported disk space, so no
results exist. Broader import/distribution-specific RSS, transaction and journal evidence,
publication-failure timing, million-source directory history, broader cold evidence and p95
remain open. **True physical peak journal storage is blocked on a suitable filesystem and
validated allocation/free/reservation instrumentation**; logical VFS growth or faster apparent
file-size polling is not an approved replacement. Sampled heap/journal and transaction API
durations do not prove physical storage bounds. Propose an ADR for measurement semantics
before accepting a changed accounting boundary; none is created or approved here.

Observation/distinct-path limits and latency/storage/import/recovery budgets still require
explicit user approval. Broad search, valid replica-filter no-match and large filtered directory
comparisons retain slow cases to improve or explicitly accept. The final `go test ./...`,
`go test -race ./...` and `go vet ./...` checks after the latest implementation are pending
with the parent session; this documentation does not claim they passed. No frontend work or
final backend acceptance is authorized by completing these measurement slices.
