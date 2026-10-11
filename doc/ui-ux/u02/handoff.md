# U02 — Shared visual foundation handoff

**Complete: 2026-10-11.** U01 was explicitly owner-approved on 2026-10-10 and the owner
directed continuation. Approved defaults need no new approval; packages remain sequential.
Optional user-feedback tests are deferred. No required blockers remain for U03.

## Files and shared interfaces

- `frontend/src/app.css`: approved U01 palette token names, typography/spacing/radius/border
  tokens, 32px standard / 28px compact / 36px search minimum heights. Native button, input,
  select, textarea, checkbox/radio and fieldset treatment; visible keyboard focus, hover,
  pressed/selected, invalid, disabled and busy states. Heights are minimums, not clipping boxes.
- `frontend/src/lib/components/Shell.svelte`: adds `shell-header` / `shell-main` hooks only.
  Header padding/border and navigation flex styles are scoped; unrelated headers/navigation
  are not styled. Connection ownership, status text, request timing and skip link stay intact.
  U03 owns compact app-bar composition, active sections, workspace widths and narrow gutters.
- `frontend/src/lib/components/PageControls.svelte`: `page-controls` replaces the old global
  navigation styling; `page-load` reserves 10rem for Load more / Loading page… without changing
  callbacks, conditions, counts or pagination semantics. U13 still owns feedback/paging polish.
- `frontend/playwright.u02.config.ts`, `frontend/tests/visual/u02-controls.spec.ts`: isolated
  static-host foundation inspection; existing U01 capture test is reused, not rewritten.
- `frontend/AGENTS.md`: local capture command and native focus-testing lesson. Existing
  untracked root `AGENTS.md` is untouched and unstaged.
- Plan and U01 specification record actual owner approval; U01's original PNGs are untouched.

### Native-element CSS contract

| Class / attribute | Use |
| --- | --- |
| `button-primary`, `button-secondary`, `button-quiet` | Choose one variant on a native button; unclassified buttons default to secondary |
| `button-compact` | Compose with a variant for row actions; minimum 28px, grows for wrapped text |
| `button` | Opt-in action styling for a real navigation link, never a substitute for a native button |
| `input-search` | Opt-in 36px main query field |
| `field`, `check-field` | Label/control stack or native checkbox/radio label with 28px click-target height |
| `actions`, `control-group` | Wrapping flex groups, 8px gaps, shrinkable children |
| `panel` | Restrained surface/border/padding, no request or disclosure behavior |
| `metrics`, `metric` | Grid of semantic `dl.metric` with `dt` / `dd`; two columns below 900px |
| `badge`, `status-success`, `status-warning`, `status-error` | Compact text marker / explicit status text color; always include words |
| `muted`, `literal`, `numeric` | Secondary 13px text / full literal pre-wrapped monospace / tabular right-aligned quantities |
| `data-table` | Opt-in native table with caption, 40px minimum rows, wrapping cells and hover/selection treatment |
| `aria-pressed="true"`, `aria-selected="true"`, `aria-invalid="true"` | Visual hooks; consumers must set these only when semantically appropriate |
| `disabled`, `aria-busy="true"` | Native disablement / busy cursor; busy alone does not disable interaction |

Do not globally apply table or panel styles to all existing feature layouts. Owners integrate
opt-in classes as their packages land. Narrow path-first records belong to U07/U10/U11;
foundation tables preserve all headers, values and actions without inventing record behavior.
Use explicit column sizing where path width matters; `data-table` defaults to fixed layout.
Do not use `aria-disabled` on links and assume CSS prevents navigation: it only supplies a
visual treatment. Use native `disabled` on buttons; keep real navigation links operable.

For variable loading labels, use native button content with both labels sharing a grid cell:

```html
<button class="button-primary" disabled aria-busy="true">
  <span class="button-labels">
    <span aria-hidden="true">Save</span>
    <span>Saving changes…</span>
  </span>
</button>
```

Toggle `aria-hidden` between the two label spans. The hidden span retains its intrinsic size
but is neither visible nor part of the accessible name. Both labels can wrap; tests assert
unchanged width/height between states. No button-width equalization or fixed-height truncation.

