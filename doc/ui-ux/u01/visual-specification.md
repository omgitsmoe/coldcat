# U01 — Browser baseline and visual specification

**Status: specification ready for owner review; approval pending.**
The overall improvement plan was approved on 2026-10-10; this specific visual specification
has **not** been approved. U02 must wait for that decision. This task changes documentation
and capture tooling only, not the application, API, or request lifecycle.

## Evidence and limits

Inspection date: 2026-10-10. Application source revision:
`71d26143c6aa81df07a2bbd046d87ae93e23b438` (no application changes in U01).

The existing `tests/serve-built.ts` host served a freshly built UI. Isolated Playwright
contexts intercepted health/catalog/feature reads using the existing typed browser fixtures,
extended in `frontend/tests/visual/u01-baseline.spec.ts`. No SQLite catalog was opened or
modified. This is browser-rendered fixture evidence, **not** real-backend or production
hosting acceptance. All contexts use Chromium, en-US, UTC, scale factor 1, and a 900px-high
viewport. Full-page captures intentionally include content below the fold.
Search captures explicitly focus the query field at every width for a consistent focus-state
reference; this does not assert that the application auto-focuses at every width.

The [manifest](baseline/manifest.json) records routes, browser version, dimensions and table
positions for **18 captures: six scenarios at each of 1440, 1024, and 768 CSS pixels**.
No pixel-golden assertions are introduced: these are the _before_ reference, not the desired
appearance. OS/font changes may change rasterization; do not bless regenerated images silently.

| Scenario        | 1440                                       | 1024                                       | 768                                       | Stress represented                                                                                        |
| --------------- | ------------------------------------------ | ------------------------------------------ | ----------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Search          | [PNG](baseline/search-1440.png)            | [PNG](baseline/search-1024.png)            | [PNG](baseline/search-768.png)            | Long path/name, Unicode and literal punctuation, signed-64-bit maximum bytes, Unknown, 0 B, unknown mtime |
| Disks           | [PNG](baseline/disks-1440.png)             | [PNG](baseline/disks-1024.png)             | [PNG](baseline/disks-768.png)             | Long literal label, maximum declared capacity, zero capacity, empty serial/notes, no complete inventory   |
| Content detail  | [PNG](baseline/content-history-1440.png)   | [PNG](baseline/content-history-1024.png)   | [PNG](baseline/content-history-768.png)   | Unknown size, full digest, long path, current versus historical observation, precise capture timestamp    |
| Directory       | [PNG](baseline/directory-history-1440.png) | [PNG](baseline/directory-history-1024.png) | [PNG](baseline/directory-history-768.png) | Deep breadcrumbs, historical source/current destinations, incomplete sizes and large known subtotal       |
| Search empty    | [PNG](baseline/search-empty-1440.png)      | [PNG](baseline/search-empty-1024.png)      | [PNG](baseline/search-empty-768.png)      | Successful zero matches, explicit history suggestion, loaded count                                        |
| Directory error | [PNG](baseline/directory-error-1440.png)   | [PNG](baseline/directory-error-1024.png)   | [PNG](baseline/directory-error-768.png)   | Summary-only HTTP 500; entries remain successful with independent recovery                                |

Visual inspection covered representative search, disks, content, directory, empty and error
captures, including narrow desktop examples; every capture has measured document dimensions.
The state matrix is deliberately small. It does not claim visual coverage of every route,
loading phase, uncertain write, paging transition, comparison result, or connection outage.
Those remain existing behavior tests and later U14 integration work. No extended accessibility
audit, manual AT gate, mobile support, throughput or latency measurement was performed.

## Prioritized observed issues

Coordinates below are document CSS pixels from a 900px-high viewport, not performance results.
The table origin includes its caption; the data rows start lower.

