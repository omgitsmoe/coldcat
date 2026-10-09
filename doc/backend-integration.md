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
small-fixture correctness audit are committed; the 50,000-current-observation matrix was
measured, while larger-scale measurements remain open. Consult the canonical results and
remaining acceptance work in [backend-foundation.md](backend-foundation.md) and the
[active acceptance sequence](coldcat-backend-api.md#active-acceptance-sequence), rather than
treating every history-heavy measurement as unfinished. Warm serial means are not p95.

Remaining gates include filesystem-cold acceptance evidence, broader importer and disk/history
distributions, larger history comparisons, true peak RSS/journal storage, and explicit approval
of observation/distinct-path limits and latency/storage/import/recovery budgets. Sampled heap
and journal growth and transaction API durations do not establish those peak bounds. Broad
search, replica-filter no-match and large filtered directory comparisons have documented slow
cases that must be improved or explicitly accepted; this audit does neither. Rerun required
`go test ./...`, `go test -race ./...`, and `go vet ./...` after implementation changes and
record their results before closing the gate. In-progress cold, RSS and optimization work is
not accepted by this handoff; follow the active acceptance sequence before frontend work.
