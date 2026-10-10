# Coldcat frontend

Shell, route state, request/page lifecycle, and wire/domain helpers are implemented; no catalog
feature UI or Go asset hosting yet. Backend acceptance remains
open. Architecture: [ADR 0002](../doc/adr/0002-static-browser-frontend.md).

## Toolchain and commands

Use Node **24.21.0** and npm **11.19.0** (enforced by `.npmrc`/engines). From `frontend/`:

```sh
npm ci
npm run check
npm run lint
npm run test:unit
npm run api:check
npm run test:api-generation
npm run test:proxy
npm run build
npx playwright install chromium
npm run test:e2e
```

Linux also needs Playwright's Chromium system libraries. For an image without them, provision
them with the authorized
`npx playwright install-deps chromium` using appropriate system privileges. Installing the
browser alone does not supply system libraries; a launch failure is not a browser pass.
Chromium is the bootstrap test target, not a finalized supported-browser policy.

`test:unit` runs Vitest/jsdom unit/component tests. `test:proxy` owns a disposable loopback
HTTP fixture and Vite process in-process, verifies real forwarding and connection-refusal
502, and shuts both down. The expected ECONNREFUSED log is evidence of the tested outage.
`test:e2e` builds first and owns a test-only static HTTP host on `127.0.0.1:4173` with no
reused server. Its health responses are intercepted or unavailable; it does not start Go,
open a catalog, or prove real-backend workflows (F11a). Browser fixtures never touch the
workspace catalog.

## Wire client and domain helpers

`npm run api:generate` regenerates `src/lib/api/generated/{wire,contract}.ts` from the
local `../doc/openapi.json`. Commit both outputs with contract changes. `npm run api:check`
regenerates in memory and fails on drift without writing. `test:api-generation` checks
determinism, source/output drift, read-only failure and unsupported schema assertions in
owned temporary directories; it does not modify the real contract or generated files.

The dependency-free generator supports this contract's schema subset, not general OpenAPI.
Unknown assertion keywords, reference siblings and unsupported operations fail explicitly.
The generated descriptors power runtime shape validation, avoiding handwritten DTO schemas.
`openapi-typescript@7.13.0` was not installed: its TypeScript 5 peer conflicts with the pinned
TypeScript 6 toolchain. No dependency/lockfile change or forced peer override was needed.

- `src/lib/api/client.ts`: `createClient(fetcher?)` returns explicit wrappers for all 19
  operations. Exported `Client`, `Query<operation>` and `Result<operation>` types use generated
  `operations`; generated `components['schemas'][name]` supplies wire DTOs. Reads accept an
  optional `AbortSignal` (after the query, where applicable); writes also accept one.
- Calls use same-origin relative URLs only, declared query keys, literal empty roots and
  repeated allow/block parameters. Unknown keys fail locally. Glob byte/count limits are
  checked without interpreting patterns; backend query-combination validation stays backend-owned.
- `ApiError.kind`: `request`, `cancelled`, `transport`, `contract`, or `http`. Structured
  HTTP errors retain `status`, `code`, and literal `message`; branch on status/code. Non-JSON
  502/504 proxy errors are transport failures with status; unexpected HTML/JSON/status is a
  contract error. Raw bodies are not error messages. `cause` retains diagnostic context.
  `uncertainWrite` marks post-dispatch transport/cancellation/malformed write responses:
  reconcile by reloading before resubmitting. The client never retries or supplies mock fallbacks.
- Runtime checks enforce required shapes, nullability, enums, decimal strings/range, UTC dates,
  page items/cursor presence and directory-entry kind consistency. Returned DTOs remain JSON
  serializable and unchanged. This is not full domain-semantic or cross-resource validation.
- `buildDiskPatch(before, draft)` emits only changed metadata, canonicalizes capacity and
  treats null/empty optional text as equivalent clearing. An unchanged draft returns `{}`;
  do not submit it (the wrapper rejects empty PATCH). Changed text is never trimmed.
- `src/lib/format/`: exact BigInt arithmetic/locale counts and byte text (`Unknown` vs `0 B`),
  signed-64-bit capacity validation, safe percentages (null for an empty denominator),
  zero-safe **content-level** other counts, labeled dates retaining exact UTC fractional
  seconds, slash-only breadcrumbs/containing directories and encoded directory URLs.
  `otherCount` is not observation-specific replica subtraction.
- `tests/fixtures/api.ts` exports fresh generated-type disk/search fixtures. OpenAPI examples
  are separately tested endpoint shapes, not a linked catalog or real-backend workflow.