| Priority | Observation / evidence                                                                                           | Treatment / owner                                                             |
| -------- | ---------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| P0       | Shared header is 143px tall at all three widths although there are only three navigation links                   | One compact app bar, quiet readiness; U03                                     |
| P0       | Search table starts at y=673/767/791 (1440/1024/768); long-name selection button competes with path width        | Common toolbar + disclosed advanced fields; bounded row action labels; U07    |
| P0       | Directory table starts at y=1641/1725/1773; the summary, histogram and filter prose precede entries              | Keep essential totals/context visible; detailed statistics secondary; U11     |
| P0       | Content locations begin at about y=1066 at 1440/1024 and y=1090 at 768                                           | Group identity and current/history metrics horizontally before locations; U10 |
| P1       | Disk creation consumes most of the first viewport; just two disks produce a roughly 1956px document at 1440/1024 | List-first rows, explicitly opened create panel; U06/U08                      |
| P1       | Native light fields/buttons sit on a dark page; inputs/selects/buttons have different heights                    | Shared dark control surfaces, dimensions and focus states; U02                |
| P1       | Maximum size is shown as `9,223,372,036,854,775,807 B`, taking width away from paths                             | Approximate readable value + reachable exact bytes; U05                       |
| P1       | At 768, a long search path becomes many lines while repeated capture text and actions consume columns            | Path-first two-line record grouping at narrow desktop widths; U07/U10/U11     |
| P1       | Summary error displays both Retry and Reload summary adjacent to each other                                      | One contextual recovery action, without reloading entries; U13/U11            |
| P2       | Dates introduce repeated Exact UTC disclosures; serial/notes appear even when empty                              | Compact shared dates; secondary metadata sections; U05/U08                    |

**Keep what works:** Unknown is distinct from 0 B; incomplete directory totals explicitly say
Unknown and preserve known subtotals; historic-source/current-replica wording is present;
literal paths survive wrapping; native links remain available; the error leaves entries usable.
Measured document width equals viewport width in all 18 captures. This does not establish
that every arbitrary label/path or other route is overflow-safe.

## Proposed foundation (owner decision required)

These are implementation-ready defaults to review, not tokens already shipped. The browser
review supports reducing chrome and increasing path width. Proposed 32/28/36px controls have
**not been prototyped**; U02/U14 must validate them with real controls, wrapped labels and
visible keyboard focus rather than forcing labels into a fixed-height box.

### Palette

| Proposed token                                | Value                 | Purpose                                       |
| --------------------------------------------- | --------------------- | --------------------------------------------- |
| `--color-bg`                                  | `#111820`             | Page background                               |
| `--color-surface`                             | `#18222d`             | Toolbar/form/summary background               |
| `--color-raised`                              | `#202c39`             | Hover/disclosure surface                      |
| `--color-text`                                | `#e7edf5`             | Primary text                                  |
| `--color-muted`                               | `#acb9c8`             | Secondary labels; never hide uncertainty      |
| `--color-border`                              | `#344352`             | Decorative separators/table rules             |
| `--color-control-border`                      | `#71869c`             | Detectable control boundaries                 |
| `--color-accent`                              | `#a5d4ff`             | Links, selected navigation, accent border     |
| `--color-primary-bg` / `--color-primary-text` | `#9acbfa` / `#102031` | Single primary action                         |
| `--color-focus`                               | `#ffcb69`             | 2px focus outline with 2px offset             |
| `--color-success`                             | `#85d5ab`             | Verified/saved text plus explicit label       |
| `--color-warning`                             | `#ffcb69`             | Draft/unverified/incomplete text plus label   |
| `--color-error`                               | `#ffb1ad`             | Error text and border plus explanation/action |

One action accent, not multicolored panels. Status colors supplement words, never replace
them. Do not use the decorative separator color as the only control boundary. U02 must
check contrast in the actual states (including focus/hover/disabled), without introducing
an extended accessibility gate. No gradients, charts, animated decoration or light theme.

### Type, spacing and dimensions

| Element                          | Default                                                                              |
| -------------------------------- | ------------------------------------------------------------------------------------ |
| UI text                          | Existing system-ui stack, 14px / 20px; labels 600 weight                             |
| Page title                       | 24px / 32px, 650–700 weight                                                          |
| Section heading                  | 18px / 24px, 600 weight                                                              |
| Secondary text                   | 13px / 20px; no essential information below 13px                                     |
| Literal path/hash/ID/exact value | `ui-monospace, SFMono-Regular, Consolas, monospace`, 13px / 20px                     |
| Quantities                       | Tabular figures; numeric columns right aligned                                       |
| Spacing scale                    | 4, 8, 12, 16, 24, 32px; section gap 24, control gap 8, label gap 4                   |
| Standard input/select/button     | Minimum 32px high, 8–12px horizontal padding                                         |
| Main search input                | Minimum 36px high, flexible width                                                    |
| Compact row action               | Minimum 28px high, 8px horizontal padding                                            |
| Checkbox                         | Native semantic control; label click target occupies at least 28px high              |
| Radius/border                    | 4px / 1px; no rounded-card dashboard treatment                                       |
| Table                            | 6px vertical / 8px horizontal cell padding, minimum 40px row; taller when text wraps |
| App bar                          | Minimum 56px; content-driven wrapping, not forced clipping                           |
| Page gutters                     | 24px at 1440/1024; 16px at 768                                                       |
| Workspace width                  | Fluid tables up to 1600px; form body at most 720px                                   |

