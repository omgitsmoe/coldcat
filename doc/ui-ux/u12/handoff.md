# U12 — Comparison editor and results

**Complete: 2026-10-11.** Only the owner-approved U12 package; follows U01 specification,
U02/U05 shared presentation, U11 host contract and the artifact policy. No backend/API/catalog
changes, other agents, extended audits or expanded browser support.

## Executed plan

1. Preserve existing explicit request/traversal owners and literal rule validation.
2. Compact allow/block rows, disclose contextual syntax/limits, and expose draft/applied state.
3. Align destination results while keeping whole-tree equality, filtered equality and content
   coverage distinct, including empty selections and incomplete source-byte information.
4. Run frontend gates and focused browser regressions, inspect external captures, publish this
   U13/U14 handoff and commit explicit U12 files only.

All steps complete. No new architecture or state-management decision; the parent plan's optional
ADR proposal remains unchanged.

## Files and interfaces

- `frontend/src/lib/features/directories/RuleEditor.svelte`: existing `draft/changed/apply`
  callbacks retained. Native fieldsets now use side-by-side allow/block groups, stacking below
  900px, compact Add/Remove actions and literal inputs. `scope?: 'trees' | 'source'` defaults to
  `trees`; coverage passes `source` so syntax help correctly states destination paths are ignored.
  It changes help only, not parsing/requests. Limits/examples live in a native disclosure; empty
  allow/block groups explain their effect. Validation still uses `ruleInput` without trimming,
  splitting, interpreting escaping or silently removing empty rows.
- `AppliedRules.svelte`: comparison-local presentation-only component, props
  `{ applied?: Rules, dirty: boolean, limit: string }`. Counts and applied page size are visible
  in a native disclosure summary; expanded rows preserve full literal text. Dirty state explicitly
  labels unapplied changes, retired results and **Last applied rules**, including the old page size.
- `Replicas.svelte` / `Coverage.svelte`: unchanged keyed `id/path` interfaces and request ownership.
  Each captures `appliedLimit` only with an explicit valid run; edits to page size still retire
  results. Success says **Comparison complete for loaded destinations**, not all catalog results.
  Existing loading/cancel, independent errors, retry/restart, retained rows and stale handling stay
  with `RequestFeedback` and the existing traversal. No comparisons on mount/help/edit.
- `comparison.css`: comparison-local `.comparison` namespace; opt-in native `.comparison-results`
  tables. `.tree-results` separates destination, equality and selected/excluded counts;
  `.coverage-results` separates destination, status and Covered/Missing metric columns. Below
  900px named records put literal destination first; coverage retains two aligned metric columns.
  Narrow cells override shared fixed height with `height: auto` to prevent vertical content spill.
  Full paths/labels wrap; native disk/inventory/directory links and U05 date/size disclosures remain.
- `tests/browser/replicas.spec.ts` / `coverage.spec.ts`: intentional covered/missing column locators
  plus two width/editor regressions. Existing no-preload, literal rules, empty selection, cancellation,
  late-response rejection, stale/error continuation and 12-page bounded traversal assertions retained.
- `tests/real/coverage.spec.ts`: only intentional row-scoped covered/missing locator updates,
  preserving both disks' exact counts/bytes/unknown counts. **Not executed in U12**; U14 must run it.
- Parent plan and this handoff record progress. No AGENTS or image files included.

## Validation and inspection

From `frontend/`, passed:

- `npm run check`: **0 errors / 0 warnings**.
- `npm run lint`: ESLint/Prettier passed, no suppressions.
- `npm run test:unit`: **23 files / 258 tests**, including unchanged rule-copy/UTF-8 limit and
  comparison-owner cancellation/current-destination unit tests.
- `npm run api:check`: no generated wire drift.
- `npm run build`: passed without Svelte warnings; final log `/tmp/opencode/u12-build.log`.
- `npx playwright test tests/browser/replicas.spec.ts tests/browser/coverage.spec.ts
  tests/browser/directories.spec.ts tests/browser/readable-values.spec.ts
  tests/browser/details.spec.ts`: **47 passed**, fixture-only static host, no catalog.

New tests cover contextual help without requests, applied literal rows, add/remove retirement,
old applied page size after editing, valid rerun and invalid 201-page-size rejection without dispatch,
loaded-destination success status, 32px input / 28px compact action minima, full encoded literal
destination links, numeric alignment, nonoverlapping coverage cells, cell-content fit and no document
overflow at 1440/1024/768px. Existing tests cover complete/partial coverage, huge exact counts,
Unknown source total/known subtotal, empty selection and independent section failures.

The initial literal-link test expected `%20` instead of URLSearchParams' `+`; corrected expected
encoding, not link behavior. Screenshot inspection then caught vertical content spill at 768px
despite nonoverlapping cell boxes. Fixed narrow cell heights and strengthened both table tests to
assert `scrollHeight <= clientHeight + 1`. Final screenshots show full content without collision.

Six full-page captures were inspected: `/tmp/opencode/u12-screenshots/{tree,coverage}-{1440,1024,768}.png`.
Widths equal viewports; Chromium desktop, 900px height. Tree captures stress long literal labels/
paths and expanded applied rules; coverage captures stress huge counts and incomplete bytes.
Both deliberately retain an independently failed summary while comparison results succeed.
These are local temporary inspection evidence, not pixel goldens, durable hosting or a density
benchmark. Reproduce with build and the browser command above; no PNGs committed, no LFS claim,
no replacement of U01 baselines. Follow [artifact policy](../artifact-policy.md).

## U13 / U14 constraints and remaining issues

- **U13:** `RequestFeedback` and `PageControls` props/callbacks are unchanged; do not introduce
  request ownership, auto-run on reconnect/help/edit, totals derived from loaded rows, combined
  summary/comparison reloads or automatic page exhaustion. Comparison retry remains owner-scoped;
  retained rows on continuation failure stay visibly not freshly verified. Keep cancellation and
  dirty retirement explicit; `AppliedRules` is metadata, not proof of a successful comparison.
- **U14:** run real/deployed integration and operation-matrix gates; adapted real coverage locators
  are unexecuted here. Extended accessibility specs remain excluded by owner direction; no manual
  AT gate. No backend/race/benchmark/catalog work was needed or performed.
- Preserve historical source/current destination semantics, source-disk exclusion for coverage,
  same-disk labels for tree copies, whole-tree versus filtered equality, and content-anywhere versus
  path equality. **No files selected** is not **No copies**. Coverage is not a backup guarantee;
  overlapping per-disk counts must not be added. Known subtotals never become complete totals.
- No U12 blockers. Unrelated root/frontend AGENTS work, catalog/lock and `.million-distributions-*`
  remain unchanged and unstaged.
