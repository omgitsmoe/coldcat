# U14 — Frontend integration release gate

**Complete: 2026-10-11. All U01–U14 packages complete within approved scope.** No required owner
blocker remains. Optional user-feedback tests are deferred; separate backend acceptance stays open.

## Executed plan and changes

1. Read applicable instructions, U01 approved specification/artifact policy, all U02–U13 handoffs
   and relevant fixture/real lifecycle tests. Keep existing independent request/write owners.
2. Add deterministic geometry coverage across all eight route families and representative states
   at 1440/1024/768px; inspect external captures, including comparison and recovery/paging evidence.
3. Run required frontend checks and browser suites sequentially. Adapt intentional interaction
   locators without weakening correctness assertions; document conventions/results and commit
   only explicit U14 files.

Files:

- `frontend/tests/browser/layout-regression.spec.ts`: **36 new tests** (12 scenarios × three
  widths), isolated intercepted reads on the existing built-static host; no catalogs. Covers
  Search, Contents, content history/locations, observation, disk list/detail/history, inventory,
  directory, plus create, expanded provenance, search empty and independent summary failure.
  Long Unicode/literal paths and labels, unknown/zero/maximum bytes, historical/current records,
  full digest and exact input preview remain present. Unexpected endpoints/writes and browser
  exceptions fail. Assertions cover no document overflow, 57px ready shell, 24/16px gutters,
  32/28/36px control minima, no vertical control clipping, no cell overlap/content spill and
  bounded table origins. Every page is closed in `finally`.
- `frontend/tests/real/core.spec.ts`: source status now targets `.source-context`; exact Unknown/
  zero size assertions target `.size-value`, excluding the intentionally added record label.
- `frontend/tests/real/lifecycle.spec.ts`: shell readiness/outage assertions use the banner landmark,
  not every `header` (comparison editors now have a local header). All request-count, no-cursor,
  stale/reconnect, no-auto-comparison and uncertain-write assertions remain intact.
- `frontend/README.md`: approved visual/control/value conventions and changed interactions;
  updated guarded form-close, capacity and shared feedback contracts.
- Parent plan and this handoff: final status, gates, evidence and remaining boundaries.
- Brief local reproduction guidance appended to `frontend/AGENTS.md` but **left unstaged** along
  with its pre-existing unrelated changes. No new agents, tools/dependencies or architecture/ADR.

No production application/backend code changed. Initial layout-tool failures were incorrect
measurements of the main border edge rather than padding, and a preview assertion expecting
localized bytes instead of the canonical input preview. Corrected the tests, not application
behavior. Initial real run had seven failures from stale source/size/header locators and the
resulting incomplete core read matrix; final full real/deployed runs pass without loosening
operation coverage, values, request counts or lifecycle guarantees.

## Required gates

From `frontend/`; suites ran **sequentially**, without extended accessibility specs:

| Command | Final result |
| --- | --- |
| `npm run check` | 0 errors / 0 warnings |
| `npm run lint` | ESLint/Prettier passed, no suppressions |
| `npm run test:unit` | 23 files / **262 passed** |
| `npm run api:check` | Passed, no wire drift |
| `npm run build` | Passed |
| `npx playwright test tests/browser/layout-regression.spec.ts` | **36 passed**; also in mocked gate |
| `npm run test:e2e -- 'tests/browser/(?!accessibility\.spec\.ts$)[^/]+\.spec\.ts$'` | **156 passed** |
| `npm run test:real -- 'tests/real/(?!accessibility\.spec\.ts$)[^/]+\.spec\.ts$'` | **27 passed** |
| `npm run test:deployed -- 'tests/real/(?!accessibility\.spec\.ts$)[^/]+\.spec\.ts$'` | **27 passed**, deployed browser matrix **18/18** |

Logs: `/tmp/opencode/u14-{check,lint,test-unit,api-check,build,layout,e2e,real-final,deployed}.log`.
The earlier failed real log is `u14-real.log`; it is not a final pass. Expected proxy
ECONNREFUSED messages belong to deliberate stopped-backend lifecycle tests. Real/deployed harnesses
own disposable offline-imported catalogs and stop their processes before cleanup; no workspace
catalog or benchmark directory was used. No harness code change warranted extra harness suites.