Standard controls share heights, **not widths**. Keep text for Search, Apply, Create, Save,
Cancel, Retry and pagination. No new icon library is required. Quiet actions still have
discernible hover/focus states. Loading text should fit a reserved minimum width, but labels
may wrap and controls grow. Disabled state is semantic and legible; do not dim the whole panel.

### Shared layout/overflow rules

- 1440: full tables and horizontally grouped controls; spare width belongs to paths, not cards.
- 1024: wrap toolbars at field-group boundaries; keep common controls visible, not in a drawer.
- 768 (desktop-narrow, not a mobile commitment): query/actions and common controls occupy
  separate lines; metrics wrap to two columns; forms remain one column. Below 900px, use
  path-first grouped records for search/location/directory rows, with named secondary values
  underneath instead of compressing five columns. Preserve table semantics or equally explicit
  record labels; do not simply conceal headers or drop actions.
- Grid/flex children need `min-width: 0`; paths/digests use literal whitespace and
  `overflow-wrap: anywhere`. Never normalize slashes, case, spaces, Unicode or punctuation.
- Full paths wrap by default in result records; **no automatic ellipsis or line clamp**.
  Tall pathological rows are preferable to hidden identity in this first implementation.
- Breadcrumbs wrap only with full literal segments reachable. No tooltip-only exact values.
- No new sticky header/table behavior in this pass. Normal document scrolling and native links.

## Annotated target layouts

The boxes below describe order and ownership, not individual bordered cards. Each numbered
area is explained immediately below. The compactness goals apply to the captured fixtures
with advanced/details closed, not arbitrary path lengths or error/draft states. These are
proposed review targets, not measured post-redesign results.

### Search — U07

```text
┌ 1 Coldcat  Search*  Contents  Disks                         Ready ┐
│ 2 Search                                                      │
│   [ Filename or path ………………………………………… ] [Search] [Clear]       │
│ 3 [Name/Path] [Contains/Exact] [Current/History] [Advanced (n) ▾] │
│ 4 Applied: …        Unapplied changes — [Apply filters]         │
│ 5 File / literal path      Disk · capture   Size   Copies Action│
│   basename link                                               │
│   full/path/wraps           inventory badge …      …    [Select]│
│ 6 3 loaded   [Load more / Previous / Next]             [Reload] │
└───────────────────────────────────────────────────────────────┘
```

1. Shared shell, active section not a new feature request. 2. Query remains primary, existing
   type-to-search/IME/Enter/Escape behavior is preserved. 3. Common controls never disappear;
   advanced membership and redundancy fields are grouped within a native disclosure.
2. Applied state describes what rows matched; draft state explicitly retires rows, not a
   misleading stale match. Open relevant advanced fields on bookmarked filters and show their
   summary/count while closed. No silently discarded history/bounds combination.
3. Path gets about half the wide table; compact Select label uses a descriptive accessible
   name. Observation/directory links remain real links. Caption retains catalog-wide counts.
4. Loaded count is not a total; existing bounded traversal/reload rules remain intact.

At 1024 controls wrap once; at 768 query/actions sit above common controls, and each result
groups path first, then labeled disk/capture/size/copies and actions. Target default table
origin: around 320–400px at 1440 (versus baseline 673px). Advanced opening may increase it.
Empty results occupy area 5 with query guidance, never imply a failed request or auto-switch
to history. Validation and recovery sit adjacent to the relevant query/results.

### Disks — U06/U08

```text
┌ 1 Shared app bar ──────────────────────────────────────────────┐
│ 2 Disks                                         [Create disk] │
│ 3 Label + ID    Declared capacity   Latest capture  Availability│
│   Archive      ≈ … TB [Exact]       date            [Open]      │
│   Uncataloged  0 B                 — No inventory   [Open]      │
│ 4 loaded count / paging / contextual reload                    │
│ 5 Create disk (only when opened; max 720px form body)           │
│   Label                                                       │
│   Amount [           ] Unit [TB ▾]  Exact bytes: …             │
│   Optional serial / notes                                     │
│   [Create disk] [Cancel]                                      │
│ 6 CLI-only import guidance / focused write-recovery panel      │
└───────────────────────────────────────────────────────────────┘
```

