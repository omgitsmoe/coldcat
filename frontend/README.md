# Coldcat frontend

The primary search workspace, content/location and observation details, hash lookup, read-only
disk/inventory pages, directory browsing/sizing, shell, request/page lifecycle, and wire/domain helpers are implemented.
Disk writes, comparisons and Go asset hosting remain separate packages. Backend acceptance
remains open.
Architecture: [ADR 0002](../doc/adr/0002-static-browser-frontend.md).

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
  fail explicitly. `safeSearchReturn` accepts only local search URLs. `safeDetailReturn` also
  accepts validated snapshot-directory URLs, preserving literal paths and filters. Neither
  permits fragments, nested returns or cursors; invalid destinations return undefined.
- F5: `parsePageLimit` for the disk list, `parseInventoryRoute` for disk detail/history
  limit plus validated detail context, and `parseDetailContext` for snapshot detail;
  IDs come from route parameters.
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

### Search workspace and F4/F6 links

`lib/features/search/` owns literal query validation, 200 ms scheduling, selection, keyboard
interaction and one retained in-memory search session (at most ten pages; 50 items by default). Filter drafts
retire results immediately and dispatch only on Apply. Typing uses replacement navigation;
discrete filter submissions create history. Only the type-to-search preference is persisted
(`coldcat.typeToSearch`); responses, cursors and selection are never stored in browser storage.
Connection invalidation clears selection/scroll and retires or marks pages unverified via F2.

- Content links use `contentURL(id, { scope: 'current' }, { returnTo, observation })`.
  Search history does **not** silently enable historical locations on content detail.
- Observation links use `routes.observation(id, { returnTo, observation })`.
- Directory links use `directoryBrowseURL(snapshotID, { path: containingDirectory(path) })`;
  root stays the literal empty path. F6 does not receive detail-context parameters.
- `return_to` is the ordinary search URL with query/filters and no cursor. F4 parses it with
  `parseContentRoute` / `parseDetailContext`; render a native return anchor using the validated
  destination. The selected observation is independent of content's location scope.
- The retained session is reused only for the same connection and search inputs, with successful
  pages. Otherwise search refetches page one. An application/document reload always refetches.
  F4 browser coverage now exercises search → content → observation → return within the same
  application, retained selection/scroll, ordinary Back, and first-page refetch after reload.
  F11a still owns the real-catalog workflow proof.

Mocked browser coverage includes delayed first pages and continuations, Unicode/IME scheduling,
keyboard guards/preference, invalid filters, failures/stale cursors, URL history, first-page
reconstruction and later rows past the retention bound. These tests do not establish backend
performance or real-catalog workflow acceptance. Manual screen-reader/input-method audits remain
F11b work.

### Content and observation details (F4 handoff)

- `/contents/[id]` uses the F2 `contentDetail` and `locationPages` adapters independently.
  Initial navigation issues exactly two feature requests: summary with `scope=history` to
  expose all-complete-inventory counts, and locations with `scope=current` by default.
  Current counts remain explicitly separate from historical distinct disks/disk-paths and
  repeated observations. No per-location requests are issued.
- Location scope/limit and validated `return_to`/`observation` follow the existing route
  builders/parsers. Scope/resource/query changes destroy the old owners immediately. History
  pagination uses F2's ten-page retention and explicit later-page navigation, stale-cursor
  reset and independent retry. A disconnected continuation retry restarts page one rather
  than reusing suspended cursors. Selection is still linked even when absent from the loaded
  page/current scope; it does not trigger an observation fetch.
- `/observations/[id]` fetches only its embedded-context DTO and uses its supplied other-current
  counts (not content-level subtraction). Content links preserve search context; disk and
  inventory links target F5, and containing-directory links target F6 with snapshot plus
  literal path, including `path=` for root. The DTO has no `is_current` flag; the observation
  page does not infer one from dates or invent a historical/current badge.
- Successful detail sections survive an independent failure. On disconnect, retained identity
  is explicitly unverified; reconnect/revision/metadata invalidation clears it. Feature retries
  are manual, not shell readiness retries. Plain traversal-class getters are projected into
  reactive paging flags in the subscription, so controls update after every page transition.
- `/contents` currently hosts `HashLookup.svelte`, not the F8 content explorer. F8 should compose
  this form with its list. `lookupInput(algorithm, digest)` and `algorithms` derive validation
  from generated descriptors, preserve input case, and search history explicitly. Supported
  digest lengths remain server-owned because OpenAPI specifies only hex byte pairs. The form
  separates known, valid 404, malformed/error and obsolete-request states; a known result has
  an ordinary content link, defaulting its locations to current.
- Shared `CopyButton.svelte` takes `text`/`label` and reports rejected clipboard writes;
  `DateValue.svelte` takes `value`/`meaning`, labels source mtime versus capture/import time,
  preserves null and exposes exact UTC fractional seconds. F5/F6 can reuse them. Decimal
  presentation continues to use F1's BigInt-safe helpers.

