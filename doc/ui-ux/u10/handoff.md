# U10 — Contents, locations, and observation details

**Complete: 2026-10-11, before U09 integration.** Only the approved U10 package was implemented.
No backend/API/catalog changes, extended audits, new agents or other feature packages.

## Executed plan

1. Retain the existing content, locations, observation, traversal and return-context owners.
2. Prioritize distinct identities, style hash lookup as a secondary panel with a full-width
   monospace digest field, and reuse U07 native advanced filters and replica modes.
3. Group identity/current/history metrics, compact location navigation and clipboard feedback,
   and group observation path/resource/count/date context without inventing current status.
4. Add helper/browser regressions, inspect external captures at the approved widths, run gates,
   and commit explicit task files while leaving unrelated instructions/catalogs unstaged.

All steps completed. The browse workspace remains above lookup; lookup is not collapsed and
still validates an explicitly supplied algorithm/digest. Separate algorithms never imply equality.

## Files and interfaces

- `frontend/src/lib/features/contents/Filters.svelte`: Scope, page limit and Apply stay visible.
  Native advanced disclosure groups Membership and Current redundancy, counts each membership
  restriction, locations metric and active bounds once, and opens for valid bookmarked restrictions.
  Uses U07 `ReplicaBounds`, `boundsMode` and `boundsQuery` directly; no new URL mode parameter.
  Existing route validation now runs immediately on the active draft. History conflicts retain
  editable values; Any deliberately removes active bounds without clearing inactive draft strings.
  Presets preserve their existing explicit current/disks semantics and do not execute requests.
- `frontend/src/lib/features/contents/filters.ts`: content-specific `contentFilterCount` and
  `contentSummary`; root and zero are distinct from absence, and literal membership text survives.
- `frontend/src/lib/features/contents/Explorer.svelte`: applied/last-applied identity plus explicit
  unapplied changes. Existing edits still retire rows/cursors immediately; matching the original
  query restarts the same owner, different queries navigate through the existing ID-based URL.
  Opt-in `data-table`, numeric columns and labeled records at/below 900px preserve identity,
  current catalog-wide counts, scoped distinct disk/path counts and all-inventory observations.
- `frontend/src/lib/features/contents/HashLookup.svelte`: secondary panel, constrained 720px form
  and monospace digest; validation/cancellation/404 handling are unchanged.
- `frontend/src/lib/features/contents/ContentDetail.svelte`: grouped native context links,
  full identity, separate Current copies / History metrics and compact active location scope.
  Summary still requests history independently of the selected location scope. Summary failure
  does not remove successful locations, and locations retry does not reload summary.
- `frontend/src/lib/features/contents/Locations.svelte`: 42/18/25/15-percent wide column shares,
  current/historical badges supplied by each row's `is_current`, preserved selected observation,
  literal path wrapping and named path-first records below 900px. Disk, inventory, observation
  and containing-directory links retain their existing destinations; no per-row reads.
- `frontend/src/lib/features/observations/ObservationDetail.svelte`: path/resource identity/counts
  in a grouped context panel, dates in a compact wrapping group, and explicit observation-versus-
  identity wording. Supplied other-current counts retain their observation-specific exclusion
  semantics; no current/historical badge is inferred from dates or the detail route.
- `frontend/src/lib/components/CopyButton.svelte`: quiet 28px-minimum native action, inline 13px
  success/failure feedback; generation, pending disablement and failure/manual-copy text unchanged.
  Its only current consumers are the U10 content and observation details.
- `frontend/tests/unit/content-filters.test.ts`: three presentation-helper cases.
- `frontend/tests/browser/contents.spec.ts`, `details.spec.ts`: intentional matching-mode locator
  updates plus draft/closed-summary/root/zero/history, widths, table-cell overlap, related-resource
  context and independent locations-retry regressions. Existing behavior assertions retained.
- Parent plan and this handoff record progress. No image or AGENTS file is committed.

### U09 picker integration contract

Replace the **Disk ID label/input within Filters.svelte's Membership fieldset**, retaining the
`disk` string binding and the form's `oninput={retire}` / `onchange={retire}` ownership. If picker
selection updates state programmatically, call `retire()` explicitly once; do not call `apply`,
`resume`, `goto` or restart traversal when opening/loading/selecting options. Apply remains explicit.