2. Explicit open action; no modal requirement. 3. List-first table; open disk label is a
   native link. No declared-capacity progress bar, used/free arithmetic, or fabricated root
   for an uncataloged disk. Exact capture UTC remains reachable.
3. Form follows the list; opening it brings its heading into view. Required label/capacity
   before optional metadata. Unit selector reinterprets quantity; preview explains it. Do not
   round an existing byte capacity. Dirty-draft cancellation and pending writes belong to U06.
4. Unknown write outcome has one “Check whether saved” action; Cancel cannot undo a dispatched
   write. Keep draft/reconciliation protection and CLI-only import guidance.

At 1024 the same columns wrap label text. At 768 each row groups label/action above capacity,
capture and availability. Disk detail (U08) uses metadata, cataloged subtotal/completeness,
latest capture and history rows as separate sections; editing is explicit. Empty list offers
Create and CLI import explanation, not an import control. Target list starts within the first
300px at 1440, instead of the baseline first disk article below most of the initial viewport.

### Content detail — U10

```text
┌ 1 Shared app bar ──────────────────────────────────────────────┐
│ 2 Contents / Content 1                  Return to search/dir  │
│ 3 sha256  full literal digest (wraps)              [Copy]      │
│   Size: Unknown                                               │
│ 4 Current: 1 disk · 2 locations                                │
│   History: 1 distinct disk · 2 disk/path locations · 5 records  │
│ 5 Locations    [Current locations] [History*]        [Reload]  │
│   History includes repeated observations, not current copies. │
│ 6 Path                     Disk/inventory Size/capture Actions │
│   literal path (wraps)      Historical    Unknown …    links    │
│ 7 loaded count / bounded paging                               │
└───────────────────────────────────────────────────────────────┘
```

2. Preserve return and selected-observation context; selection is independent of location
   scope/pages. 3. Copy feedback stays inline; failure is explicit. Summary failures never
   remove successful locations. Exact size/UTC detail uses labeled native disclosure.
3. Two clearly labeled groups, not an ambiguous “copies” total or decorative metric cards.
4. Location scope does not change the history-backed identity summary. 6. Current/historical
   marker is per observation, not inferred from which tab was clicked.

At 1024 the metrics can wrap; at 768 digest/copy wrap and current/history groups stack,
locations use path-first records. Target default locations origin around 400–480px at 1440,
versus baseline 1066px. Identity is never shortened solely to meet that target. Failure/loading
banners remain scoped separately to identity and locations with independent retries.

### Directory browser — U11/U12

```text
┌ 1 Shared app bar ──────────────────────────────────────────────┐
│ 2 Directory · Inventory 1 · Historical source · captured date │
│   Root / archive / … full literal breadcrumb segments          │
│ 3 All descendants (unfiltered): 3 files / 3 contents            │
│   File bytes: Unknown; known subtotal ≈ … EiB + 1 unknown      │
│   Unique bytes: Unknown; known subtotal ≈ … EiB + 1 unknown    │
│   [Detailed statistics ▾]                         [Reload]    │
│ 4 [Entries*] [Exact tree copies] [Content coverage]             │
│ 5 [Recursive] [Metric] [Any/Exactly/Range] [Advanced ▾] [Apply] │
│   Replicas: current destinations; source disk excluded         │
│ 6 Path / kind         Size        Current other replicas Links│
│   child directory     Unknown     navigation entry       Open  │
│   literal file path   Unknown     0 disks · 1 location    links │
│ 7 loaded count / bounded paging                               │
└───────────────────────────────────────────────────────────────┘
```

2. Source inventory/historical state and literal breadcrumb context always visible.
3. Completeness, known subtotal and unknown count cannot be hidden under statistics. Distinguish
   recursive occurrence bytes from unique-content bytes; neither is disk allocation/free space.
   Redundancy histogram and provenance may expand, but “unfiltered” remains visible.
4. Opening a comparison makes **no** comparison request. Rule editing/execution stays U12;
   exact equality, filtered equality and coverage remain distinct.
5. Entry filters do not change summary totals. Child directories remain navigable under file
   replica filters. 6. Show historical source/current destination semantics without repeating
   the full paragraph in every row. A summary error replaces only area 3 with one Retry.

