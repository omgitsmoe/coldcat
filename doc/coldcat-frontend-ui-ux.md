# Coldcat frontend UI/UX improvement plan

Status: owner-approved on 2026-10-10. Implementation has not started.

**Direction: a compact, polished technical workspace—not a dashboard makeover.** Keep
information density and keyboard efficiency, but introduce clearer hierarchy, consistent
controls, readable quantities, and task-focused layouts.

This plan is based on the frontend source and existing test documentation, not a live visual
audit. The first task includes browser inspection to validate the proposed dimensions and layouts.

## 1. Problems identified

Paths below are relative to `frontend/src/` unless otherwise noted.

| Issue | Evidence / location | Proposed improvement |
| --- | --- | --- |
| Controls lack a consistent visual system | `app.css` mostly supplies font and button padding; feature styles add their own margins | Shared sizes, variants, spacing, and interaction states |
| Header consumes space without establishing hierarchy | `lib/components/Shell.svelte` stacks brand, navigation, and “Backend ready” text | Compact app bar with active navigation and quiet connection status |
| Search filters dominate the workspace | `lib/features/search/Workspace.svelte` exposes 11 filters plus several explanatory paragraphs | Main search toolbar, common controls, expandable advanced filters |
| Filters require users to know internal IDs | Search and content filters use raw disk/snapshot ID fields | Label-based resource pickers with explicit ID fallback |
| Capacity entry requires exact bytes | `lib/features/disks/DiskForm.svelte` | Exact, unit-aware capacity input |
| All sizes are displayed as raw bytes | `lib/format/decimal.ts` returns formatted integers followed by `B` | Readable units with exact bytes available |
| Disk creation precedes the disk list | `routes/disks/+page.svelte` | List-first page with an explicit “Create disk” action |
| Disk and inventory lists repeat full detail sections | `lib/features/disks/InventoryPages.svelte` | Compact disk table and inventory history list |
| Search rows have oversized, variable-length actions | `lib/features/search/Results.svelte` includes “Select {basename}” buttons | Small, consistent actions with descriptive accessible names |
| Technical explanations compete with primary data | Content, disk, directory, and comparison pages | Short contextual notes; expanded explanations on demand |
| Detail pages are long stacks of definition lists | `ContentDetail.svelte`, `DiskDetail.svelte` | Grouped identity, summary metrics, and related records |
| Each date can introduce another disclosure control | `lib/components/DateValue.svelte` | Compact date presentation with accessible exact-time details |
| Directory summary delays access to entries | `lib/features/directories/Browse.svelte` | Compact summary above the listing; expandable detailed statistics |
| Comparison output is difficult to scan across disks | `lib/features/directories/Coverage.svelte` uses paragraph-heavy lists | Consistent result rows with aligned metrics and explicit status |
| Exact/minimum/maximum filters expose incompatible choices together | Search, content, and directory filters | Explicit “Any / Exactly / Range” control |
| Filter draft and applied state need clearer presentation | Changes currently retire results immediately | Visible “Unapplied changes” state and applied-filter summary |
| Reload and recovery controls are scattered | Shared paging and feature-specific buttons | Consistent result toolbar, status banners, and contextual recovery actions |
| Narrow layouts are inconsistent | Search has a local stacked layout; other pages have separate styling | Shared desktop-narrow behavior and overflow rules |

## 2. Design principles and boundaries

### Visual direction

- Retain the dark technical character.
- Use restrained surfaces, subtle borders, one accent color, and clear typography.
- Use monospace for paths, hashes, IDs, and exact values—not all interface text.
- Favor tables and compact lists over large cards.
- Establish one clear primary action per form or section.
- Avoid decorative charts, excessive rounded panels, and unnecessary animation.

### Proposed control sizing

Validate these during the initial browser review:

- Standard buttons, inputs, and selects: **32px minimum height**.
- Compact row actions: **28px minimum height**.
- Main search input: approximately **36px minimum height**.
- Shared padding, line height, border radius, and focus treatment.
- Consistent height does **not** mean forcing every button to the same width.
- Use fixed dimensions for icon-only controls; retain visible text for important actions.

### Preserve existing correctness

The redesign must not weaken:

- Exact integer arithmetic and signed-64-bit capacity limits.
- Unknown versus zero sizes.
- Declared capacity versus cataloged bytes versus actual disk usage.
- Current versus historical inventory semantics.
- Catalog-wide replica counts despite membership filters.
- Literal paths, labels, hashes, Unicode, and rule text.
- Explicit comparison execution.
- Bounded pagination and cursor invalidation.
- Independent loading/error handling for separate sections.
- Uncertain-write reconciliation and prevention of blind retries.
- Native links, URL filters, keyboard search, and Back restoration.

