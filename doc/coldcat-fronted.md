# Coldcat frontend plan

Status: F0–F12 complete within the approved scope, including the full deployed-Go real-browser
operation matrix, revision/reconnection/write reconciliation and approved 20-run UI responsiveness
checks and final integration/release handoff. Accessibility is best effort by owner decision;
no manual AT acceptance gate. Backend gate unresolved. Frontend completion is not final
backend or combined release acceptance closure.
The first-usable-slice real-backend harness precedes F7/comparisons; see the
[core integration interface and coverage](../frontend/README.md#real-backend-core-integration-f11a-core).

## Implementation approvals

On 2026-10-10 the user authorized frontend development in parallel with the unresolved
backend gate, approved the full F0–F12 initial scope, npm, SvelteKit static SPA, same-origin
Go hosting with an explicit asset directory, and desktop-only disableable type-to-search
enabled by default. Creating the architecture ADR is explicitly approved. These approvals
do not close or waive the backend gate.

The owner's subsequent F11b decision on 2026-10-10 approved the documented Chromium/container
measurement profile, input <50 ms, idle scheduling 200±50 ms, first/single 50-row render proxy
≤50 ms and bounded accumulation ≤100 ms, with 20 fresh-context runs reporting max/p95.
Initial browser support is Chromium desktop only; narrow-viewport tests are not mobile support.
Accessibility is best effort (“don't test accessibility ... no extended checks”): no additional
accessibility/screen-reader audit or manual AT acceptance requirement. Historical checks are
evidence, not screen-reader success. This scope decision supersedes earlier manual-audit wording;
it neither establishes hardware-general guarantees nor approves backend performance.

[ADR 0002](adr/0002-static-browser-frontend.md) records the authorized architecture.
[Frontend README](../frontend/README.md) defines setup, hosting and shared feature interfaces.
Detailed execution logs and transient blockers belong in sessions/PRs; the final release
handoff below records reproducible commands, check scope and durable limitations.

## 1. Goal, scope, and release boundaries

Build a functional, keyboard-friendly browser frontend for the existing Coldcat HTTP API.
The audience is tech enthusiasts and developers. Prioritize fast interaction, useful tables,
explicit domain semantics, and reliable navigation over animation or dashboard decoration.

The primary workflow is:

1. Start typing a remembered filename or disk-relative path.
2. Scan matching observations without navigating through a dashboard first.
3. Open a result and see its content identity and known locations.
4. Inspect its disk, inventory, dates, and other copies.
5. Continue into the containing directory or historical observations when needed.

### Delivery boundaries

- **First usable slice:** search, content and location details, observation details, basic
  disk/inventory navigation, and directory browsing. Deliver this before comparison tools.
- **Full initial frontend:** disk creation/editing, snapshot history, content/redundancy
  listing, directory exact replicas, and directory content coverage.
- **Deferred:** UI imports, snapshot diffs, thumbnails, extraction, jobs, Wails, host file
  opening, downloads, authentication, multi-catalog switching, and Internet deployment.

The explicit implementation approval recorded above supersedes the older backend document's
frontend prohibition. Development proceeds in parallel with the unresolved backend gate;
do not silently mark backend performance tasks complete.

Canonical references:

- [OpenAPI](openapi.json): exact routes, DTOs, parameters, errors, limits, and examples.
- [Backend integration](backend-integration.md): startup, ownership, and browser-origin setup.
- [Backend plan](coldcat-backend-api.md): domain semantics and remaining acceptance decisions.
- [Foundation](backend-foundation.md): implementation and measured performance limitations.
- [ADR 0001](adr/0001-search-without-fuzzy.md): exact/substring search, no fuzzy matching.

If documents conflict, inspect the implemented contract and ask for resolution rather than
inventing a frontend workaround. Physical-journal tracing is scrapped by owner decision;
retained historical instrumentation is not an active frontend or backend release gate.

## 2. Proposed architecture

### Stack

- Current stable Svelte and SvelteKit, with TypeScript in strict mode.
- SvelteKit `adapter-static`, used as a client-rendered application; no SSR application server.
- Vite same-origin development proxy for `/api/v1/*` and `/healthz`.
- OpenAPI-generated TypeScript wire types; a small explicit API client around `fetch`.
- Vitest, Svelte Testing Library, and Playwright.
- Plain CSS with design tokens and small shared components. Add accessible headless widgets
  only when native HTML cannot reasonably meet a requirement.
- One package manager, pinned dependency versions, and one committed lockfile. Recommend npm
  unless the project owner prefers another tool before scaffolding.

SvelteKit adds useful routing, layouts, and navigation conventions to this multi-page UI.
SSR and a persistent Node service would add deployment complexity without improving this
local, private catalog workflow. Do not duplicate backend validation/query logic in Kit
server routes or access SQLite from the frontend.

### Local development and distribution

Development: the browser accesses the frontend dev server; its fixed development proxy
forwards only backend API/readiness paths to a configured loopback address. No CORS change
is needed. A missing backend is a visible error, never a switch to demo responses.

Recommended distribution: build static assets and have the Go server serve them on the same
origin as its API. This is a **new, bounded backend integration task**, not an existing feature.
Initially prefer an explicit asset-directory option to embedding generated files in Go.
It keeps frontend builds independent of ordinary Go tests/builds. Embedded distribution can
be considered separately when packaging requirements justify it.

The asset-serving task must reserve `/api/v1/*` and `/healthz` for the backend, serve valid
SPA deep links through the application shell, and return real 404s for missing assets.
API failures must never become an HTML success response. Test encoded paths, traversal,
HEAD requests, and API/static route precedence. Keep loopback defaults; the backend has
no authentication and must not be presented as safe for public exposure.

Before scaffolding, record approval of the stack and distribution choice. Propose an ADR
covering the decision, local-browser context, bare Svelte/SSR/separate-server alternatives,
and build/deployment consequences. Do not create an ADR automatically.

### Proposed layout

```text
frontend/
  package.json
  src/
    routes/                   # Thin route composition and route-state binding
    lib/
      api/                    # Generated wire types, client, endpoint wrappers
      state/                  # Request lifecycle, cursor traversal, route parsing
      format/                 # Decimal bytes/counts, dates, literal paths
      components/             # Small shared presentation/accessibility primitives
      features/
        search/
        contents/
        observations/
        disks/
        snapshots/
        directories/
  tests/                      # Fixtures, browser tests, real-backend harness
```

Keep feature-specific components beside their feature. Avoid a universal table framework,
plugin architecture, global event bus, or a client-side copy of the whole catalog. Introduce
abstractions after two real callers establish the same need, not before.

## 3. Information architecture and presentation

### Routes

| UI route | Purpose |
| --- | --- |
| `/` | Search workspace; default landing page |
| `/contents` | Distinct-content list, membership and redundancy filters |
| `/contents/[id]` | Content identity, current locations, optional history |
| `/observations/[id]` | Selected file observation and inventory context |
| `/disks` | Paginated disks and create-disk action |
| `/disks/[id]` | Disk metadata, latest inventory, paginated inventory history |
| `/snapshots/[id]` | Inventory provenance and directory-root entry point |
| `/snapshots/[id]/directory?path=...` | Directory summary, entries, comparison tabs |

Put arbitrary disk-relative directory paths in query parameters, not URL path segments.
Root is the empty string. Use a URL builder for every query, including repeated glob rules.
IDs stay opaque decimal strings.

On desktop, use compact navigation, a prominent search field, and a dense result table.
On narrow screens, use a readable stacked result layout and full-page details. Do not make
essential fields accessible only through hover. Provide visible keyboard focus, useful
contrast, selectable/copyable paths and hashes, and reduced-motion support.

Prefer native links, buttons, forms, and tables. Do not make a row's click handler the only
way to open an item. Links must support new tabs and normal browser navigation.

### URL state and returning to search

- Search query, scope, field, match, and membership/replica filters belong in the URL.
- Search inputs retain their literal value: whitespace and punctuation are significant.
- Typing updates the URL with replacement, not one history entry per character.
- Discrete filter submissions can create navigation history entries.
- Content/result links retain an internal search-return context. Validate return URLs as
  local application destinations; do not introduce an open redirect.
- Back navigation restores query, filters, selected observation, and scroll when possible.
  Refetch the first page if no valid in-memory traversal survives.
- Loading additional pages does not create a browser-history entry. Cursors are session
  traversal state, not long-lived bookmark identifiers.
- Reloading a search URL reconstructs the first page; it need not reconstruct every page
  previously loaded. Never persist cursors or catalog responses in local storage.

Use an explicit, bounded in-memory search session for restoration. Clear it on application
reload, inventory revision changes, and disk writes that would leave displayed labels stale.
Revision is not a catalog identity or a metadata version.

## 4. Primary search UX

### Entry and keyboard behavior

Search should work without hunting for an input:

- On the desktop search landing page, focus the search field on initial entry when doing so
  will not override restored focus. Do not force a mobile software keyboard to open.
- `/` and `Ctrl/Cmd+K` focus the global search entry when no editable control owns the event.
- On the search workspace, an ordinary printable key typed outside an editable control
  focuses search and inserts that character exactly once. Enable this desktop convenience
  by default, with a preference to disable it and a short explanation in keyboard help.
- Do not globally capture printable typing on disk edit forms or detail pages. Elsewhere,
  users reach search through its visible navigation entry or explicit hotkey.
- Ignore events already handled, composition/dead-key events, editable/contenteditable
  targets, open dialogs, and modifier combinations. Do not hijack browser find, copy, paste,
  text selection, or assistive-technology navigation.
- Arrow keys in the search field move the active result; Enter opens its content detail.
  With no active result, Enter submits the valid query immediately.
- Escape first cancels an active result selection or closes a filter popup; it does not
  unexpectedly erase the query. Provide a visible clear action.
- Tab reaches every control normally. Avoid an ARIA combobox unless its full interaction
  model is implemented; a search form plus table/list is sufficient.

Show shortcut help. The optional type-to-search behavior must have browser tests, including
IME input and typing in forms. Keyboard shortcuts must not be required for any operation.

### Query semantics

Defaults: `field=name`, `match=substring`, `scope=current`, replica metric `disks`.

- Empty query: show a concise prompt and optional current catalog totals; do not list the
  entire catalog or issue an invalid search request.
- One/two Unicode code points in substring mode: show “Type at least 3 characters, or choose
  Exact.” Do not silently switch to exact matching.
- Exact mode: submit any valid nonempty query, including short names.
- Count Unicode code points, not UTF-16 code units. Enforce the backend's UTF-8 byte limit
  and NUL prohibition; preserve whitespace rather than trimming it silently.
- Name/path mode stays explicit. Typing `/` in a query must not silently change modes.
- Historical scope is explicit and visibly marked. Do not automatically broaden a failed
  current search to history; offer the user a deliberate “Search history” action.
- No fuzzy toggle, typo correction, glob query interpretation, or client-side case folding.

Advanced filters: disk, snapshot, directory membership, and other-copy bounds. Render only
valid combinations from OpenAPI. An older-snapshot shortcut explicitly uses history scope.
Explain that replica bounds require current scope where the endpoint enforces that rule;
do not silently remove a filter when switching scope.

### Requests and visible states

Use an initial proposed debounce of 200 ms, adjustable after browser measurements. Enter
flushes the debounce. This is a UI scheduling value, not a claim about backend latency.

Represent states explicitly: idle, short/invalid query, debouncing, loading, success,
empty success, loading another page, and failure.

- Cancel obsolete requests with `AbortController` and also reject late responses using a
  generation/request key. Aborting alone is insufficient for response-order correctness.
- Any query/filter change immediately retires the old traversal and active selection.
  Hide old results or unmistakably mark them stale; do not let Enter open an old result.
- Coalesce typing into the latest desired request. Do not retry canceled requests, spawn
  speculative prefix queries, or launch background requests for every intermediate term.
- Keep the input interactive during slow searches. Show “Searching…” and a non-alarming
  extended-wait message after a proposed one second. Do not declare an empty result early.
- Fetch a proposed 50 items per page and expose a clear “Load more” action. Follow backend
  ordering; do not locally sort or cap candidates while claiming exhaustive results.
- Show the loaded count, not an invented total. `next_cursor=null` means traversal complete.
- Bound retained results to a proposed 10 pages. At the bound, offer explicit next-page
  navigation with limited previous-page restoration rather than silently truncating results
  or fetching all pages. Test the transition and preserve access to later results.
- Pagination errors retain the last successful page and allow retry. Parameter changes
  cancel both first-page and continuation requests.

### Result presentation

Each result is an observation, not a deduplicated content row. Show:

- Basename and full disk-relative path; expose the untruncated path on demand.
- Disk label and inventory capture date.
- Known size or “Unknown”; known zero is “0 B”.
- Current/historical badge from `is_current`.
- Current disk/location counts, clearly labeled; scoped history counts must not look like
  current replicas. Relevance can be a small exact/prefix/substring label.
- Primary content link, separate observation link, and containing-directory action.

Same-name files with different hashes remain separate. Different names for identical
content remain searchable. Repeated historical observations remain distinguishable by
inventory rather than being collapsed into a supposedly current location.

Do not issue per-result detail requests. The search payload already supplies useful list
data. Avoid client-side highlight logic that claims to reproduce backend Unicode folding;
omit highlights initially or use a clearly limited, tested presentation-only technique.

## 5. Detail, browsing, and management workflows

### Content and observations

Content detail fetches the content summary and first location page, independently so one
failure does not erase the successful section. Show algorithm plus full hash, copy action,
size, current disks/locations, scoped counts, and historical observation count.

Default the location list to current inventories. Offer a separate history view with paths,
disks, capture dates, mtimes, and historical/current badges. Never treat repeated snapshots
as extra current copies. Preserve the selected observation context when arriving from search.

Observation detail shows its path, content, disk, inventory, other-location/disk counts,
and all three date meanings: source mtime, captured at, and imported at. Link to the content,
snapshot, disk, and containing directory. Missing mtime is “Unknown”, not import time.

Add a dedicated hash-lookup form to the content area: explicit algorithm and digest fields,
contract-driven validation, known/not-found/error states, and navigation to returned content.
Do not infer content equality from hashes produced by different algorithms.

### Disks and inventories

- Paginated disk list: metadata and latest complete inventory; disks without inventories
  have a real empty state and no fabricated root link.
- Disk detail: metadata, cataloged bytes and completeness, latest capture time, and inventory
  history. Cataloged bytes are not physical usage or free capacity; avoid a “disk full” gauge.
- Creation/editing: label, capacity, serial, notes; exact decimal-byte capacity input with
  range validation, plus optional human-readable preview. Never use a numeric HTML input
  that loses large integer precision.
- PATCH includes only changed fields. Omitted means unchanged; explicit null/empty clearing
  follows the contract. Surface conflicts without discarding the user's entered values.
- After writes, refetch affected disk metadata and invalidate related in-memory displays.
  Do not rely on inventory revision to detect label/notes changes.
- Snapshot detail: capture/import dates, provenance, source input path/format, counts, and
  root browse link. Label source input paths as backend provenance, not browser file links.

Explain CLI-only imports in empty states: the server must be stopped before catalog CLI
operations. Do not add upload buttons, pretend an import is running, or delete lock files.

### Directory browsing

Use the snapshot directory summary and entries endpoints. Display:

- Snapshot/disk context and whether the source inventory is current.
- Breadcrumbs built from literal `/`-separated segments; root labeled with disk context.
- Immediate files/directories by default, with an explicit recursive-files option.
- Recursive file bytes versus unique-content bytes, unknown-size counts, and completeness.
- Current other-disk redundancy histogram, with textual counts rather than chart-only data.
- Paginated entries and current replica bounds. Child directories remain navigation entries
  in immediate listings even when their files do not meet a replica bound.

Never normalize case/Unicode, interpret backslashes as separators, or treat `foo` membership
as including `foobar`. Avoid eagerly loading an entire directory tree. The directories-only
endpoint can support an optional lazy directory navigator; it is not needed just to reproduce
the entries endpoint's existing directory rows.

### Directory comparisons

Separate tabs/actions for **Exact tree copies** and **Content on other disks**. Execute a
comparison only when requested, not for every directory visited or filter keystroke.

Provide allow/block rule lists, one editable pattern per row, with examples and explicit
Apply. Preserve escaping and whitespace; send repeated query parameters. Block wins over
allow. Enforce contract limits in UTF-8 bytes, not just JavaScript string lengths. The backend
remains the glob interpreter; do not approximate matching by JavaScript regex.

Both views display applied rules and retained/excluded counts. Draft rule changes invalidate
or clearly mark old output until applied. Cancel obsolete requests and reset cursors.

- Exact replicas: destination disk/root/inventory, same-disk indicator, and `whole_tree_equal`.
  A filtered match is “Equal under these filters”, not a claim about the whole tree.
- Coverage: per-other-disk covered/missing file and distinct-content counts, completeness,
  and known/unknown bytes. Calculate any display percentage with safe integer arithmetic,
  clearly label its denominator, and handle zero without division.
- An empty comparison is “No files selected”; it is neither 100% coverage nor “no copies”.
- Coverage distributed over multiple disks is not one complete directory backup. Do not
  infer aggregate full coverage by adding overlapping per-disk counts.
- Historical source inventories still compare against current destination inventories.

Large filtered comparisons can take seconds. Keep navigation and cancellation usable; do
not preload comparison tabs, impose a short success timeout, or claim a latency guarantee.

### Distinct-content and redundancy list

Implement current/history contents, disk/directory membership, and current replica bounds.
Use “Other disks” by default and offer “Other locations” explicitly. Exact bounds and range
bounds are mutually exclusive. All counts stay catalog-wide despite membership filtering.
For content-level other counts, historical-only content must not produce negative values.
Do not reuse observation-specific subtraction rules for a historical source observation.

Provide useful current-scope presets such as zero other disks and at least one other disk,
but show the actual filter values. Link each item to content detail; no automatic all-page
fetch, bulk deletion, cleanup recommendation, or unimplemented export action.

## 6. API client, numeric safety, and errors

Generate wire types reproducibly from local OpenAPI and check generated output for drift.
Generated types are compile-time help, not runtime validation. Use strict decoding for
shared response invariants and contract-fixture tests for endpoint shapes. If adopting a
runtime validator, derive it from the contract where practical; do not maintain a second
handwritten schema for every DTO without a demonstrated need.

Keep endpoint wrappers explicit and thin:

- Build only allowed query keys; omit absent scalars, retain literal empty root paths where
  allowed, and append repeated allow/block values.
- Pass `AbortSignal` through every read. Distinguish cancellation, transport/proxy failure,
  malformed/unexpected response, and structured HTTP errors.
- Use JSON content type for disk writes. Do not automatically retry POST/PATCH after an
  uncertain network outcome; provide reload/reconciliation before resubmission.
- IDs, counts, revisions, and bytes remain decimal strings. Use `BigInt` for exact arithmetic
  and integer-safe formatting; never pass wire integers through `Number` first.
- Keep wire objects serializable; do not replace their decimal strings with `BigInt` values.
- Unknown size/mtime remains null. Known zero, an empty list, missing resource, and backend
  unavailable are distinct states. Missing required data is a contract error, not zero/empty.
- Display dates with distinct labels and offer exact UTC values. Relative capture age is
  inventory age, not proof that a disconnected disk was recently checked.

### Cursor lifecycle

Traversal identity includes endpoint/resource, query, scope, filters, and page limit. Cursors
remain opaque. Do not decode them, mutate filters mid-traversal, or request all pages at boot.

On `409 stale_cursor`, discard that traversal and show “Inventory changed; reload results”.
Offer an explicit first-page restart with the same inputs; never append new-revision results
to old pages. On reconnect after a backend restart, refresh catalog context and invalidate
inventory-dependent traversals when revision changed. Since revision cannot identify a
recreated/switched catalog, conservatively clear client data across application reloads and
backend reconnection rather than claiming cross-catalog cursor safety.

### Error presentation

- 400: show parameter correction without hiding submitted inputs.
- 404: resource-specific unavailable/not-found page, not an empty result list.
- 409 stale cursor: traversal reset flow; disk conflict: form-level conflict flow.
- 503/network failure: visible disconnected/unavailable state and manual retry.
- 500/unexpected payload: explicit failure with useful status/code, not raw HTML or stack traces.
- Clipboard failures: visible failure; do not announce success when permission was denied.

Render backend messages and paths as text, never injected HTML. Use a bounded readiness
recheck/backoff after disconnect, not continuous per-component health polling. Preserve
local form inputs during outages and do not make cached content look freshly verified.

## 7. Testing strategy and proposed UI performance checks

Each task begins with behavioral tests. Use three layers:

1. **Unit:** route parsing, query encoding, decimal formatting, validation, request ordering,
   keyboard guards, cursor identity, and patch construction.
2. **Component/browser mocks:** deterministic DTO fixtures and intercepted requests for
   loading, empty, partial failure, delayed/out-of-order responses, stale cursors, and writes.
3. **Real backend browser tests:** start Go with a disposable SQLite catalog, import fixtures
   offline, then start the server and frontend/proxy. Exercise the actual workflow end to end.

Fixtures should include same-name/different-hash files, same-content/different-path files,
same-disk copies, three current disks, repeated/older histories, a historical-only file,
unknown size/mtime, known empty content, long/Unicode/literal-special-character paths,
renamed exact trees, filtered tree matches, and distributed coverage without one full copy.

Include synthetic DTO values above `Number.MAX_SAFE_INTEGER`, even if real fixture IDs are
small. Build fixture helpers against generated types. OpenAPI examples are shape examples,
not one linked catalog: do not chain their unrelated IDs as if they were an imported fixture.

Real tests must never touch `coldcat.sqlite` or the preserved benchmark directories. Own
child processes, temporary directories, and ports; wait for readiness and terminate children
on success/failure. The backend process owns its catalog; do not open it concurrently from
tests. Restart/import cursor scenarios stop the server before importing. Reuse the project's
ext4 temporary-directory guidance for full Go tests; no cold-cache benchmark is needed for
ordinary frontend correctness tests.

### Browser acceptance cases

- Type-to-search inserts the first character once; input/forms, IME, selection, dialogs,
  browser shortcuts, and disabled preference are respected.
- Short substring input causes no search request; exact short names work.
- Rapid edits cannot display an earlier response as the latest result or append an old page.
- Search → content → locations → observation/directory works with keyboard alone.
- Back, reload, deep links, and new-tab links preserve meaningful route state.
- Multiple pages are reachable without duplication, resorting, or an invented total.
- Historical badges and current copy counts remain correct for repeated/deleted observations.
- Unknown/zero sizes and large decimals render accurately.
- Disk clearing sends the right PATCH, conflicts retain input, uncertain writes are not retried.
- Empty filtered comparisons, filtered equality, and distributed coverage are correctly labeled.
- Backend unavailable, page failure, stale cursor, and malformed response are explicit.
- Filenames/labels containing markup render as text; clipboard/path encoding is safe.
- Existing keyboard/landmark/focus/live-status, narrow-viewport, reduced-motion and high-contrast
  checks are retained as best-effort evidence. The revised owner scope requires no additional
  accessibility/screen-reader/extended audits and does not claim assistive-technology success.

### Frontend performance evidence, not backend acceptance

Proposed initial UI checks: no catalog-sized preload, no per-result API calls, one scheduled
query for the latest settled input, bounded retained pages, and no eagerly executed directory
comparisons. Use a delayed backend/mock to prove typing/navigation remain responsive.

Measure browser input handling, request scheduling, response-to-render time, DOM row count,
and memory under a documented browser/hardware/profile. A provisional goal is no input
handler or 50-row render producing a main-thread long task over 50 ms on the chosen target.
Agree hardware and thresholds before calling this an acceptance budget; debounce time and
backend response time must be reported separately. Unit-test fake-clock timing does not
establish production latency. Do not reinterpret backend warm means or cold open-plus-query
samples as browser p95 guarantees.

## 8. Bounded agent work packages

The packages below are designed for separate sessions, comfortably below a 250k context
window. Context ranges are planning estimates, not measured token use or guarantees. Aim
for 30–100k per package; split at the named seams before reaching roughly 150k. Do not ask
one agent to build the entire frontend in one session.

### Common execution and handoff contract

Every agent receives this plan, root `AGENTS.md`, the exact OpenAPI sections it uses, relevant
integration semantics, predecessor handoff notes, and only the feature's source/tests. Avoid
reading the entire backend foundation or benchmarking history for ordinary UI tasks.

For every package:

1. Confirm scope and prerequisites; identify shared files before editing them.
2. Write focused behavior tests, implement the bounded feature, run its checks.
3. Report changed paths, commands/results, public interfaces, open issues, and next-task notes.
4. Stop at the package boundary. Record a failing test/blocker rather than silently adding
   backend features, suppressing type/lint errors, or substituting fake production data.

Use existing sessions/PR descriptions for handoffs unless persistent notes are requested.
Update project-local agent guidance briefly when actual build/test conventions are learned,
following root instructions. Do not make guessed commands authoritative.

Shared route registries, dependencies, layout, generated types, and global CSS have one owner
at a time. Parallel work starts only after shared contracts stabilize; use separate worktrees
if available and do not run large backend benchmarks alongside browser acceptance tests.

### F0 — decisions and frontend bootstrap

- **Prerequisites:** explicit implementation authorization and stack/distribution decision.
- **Scope:** `frontend/` scaffolding, package scripts, static SPA route policy, dev proxy,
  strict typing, minimal shell, test setup, and dependency/engine pinning.
- **Acceptance:** clean install, checks, build, one component test, one browser smoke test;
  API proxy works and backend failure is visible; built nested routes have a defined hosting
  contract. No real feature UI or Go asset-serving implementation yet.
- **Context estimate:** 30–60k.

### F1 — wire client and numeric/domain presentation

- **Prerequisites:** F0.
- **Scope:** `lib/api/`, `lib/format/`, generated-type command/drift check, typed endpoint
  wrappers, shared errors, safe decimal/date/path helpers, typed mock fixtures.
- **Acceptance:** cover each planned operation's wrapper; URL escaping and repeatable globs;
  huge decimals, null/zero, structured/transport errors, cancellation, and changed-field PATCH.
- **Boundary:** no UI tables, search state machine, or duplicated backend query logic.
- **Context estimate:** 40–80k; split generation/client from formatting if necessary.

### F2 — shell, route state, and bounded request/pagination primitives

- **Prerequisites:** F0–F1.
- **Scope:** shared shell/controls, query parsing, route builders, explicit request lifecycle,
  cursor traversal identity, limited-page retention, error/loading presentation, connection
  context, and keyboard-event guards. Define feature interfaces before parallel work.
- **Acceptance:** parameter changes cancel traversal, late results are rejected, bounded
  page transition works, stale cursor never mixes revisions, and links are ordinary links.
- **Boundary:** do not turn shared state into a feature-independent data framework.
- **Context estimate:** 50–90k.

### F3 — primary search workspace

- **Prerequisites:** F1–F2.
- **Scope:** search feature and `/`; debounce, hotkeys/type-to-search, results, filters, load
  more/page transition, URL binding, selection, return-state restoration, and shortcut help.
- **Acceptance:** all section 4 behaviors with delayed/out-of-order browser mocks; no
  per-result requests; literal/Unicode queries and scope/filter restrictions are covered.
- **Handoff:** explicit content/observation/directory link contract for F4/F6.
- **Context estimate:** 70–120k; split keyboard/result interaction from advanced filters and
  restoration if needed. Do not postpone race tests until final integration.

### F4 — content, locations, observation details, and hash lookup

- **Prerequisites:** F1–F2; F3's link contract, not necessarily completed search UI.
- **Scope:** contents detail, locations/history, observation route, hash-lookup form, copy
  actions, independent section failures, and selected-observation context.
- **Acceptance:** same-disk copies versus other disks, historical-only content, dates,
  partial failures, decimal safety, and return navigation; two-request initial detail flow
  does not expand into per-location requests.
- **Context estimate:** 50–90k.

### F5 — read-only disks and snapshot history

- **Prerequisites:** F1–F2.
- **Scope:** disk list/detail, snapshot pages/history, metadata completeness, provenance,
  latest-snapshot/root links, and empty-inventory states.
- **Acceptance:** paginated history, capture-time rather than import-time presentation,
  disks without snapshots, unknown cataloged totals, and no physical-free-space claims.
- **Context estimate:** 40–80k.

### F6 — directory browsing and sizing

- **Prerequisites:** F1–F2; route/link interfaces from F4–F5.
- **Scope:** directory route, breadcrumbs, summary, entries, recursive mode, redundancy
  filters/histogram, and links to files/content. Optional lazy directories-only navigator
  only if the basic browser demonstrates a need.
- **Acceptance:** root, literal path escaping, segment boundaries, historical source/current
  replica semantics, known/unique bytes, and directory navigation under file filters.
- **Context estimate:** 50–100k.

**First usable slice checkpoint:** F3–F6 work against real imported fixtures. Search remains
the landing page; keyboard search to known locations works; basic directory navigation works.
Run the core real-backend browser tests before adding comparison/management scope.
The separate `npm run test:real` and `npm run test:real-harness` commands cover this bounded core;
full F11a revision/reconnect and later feature integration also run through `npm run test:deployed`.

### F7 — disk creation/editing

- **Implemented interface:** create form on `/disks`, edit form on `/disks/[id]`, with
  explicit write reconciliation and metadata invalidation; see the
  [F7 handoff](../frontend/README.md#disk-creation-and-editing-f7-handoff).
- **Prerequisites:** F5 and stable invalidation interfaces from F2.
- **Scope:** forms, decimal capacity validation, PATCH semantics, conflicts, uncertain-write
  reconciliation, and affected metadata refetch.
- **Acceptance:** omitted versus cleared fields, no precision loss, preserved form on error,
  and label refresh even though inventory revision stays unchanged.
- **Context estimate:** 30–60k.

### F8 — distinct-content/redundancy explorer

- **Implemented interface:** `/contents` composes the bounded distinct-content explorer and
  F4 hash lookup; see the [F8 handoff](../frontend/README.md#distinct-content-explorer-f8-handoff).
- **Prerequisites:** F1–F2 and F4.
- **Scope:** `/contents` list, hash-lookup entry point, membership filters, current/history,
  metric/bounds controls, and explicit redundancy presets.
- **Acceptance:** pagination, valid combinations, catalog-wide counts despite membership,
  and no historical-only negative other-count display.
- **Context estimate:** 40–70k.

### F9a — directory exact replicas and shared rule editor

- **Implemented interface:** explicit Exact tree copies view on the F6 directory route,
  controlled literal rule editor and bounded replica traversal; see the
  [F9a handoff](../frontend/README.md#directory-exact-replicas-f9a-handoff).
- **Prerequisites:** F6.
- **Scope:** shared allow/block draft/applied state and limit validation, replicas tab,
  paginated results, filtered/whole-tree labels, and empty-comparison handling.
- **Acceptance:** escaped/repeated glob serialization, same-disk replicas, filters invalidate
  traversals, and explicit execution/cancellation under delayed responses.
- **Context estimate:** 40–80k.

### F9b — directory content coverage

- **Implemented interface:** explicit Content on other disks view, reusing F9a's controlled
  rule editor and bounded traversal; see the
  [F9b handoff](../frontend/README.md#directory-content-coverage-f9b-handoff).
- **Prerequisites:** F9a's rule editor contract and F6.
- **Scope:** coverage tab, covered/missing counts, safe optional percentages, completeness
  and unknown bytes, and links to destination disks.
- **Acceptance:** distributed partial copies never look like one exact backup; empty selection
  never yields 100%; historic sources use current destinations; pagination and slow calls work.
- **Context estimate:** 30–60k.

### F10 — same-origin static deployment

- **Implemented interface:** `serve --assets <built-directory>` hosts the static SPA at
  origin root with confined file access and explicit deep-link dispatch; see
  [local hosting and F11a harness interface](../frontend/README.md#built-route-and-same-origin-go-hosting-f10).
- **Prerequisites:** F0 architecture approval and built route contract; runnable UI for smoke
  tests. Can proceed independently of later comparison screens.
- **Scope:** minimal Go asset-directory option/handler, static/SPA dispatch tests, and local
  run/build documentation. No CORS/auth/import implementation or schema changes.
- **Acceptance:** built frontend works without Node at runtime; nested reloads, missing
  assets, API errors, HEAD, path traversal, and reserved route precedence are tested.
- **Checks:** focused Go handler/CLI tests, vet, built-browser smoke; required Go checks for
  production Go changes at final integration, respecting the race-run guidance in AGENTS.
- **Context estimate:** 30–70k. Backend integration has a distinct owner from UI features.

### F11a — real-backend workflow integration

- **Implemented interface:** `npm run test:deployed` builds then runs the complete real-browser
  suite against Go-only same-origin hosting, with an OpenAPI-derived 18-operation coverage gate.
  `npm run test:real` preserves development/proxy coverage. Owned stop/import/restart, stale
  cursors, same/changed-revision reconnect clearing and committed POST/PATCH response-loss
  reconciliation are covered; see the
  [full integration matrix and ownership contract](../frontend/README.md#full-real-backend-integration-f11a).
  Revision is not catalog identity; reconnect tests assert clearing, not cross-catalog safety.
- **Prerequisites:** F3–F6; extend incrementally for F7–F9.
- **Scope:** disposable fixture/import/server harness, real browser workflows, revision and
  reconnect scenarios, contract coverage matrix, and targeted fixes in coordination with
  feature owners. No performance benchmark expansion.
- **Acceptance:** the core user workflow and every UI-exposed operation work with the real
  API; mocked tests are not the only proof. Process/port/catalog cleanup works on failures.
- **Context estimate:** 50–100k. Split primary reads from write/comparison/restart tests if
  necessary; do not make the integration agent reimplement all features.

### F11b — accessibility, responsive behavior, and frontend responsiveness

- **Implemented evidence:** deployed screen/state axe checks at desktop/320px, native keyboard
  and delayed-request behavior, bounded DOM/heap and instrumented Chromium measurements; see
  [F11b approved evidence and scope](../frontend/tests/F11b-acceptance.md).
  **Complete under the revised owner scope:** approved profile/budgets passed in all 20 fresh
  Chromium contexts; max/p95 and per-run evidence are committed. Accessibility remains best
  effort, with no additional audit or manual AT gate and no claimed screen-reader success.
- **Prerequisites:** integrated feature screens.
- **Scope:** retain prior best-effort keyboard/automated accessibility/narrow-layout/reduced-motion
  evidence; complete only delayed-backend responsiveness, bounded DOM/memory and approved repeated
  measurements. No extended accessibility/screen-reader checks after the owner scope decision.
- **Acceptance:** no lost typing under slow responses and agreed profile-specific UI budgets
  pass; accessibility is best effort, without claiming assistive-technology or backend approval.
- **Context estimate:** 40–80k.

### F12 — release handoff and final checks

- **Implemented handoff:** [final release checks and limitations](#f12-release-handoff), with
  non-accessibility browser regression scope and focused final Go race coverage stated explicitly.
- **Prerequisites:** all selected release packages and explicit gate decisions.
- **Scope:** clean install/build/test run, supported-browser statement, local setup/help,
  import/server ownership instructions, known limitations, and final acceptance checklist.
- **Acceptance:** another agent/user can build and run from documentation; no stale mock
  switch, unfinished action, silent fallback, or unapproved backend acceptance claim.
- **Context estimate:** 30–60k; documentation/check orchestration, not a new feature session.

### Dependency and parallelization summary

```text
F0 → F1 → F2 → F3
               ├→ F4
               ├→ F5 → F7
               └→ F6 → F9a → F9b
F4 + F2 → F8
F0 + built-route contract → F10
F3–F6 → first usable slice + F11a core tests
Selected features + F10 → F11a full tests + F11b → F12
```

F3/F4/F5 can be parallel agents after F2 freezes shared contracts. F6 can begin with stable
link interfaces rather than waiting for every detail screen. F7/F8 and F10 are largely
independent afterward. F9b follows F9a to avoid competing rule-editor implementations.
Only the integration owner edits shared registrations/configuration during parallel work.

## 9. Implementation commands and gates

The frontend package establishes the scripts below. Report execution evidence in sessions/PRs:

- `npm ci`: reproducible installation in `frontend/`.
- `npm run dev`: dev server with fixed API/readiness proxy.
- `npm run check`: Svelte/TypeScript checks.
- `npm run lint`: consistent lint/style checks; fix issues, do not suppress them.
- `npm run test:unit`: noninteractive unit/component suite.
- `npm run test:e2e`: browser acceptance suite with defined backend ownership.
- `npm run build`: deployable static output.
- `npm run api:generate` and `npm run api:check`: generation and contract drift check.
- `npm run test:api-generation`: reproducibility, read-only drift and unsupported-schema tests.

Document browser installation prerequisites and separate mocked browser tests from the
real-backend harness if they require different commands. Pure frontend tasks run focused
frontend checks; they do not rerun importer benchmarks or the full Go race suite.

For Go asset-serving changes, use the project's established normal tests/vet and final race
guidance. Run a full race check at most once at the final Go integration boundary, with
serial/focused checks preferred where appropriate. Record actual commands/results; proposed
scripts and unexecuted checks are not evidence of completion.

## 10. Decisions and final completion criteria

### Decisions needed before implementation

1. Authorize implementation relative to the still-open backend acceptance gate.
2. Approve SvelteKit static SPA and same-origin Go asset-directory serving, or choose an
   alternative local hosting strategy. Propose an ADR; do not create it without approval.
3. Confirm first-usable versus full-initial-release scope and package manager.
4. Confirm the desktop-only, disableable type-to-search default. Explicit search shortcuts
   and normal controls remain available regardless of this preference.

F11b finalized initial Chromium-desktop support and the profile-specific UI measurement budgets
recorded above; all 20 approved runs passed. Debounce, page size/retention and wait-message
thresholds are UI tuning values, not approved backend limits. Additional features require a
separate scope decision.

### Completion checklist

- [x] Entry-point search is keyboard-friendly and works without deliberate mouse focus.
- [x] Exact/substring, name/path, current/history, and filters match the backend contract.
- [x] Latest request wins; loading, empty, error, and stale-cursor states are explicit.
- [x] Search → content → current locations → observation/disk/directory is exercised with
      real temporary catalogs, not only mocks.
- [x] History and replica metrics are explicitly labeled, without live backup guarantees.
- [x] Numeric strings, null metadata, literal paths, and date meanings remain accurate.
- [x] Directory browsing, comparisons, and selected management/list features meet tests.
- [x] Back/reload/deep links work; pagination is bounded but later results remain reachable.
- [x] F11b best-effort keyboard/accessibility/responsive evidence and approved profile-specific
      UI responsiveness checks are complete under the revised owner scope (no manual AT claim).
- [x] Static same-origin distribution and API precedence are tested; no public-safety claim.
- [x] Build/test/setup commands are reproducible and known limitations are documented.
- [x] Remaining backend performance decisions are explicitly approved, waived for this
      release, or still labeled unresolved; successful UI tests do not close them.

Deliver by usable vertical slices. If work must stop early, leave a tested search/location
workflow rather than many half-built screens.

## F12 release handoff

### Release package and owner decisions

F0–F10 implementation, F11a real-backend integration, F11b approved profile evidence and F12
final integration are complete within the full initial frontend scope. No new feature or
backend benchmark was added during F12. Production client, connection/proxy, route dispatch,
asset handler and CLI ownership paths were reviewed: no production mock switch, unfinished
UI action or silent response fallback was found. Test-only hosts/interception remain test-only.

Owner authorization, not test output, establishes npm/SvelteKit/Go-assets architecture
([ADR 0002](adr/0002-static-browser-frontend.md)), parallel frontend development, Chromium
desktop-only support and best-effort accessibility. The accepted
[20-run F11b profile/budget evidence](../frontend/tests/F11b-acceptance.md) is retained unchanged;
it is not backend latency approval or a hardware-general guarantee. Final browser runs exclude
both existing accessibility specs; no accessibility suite or manual AT gate was added or rerun.

### Reproducible final checks (2026-10-10)

From `frontend/`, with Node 24.21.0/npm 11.19.0 and Chromium system libraries installed:

```sh
npm ci
npm run api:check
npm run test:api-generation
npm run check
npm run lint
npm run test:unit
npm run build
npx playwright install chromium
npm run test:proxy
npm run test:e2e -- 'tests/browser/(?!accessibility\.spec\.ts$)[^/]+\.spec\.ts$'
npm run test:real-harness
npm run test:real -- 'tests/real/(?!accessibility\.spec\.ts$)[^/]+\.spec\.ts$'
npm run test:deployed -- 'tests/real/(?!accessibility\.spec\.ts$)[^/]+\.spec\.ts$'
npm run test:hosting-harness
npm run test:hosting
```

Run browser suites sequentially because they share `test-results/`. The filename regex
excludes only `accessibility.spec.ts`; this is intentionally not an unfiltered full-suite claim.
The deployed command retains the mandatory 18-operation exposed-API matrix. The unexposed
directories-only operation is excluded by design, not missing UI coverage.

From the repository root, use Go 1.27.1 and a disposable non-tmpfs test directory, following
project-local ext4 guidance for cold-cache correctness tests. Final Go checks passed using
disposable non-tmpfs storage; this is not an ext4 or cold-performance measurement claim.

```sh
go vet ./...
test_tmp=$(mktemp -d /home/m/coldcat-test-tmp-XXXXXXXX)
TMPDIR="$test_tmp" go test -count=1 ./...
rmdir "$test_tmp"
test_tmp=$(mktemp -d /home/m/coldcat-test-tmp-XXXXXXXX)
TMPDIR="$test_tmp" go test -race -p 1 -count=1 ./internal/httpapi ./cmd/coldcat -timeout=30m
rmdir "$test_tmp"
go build -o /tmp/opencode/coldcat-f12 ./cmd/coldcat
/tmp/opencode/coldcat-f12 serve --help
```

The final race command ran once, serially, for HTTP/asset hosting and CLI packages only;
it is **not** `go test -race ./...`. Full normal Go tests and vet cover all packages.
All catalogs/processes were disposable and owned by tests; the workspace catalog and
preserved benchmark directories were untouched. Check results are recorded below.

| Final check | Result |
| --- | --- |
| Fresh `npm ci` | Pass; 233 packages, zero audit vulnerabilities |
| `api:check`, `test:api-generation` | Pass; no drift, 2 generator tests |
| `check`, `lint` | Pass; zero Svelte errors/warnings; formatting/lint clean |
| `test:unit`, `build` | Pass; 143 tests across 15 files; static assets built |
| Chromium install, `test:proxy` | Pass; 1 proxy test including expected connection-refusal/502 |
| Non-accessibility `test:e2e` | Pass; 72 mocked browser tests, including one responsiveness regression |
| `test:real-harness` | Pass; 10 startup/lifecycle/cleanup tests in both host modes |
| Non-accessibility `test:real` | Pass; 27 development-proxy real-backend tests |
| Non-accessibility `test:deployed` | Pass; 27 Go-hosted real-backend tests; 18/18 exposed-operation matrix |
| `test:hosting-harness`, `test:hosting` | Pass; 4 owned cleanup tests and 1 built-Go browser smoke |
| `go vet ./...`, full normal `go test -count=1 ./...` | Pass; all packages, disposable non-tmpfs storage |
| One focused serial final race run | Pass; `internal/httpapi` (45.455 s), `cmd/coldcat` (58.058 s); no full race suite |
| CLI build and `serve --help` | Pass; loopback default and explicit `--assets` documented |

The single final responsiveness regression is not a replacement for the approved 20-run
F11b measurement. Existing profile evidence and accessibility scope remain unchanged.

### Local startup and distribution

Use [backend setup](backend-integration.md#local-setup) to create disks and import a complete
inventory into a separate catalog, offline. Supply its actual capture time (or explicitly
use source mtime). For development, start the loopback Go API, then run `npm run dev` from
`frontend/`; `COLDCAT_BACKEND` selects another validated loopback origin. A missing backend
is an explicit failure, never demo data. See [frontend setup](../frontend/README.md#development).

For deployment, build assets with `npm run build`, then from the repository root:

```sh
go build -o /tmp/opencode/coldcat ./cmd/coldcat
/tmp/opencode/coldcat --db /tmp/opencode/demo.sqlite serve \
  --listen 127.0.0.1:8080 --assets frontend/build
```

Open `http://127.0.0.1:8080/`. Distribute the Go binary plus the complete trusted `build/`
directory; Node is not needed at runtime. Relative asset paths resolve from the server's
working directory. Stop Go before replacing assets, keeping shell and hashed files together.
Omit `--assets` for API-only serving. Deep links are recognized explicitly; API failures and
missing assets never become HTML success responses. The server holds the exclusive catalog
lock: stop with SIGINT/SIGTERM before any CLI/import operation; never delete lock files.

### Known limitations and unresolved gate

- Chromium desktop only; other browsers and real mobile devices are unsupported initially.
  Accessibility is best effort, without WCAG conformance or assistive-technology success claims.
- Local/private use only: no authentication, CORS option or safe public/Internet exposure.
  Keep loopback binding. Origin-root asset hosting only; no embedded assets/subpath package.
- Imports remain CLI-only; no uploads, extraction/downloads, host-file opening, snapshot diffs,
  jobs or multi-catalog switching. These are deferred, not unfinished UI actions.
- Current replicas describe latest recorded complete inventories, not live filesystem checks
  or backup guarantees. Distributed content coverage is not one exact tree backup.
- Pagination/restoration is bounded and process-local; reload reconstructs page one, reconnect
  clears retained data conservatively. Revision is not catalog identity. Metadata writes have
  no idempotency key/version; uncertain writes require deliberate manual reconciliation.
- **Backend acceptance remains unresolved**, neither waived nor approved by frontend completion.
  Supported observation/history limits, latency/storage/import/recovery budgets, slow broad/no-match
  searches and filtered comparisons, and required remaining operational evidence need owner
  decisions in the [backend closure list](coldcat-backend-api.md#remaining-decisions-and-blockers).
  Physical journal-peak tracing is scrapped, not a reopened blocker. Existing journal sizes,
  warm means and cold open-plus-query samples retain their stated measurement boundaries.
