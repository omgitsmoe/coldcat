# U11 — Directory entries and summary layout

**Complete: 2026-10-11.** Implements only the approved U11 package, using U01's approved
specification and U02/U03/U05/U07/U10 interfaces. No other agents, backend/API/catalog changes,
extended accessibility audit or expanded browser support.

## Executed plan

1. Keep the existing independent directory summary and entry traversal owners.
2. Prioritize literal breadcrumbs and verified source context; compact recursive/unique totals
   while keeping unfiltered membership, Unknown, incompleteness and known subtotals visible.
3. Disclose detailed statistics; reuse native U07 replica modes, draft/applied status and tables.
4. Validate literal/narrow records, independent loading/retries and bounded pages, inspect external
   captures, run frontend gates and commit explicit U11 files only.

All steps completed. Counts, completeness and unknown-size counts are not hidden in statistics.
Current destination inventories and source-disk exclusion remain explicit for historical sources.

## Files and interfaces

- `frontend/src/lib/features/directories/Browse.svelte`: `.source-context` groups inventory,
  current/historical source marker, disk and capture date from the independently available response.
  Literal breadcrumbs retain full segments and native filter-preserving links. Summary and entries
  still load/fail/retry separately. A failed summary has only the shared Retry, not an adjacent
  duplicate Reload. Detailed statistics contains logical-byte explanation, maximum known mtime
  and the unfiltered other-disk histogram. `.directory-views` styles the existing native buttons;
  tab identities, component keys, explicit execution and disposal remain unchanged.
- `Sizes.svelte`: unchanged `item: DirectorySummary` interface; two semantic metric groups for
  occurrence bytes and unique-content bytes. Incomplete totals pass null to U05 `SizeValue` and
  visibly retain the known subtotal and unknown count. Complete totals omit redundant subtotal
  repetition. Descendant file/content counts explicitly say **unfiltered**.
- `Filters.svelte`: unchanged `id/query/retire/resume` props. Recursive mode, replica metric,
  U07 `ReplicaBounds` and page limit remain visible. `boundsMode` restores active URL bounds;
  `boundsQuery` projects only the deliberate mode, retaining inactive draft strings in memory.
  Decimal syntax and min/max ordering are explicitly validated immediately; URL construction
  alone does not check ordering. Historical source bounds remain valid because destinations are
  current; do not copy Search/Contents history prohibitions here. No URL mode parameter added.
- `filters.ts`: `directorySummary(query: DirectoryRoute): string` names applied listing mode,
  current destination metric, exact/range values (including zero) and page limit. The host labels
  this Applied or Last applied; edits still retire rows and cursors before application.
- `Entries.svelte`: unchanged `items/id/query/returnTo` props. Opt-in `data-table` with 44/23/18/15
  percent column shares, numeric alignment and full literal paths. Below 900px native rows become
  named path-first records with Size, Current other replicas and Actions labels. Local overrides
  prevent shared 40px cell heights and numeric alignment leaking into stacked records. Directory
  paths remain links under file replica bounds; content/observation links preserve return context.
- `tests/unit/directory-filters.test.ts`: three applied-summary cases.
- `tests/browser/directories.spec.ts`: intentional matching-mode/statistics locators plus four
  regressions for approved widths, active range validation/draft retirement, independent pending
  summary/retry and retained-page traversal. Existing literal navigation, return links, stale
  cursors and continuation failure assertions retained.
- `tests/real/core.spec.ts`: only intentional directory matching-mode/source-context locator
  adaptations; this suite was **not run** in U11 and remains U14's integration gate.
- Parent plan and this handoff record progress; no AGENTS changes or images are included.

## Validation and inspection

From `frontend/`, passed:

- `npm run check`: **0 errors / 0 warnings**.
- `npm run lint`: ESLint/Prettier without suppressions.
- `npm run test:unit`: **21 files / 244 tests**.
- `npm run api:check`: no wire drift.
- `npm run build`: passed without Svelte warnings.
- `npx playwright test tests/browser/directories.spec.ts tests/browser/replicas.spec.ts
  tests/browser/coverage.spec.ts tests/browser/readable-values.spec.ts tests/browser/details.spec.ts`:
  **45 passed**, isolated intercepted fixtures on the existing static browser host.

New assertions cover full Unicode/backslash/punctuation/deep literal paths, immediate directories
under exact-zero file bounds, visible unfiltered/incomplete subtotals, disclosed histogram without
requests, numeric alignment and nonoverlapping cells, no document overflow, draft retirement,
range ordering without dispatch, summary loading with successful entries, one scoped summary
retry, entry-only reload and 12-page traversal with retained Previous navigation and no automatic
exhaustion. Existing comparison checks preserve no preload, explicit execution, rule retirement,
current destination semantics, empty selection, stale cursors and retained continuation errors.

Initial validation caught Svelte initial-state warnings, missing explicit range-order validation,
an unscoped status locator matching the shell, and narrow numeric alignment leakage. All were
fixed in code or correctly scoped tests; no checks or assertions were disabled.

Three inspected full-page Chromium desktop captures, 900px viewport height, are written directly
to `/tmp/opencode/u11-screenshots/entries-{1440,1024,768}.png`; `measurements.json` records:

| Width | Table origin, caption included | Document width |
| --- | --- | --- |
| 1440 | 675.09px | 1440px |
| 1024 | 695.09px | 1024px |
| 768 | 800.09px | 768px |

The fixture includes a 160-character literal segment and exact-zero filter. Entries begin in the
first viewport without expanding statistics. These are not the identical U01 fixture, so origins
are not a like-for-like density benchmark; the aspirational 480–560px wide target is not claimed.
Full paths/context are not truncated to meet it. Reproduce with build and the directory browser
suite above. Follow [artifact policy](../artifact-policy.md): external temporary evidence only,
not durable hosting/pixel goldens, no regular-Git PNGs or LFS claim, no replacement U01 baselines.

## U12 / U13 handoff and constraints

- **U12:** consume the existing `replicas` / `coverage` host tabs and keyed `id/path` interfaces.
  The visible coverage button remains **Content on other disks** to retain existing browser/real
  locators; its internal tab remains `coverage`. RuleEditor/Replicas/Coverage were not edited.
  Preserve no comparison request on opening, explicit run, immediate retirement on rule edits,
  empty selection versus no copies, exact/filtered equality versus coverage and no backup guarantee.
- **U13:** RequestFeedback/PageControls props/callbacks are unchanged. Summary failure now has
  one Retry. Entry failure still has shared Retry plus Reload entries except stale status; broader
  contextual recovery consolidation remains your task. Keep retained rows explicitly unverified,
  stale/disconnected restart behavior, loaded counts (not totals), bounded traversal and summary/
  entry independence. Styling must not create a request owner or reload both sections together.
- **U14:** run full real/deployed operation-matrix acceptance; U11 ran no real backend/deployed,
  backend race/benchmarks, catalogs or extended audits. Real core locator changes remain unexecuted.
- No U11 blockers. Unrelated root/frontend AGENTS edits, catalog/lock and `.million-distributions-*`
  were untouched and remain unstaged. Approved shared UI/value conventions are reused, with no new
  architecture decision; the plan's optional ADR proposal remains unchanged.