### Out of scope

- Backend/API changes unless separately approved.
- Upload/import UI, deletion, export, or new catalog operations.
- A new UI framework or broad state-management rewrite.
- Light-theme support or a theme selector.
- Expanded browser/mobile support.
- Extended accessibility audits or manual assistive-technology acceptance gates.

Basic semantics, focus visibility, keyboard usability, and contrast remain implementation
requirements.

## 3. Target page layouts

### Search

1. Compact app bar.
2. Prominent query input with Search and Clear.
3. Common controls: Name/Path, Contains/Exact, Current/History.
4. Advanced filters disclosure, showing an active-filter count.
5. Applied-filter summary and draft-status message.
6. Results table.
7. Consistent pagination/footer controls.

Opening a bookmarked filtered search must expose the relevant filter state; hidden controls
must not conceal why results are restricted.

### Disks

1. Page title and “Create disk” action.
2. Disk table: label, declared capacity, latest capture, inventory availability, open action.
3. Explicit create panel when requested.
4. Helpful empty state explaining CLI-only inventory imports.

Disk detail separates metadata, cataloged inventory statistics, latest inventory, and history.
Editing becomes an explicit mode with clear completion and cancellation.

### Contents

- Distinct-content browsing remains the primary workspace.
- Hash lookup becomes a clearly labeled secondary panel.
- Common filters remain compact; advanced membership and replica controls expand on demand.
- Results retain the distinction between content identities and file observations.

### Directory browser

1. Source inventory context and literal-path breadcrumbs.
2. Compact recursive-size summary.
3. Entries / Exact tree copies / Content coverage view navigation.
4. View-specific controls and results.
5. Expanded statistics and explanations when requested.

Summary totals remain explicitly independent of entry filters.

## 4. Subagent task packages

Each package is a bounded implementation assignment. The context figures below are
**suggested working budgets, not measured guarantees**. Each agent should receive only the
approved plan, project instructions, listed files, relevant tests, and dependency handoff—not
the entire repository history.

Implementation paths below are relative to `frontend/src/lib/` unless otherwise noted.

### U01 — Browser baseline and design specification

**Priority:** P0  
**Dependencies:** None  
**Suggested context:** 30–60k tokens

**Scope**

- Inspect representative screens using disposable fixtures.
- Capture baseline screenshots at approximately 1440, 1024, and 768 CSS pixels.
- Include long paths, large values, unknown metadata, empty results, history, and errors.
- Finalize palette, typography, spacing, control dimensions, and page sketches.
- Inventory repeated patterns needing shared treatment.

**Deliverables**

- Approved visual specification and representative annotated layouts.
- Screenshot baseline and prioritized issue list.
- Shared-component ownership map for subsequent tasks.

**Acceptance**

- Search, disks, content detail, and directory browsing have agreed layouts.
- Compactness does not depend on hiding essential information.
- Proposed changes are separated into presentation-only and behavioral work.

### U02 — Shared visual foundation and controls

**Priority:** P0  
**Dependencies:** U01  
**Suggested context:** 40–70k tokens

**Primary files**

- `frontend/src/app.css`
- `frontend/src/lib/components/`

**Tasks**

- Introduce CSS tokens for colors, spacing, typography, borders, and control heights.
- Define primary, secondary, quiet, and compact button treatments.
- Standardize inputs, selects, textareas, checkboxes, fieldsets, and action groups.
- Provide reusable panel, summary-metric, badge, and table styling.
- Establish loading, disabled, selected, hover, and focus states.
- Avoid global selectors that unintentionally style every navigation or header identically.

**Acceptance**

- Controls align consistently regardless of label length.
- Long labels wrap without clipping.
- Loading labels do not unnecessarily change button dimensions.
- Semantic native elements remain usable; no unnecessary wrapper framework.

### U03 — Application shell and navigation

**Priority:** P1  
**Dependencies:** U02  
**Suggested context:** 25–45k tokens

**Primary files**

- `components/Shell.svelte`
- `frontend/src/routes/+layout.svelte`
- Relevant shell tests

**Tasks**