At 1024 summary rows can wrap; at 768 breadcrumbs wrap, metrics form two columns or stack,
and entries use path-first records. Target default entries origin around 480–560px at 1440
for this deep-path fixture, versus baseline 1641px. Empty filtered entries explicitly do not
imply an empty recursive summary. Comparisons preserve “No files selected” versus “No copies”
and never claim a backup guarantee.

## Presentation versus behavior

| Presentation-only (request semantics unchanged)                                             | Behavioral work requiring focused tests                                                                     |
| ------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| Tokens, typography, dark fields, control minimum heights, wrapping, numeric alignment (U02) | None merely from restyling; avoid global nav/header selectors                                               |
| Shell grouping, active route, quiet readiness (U03)                                         | Keep shortcut/connection ownership unchanged                                                                |
| Readable sizes/dates, grouped exact details (U05)                                           | Exact integer formatter and disclosure/copy reachability; unknown/zero/completeness/UTC tests               |
| List rows, grouped detail metrics, path-first narrow records (U08/U10/U11)                  | Preserve independent requests, return context, pagination and current/history semantics                     |
| Shared banners/toolbar spacing (U13)                                                        | Retry consolidation must preserve scoped owner, retained rows and invalidation rules                        |
| Advanced/help disclosures                                                                   | U07/U10/U11: bookmarked filter visibility, active summaries, draft retirement and incompatible combinations |
| Compact Select text                                                                         | U07: accessible name, selection, keyboard/IME and Back restoration                                          |
| Create/edit panel appearance                                                                | U06: explicit open/close, deliberate draft discard, pending/uncertain writes and reconciliation             |
| Capacity field appearance                                                                   | U04: exact amount/unit arithmetic, bounds, clearing, lossless edits and preview                             |
| Resource picker appearance                                                                  | U09: bounded on-demand loading, ID fallback, unavailable/bookmarked resources; no automatic apply           |
| Comparison result alignment                                                                 | U12: explicit execution, immediate retirement after edits, empty-selection semantics                        |

No backend/API changes or state-management rewrite are authorized here. Do not equate a visual
move with permission to combine requests, auto-run a comparison, preload a catalog, reinterpret
paths, or silently alter draft/apply behavior.

## Shared ownership and handoff interfaces

Paths below are relative to `frontend/src/lib/` unless otherwise specified. Names for new
components are proposed; use native elements/CSS classes where a wrapper adds no value.

| Single owning task | Files / shared surface                                                                         | Consumer contract and sequencing                                                                                                                         |
| ------------------ | ---------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| U02                | `frontend/src/app.css`, shared control/panel/table classes; optional `SummaryMetric` / `Badge` | Publish token names above, standard/compact/primary/quiet variants, literal/numeric classes; no request hooks                                            |
| U03                | `components/Shell.svelte`, `frontend/src/routes/+layout.svelte`                                | Own app bar, gutters and active-section mapping; feature pages do not create competing shells                                                            |
| U04                | Capacity parser/helper, `features/disks/DiskForm.svelte`                                       | Canonical byte-string input/output; string/BigInt only; land before U06 edits same form                                                                  |
| U05                | `format/decimal.ts`, `format/date.ts`, `components/DateValue.svelte`, optional `SizeValue`     | Input decimal string or null; readable unit, marked approximation, exact accessible text; no Number conversion; coordinate U04 helper edits sequentially |
| U06                | Disk form lifecycle, disks route create panel, explicit disk edit mode                         | U08 consumes verified completion and metadata display; U06/U08 sequence `DiskDetail.svelte` edits                                                        |
| U07                | Search workspace/results; initial shared filter layout and matching-mode contract              | Draft/applied summary and `Any/Exactly/Range`; U10/U11 reuse after this task, no raw URL changes                                                         |
| U08                | Disk metadata/list/history, inventory and snapshot summary layout                              | Use U05 values, U13 feedback/paging; no new per-row requests                                                                                             |
| U09                | Shared resource picker/read helper; later search/content integration                           | ID-based value, label+ID display, explicit bounded next page, direct ID entry; no apply side effects; integrate only after U07/U10 layouts               |
| U10                | `features/contents/*`, observation detail                                                      | Own identity/locations and return context; use U07 filter convention, U05 value contract                                                                 |
| U11                | Directory Browse/Sizes/Filters/Entries                                                         | Own view host/context and unfiltered summary; delegate comparison editor/results to U12                                                                  |
| U12                | Directory RuleEditor/Replicas/Coverage                                                         | Explicit comparison execution; use U11 view host, own rule draft/results lifecycle                                                                       |
| U13                | `components/RequestFeedback.svelte`, `components/PageControls.svelte`                          | Keep status/error/retry inputs and paging flags/callbacks explicit; styling must not infer totals or own requests; feature owners integrate              |
| U14                | Focused visual regression/integration tests                                                    | Reuse U01 routes/stress matrix; do not compare new UI against old pixel goldens                                                                          |

