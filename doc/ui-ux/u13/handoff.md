# U13 — Shared feedback and pagination polish

**Complete: 2026-10-11, after U07–U12 feature integrations.** Only the approved U13 package.
No backend/API/catalog changes, other agents, extended audits or expanded browser support.

## Executed plan

1. Read project instructions, approved U01 specification, U07–U12 handoffs and artifact policy.
2. Keep existing independent request/traversal owners; standardize shared state presentation and
   remove adjacent duplicate recovery actions without combining requests.
3. Align loaded counts/paging/reload, retain stable loading action labels and explain bounded
   accumulation-to-single-page navigation without fabricating catalog totals or page counts.
4. Test shared controls with the existing controller, fixture-only browser lifecycle and narrow
   layouts; inspect external evidence, run frontend gates and commit explicit U13 files only.

All steps complete. No new architecture/state-management decision; the parent plan's optional
shared-UI ADR proposal remains unchanged, with no ADR automatically created.

## Shared interfaces and feature integrations

- `frontend/src/lib/components/RequestFeedback.svelte`: existing `status/error/resource/retry/
  loadingLabel` inputs and extended-loading timer remain. Add optional `retryLabel`, `staleLabel`,
  `staleActionLabel`, `validationLabel`, `invalidatedLabel`, `emptyLabel`, `retained` and
  `retainedLabel`. Validation is explicit `status="validation"`; empty feedback renders only on
  success, invalidation only on idle, retained warnings only on failure with caller-supplied
  evidence. No rows, totals, resource existence, retries or verification are inferred. Shared
  restrained surface/border treatments use U02 tokens; labels and alerts remain textual. Recovery
  buttons are `type="button"`, including inside picker forms. Uncertain writes still suppress Retry.
- `PageControls.svelte`: existing flags/counts/callbacks retained. Optional `reload/reloadLabel`
  places owner-scoped explicit reload alongside loaded count and paging, disabled while busy.
  Paged status includes **Page N. X loaded on this page. Earlier pages retained from page Y.**
  Unpaged counts still say **X loaded**, never catalog total. At the bound, **Next shows one page
  at a time; older pages expire.** No last-page/page-count inference. Presentation-only memory of
  the preceding more/next action preserves its pending label and dimensions, including the first
  transition beyond the retention limit; it owns no request or cursor. Native navigation carries
  `aria-busy`; reload aligns right and controls wrap at group boundaries.
- `InventoryPages`, Contents `Explorer`, `ContentDetail` locations, Directory `Browse` entries:
  contextual invalidation/empty/retained failures consume shared feedback. Successful explicit
  reload moves into PageControls; continuation loading retains the disabled reload. Failure uses
  one scoped Retry, stale uses one shared Reload results, idle retains its explicit section reload.
  Retained failure keeps a distinct **Reload from first page** alongside continuation Retry:
  they are different operations, not duplicate recovery. The same existing owner callbacks decide
  continuation versus restart. Empty successful pages
  retain reload through their page toolbar; failures never masquerade as empty lists.
- Search `Workspace`: shared empty and retained-failure feedback, loaded count in paged toolbar
  instead of a duplicate count paragraph, and **Reload from first page** in PageControls. This
  distinct deliberate restart remains available alongside continuation Retry; do not conflate it
  with retrying an ordinary failed page. Typing, IME, selection, draft retirement and Back unchanged.
- Directory `Replicas` / `Coverage`: use shared retained warning labels, retaining explicit run,
  cancellation, dirty retirement, empty-selection semantics and all comparison metadata. No
  comparison is dispatched by feedback, disclosure, mount or reconnect.
- `ResourcePicker.svelte`: shared loading/stale/failure/invalidated/empty/retained feedback with
  existing option-specific labels. One Retry options on failure or Reload options on stale; no
  adjacent duplicate Reload options when no pages remain. Retained failures preserve a distinct
  **Reload options from first page**, rather than removing the deliberate restart escape hatch.
  On idle/success the explicit reload remains. Picker-specific
  native paging names/25-row requests, lazy owner, disabled unverified selection, direct ID drafts
  and older-capture semantics remain unchanged; no picker paging API migration.
- `DiskDetail` / `SnapshotDetail`: failure offers shared Retry, not adjacent Reload disk/inventory.
  Normal/invalidated explicit reload remains, disabled during loading; cached unverified metadata
  wording and U06 form ownership remain intact. Observation/identity summary owners are unchanged.
- Hash `HashLookup`: explicit validation uses shared feedback without dispatching or offering Retry.
  Confirmed lookup 404 still means no matching identity, not a fabricated empty catalog.

## Tests and results

From `frontend/`, final gates passed:

