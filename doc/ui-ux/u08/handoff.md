# U08 — Disk and inventory browsing

**Complete: 2026-10-11.** Only U08 of the approved UI plan. No blockers.

## Executed plan

1. Keep existing independent disk/history requests and bounded traversal unchanged.
2. Replace repeated detail articles with compact disk and capture-ordered history tables.
3. Group metadata/cataloged metrics and latest actions; disclose notes and provenance.
4. Validate frontend checks, focused behavior and narrow rendering; publish external evidence.

## Files and interfaces

- `frontend/src/lib/features/disks/InventoryPages.svelte`: props and request owners unchanged.
  Disk rows use embedded latest capture, decimal declared capacity, label/ID, availability and
  native inventory/root links. No per-row requests. History rows preserve API capture order and
  display capture, files/contents and native actions, not full provenance/import details.
  Latest/historical badges require `verifiedLatest` plus successful traversal; no inference from
  row position, import time or inventory ID. Metadata failure leaves history available without
  badges. Existing retained/unverified feedback, stale restart and bounded paging are unchanged.
  CLI-only import guidance is visible for both populated and empty results.
- `frontend/src/lib/features/disks/DiskMetadata.svelte`: same `item` contract, grouped label,
  declared capacity and serial, with native **Disk notes** disclosure. Literal values wrap fully;
  no ellipsis. This component is also consumed by U06's refreshed-metadata disclosure.
- `frontend/src/lib/features/disks/DiskDetail.svelte`: cataloged quantities use shared metrics;
  known subtotal, Unknown total, completeness and unknown-size counts stay visible. Latest
  section provides capture and **Browse root / Open latest inventory {id}** actions, not a second
  full inventory summary. History retains the latest row for ordered comparison. Selection is
  capture time then ID, not import time; cached selection is explicitly unverified. Metadata,
  cataloged logical bytes and declared capacity remain distinct from used/free space.
- `frontend/src/lib/features/snapshots/InventorySummary.svelte`: existing props unchanged.
  Capture and grouped counts precede primary Browse root. With `provenance`, imported date,
  state, capture source, literal backend input path/format and date-meaning guidance are in
  **Import and capture provenance**. Backend input paths are not browser file links.
- `frontend/src/lib/features/snapshots/SnapshotDetail.svelte`: no read lifecycle changes;
  latest/current status requires opening the disk rather than guessing from standalone detail.
- `frontend/tests/browser/inventories.spec.ts`: table selectors replace intentional article
  selectors without dropping paging/order/literal assertions. Notes/provenance are explicitly
  opened. New tests cover history surviving metadata failure without latest badges and the
  three approved widths with long labels/notes/backend paths, zero capacity, unknown subtotal,
  native links, document overflow and cell overlap.
- `frontend/tests/real/core.spec.ts`: adapted selectors, deliberate secondary disclosures and
  named latest link for U14's real/deployed workflows. These tests were **not run** by U08.
- `doc/coldcat-frontend-ui-ux.md` and this handoff: progress and ownership/validation record.

### Responsive table contract

Wide layouts retain native tables with captions and column headers. At/below 900px, each row
groups identity first, then labeled secondary cells in two columns; no data/actions are hidden.
The visually hidden header remains semantic. Each cell has `data-label` for visible narrow
labels. Grid cells explicitly use `height: auto; min-height: 40px` to override shared table
heights: fixed cell heights otherwise let long literal content overlap following rows. This is
feature-scoped CSS, not a global table rewrite. Shared U02/U03/U05 interfaces remain intact.

## Validation

From `frontend/`:

- `npm run check`: 0 errors/warnings.
- `npm run lint`: ESLint/Prettier passed, no suppressions.
- `npm run build`: passed, including final responsive/provenance changes.
- `npm run test:unit`: 19 files / 238 passed.
- `npm run api:check`: passed, no wire drift.
- `npx playwright test tests/browser/inventories.spec.ts tests/browser/disk-writes.spec.ts
  tests/browser/readable-values.spec.ts tests/browser/details.spec.ts`: **51 passed**.
  Includes all 22 U06 write/lifecycle checks, ten inventory checks, exact values/date precision,
  independent loads/retries, stale/disconnected traversal and bounded retained pages.

Initial browser execution caught a new test expecting backend message text that the API client
does not expose for HTTP 500; it now asserts the actual explicit HTTP failure. Screenshot
inspection caught narrow fixed-height cell overlap despite zero document overflow; corrected
with auto cell height and a pairwise cell-bound assertion. Final focused suite passed in full.
No assertions or lint rules were disabled.

## External screenshots

Nine fixture-only Chromium full-page captures (900px viewport height) are at
`/tmp/opencode/u08-screenshots/{state}-{width}.png`: states `list`, `detail`, `provenance`;
widths 1440, 1024, 768. Detail captures open long notes; provenance captures open literal
backend source. Wide/narrow list and narrow detail/provenance were visually inspected.
All nine assert document width and nonoverlapping table cells. Disk list caption begins around
193px in the inspected wide/narrow captures, within the approved first-300px target.

Reproduce after build with the focused browser command above; captures write directly outside
the checkout. No images are committed or pixel goldens replaced. These environment-local paths
are temporary evidence, not durable hosting. See [artifact policy](../artifact-policy.md);
Git LFS remains unavailable and no migration/storage claim is made.

## Constraints and next owners

- U06's mounted editing owner, `closed` contract, guarded discard/cancel and uncertain-write
  reconciliation are unchanged. Read failure/revision cannot remount or bypass the form.
- Current/latest is verified metadata, not first row/import time. History order and ID tie-break
  come from the existing API; no client sort, catalog preload, auto-exhaustion or new request.
- Unknown versus zero and incomplete subtotals remain explicit; no capacity usage/progress
  arithmetic, upload/import UI, backend/API changes or workspace-catalog operations.
- No other agents, extended audits, backend tests/benchmarks, real/deployed/full matrix checks.
  U14 still owns integration; U13 owns shared feedback/pagination polish. U09 is next sequential
  package. Native navigation and context URLs remain unchanged.
- Unrelated `frontend/AGENTS.md`, untracked root instructions/catalogs/locks and measurement
  directories are untouched and unstaged. No new command-learning instruction edits are needed.
- No new architecture beyond approved presentation; optional shared UI/unit ADR remains
  proposed in the parent plan, not automatically created.