## Shell and feature contracts

`lib/state/` contains bounded lifecycle helpers, not a query cache or data framework.
There is no response/cursor persistence, automatic feature fetching, speculative pagination,
global event bus, or automatic write retry. Feature components own their local form drafts.

### Connection ownership

The shell provides `useConnection()` through Svelte context. It exposes `client`, `state`,
`subscribe(listener)`, `onInvalidate(listener)`, `epoch`, `check()`, `retry()`, `report(error)`
and `metadataChanged()`. Subscriptions immediately receive current state and return cleanup.
Readiness requires validated health **and** catalog responses. Transport/503 failures trigger
one shell-owned bounded schedule (1, 3, then 10 seconds); after exhaustion, retry is manual.
Only readiness/context is retried, never a failed feature operation or disk write.

Invalidation reasons are `disconnect`, `reconnect`, `revision`, and `metadata`. Reconnection
always clears retained client data, even with the same revision: revision is not catalog
identity. `metadataChanged()` must be called after disk writes/reconciliation because labels
and notes do not advance inventory revision. Navigation does not discard form drafts.
`epoch` is a process-local invalidation token, not a backend/catalog version.

### Requests and pages

`RequestSlot<T>` exposes `state`, `subscribe`, `run(signal => promise)`, `cancel`, and
`dispose`. States are idle/loading/success/failure. Replacement cancels immediately, hides
the previous result, and uses a generation to reject late success **and** failure.

Named adapters in `feature-requests.ts` supply concrete endpoint contracts:

| Feature                  | Page adapter                                                                 | Independent detail adapters                      |
| ------------------------ | ---------------------------------------------------------------------------- | ------------------------------------------------ |
| Search (F3)              | `searchPages(connection, SearchRoute)`                                       | none                                             |
| Content/observation (F4) | `locationPages(connection, contentID, LocationRoute?)`                       | `contentDetail`, `observationDetail`             |
| Disks/snapshots (F5)     | `diskPages(connection, limit?)`, `snapshotPages(connection, diskID, limit?)` | `diskDetail`, `snapshotDetail`                   |
| Directory (F6)           | `directoryPages(connection, snapshotID, DirectoryRoute?)`                    | `directoryDetail(connection, snapshotID, path?)` |

Page adapters return `{ traversal, dispose }`; detail adapters return
`{ request, reload, dispose }`. Construction performs no request. Details are independent,
so a locations failure cannot erase a successful content summary. All page limits default
to 50. Inputs are captured at construction; dispose the old owner **immediately** when any
route/filter/resource changes, then construct the replacement. Never mutate a traversal.
Features decide whether/when to reload after connection invalidation; adapters do not fetch
in the background. F3 owns debounce, active selection, scroll and bounded search restoration.
Subscribe to invalidation to retire those feature-owned restoration/selection values too.

`CursorTraversal` has `restart()`, `more()`, `next()`, `previous()`, `retire()`, `suspend()`,
`dispose()`, `subscribe`, `items`, `busy`, `canMore`, `canNext`, and `canPrevious`.
Its states are idle/loading/success/loading-more/failure/stale. `state.pages` stores complete
wire pages with one-based page numbers; `state.active` and `state.paged` describe presentation.
Traversal identity includes endpoint, resource, encoded filters/scope and limit, not cursor.

`more()` accumulates up to 10 pages. At that bound use explicit `next()` navigation:
only the active page is displayed, the oldest retained page is evicted, and later results
remain reachable. `previous()`/`next()` restore retained pages without fetching; older evicted
pages require an explicit first-page restart. `items.length` is a displayed/loaded count,
never a catalog total. Retention bounds page objects/cursors, not the entire catalog.

Page errors preserve successful pages and allow retry of the same action. Disconnect cancels
pending work, marks retained pages unverified and blocks retained cursor reuse; cached
navigation stays visibly unverified. Reconnect clears retained pages. Revision mismatch or
409 `stale_cursor` clears all pages and exposes `stale`; `restart()` explicitly starts at page
one with the captured inputs. Revision invalidation also exposes stale; metadata/reconnect
retirement exposes idle. Dispose adapters on component destruction to release subscriptions.

