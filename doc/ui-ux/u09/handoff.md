# U09 — Label-based resource filter pickers

**Complete: 2026-10-11, after U07/U10.** Only U09 and its explicitly requested membership-bookmark
correction are implemented. No backend/API/catalog changes, other agents, extended audits or
binary Git artifacts.

## Executed plan

1. Read the approved specification, U07/U10 interfaces, artifact policy and existing read owners.
2. Compose independent lazy disk/inventory option owners with explicit bounded continuation,
   ID fallback and capture/older-inventory context; integrate only the supported membership slots.
3. Test native draft ownership, unloaded/bookmarked IDs, errors, late responses, retention bounds
   and reconnect; inspect external captures and run frontend gates.
4. Record the interface/limitations and commit explicit U09 files, excluding unrelated work.

## Files and interfaces

- `frontend/src/lib/components/ResourcePicker.svelte`: bind the canonical string `value`, supply
  `edited()` to retire the host's results once on a different programmatic selection. Direct input
  events bubble to the existing form's input/change owner. `kind` is `disk` (default) or `inventory`;
  inventory browsing takes `diskID`. The direct field remains named Disk ID / Snapshot ID for
  compatibility with expert entry and existing browser controls.
- The component owns its `diskPages` or `snapshotPages` adapter, not the host's result traversal.
  Opening requests one page with `limit: 25`; continuation is explicit. Existing `CursorTraversal`
  retains at most ten pages (250 options at the API limit), then displays one active page with
  Previous/Next controls. Loaded option counts are not catalog totals. No detail/per-row reads,
  cache of the catalog, prefetch, automatic page exhaustion or new read adapter.
- Closing disposes the option owner and aborts pending reads. Reopening explicitly starts at the
  first page. Labels are verified only against displayed loaded options; closing/eviction or an
  unloaded ID returns to the explicit unknown/not-verified presentation, never “missing.” This
  avoids an unbounded label cache and additional resolution requests. The string ID is retained.
- An inventory picker opened without a valid disk explains the requirement; entering a valid disk
  while it is open requests that disk's first page. Changing disks aborts obsolete option requests
  without silently clearing a standalone/mismatched snapshot draft; the server retains ownership
  of disk/snapshot membership validation. Empty disk IDs still permit direct Snapshot ID entry.
- Complete inventory options use `captured_at` through U05 `DateValue` with exact UTC reachable.
  Latest/older labels derive from the first row of the newest-capture-first API page, not numeric
  ID comparison, import time or a separate detail read. The first-page reference stays constant
  during bounded traversal and resets on invalidation. Older selection explains History scope
  but never switches scope or applies filters.
- Ordinary continuation errors retain rows but disable unverified selection. Retry repeats the
  continuation; stale cursors/revision changes clear pages and require first-page reload. The
  existing connection binding suspends on disconnect and retires on reconnect/metadata/revision.
  Reconnect does not request options or apply drafts. Reload affects only that picker.
- All picker buttons are `type="button"`, including paging/retry/reload, because consumers place
  them inside filter forms. Opening, loading, closing, paging and date disclosures do not retire
  host results, navigate, apply filters or execute comparisons.
- `frontend/src/lib/features/search/Workspace.svelte`: replace disk/snapshot input slots with
  shared pickers; preserve `editedFilter()`, ID-based URLs, root-directory flag, draft/applied
  summaries and the existing explicit older-snapshot history action.
- `frontend/src/lib/features/contents/Filters.svelte`: only disk picker; content-list API has no
  snapshot membership slot. Root versus absent directory and `retire()` / explicit Apply stay
  unchanged. `bookmarkedDraft` prevents comparison with an invalid original query during Apply.
- `frontend/src/lib/features/contents/membership-draft.ts` and `Explorer.svelte`: narrowly expose
  malformed single `disk_id` bookmarks as unapplied, editable drafts without a results request.
  Validate all other parameters with a syntactically valid ID, then restore the exact raw disk
  string. This is validation-only, never resource resolution or dispatch using a replacement ID.
  Label the initial state “Bookmarked draft (not applied),” not Last applied. Selecting/correcting
  the ID navigates through the normal valid URL only on explicit Apply.
- Tests: `frontend/tests/unit/resource-picker.test.ts`, `membership-draft.test.ts`, and
  `frontend/tests/browser/resource-pickers.spec.ts`. Parent plan and this handoff record progress.

## Validation and evidence

From `frontend/`:

- `npm run check`: **0 errors / 0 warnings**.
- `npm run lint`: ESLint and Prettier passed, no suppressions.
- `npm run test:unit`: **23 files / 258 tests passed**, including 14 new U09 cases.
- `npm run api:check`: passed; wire contract unchanged.
- `npm run build`: passed.
- `npx playwright test tests/browser/resource-pickers.spec.ts`: **10 passed**.
- `npx playwright test tests/browser/resource-pickers.spec.ts tests/browser/search.spec.ts
  tests/browser/contents.spec.ts tests/browser/details.spec.ts tests/browser/readable-values.spec.ts
  tests/browser/shell.spec.ts tests/browser/smoke.spec.ts`: **56 passed**, fixture-only hosting.

Coverage includes large string IDs, literal labels, standalone snapshot bookmarks, explicit
draft-only selection, Contents root membership, malformed disk-ID correction, rejected unrelated
invalid bookmarks, capture labels/older scope explanation, lazy loading, empty versus 404,
continuation failure/retry/stale reset, 12-page traversal, retained navigation, disk-change late
responses, disconnect/reconnect draft retention and independent sibling reload.

The browser fixture dispatcher records unexpected endpoints, so these tests also reject accidental
detail reads or comparisons. An initial run exposed missing `disk_id` in test SnapshotPage bodies,
a nullable directory in a test ContentPage body, and the invalid-original-query comparison in the
new correction path. Fixtures and the correction path were fixed; assertions were not weakened.

Chromium desktop, isolated intercepted fixtures, 900px viewport height; no catalog opened.
Six full-page captures and `measurements.json` are written directly to:

`/tmp/opencode/u09-screenshots/{search,contents}-{1440,1024,768}.png`

All six assert document width equals viewport width. Inspected open label/capture choices, older
selection and unknown ID presentation retain full text and reachable controls at narrow widths.
Reproduce with the focused picker suite after building. These are temporary environment-local
inspection evidence, not pixel goldens or durable hosting. Follow [artifact policy](../artifact-policy.md);
no LFS migration or tracked images, and original U01 references remain unchanged.

## Remaining issues and boundaries

- No U09 blocker. Contents still rejects invalid history/bounds combinations, repeated IDs,
  unexpected parameters and cursor bookmarks before mounting controls. This existing broader
  parser/correction limitation is reported, not repaired by the picker work.
- No label is inferred for an unloaded resource. Explicit unknown state and direct canonical ID
  entry are intentional; no existence claim, preload or per-resource background resolution.
- U13 still owns broader shared feedback/pagination polish; U14 owns deployed operation-matrix
  and final integration gates. No real-backend/deployed tests, backend race/benchmarks, catalog
  access or extended accessibility audits were run or claimed.
- Unrelated root/frontend AGENTS work, workspace catalog/lock and `.million-distributions-*`
  remain untouched/unstaged. The approved shared-UI ADR proposal remains optional; no ADR created.
