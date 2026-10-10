# F11b evidence and owner acceptance checklist

Status: automated checks and bounded fixes implemented; **F11b acceptance pending**.
No target hardware, performance budget, supported-browser policy, manual assistive-technology
audit, backend performance acceptance, or F12 release approval is implied.

## Reproduce (from `frontend/`, sequential browser commands)

```sh
npm run check
npm run lint
npm run test:unit
npm run test:e2e
npx playwright test --config playwright.deployed.config.ts tests/real/accessibility.spec.ts
npx playwright test tests/browser/responsiveness.spec.ts --reporter=json > /tmp/opencode/ui-evidence.json
```

Build before the focused deployed command if not running `test:e2e` first. The deployed audit
uses F11a's owned disposable Go catalog/static hosting, not the workspace catalog. The timing
test uses a built-static test host and intercepted API, not backend performance measurements.
Playwright JSON attachments retain the full timing/heap report; default reporters may not write
inline evidence to separate files. Suites share `test-results/`; do not run them concurrently.

Verified for this change: `check` (zero errors/warnings), `lint`, 143 unit tests, all 74 mocked
browser tests and all 29 deployed real-browser tests (`npm run test:deployed`, 18/18 exposed
operations). No Go race checks or backend benchmarks were run.

## Behavior coverage and fixes

- `real/accessibility.spec.ts`: 13 screen/state audits at **1280×900 and 320×900**: search,
  content, observation, contents/hash lookup, disk list/create, disk detail/edit, inventory,
  directory, exact-copy editor/results and coverage editor/results. Axe 4.13 WCAG A/AA 2.0/2.1
  checks, one banner/main/h1, named main navigation, no document-wide horizontal scrolling.
  On the recorded run: zero violations and zero incomplete axe checks in all 26 audits.
  This is not a WCAG conformance claim or proof of spoken announcements.
- `browser/accessibility.spec.ts`: native Tab/Enter reaches the skip link, search result,
  observation and containing-directory action; visible computed focus outline; forced colors
  active and reduced-motion screen has no nonzero CSS animation/transition durations. Typing
  remains literal during held requests; keyboard navigation can leave the pending search.
  Loading/extended wait/empty/error states expose status/alert semantics; late responses do
  not replace the destination screen. Existing F3 IME/shortcut guards remain tested.
- `browser/responsiveness.spec.ts`: six keys 20 ms apart produce one settled query; twelve
  explicitly requested 50-item pages, no detail/preload calls; 500 displayed rows at the
  accumulation bound, then 50 on later pages, zero after Clear. Existing traversal unit tests
  enforce the ten retained-page bound (DOM counts alone cannot prove retained-data bounds).
- Focused fixes: missing route-specific document titles, duplicate contents h1, live empty
  search feedback and shared loaded/page count feedback. No new feature/backend behavior.
- Agent visually inspected the 320px coverage screenshot: wrapped breadcrumbs, rule controls,
  counts and destination links remain readable with no clipped core action. This limited
  screenshot inspection and automated keyboard exercise are **not a manual screen-reader audit**.
  Actual OS high-contrast settings, touch/input methods and other browsers remain unverified.

## Recorded browser measurement (2026-10-10)

One instrumented sample; descriptive evidence, not p95 or acceptance. Built production assets,
Playwright 1.64.0 headless Chromium **156.0.8078.4**, Linux **7.2.6-arch2-1**, AMD Ryzen 5 9600X,
12 exposed logical CPUs/32,750,096,384 host memory bytes; this container is capped at **4 CPU
equivalents** (`cpu.max=400000 100000`) and **8 GiB** (`memory.max=8589934592`). No DevTools CPU
throttle; viewport 1280×720; one worker, fresh context, no extensions. Synthetic rows use the
existing Unicode/markup-safe DTO fixture. API interception holds every search page **1200 ms**.

| Measurement                                                          | Observed                                                |
| -------------------------------------------------------------------- | ------------------------------------------------------- |
| Six synchronous input dispatch spans                                 | 0.4–1.4 ms                                              |
| Final input → first fetch dispatch                                   | 201.0 ms (configured debounce 200 ms)                   |
| First dispatch → response text available                             | 1203.7 ms (includes mocked delay)                       |
| Response text available → first 50 rows' next animation frame        | 24.5 ms                                                 |
| Same span for accumulating pages 2–10                                | 23.3, 23.2, 31.5, 40.3, 47.0, 53.2, 56.0, 55.7, 76.9 ms |
| Same span for single-page views 11/12                                | 19.4 / 12.2 ms                                          |
| Observed main-thread long tasks across entire instrumented traversal | 50 / 53 / 59 ms                                         |