- `npm run check`: **0 errors / 0 warnings**.
- `npm run lint`: ESLint/Prettier passed without suppressions.
- `npm run test:unit`: **23 files / 262 tests**.
- `npm run api:check`: no wire drift.
- `npm run build`: passed without warnings; log `/tmp/opencode/u13-build.log`.
- Fixture-only static-host browser suite: **119 passed**, log `/tmp/opencode/u13-browser.log`:

  ```sh
  npx playwright test tests/browser/feedback-paging.spec.ts \
    tests/browser/inventories.spec.ts tests/browser/resource-pickers.spec.ts \
    tests/browser/search.spec.ts tests/browser/contents.spec.ts tests/browser/details.spec.ts \
    tests/browser/directories.spec.ts tests/browser/replicas.spec.ts tests/browser/coverage.spec.ts \
    tests/browser/disk-writes.spec.ts tests/browser/readable-values.spec.ts \
    tests/browser/shell.spec.ts tests/browser/smoke.spec.ts
  ```

New shared-control cases in `tests/unit/controls.test.ts` cover stable pending more/next labels,
disabled busy reload, validation versus empty/idle/failure, retained warning, callback ownership,
and actual bounded controller navigation while suspended without fetching or clearing its failure.
Existing controller/connection tests still verify late-response rejection, stale cursor/revision
retirement, cursor opacity and explicit recovery. No controller implementation changed.

New `tests/browser/feedback-paging.spec.ts` covers 10-page accumulation then paged navigation,
ordinary continuation failure retaining the active row and retrying exactly the same cursor,
deliberate restart from retained failure without cursor reuse,
503 suspension preserving visibly unverified cached Previous/Next navigation without requests,
reconnect clearing rows without feature reads, explicit first-page reload, revision mismatch with
one recovery action rather than empty success, and unchanged button bounding-box dimensions for
both pending Load more and the first Next page transition at all three approved widths. Loaded
counts never use the fixture's catalog count. Existing independent summary/location/history/entry
retry tests and all 22 disk-write/lifecycle checks pass. Inventory metadata-failure test now uses
the single scoped Retry instead of the deliberately removed adjacent Reload disk, preserving its
assertion that history is not reloaded.

Inspection caught a potential width change at the first pending Next page: applying Load more's
reserved width to Next was wrong. The busy button now retains the preceding action's class as
well as label; unit and browser bounds assertions cover this. No tests or lint rules weakened.

## External inspection evidence

Nine Chromium desktop full-page captures, 900px viewport height, deterministic intercepted
fixtures, write directly outside the checkout:

`/tmp/opencode/u13-screenshots/{loading,transition,retained}-{1440,1024,768}.png`

Inspected narrow loading/transition/retained and wide retained output. Counts/actions are aligned,
pending controls retain dimensions, long accumulated narrow lists scroll normally, and retained
navigation wraps without overlap or clipped content. Tests assert document width equals viewport
at all three widths. Reproduce with build and `npx playwright test
tests/browser/feedback-paging.spec.ts`; other suite captures belong to their existing packages.

Follow [artifact policy](../artifact-policy.md): these are temporary environment-local evidence,
not pixel goldens or durable hosting. No regular-Git PNGs, LFS claim, dependency installation or
replacement of U01 before-references. Git LFS remains unavailable; preserve external evidence in
owner-managed storage before discarding this environment if needed.

## U14 constraints and remaining issues

- No U13 blockers. **U14 still owns real/deployed integration and the full operation-matrix gate.**
  Those suites were not run here; no backend/race/benchmark/catalog or extended accessibility work.
  Existing U08/U10/U11/U12 real-suite locator adaptations remain for U14 to execute.
- Keep loaded counts distinct from catalog-wide domain counts; current/historical observations,
  unknown/zero/incomplete bytes and complete/partial coverage remain feature-owned. Page labels
  describe retained windows, not totals; an exhausted cursor does not invent a last-page number.
- Keep retained continuation rows, suspension warnings and cached navigation; stale/revision and
  reconnect retire rows/cursors and require explicit reload. Do not auto-run on reconnect, merge
  section reloads, preload options or automatically exhaust pages.
- Resource-picker paging intentionally retains its option-specific controls; shared feedback and
  duplicate recovery consolidation are integrated. Every retained list preserves its distinct
  explicit first-page reload, not a duplicate continuation Retry. No request-controller semantics
  changed. Real-suite recovery locators may need the intentional **Reload from first page** label
  during retained failure; success/idle section reload labels remain unchanged.
- Root/frontend AGENTS work, workspace catalog/lock and `.million-distributions-*` remain untouched
  and unstaged. Commit includes only shared presentation, scoped integration/tests and these docs.