## Validation and screenshots

From `frontend/`:

- `npm run check`: passed, 0 errors/warnings.
- `npm run lint`: passed (including new capture tooling and local instructions).
- `npm run test:unit`: passed, 15 files / 143 tests.
- `npm run api:check`: passed; no generated API drift.
- `npm run build`: passed; rerun after final CSS adjustment.
- `npx playwright test --config playwright.u02.config.ts`: passed, two tests covering
  18 representative pages and native controls at all three widths. Asserts dimensions,
  long-label wrapping without clipping, stable loading labels, full literal paths,
  native checkbox label activation/select operation, visible keyboard focus, hover and
  disablement, and no feature-header/navigation leakage. Basic computed contrast checks
  cover primary/disabled/field text (at least 4.5:1), control boundary/focus (at least 3:1).
- `npx playwright test tests/browser/search.spec.ts tests/browser/inventories.spec.ts
  tests/browser/details.spec.ts tests/browser/directories.spec.ts
  tests/browser/disk-writes.spec.ts`: passed, 49 tests. Search keyboard/IME/restoration,
  literal values/exact capacities, uncertain writes, bounded pages and independent sections
  retain their existing assertions.

The initial control test assumed programmatic focus after mouse interaction would match
`:focus-visible`; native Chromium correctly did not. The test now exercises Tab/Shift+Tab,
not a forced always-on outline or weakened assertion.

All 21 full-page PNGs are preserved in [screenshots/](screenshots/), independently of U01's
before images. Chromium 156.0.8078.4, en-US, UTC, scale factor 1, 900px viewport height:

| Scenario | 1440 | 1024 | 768 |
| --- | --- | --- | --- |
| Native control fixture | [PNG](screenshots/controls-1440.png) | [PNG](screenshots/controls-1024.png) | [PNG](screenshots/controls-768.png) |
| Search | [PNG](screenshots/search-1440.png) | [PNG](screenshots/search-1024.png) | [PNG](screenshots/search-768.png) |
| Disks | [PNG](screenshots/disks-1440.png) | [PNG](screenshots/disks-1024.png) | [PNG](screenshots/disks-768.png) |
| Content history | [PNG](screenshots/content-history-1440.png) | [PNG](screenshots/content-history-1024.png) | [PNG](screenshots/content-history-768.png) |
| Directory history | [PNG](screenshots/directory-history-1440.png) | [PNG](screenshots/directory-history-1024.png) | [PNG](screenshots/directory-history-768.png) |
| Search empty | [PNG](screenshots/search-empty-1440.png) | [PNG](screenshots/search-empty-1024.png) | [PNG](screenshots/search-empty-768.png) |
| Directory summary error | [PNG](screenshots/directory-error-1440.png) | [PNG](screenshots/directory-error-1024.png) | [PNG](screenshots/directory-error-768.png) |

All 18 page document widths match their viewport widths. Representative screenshots were
inspected for dark native surfaces, wrapping and visible focus; these are inspection evidence,
not pixel goldens. The control gallery is test-only HTML using shipped styles, not a new route.

## Constraints, issues and next owners

- No backend/API/catalog changes, no live catalogs, no comparison execution changes, no
  new requests or framework. All browser inputs are disposable intercepted fixtures.
- Unknown/zero, incomplete subtotals, current/history, literal paths/Unicode, exact integers,
  independent retries and uncertain-write reconciliation remain unchanged.
- U02 establishes the foundation, not the final page hierarchy. Search still has all filters
  exposed; disk creation still precedes browsing; summary stacks/date disclosures and variable
  row action names remain for their assigned packages. These are not U02 blockers.
- U03 is next. U04–U14 remain unimplemented here; run them sequentially, including full deployed
  operation-matrix validation at U14. No full backend/deployed gate or extended accessibility
  audit was run or claimed for U02.
- The plan already proposes a short ADR for shared UI/unit conventions if they become
  architecture policy. Consider decision/context/alternatives/consequences then; none created
  automatically in this presentation-only package.