- Place brand, navigation, and compact connection status in a coherent app bar.
- Add active-section styling, including nested detail routes.
- Make readiness quiet; make outages prominent with the existing retry action.
- Establish shared page width and gutters: wider for tables, constrained for forms.
- Preserve skip link, global search shortcut, and connection ownership.

**Acceptance**

- Header wraps cleanly at narrow desktop widths.
- Connection status is understandable without color alone.
- No feature requests or retries are introduced by navigation styling.

### U04 — Exact unit-aware capacity input

**Priority:** P1  
**Dependencies:** U02  
**Suggested context:** 35–60k tokens

**Primary files**

- `format/decimal.ts` or a dedicated capacity helper
- `features/disks/DiskForm.svelte`
- Capacity unit tests and disk-write browser tests

**Recommended interaction**

- Amount field plus unit selector.
- Units: B, KB, MB, GB, TB, KiB, MiB, GiB, TiB.
- Decimal storage units use powers of 1000; binary units use powers of 1024.
- Visible exact-byte preview.
- Existing arbitrary capacities open in bytes unless a lossless representation is chosen.

**Tasks**

- Convert decimal amounts using string parsing and BigInt arithmetic.
- Accept values such as `1.5 TB` through the amount/unit controls.
- Reject negative values, exponent notation, ambiguous separators, overflow, and fractional-byte
  results.
- Changing the selector changes the entered quantity’s interpretation; update the preview
  immediately and label this clearly.
- Preserve canonical byte-string API payloads.
- Keep unchanged edits unchanged after conversion.
- Associate validation with the capacity field.

**Acceptance examples**

- `1.5` + TB → `1500000000000`.
- `1` + TiB → `1099511627776`.
- Zero and `9223372036854775807` B remain valid.
- Values above the maximum fail explicitly.
- No rounding or floating-point conversion.
- Clearing optional metadata and uncertain-write recovery still work.

Suffix parsing such as `1.5tb` can be a later enhancement; one unambiguous interaction is
preferable to shipping two competing entry mechanisms initially.

### U05 — Shared readable size and date presentation

**Priority:** P1  
**Dependencies:** U02; coordinate with U04  
**Suggested context:** 30–55k tokens

**Primary files**

- `format/decimal.ts`
- `format/date.ts`
- `components/DateValue.svelte`
- New shared size presentation, if needed

**Tasks**

- Add readable byte formatting using exact integer arithmetic.
- Recommend binary units for file/catalog totals; decimal units for declared drive capacity.
- Label approximate human-readable values explicitly through the presentation.
- Keep exact bytes available through accessible details/copy controls.
- Reduce repeated date disclosures while preserving date meaning and exact UTC precision.
- Distinguish Unknown, 0 B, and incomplete known subtotals.

**Acceptance**

- Large sizes can be understood without counting digits.
- Exact values are reachable without relying solely on hover.
- No unknown total is presented as a complete subtotal.
- No timezone or precision is silently lost.

### U06 — Disk creation/editing lifecycle UX

**Priority:** P1  
**Dependencies:** U02, U04  
**Suggested context:** 45–75k tokens

**Primary files**

- `frontend/src/routes/disks/+page.svelte`
- `features/disks/DiskForm.svelte`
- `features/disks/DiskDetail.svelte`
- Disk-write and lifecycle tests

**Tasks**

- Replace the always-open creation form with an explicit create panel.
- Add clear Cancel/Close behavior and dirty-draft confirmation.
- Do not permit cancellation to imply a dispatched write was undone.
- Organize required fields before optional serial/notes.
- Show field-level validation and clear saving/success states.
- Replace the sprawling reconciliation presentation with a focused recovery panel.
- Explain “Reconcile” in plain language: check whether the change was saved.
- Preserve drafts through connection changes and recovery.

**Acceptance**

- Browsing disks is the default.
- Successful creation offers a clear route to the verified disk.
- Closing an edited draft requires deliberate discard.
- Uncertain writes cannot be blindly resubmitted.
- Existing real committed-response-loss tests still pass.

### U07 — Search workspace and result density

**Priority:** P1  
**Dependencies:** U02, U03, U05  
**Suggested context:** 60–100k tokens

**Primary files**

- `features/search/Workspace.svelte`
- `features/search/Results.svelte`
- Search browser and unit tests

**Tasks**

- Implement the proposed search layout.
- Group advanced filters by membership and redundancy.
- Add applied-filter summary and clear unapplied-change state.
- Replace simultaneous exact/min/max entry with an explicit matching mode.
- Keep incompatible history/bounds combinations visibly invalid; never silently discard bounds.
- Replace variable-length row buttons with compact selection controls.
- Align numeric columns and separate basename from secondary path text.
- Preserve full literal paths through wrapping or explicit expansion.
- Consolidate selection actions and keyboard help.

