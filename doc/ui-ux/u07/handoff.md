# U07 — Search workspace and result density

**Complete: 2026-10-11.** Implements only the owner-approved U07 package on U02/U03/U05.
No backend/API/catalog changes, additional agents, extended audits or other feature packages.

## Executed plan and outcome

1. Preserve the existing search request/keyboard owner and URL/session restoration.
2. Compose the primary query/Search/Clear toolbar and visible Name/Path, Contains/Exact,
   Current/History controls. Disclose advanced membership and redundancy groups with counts.
3. Show applied filters and explicit unapplied-change/result-retirement status; validate bounds
   immediately and require deliberate mode selection rather than silently dropping conflicts.
4. Compact results, align quantities and group narrow records without losing literal paths.
5. Add unit/browser regressions, inspect external screenshots, run frontend gates and commit
   explicit U07 files while leaving unrelated instructions/catalog/measurement files unstaged.

All steps completed. The redundant Search history shortcut is replaced by the visible Scope
control; the contextual older-snapshot action remains inside membership. Short-query guidance
retains the existing message and explicitly explains three Unicode code points and Exact.
Selection actions and keyboard help share one results toolbar. Pagination/request recovery
callbacks remain unchanged; U13 still owns shared feedback/pagination polish.

## Files and reusable interfaces

- `frontend/src/lib/features/search/Workspace.svelte`: layout, advanced visibility/count,
  last-applied summary and immediate draft validation. Filter edits still retire rows, selection
  and traversal immediately. Query scheduling, composition guards, shortcut ownership, explicit
  apply, URL replace/push, Back/scroll restoration and bounded paging stay with existing owners.
- `frontend/src/lib/features/search/Results.svelte`: opt-in `data-table`, numeric columns,
  basename link and secondary full pre-wrapped literal path. Wide column shares are
  43/20/9/15/13 percent. At/below 900px records retain the native table and supply visible
  Disk/inventory, Size and Current copies labels. No ellipsis, clamping or path normalization.
  Select is a 28px-minimum quiet native button with an accessible name containing basename,
  full path and observation ID; `aria-pressed` and the selected native link remain intact.
- `frontend/src/lib/components/ReplicaBounds.svelte`: native shared matching-mode controls.
  Bind `mode`, `exact`, `minimum`, `maximum`; optionally supply `invalid` for semantic/visual
  invalid state. It owns no requests, routes, automatic clearing or application.
- `frontend/src/lib/features/search/filters.ts`: `BoundsMode`, `BoundsDraft`, `boundsMode`,
  `boundsQuery`, `advancedCount` and `searchSummary`. The last two helpers are search-specific.
- `frontend/tests/unit/search-filters.test.ts`, `replica-bounds.test.ts`: helper and native
  control coverage. Existing search owner tests remain unchanged.
- `frontend/tests/browser/search.spec.ts`: four added regressions plus existing interactions
  adapted to intentional disclosure/mode controls. `details.spec.ts` asserts the full new
  selection name. `readable-values.spec.ts` targets the size value inside its now-labeled cell;
  exact Unknown/zero/maximum, keyboard and disclosure assertions are retained, not weakened.
- Parent plan and this handoff record completion. No images or instruction files are committed.

### Bound-mode contract for U10/U11

`boundsMode(exact, minimum, maximum)` distinguishes absent values from zero. It returns `any`,
`exact`, `range`, or internal `conflict` when a bookmarked exact value coexists with range
endpoints. Conflict exposes all values; it is not silently repaired on load.

`boundsQuery({ mode, exact, minimum, maximum })` projects only the explicitly selected mode into
the existing `other_replicas`, `min_other_replicas`, `max_other_replicas` string fields. Any emits
no bounds; Exactly requires a value; Range requires at least one endpoint. Conflict fails until
the user explicitly chooses a mode. Changing modes retains inactive draft strings in memory,
so choosing Any is deliberate bound removal, not destructive clearing. Only active fields enter
the URL; reload/Back restores the URL's active fields, not inactive in-memory drafts.