## Screenshot review and durable layout protection

36 full-page captures and per-capture measurements are external at:

`/tmp/opencode/u14-screenshots/{scenario}-{1440,1024,768}.{png,json}`

Scenarios: `search`, `search-empty`, `contents`, `content-history`, `observation`, `disks`,
`create`, `disk-detail`, `inventory`, `provenance`, `directory`, `directory-error`.
Chromium desktop, en-US/UTC, scale factor 1, 900px viewport height. Reproduce after build with the
focused command above. Existing full mocked gate refreshes comparison evidence at
`/tmp/opencode/u12-screenshots/{tree,coverage}-{1440,1024,768}.png`, picker evidence in
`/tmp/opencode/u09-screenshots/`, and paging evidence in `/tmp/opencode/u13-screenshots/`.

Representative wide/medium/narrow captures were inspected across the routes: restrained hierarchy,
compact shell and controls, list-first disks, browse-first Contents, full digest/path wrapping,
unfiltered/incomplete summary visibility, named narrow records and reachable actions. Comparison
tables keep equality separate from coverage and covered/missing quantities; failed summaries do not
hide successful entry/comparison results. Long pathological text increases row height rather than
being truncated. Some detail/expanded comparison output requires vertical scrolling by design.

Measured table origins (caption included; CSS pixels, not a performance benchmark):

| Scenario | 1440 | 1024 | 768 |
| --- | --- | --- | --- |
| Search | 369.16 | 369.16 | 409.16 |
| Disks | 149.16 | 149.16 | 149.16 |
| Contents | 339.16 | 359.16 | 379.16 |
| Content history locations | 559.16 | 559.16 | 699.16 |
| Directory entries | 643.09 | 663.09 | 704.09 |
| Directory with summary error | 591.09 | 611.09 | 632.09 |

All 36 document widths match their viewport; no checked cell overlap/content spill or control
clipping. Content/directory origins do not meet every aspirational U01 density target; essential
literal/source/history/completeness context is retained rather than concealed to meet a number.
Geometry gates prevent major hierarchy/density regression, not every cosmetic/raster change.

**No pixel-golden coverage.** Geometry/layout assertions are the durable regression mechanism.
External images are temporary environment-local inspection evidence, not durable hosting or
checkout assets. No images committed, no LFS migration/object storage invented, no baseline
replacement/history rewrite. Follow the [artifact policy](../artifact-policy.md); owner-managed
storage is needed before discarding this environment if images must remain available.

## Preserved semantics and remaining limits

- Exact integer/unit arithmetic and int64 limits; no rounding in payloads; null optional clears;
  unknown versus zero/incomplete subtotals; declared capacity is not measured disk usage.
- Catalog-wide replica counts despite membership; current captures versus repeated history;
  historical sources/current destinations; literal IDs/labels/paths/rules and native links.
- Draft retirement/explicit Apply, lazy bounded pickers, bounded retained paging, independent
  retry owners, no comparisons on opening/editing/reconnect, stale/revision cursor retirement.
- Keyboard/IME/Enter/Escape/Back restoration protections; both real committed POST/PATCH response
  loss checks retain drafts, block blind retry and verify persisted writes without duplicates.
- No required U14 issue remains. Existing Contents parser limits for unrelated malformed bookmarks
  remain explicit failures (U09 boundary), not silently repaired. Suffix capacity parsing/localized
  entry and broader browser/mobile support remain deferred/out of scope.
- No backend race/benchmarks, responsiveness remeasurement, extended accessibility/manual AT gate
  or new performance claims. The optional shared-UI/unit ADR proposal remains in the parent plan;
  none created automatically. Separate backend release acceptance is not closed by these results.
- Unrelated root/frontend instructions, workspace catalog/lock and `.million-distributions-*`
  remain unchanged/unstaged except the explicitly noted local instruction append.