**Acceptance**

- Results appear substantially higher on the page.
- Short-query guidance explains the three-code-point substring rule and Exact alternative.
- Typing, IME handling, selection, Enter, Escape, Back, and restoration remain correct.
- Editing filters does not display old results as if they matched the draft.

### U08 — Disk and inventory browsing

**Priority:** P1  
**Dependencies:** U02, U03, U05, U06  
**Suggested context:** 40–70k tokens

**Primary files**

- `features/disks/InventoryPages.svelte`
- `features/disks/DiskMetadata.svelte`
- `features/disks/DiskDetail.svelte`
- `features/snapshots/InventorySummary.svelte`
- `features/snapshots/SnapshotDetail.svelte`

**Tasks**

- Replace full per-disk metadata articles with compact list rows.
- Present inventory history as capture-ordered rows.
- Group disk-detail statistics and latest-inventory actions.
- Move detailed provenance and long notes into appropriately labeled secondary sections.
- Avoid duplicating latest-inventory detail in both summary and history.
- Keep CLI-only import guidance readily discoverable.

**Acceptance**

- Multiple disks/inventories can be compared without scrolling through full detail blocks.
- Latest/current badges remain based on verified data.
- Declared capacity is never shown as used/free-space progress.

### U09 — Label-based resource filter pickers

**Priority:** P2  
**Dependencies:** U02, U07; coordinate with U10  
**Suggested context:** 45–80k tokens

**Primary scope**

- New shared resource-picker component/helper
- Search and content membership filters
- Existing disk/snapshot read adapters

**Tasks**

- Offer disk labels with IDs as secondary identifiers.
- Load options only when requested, using explicit bounded pagination.
- Retain direct ID entry for expert use and unloaded resources.
- Offer capture-labeled inventory selection for a chosen disk.
- Clearly explain when a selection refers to an older inventory.
- Handle loading, empty, unavailable, and unknown/bookmarked IDs explicitly.

**Acceptance**

- Common filtering no longer requires manually discovering IDs.
- No catalog-wide preload, per-row request pattern, or automatic page exhaustion.
- Existing URL state remains ID-based.
- Opening a picker does not apply filters or execute comparisons.

### U10 — Contents, locations, and observation details

**Priority:** P1  
**Dependencies:** U02, U03, U05; U09 integration afterward  
**Suggested context:** 50–85k tokens

**Primary files**

- `features/contents/*`
- `features/observations/ObservationDetail.svelte`
- Relevant browser tests

**Tasks**

- Make content browsing primary and hash lookup a secondary panel.
- Give the digest input adequate width and monospace styling.
- Reuse the filter layout and replica-bound interaction established by U07.
- Group identity and current/history metrics.
- Improve locations table density and related-resource navigation.
- Make copy feedback compact but explicit.
- Preserve search/directory return context.

**Acceptance**

- Users can distinguish content identity, file location, and historical observation.
- Current copies are not confused with repeated historical records.
- Summary and locations still fail/retry independently.
- Zero, unknown, large values, and clipboard failures remain covered.

### U11 — Directory entries and summary layout

**Priority:** P1  
**Dependencies:** U02, U03, U05, U07  
**Suggested context:** 45–80k tokens

**Primary files**

- `features/directories/Browse.svelte`
- `features/directories/Sizes.svelte`
- `features/directories/Filters.svelte`
- `features/directories/Entries.svelte`

**Tasks**

- Prioritize breadcrumbs and entries.
- Compact recursive and unique-size summaries.
- Expand detailed redundancy statistics on demand.
- Apply shared filters, tables, numeric alignment, and status treatments.
- Establish consistent view-navigation styling.
- Handle long/deep paths without losing literal content.

**Acceptance**

- Entries are visible without traversing lengthy explanatory blocks.
- Summary remains clearly unfiltered.
- Immediate directories stay navigable under file replica filters.
- Historical source/current destination semantics remain explicit.

### U12 — Comparison editor and results

**Priority:** P2  
**Dependencies:** U02, U05, U11  
**Suggested context:** 45–80k tokens

**Primary files**

- `features/directories/RuleEditor.svelte`
- `features/directories/Replicas.svelte`
- `features/directories/Coverage.svelte`

**Tasks**