**Execution constraint:** the user wants one agent at a time. The plan's parallel opportunities
are ownership guidance only; perform subsequent packages sequentially. No two tasks should
edit `DiskForm`, `DiskDetail`, decimal helpers, search/content filters, or directory Browse
simultaneously. Reusable filter design belongs to U07, value presentation to U05, feedback
to U13; a feature task should not independently invent replacements for those surfaces.

## Capture workflow and verification

From `frontend/`, run sequentially (Playwright suites share `test-results/`):

```sh
npm run build
npx playwright test --config playwright.u01.config.ts
```

Output: `frontend/test-results/u01-baseline-capture-U01-representative-browser-baseline/baseline/`.
It includes all PNGs and `manifest.json`. A separate config keeps this inspection tool out of
ordinary browser/real/deployed suites and their operation matrix. It starts/stops the existing
static host, intercepts all API reads, fails on unexpected endpoints or page exceptions, waits
for representative content plus readiness, and asserts successful entries despite summary
failure. No new browser installation or global executable override is needed.

To intentionally replace the tracked evidence, inspect the fresh output first, then copy only
its PNGs and manifest to `doc/ui-ux/u01/baseline/`. Never run this copy automatically in ordinary
tests; future visual changes must not erase the pre-redesign record. There are only 18 images,
no video, trace, duplicate viewport crop or catalog files.

U01 validation results:

- `npm run build`: passed.
- Capture: passed, one test exercising all 18 scenario/width combinations; no unexpected API
  calls or page exceptions. Initial tool-development failures exposed an invalid nullable disk
  capacity fixture and incorrect expectations for a file-path link/error message. Fixed the
  fixture/assertions rather than changing application behavior or suppressing checks.
  A later automatic-focus expectation failed at narrow width; capture now explicitly focuses
  the query instead of making an unsupported application auto-focus claim.
- `npm run check`: passed, 0 errors/warnings; `npm run lint`: passed.
- `npm run test:unit`: passed, 15 files / 143 tests.
- Tracked artifact check: all 18 PNG header dimensions match the manifest; every measured
  document width equals its viewport width.
- `npx playwright test tests/browser/search.spec.ts tests/browser/inventories.spec.ts
  tests/browser/details.spec.ts tests/browser/directories.spec.ts`: passed, 39 tests, run after
  preserving the capture artifacts and sequentially with the capture suite.
- Real-backend/deployed/full integration suites and backend race/benchmarks are not U01 gates.

## Required owner approval — parent prompt

Ask the owner to review this document and the linked images before starting U02:

1. **Approve or revise this palette/type/density proposal?** Recommended: the token table,
   14px UI text and 32/28/36px minimum controls. Alternative: retain 16px body text while keeping
   the same hierarchy; row heights will grow. Do not claim either has been user-tested.
2. **Approve the four annotated page layouts and narrow record grouping?** Recommended: list-first
   disks, visible essential totals, advanced/details disclosures, and path-first records below
   900px. Alternative: keep a locally scrollable table at 768px rather than grouped records;
   that trades vertical density for horizontal navigation, without hiding columns.
3. **Approve full-path wrapping and labeled exact-value disclosures?** Recommended: never clamp
   paths; show readable approximate sizes with exact bytes/UTC reachable by keyboard. Alternative:
   an explicit per-row expansion design needs a separately agreed interaction before implementation.

An explicit “Approve U01 as proposed” resolves these choices; otherwise revise the specification
and resubmit. Record the owner's actual response/date in the plan; **do not infer approval**
from the earlier general plan approval or this commit. U01's approved-specification/agreed-layout
acceptance remains open. If the unit/value or shared-UI conventions become architecture policy,
offer a small ADR (decision, context, alternatives, consequences); none is created automatically.