Directory uses a separate `useDirectory` flag: enabled empty text is root, disabled means no
membership restriction. Keep directory literal, disk requirement, immediate validation, advanced
counts and closed applied/last-applied summaries. IDs remain canonical URL/API values; labels are
presentation, not replacements for IDs. No preload, automatic exhaustion or per-row resource reads.

**Content-list membership has no snapshot filter in its existing API contract.** Integrate a disk
picker here; do not copy Search's snapshot slot into Contents or change the API. U09's capture-
labeled inventory picker belongs to the already supported Search membership slot. History content
membership and historical observation locations remain distinct from a selected inventory filter.

The existing content URL parser still rejects invalid bookmarks before mounting filters. Invalid
bookmarked history/bounds combinations make no requests and show their explicit error/raw query;
U10 does not broaden route parsing or silently repair them. Valid bookmarks expose advanced values.

## Validation and evidence

From `frontend/`, final gates passed:

- `npm run check`: **0 errors / 0 warnings**.
- `npm run lint`: ESLint and Prettier passed without suppressions.
- `npm run test:unit`: **20 files / 241 tests passed**.
- `npm run api:check`: passed; wire contract unchanged.
- `npm run build`: passed.
- `npx playwright test tests/browser/contents.spec.ts tests/browser/details.spec.ts
  tests/browser/readable-values.spec.ts tests/browser/search.spec.ts
  tests/browser/shell.spec.ts tests/browser/smoke.spec.ts`: **46 passed**.

Coverage includes unknown/zero/int64-maximum sizes and larger-than-Number-precision counts,
historical-only zero current copies, repeated historical observations, exact dates, clipboard
success/failure, lookup validation/malformed failures, independent summary/location retries,
delayed/obsolete responses, stale cursors, bounded paging, reconnect invalidation, search selection,
keyboard detail return/Back/scroll restoration and directory return context through observation.

Initial browser failures were exact label locators including select-option text; tests now use
semantic combobox names. Screenshot inspection caught inherited 40px heights overflowing stacked
cells and global numeric alignment leaking into narrow records. Local height/alignment overrides
and explicit no-cell-overlap checks fix those defects; assertions were not weakened.

Nine Chromium desktop full-page captures (900px viewport height, isolated intercepted fixtures)
are written directly to `/tmp/opencode/u10-screenshots/`:

- `browse-{1440,1024,768}.png`
- `history-{1440,1024,768}.png`
- `observation-{1440,1024,768}.png`
- `measurements.json`

Reproduce after build with the focused contents/details tests above. These are temporary local
inspection evidence, not durable hosting or pixel goldens. Follow [artifact policy](../artifact-policy.md);
no regular-Git images or LFS migration, and U01 before-references remain unchanged.

| Width | History locations origin (caption included) | Document width |
| --- | --- | --- |
| 1440 | 559.16px | 1440px |
| 1024 | 559.16px | 1024px |
| 768 | 699.16px | 768px |

All three scenarios at all widths assert no horizontal document overflow. Browse and history
location cells assert no overlap; inspected narrow records retain every label/value/action.
Origins improve over U01's approximately 1066/1066/1090px but do not reach the aspirational
400–480px wide target with complete history/context labels. Nothing is truncated to meet it.

## Remaining work and constraints

- No U10 blockers. U09 picker integration can proceed sequentially; U13 owns shared feedback/
  paging polish, and U14 owns full deployed operation-matrix/integration gates.
- U05 exact/readable values, literal identities and observation-specific versus content-wide
  replica semantics remain unchanged. No snapshot status or copy guarantee is invented.
- No real-backend/deployed tests, catalog reads, backend race/benchmarks, extended accessibility
  audit or expanded browser support were run or claimed. All fixtures are disposable/intercepted.
- Unrelated root/frontend AGENTS changes, workspace catalog/lock and `.million-distributions-*`
  remain untouched and unstaged. Existing approved-plan ADR proposal remains optional; no new
  architecture decision or automatic ADR was introduced.