Hosts must validate decimal syntax, ordering and scope using their existing route/feature
validation. The component does not assume that historical source inventories prohibit current
destination bounds: U11 must retain its own source/destination semantics. U10 must retain its
own content membership/replica-count rules. Do not reuse search-specific summary text for them.

Use native `control-group`, `field`, `check-field`, fieldset/legend and details/summary patterns.
Keep common controls visible. Show immediate invalid feedback and explicit last-applied versus
unapplied status; never leave previous rows appearing to match edited controls. Bound inputs
stay editable even when invalid, including history conflicts. No new URL mode parameter.

### Membership interface for U09

Disk and snapshot remain string IDs; directory has a separate enabled flag so empty root is
distinct from no membership filter. Replace the corresponding label/input slots within the
Membership fieldset, preserving `editedFilter()` event ownership and explicit apply. Do not
apply on picker opening or option loading. Bookmarked advanced values open the disclosure;
count membership fields individually, nondefault Locations metric separately, and bounds as
one active restriction regardless of the number of endpoints. Closed applied summaries retain
IDs, full directory/root and exact/range values. No label preload or per-row resource reads.

## Validation and evidence

From `frontend/`, final validation passed:

- `npm run check`: 0 errors/warnings; `npm run lint`: passed without suppression.
- `npm run test:unit`: 19 files / **238 passed** (14 new helper/control cases).
- `npm run api:check`: passed; no generated wire-contract changes.
- `npm run build`: passed.
- `npx playwright test tests/browser/search.spec.ts tests/browser/details.spec.ts
  tests/browser/readable-values.spec.ts tests/browser/shell.spec.ts
  tests/browser/smoke.spec.ts`: **39 passed**, fixture-only static hosting.

New browser coverage asserts bookmarked root/zero/range visibility and counts, closed applied
summaries, immediate draft retirement, last-applied identity, exact request counts, invalid
history combinations without lost values, deliberate Any removal, range ordering, conflicting
and malformed bookmark correction, full selection names, numeric alignment, narrow labels,
literal wrapping, caption width, checkbox width and document overflow. Existing delayed/obsolete
requests, IME, typing preference, Enter/Escape/arrows, ordinary links, detail return/Back/scroll,
reload, stale cursors and 12-page/50-row bounded traversal assertions still pass.

Initial validation caught incorrect exact label locators and size assertions including new
hidden narrow labels. Tests now locate the semantic combobox/value while retaining exact
assertions. A harness timeout left its owned static host running; it was stopped before retry.
Visual inspection caught a shrinking narrow caption and checkbox input-width leakage; both
were fixed and covered by new assertions. No assertions were weakened or audits added.

Screenshots are direct external output at `/tmp/opencode/u07-screenshots/`:
`results-{1440,1024,768}.png`, `invalid-history-{1440,1024,768}.png`, `measurements.json`.
They use Chromium desktop, 900px viewport height, deterministic intercepted fixtures, and
full-page capture. These are temporary inspection evidence, not durable public hosting or
pixel goldens. Follow [artifact policy](../artifact-policy.md); no image retention/LFS migration
is claimed. Existing U01 references were not replaced.

| Width | Table origin (caption included) | First record | Document width |
| --- | --- | --- | --- |
| 1440 | 369.16px | 445.16px | 1440px |
| 1024 | 369.16px | 445.16px | 1024px |
| 768 | 409.16px | 445.16px | 768px |

Invalid-history captures also assert no document overflow at all three widths. Literal stress
paths remain fully visible and may make rows tall by design; no performance claim is made.

## Constraints and remaining work

- No U07 blockers. Label-based resource pickers remain U09; content/directory integration is
  U10/U11; shared feedback/paging polish remains U13 and full deployed integration U14.
- All catalogs, backend files, unrelated AGENTS changes and `.million-distributions-*` remain
  untouched/unstaged. No real-backend/deployed suite, backend race/benchmark, extended
  accessibility audit, mobile-support gate or new catalog operation was run or introduced.
- Preserve readable/exact U05 size/date components, catalog-wide counts despite membership,
  historical repeated-observation wording, native links and the existing traversal owner.
- The approved plan proposes documenting shared UI conventions in an ADR if adopted as
  architecture policy, including decision/context/alternatives/consequences. None auto-created.