`RequestFeedback.svelte` accepts status/error/resource/retry/loadingLabel, announces extended
loading after one second, and separates parameter, missing-resource, stale and unavailable
errors. It never offers blind retry for uncertain writes. `PageControls.svelte` takes the
traversal flags, `paged`, active `page`, `firstRetained`, displayed `loaded` count,
`complete` (last retained `next_cursor === null`), and
more/next/previous callbacks. Render it only for a traversal with successful retained pages;
empty-success messaging belongs to the feature, not the shell.

### Route and keyboard boundaries

`routes.ts` exports `routes` for ordinary content/observation/disk/snapshot links,
`searchURL`, `directoryBrowseURL`, `contentURL`, and `comparisonURL` (repeated literal rules).
Use native anchors, not row-only click handlers. Root directory is `path=''`; IDs stay
opaque decimal strings and directory paths remain query values. Do not append cursors to URLs.
The layout resolves implemented links through SvelteKit. Add Contents/Disks navigation when
their feature routes exist; the shell does not register placeholder pages for future features.

- F3: `parseSearch(URLSearchParams)` → `SearchRoute`, including explicit defaults and literal
  empty query. F3 validates scheduling eligibility (Unicode length, UTF-8 limits, combinations);
  an empty/short route is valid navigation state, not permission to dispatch an API request.
- F4: `parseContentRoute` → `{ context, locations }`; `contentURL(id, locations, context)`
  preserves independent history and search-return state. `parseDetailContext` handles
  observation routes. `DetailContext` has `returnTo` and selected `observation`; invalid returns
  fail explicitly. `safeSearchReturn` accepts only local search URLs, with no fragments,
  nested returns or cursors. It returns undefined for invalid destinations.
- F5: `parsePageLimit` for list/history URLs; IDs come from route parameters.
- F6: `parseDirectory` → `DirectoryRoute`. Comparison tab/rule UI remains F9-owned.

Parsers reject unknown/repeated scalar parameters, invalid enum/boolean/integer/ID values,
and bookmarked cursors. Parsing does not silently trim or normalize text, rewrite filters,
or duplicate backend query-combination validation. Report parser errors with submitted inputs.
F3 owns replacement navigation while typing and history entries for discrete submissions.

`searchKeyAction(event, options)` returns `focus`, `type`, or null. Options explicitly include
workspace/desktop/typeToSearch/selection/dialog. It ignores handled events, composition/dead
keys, modifier shortcuts, editable ancestors (including shadow-event paths) and interactive
controls. F3 supplies live selection/dialog/media-query state and wires the action to its
input; no global printable handler or preference persistence is installed by the shell.

## Development

Start the backend separately with a disposable catalog, following
[backend integration](../doc/backend-integration.md). Stop it before CLI catalog operations;
never remove a lock file. Then:

```sh
npm run dev
```

Vite binds loopback, normally port 5173, and fails if its chosen port is occupied. Only
`/api/v1` paths and exact `/healthz` are proxied, preserving path/query. To select another
backend loopback origin:

```sh
COLDCAT_BACKEND=http://127.0.0.1:8081 npm run dev
```

Non-loopback targets, credentials, query/fragment, and non-root target paths fail configuration.
The shell checks health and catalog on mount, owns bounded connection rechecks, and offers
manual retry. It never supplies cached/demo responses as proof of backend readiness.

## Built route and hosting contract (F10 handoff)

- `npm run build` writes `build/index.html` and `build/_app/` assets. Serve at origin root;
  a subpath deployment is not configured. No runtime Node service is required.
- Client rendering is global; no prerendered resource pages or server endpoints.
- The host must dispatch `/api/v1/*` and `/healthz` to the backend first, never to HTML.
- Serve existing static files with correct MIME types. Missing assets, including `_app/*`,
  must be real 404s, not HTML 200. Traversal/encoded paths and HEAD require F10 tests.
- Application navigation routes from the approved plan are `/`, `/contents`,
  `/contents/[id]`, `/observations/[id]`, `/disks`, `/disks/[id]`, `/snapshots/[id]`, and
  `/snapshots/[id]/directory`. Return `index.html` for their direct document requests;
  query strings do not change asset dispatch. Arbitrary directory paths stay in `?path=`.
- Only `/` is implemented in F0. A built nested navigation loads the shell and displays
  the client “Page not found” boundary until that feature route exists. Unknown routes
  are not approved production fallback destinations.
- `tests/serve-built.ts` is a test-only reference host with broader document fallback to
  exercise future nested URLs. Do not ship it or copy it as the production security handler.

Manual screen-reader/keyboard audits and browser support/measurement thresholds remain
F11b/F12 work. No backend benchmark or full Go race check is needed for pure frontend tasks.