F4 checks cover independent failures (including disconnect/reconnect), huge decimals,
zero/unknown values, date labels, selected context, safe return/path links, copy success/failure,
explicit hash identity and stale lookup responses, current/history traversal and bounded later
pages. These are mocked-browser tests; no Go checks, catalog access or real-backend acceptance
is implied. Optional manual accessibility/responsive audits remain F11b.

### Read-only disks and inventories (F5 handoff)

- `/disks` uses `diskPages`, displaying embedded metadata/latest complete capture without
  per-row requests. `/disks/[id]` uses independent `diskDetail` and `snapshotPages` owners;
  history follows server capture-time order, not import dates. `limit` is URL state; cursors
  are not. `InventoryPages.svelte` projects reactive paging flags and retains F2's ten-page
  bound with explicit later-page navigation, stale reset, and manual retry.
- Unknown-size files make the cataloged total **Unknown** while keeping the exact known-byte
  subtotal and unknown-file count visible. Repeated contents count per path. No inventory
  (`cataloged=null`) differs from a complete empty inventory (`0 B`, complete). Declared
  capacity is metadata, never measured usage or a free-space calculation. Empty states explain
  CLI-only imports and stopping the server before CLI catalog operations.
- `/snapshots/[id]` fetches only `snapshotDetail`; `InventorySummary.svelte` renders capture/import
  dates, explicit versus source-inventory-mtime provenance, input format/path and exact counters.
  Input paths are literal backend provenance, not browser file links. The snapshot DTO has no
  current flag: only disk history with freshly verified latest metadata labels latest/historical.
  Disconnect marks retained data unverified; reconnect/revision/metadata clears owners' data.
- `routes.disk(id, context?)` and `routes.snapshot(id, context?)` now accept the established
  validated search return/selected-observation context. F4 observation/location links carry it
  into F5; disk/inventory navigation preserves it. Existing one-argument calls remain valid.
- **F6:** root actions use `directoryBrowseURL(snapshotID, { path: '' })`, yielding literal
  `?path=` with no detail context. These entry links are browsable through F6.
- **F7:** compose write controls with `DiskMetadata`/`DiskDetail`; call
  `connection.metadataChanged()` after writes/reconciliation and manually refetch metadata.
  F5 installs no create/edit forms, write requests, or metadata-version assumptions.

Browser coverage is mocked (pagination including later retained-page transitions, latest/history,
empty inventories, unknown/zero/large totals, literal markup, date meanings, return validation,
stale cursors and disconnect/reconnect). Real-catalog integration remains F11a; the backend gate
is still unresolved.

### Directory browsing and sizing (F6 handoff)

- `/snapshots/[id]/directory` composes `features/directories/Browse.svelte` with independent
  `directoryDetail` and `directoryPages` owners. Initial navigation makes two feature requests;
  no disk/detail per-row requests, directories-only tree, or comparisons are preloaded.
- Breadcrumbs split only literal `/`; root includes the embedded snapshot's disk ID. Case,
  Unicode composition and backslashes are preserved. Child-directory links retain entry filters.
  Immediate directories remain navigation entries under replica bounds; recursive mode asks
  the server for files only. Membership and ordering remain backend-owned.
- Summary sizes cover all descendants independent of entry filters: per-path recursive bytes
  versus once-per-identity unique bytes, known subtotals, unknown counts and completeness.
  Redundancy is a textual per-file-occurrence histogram against current other disks.
  Source current/history badges come from the DTO, not dates; historical sources still use
  current destination replicas. File counts use supplied observation-specific other counts.
- Filters use explicit Apply, exact or range bounds, disks/locations and page limit. Editing
  immediately retires entries; it does not change the unfiltered summary. Paging retains F2's
  bound, manual retry and stale reset. Owners dispose on route changes and destruction.
- Content/observation anchors carry `return_to` with the directory URL (no cursor), plus the
  selected observation. F4/F5 detail pages validate it and label it “Return to directory”.
  `safeSearchReturn` remains search-only, so F3 restoration is not broadened. Direct directory
  entry links from F3–F5 keep their established snapshot/literal-path-only contract.
- Browser evidence is mocked, not proof of real catalog integration. Next is **F11a core**
  before F7/comparisons: own a disposable catalog, offline imports, backend/proxy child
  processes and ports, readiness and cleanup; exercise search → content → observation →
  directory, literal/root/history paths, real membership boundaries and directory rows under
  replica filters. Stop the server before import/revision scenarios. Do not touch the workspace
  catalog or benchmark directories. A separate real-backend command/config is still needed;
  `test:e2e` continues to own the mocked built-static host only.

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
- `/`, `/contents`, `/contents/[id]`, `/observations/[id]`, `/disks`, `/disks/[id]`, and
  `/snapshots/[id]`, and `/snapshots/[id]/directory` are implemented through F6.
  Unknown routes display the client “Page not found” boundary and are not approved
  production fallback destinations.
- `tests/serve-built.ts` is a test-only reference host with broader document fallback to
  exercise future nested URLs. Do not ship it or copy it as the production security handler.

Manual screen-reader/keyboard audits and browser support/measurement thresholds remain
F11b/F12 work. No backend benchmark or full Go race check is needed for pure frontend tasks.