- Use compact allow/block rows with consistent add/remove actions.
- Move syntax limits and examples into contextual help.
- Show draft/applied rules and explicit comparison status.
- Present destinations in aligned result rows.
- Emphasize complete/partial coverage and covered/missing counts.
- Keep exact tree equality separate from filtered equality and content coverage.

**Acceptance**

- Opening a comparison view makes no comparison request.
- Rule edits retire results immediately.
- Empty selection says “No files selected,” not “No copies.”
- Coverage is never presented as a backup guarantee.

### U13 — Shared feedback and pagination polish

**Priority:** P1  
**Dependencies:** U02; feature integration after U07–U12  
**Suggested context:** 30–55k tokens

**Primary files**

- `components/RequestFeedback.svelte`
- `components/PageControls.svelte`
- Feature integration points

**Tasks**

- Standardize loading, validation, empty, stale, unavailable, and unverified states.
- Consolidate duplicate reload controls.
- Align loaded-count information and pagination actions.
- Maintain stable control dimensions while loading.
- Explain the transition from accumulated results to retained-page navigation concisely.

**Acceptance**

- Loaded count never masquerades as catalog total.
- No fabricated last-page/page-count information.
- Continuation errors preserve valid retained rows appropriately.
- Reconnect/revision handling and explicit reload rules remain unchanged.

### U14 — Integration and visual regression checks

**Priority:** P0 release gate  
**Dependencies:** All approved implementation packages  
**Suggested context:** 50–90k tokens

**Tasks**

- Review representative screenshots at the agreed widths.
- Check control sizing, wrapping, overflow, table density, and hierarchy across all routes.
- Add focused visual regression coverage using deterministic fixtures.
- Update behavior tests for intentional interaction changes—not by weakening assertions.
- Run frontend checks and browser suites sequentially.
- Update frontend documentation with approved conventions and changed interactions.

**Acceptance**

- No accidental page-level horizontal overflow at agreed desktop widths.
- All essential content/actions remain reachable.
- Existing lifecycle and uncertain-write protections pass.
- Full deployed operation-matrix coverage still passes.
- No new performance claims without measurement.
- No extended accessibility audit or expanded browser gate.

## 5. Execution order and parallelization

1. **Approve U01’s visual direction.**
2. Complete U02’s shared foundation.
3. Parallel work:
   - U03 shell.
   - U04 capacity conversion.
   - U05 value presentation.
   - U13 shared feedback/pagination.
4. Complete U06 and U07 to establish form/filter patterns.
5. Parallel feature work:
   - U08 disks/inventories.
   - U10 contents/details.
   - U11 directories.
6. Complete U09 resource pickers and U12 comparisons.
7. Complete U14 integration.

**File ownership rule:** agents must not concurrently edit the same file. In particular,
sequence U04/U06 changes to `DiskForm.svelte`, coordinate U04/U05 decimal helpers, and merge
U09 into search/content filters after their layout tasks finish.

Every handoff should include:

- Changed files and shared interfaces.
- Tests run and results.
- Screenshots where relevant.
- Preserved semantic/lifecycle constraints.
- Remaining issues and decisions.

Split any package approaching roughly 150k tokens rather than relying on the full 250k window.

## 6. Verification

From `frontend/`:

```sh
npm run check
npm run lint
npm run test:unit
npm run api:check
npm run build
```

Run focused browser tests during each task. At integration, run these **sequentially**:

```sh
npm run test:e2e -- 'tests/browser/(?!accessibility\.spec\.ts$)[^/]+\.spec\.ts$'
npm run test:real -- 'tests/real/(?!accessibility\.spec\.ts$)[^/]+\.spec\.ts$'
npm run test:deployed -- 'tests/real/(?!accessibility\.spec\.ts$)[^/]+\.spec\.ts$'
```

Use existing disposable catalog harnesses; never touch the workspace catalog. Backend code
changes are not anticipated, so backend benchmark/race work is not part of this redesign.

## 7. Approved decisions

The owner approved these defaults together:

1. **Compact dark technical workspace**, without a theme/framework expansion.
2. **Amount + unit selector** for capacity; suffix parsing deferred.
3. **Readable sizes with exact values available**, binary for files and decimal for declared
   capacity.
4. **List-first disks and progressively disclosed advanced filters.**
5. **Existing lifecycle and correctness rules remain mandatory.**

Consider recording the capacity/unit conventions and shared UI approach in a short ADR if
they become architectural policy; no ADR is created automatically.