The input span runs from document capture to window bubble around the app's synchronous input
handler, not all input-to-paint work. Timing starts after response **text** consumption and includes
JSON parsing, runtime validation, state/DOM updates and the next rAF; it is a pre-paint proxy, not
proof of display scan-out. Continuation `afterLastInput` timestamps in the raw attachment are
elapsed time since typing, **not debounce samples**. Response-body wait is reported separately.
The performance observer includes test/browser work; long tasks are not attributed conclusively
to application code. Forced-GC intervals are recorded separately. No initial-input/first-render
long task was observed; later accumulation/transition tasks exist and must not be hidden by an
unqualified “all work <50 ms” claim. A 500-row render proxy exceeded 50 ms.

CDP `Runtime.getHeapUsage.usedSize` after explicit GC and `Memory.getDOMCounters`:

| Milestone          | Displayed rows | DOM nodes | Used JS heap bytes | Event listeners |
| ------------------ | -------------: | --------: | -----------------: | --------------: |
| Empty workspace    |              0 |       556 |          2,312,296 |              57 |
| First page         |             50 |     2,435 |          3,417,592 |              57 |
| Accumulation bound |            500 |    18,200 |         11,108,564 |              57 |
| Page 11            |             50 |     2,467 |          4,606,788 |              57 |
| Page 12            |             50 |     2,467 |          4,617,108 |              57 |
| Clear              |              0 |       679 |          3,677,640 |              57 |

Heap drops at the bounded transition and Clear; compiled modules/framework warm-up remain.
These values are fixture-specific V8 used heap, **not RSS, full renderer memory or leak proof**.
GC is invasive and may change timing. No catalog-sized preload or speculative continuation
occurred. A small fixture and one traversal cannot establish a memory budget for every payload.

## Required owner decisions (do not mark accepted until answered)

1. **Target/browser/profile:** approve the documented container/Chromium profile as the UI target,
   or provide the actual minimum target device (CPU/RAM/OS), browser/version, viewport and throttle
   profile. Confirm supported browsers separately; Chromium-only evidence cannot approve others.
2. **Budget before acceptance:** proposed _for discussion_, on the approved target:
   every measured synchronous input span <50 ms; one settled query after 200±50 ms when idle;
   response-text → next-rAF ≤50 ms for first/single 50-row views; ≤100 ms for the explicitly bounded
   500-row accumulation view; no >50 ms task attributable to initial input/first 50-row render.
   Later accumulation tasks above 50 ms are disclosed, not silently waived. Keep structural
   limits (10 pages, default ≤500 displayed rows, ≤50 in single-page mode, no per-result/preload
   calls). Memory remains descriptive unless an owner specifies a payload/profile and byte budget.
   Decide repetitions/statistic (suggest 20 fresh-context runs reporting max and p95), then rerun
   **after** agreement. Current evidence does not retroactively approve this proposal.
3. **Required manual assistive-technology check:** provide a tester and preferred screen-reader/
   browser/OS, or explicitly defer it as an unresolved release limitation. Optional extra device,
   touch/IME and visual audits can wait for feedback; the plan's manual screen-reader audit cannot
   be claimed done without a real tester. Checklist:
   - Landmarks/headings identify Search, navigation, content identity, locations and directory.
   - Labels, scope/history, literal path/hash/date meanings and table headers are spoken clearly
     at desktop and narrow layouts; each row's action can be distinguished in context.
   - Keyboard-only search → content → locations → observation → directory and Back/return works;
     focus is visible/meaningful, no trap, no shortcut interfering with reader navigation. Try
     disabling type-to-search; native controls remain sufficient.
   - Loading/extended wait, loaded/page count, empty, failure, stale cursor and clipboard feedback
     are announced once/usefully without losing typing/focus or reading stale results as current.
   - Disk create/edit labels, errors, conflict/draft retention and uncertain-write reconciliation
     are reachable/understandable; do not resubmit an uncertain write blindly.
   - Rule add/remove/Apply/Cancel and comparison results are understandable; historical sources,
     filtered equality and distributed coverage do not sound like a complete backup guarantee.
     Return AT/browser/OS versions, pass/fail per item, failing route/action, exact spoken behavior,
     expected behavior and reproducible steps. Actual speech/focus feedback may require further
     bounded fixes even though axe passes.

Suggested required prompt to owner:

> Which minimum hardware/browser/profile should F11b target, and do you agree to the proposed
> input/scheduling/50-row/500-row budgets and 20-run max/p95 protocol above (or specify changes)?
> Who can run the screen-reader checklist, on which AT/browser/OS? If unavailable, do you want
> to explicitly defer that required manual audit as an unresolved release limitation? Optional
> extra device/visual checks can wait. F11b remains pending until these decisions and the agreed
> checks are complete; this does not approve backend performance or start F12.
