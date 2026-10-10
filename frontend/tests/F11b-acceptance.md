# F11b approved responsiveness evidence and scope

Status: **F11b complete within the owner's revised scope**. The approved Chromium/container
responsiveness budgets passed in 20 fresh contexts. F12 and backend acceptance remain separate.

## Owner decisions (2026-10-10)

- Approved the documented measured Chromium/container profile and input/scheduling/render
  budgets below, with 20 fresh-context runs reporting max/p95. This is **not a hardware-general
  guarantee** or backend performance approval.
- Initial browser support: **Chromium, desktop only**. The narrow viewport was tested on desktop
  Chromium; it does not establish real-mobile support. Other browsers are not approved targets.
- Accessibility is **best effort**: “don't test accessibility it's on a best-effort basis but
  no extended checks”. No additional accessibility, manual screen-reader or extended audit work
  is required for F11b acceptance. Earlier automated/visual evidence is retained as historical
  evidence only. No screen-reader success or WCAG conformance is claimed.

## Reproduce (from `frontend/`, sequential browser commands)

```sh
npm run check
npm run lint
npm run test:unit
npm run build
npx playwright test tests/browser/responsiveness.spec.ts --repeat-each=20 --reporter=json > /tmp/opencode/ui-evidence.json
node scripts/summarize-responsiveness.mjs /tmp/opencode/ui-evidence.json /tmp/opencode/ui-summary.json
```

Only the focused responsiveness test ran after the revised owner decision; accessibility
suites were not rerun. The timing test uses a built-static test host and intercepted API,
not backend performance measurements. Every Playwright repetition creates a fresh isolated
page/context; one serial worker and zero retries. The summarizer rejects missing/failed/flaky/
skipped runs, retries, inconsistent profiles, incorrect structural bounds or budget failures.
Playwright JSON attachments retain the full timing/heap report; default reporters may not write
inline evidence to separate files. Suites share `test-results/`; do not run them concurrently.

Previously verified at `78bdd22`: `check` (zero errors/warnings), `lint`, 143 unit tests, all 74 mocked
browser tests and all 29 deployed real-browser tests (`npm run test:deployed`, 18/18 exposed
operations). No Go race checks or backend benchmarks were run.

Revised-scope checks: production build, 20 focused responsiveness runs, `check` (zero
errors/warnings), `lint`, 10 focused search/traversal unit tests, and summarizer rejection probes
for missing runs, failed runs, failed input budget and inconsistent browser profiles. No
accessibility suites, Go race checks or backend benchmarks were rerun.

## Approved repeated measurement

Run began **2026-10-10 23:03:12 UTC**, duration **351.5 s**. **20/20 passed**, zero retries,
skips or flaky results. Application sources remain at `78bdd22`; measurement instrumentation
adds explicit budget assertions and first-input/first-render long-task window boundaries.
The [committed evidence](evidence/f11b-responsiveness.json) records the full baseline commit,
measurement-test SHA-256, profile, per-run input/render/body-wait vectors, long-task/GC intervals
and heap/DOM samples. All numbers below are rounded to 0.1 ms; checks use unrounded values.

P95 is **nearest rank 19 of 20 per-run worst-case values**, not a pooled-event statistic or
population tail guarantee. Scheduling has one settled request per run.

| Approved check                                            | Max (ms) | P95 (ms) | Result              |
| --------------------------------------------------------- | -------: | -------: | ------------------- |
| Every synchronous input span <50 ms                       |      1.3 |      1.3 | Pass                |
| Idle final-input scheduling 200±50 ms                     |    201.1 |    200.9 | Pass; minimum 200.6 |
| First/single 50-row response-text → rAF ≤50 ms            |     31.4 |     30.8 | Pass                |
| Bounded accumulation (pages 2–10, up to 500 rows) ≤100 ms |     94.7 |     88.7 | Pass                |
| No >50 ms long task in initial-input/first-render window  |        0 |        0 | Pass; none observed |

All runs retained the structural request/DOM bounds: one settled initial query, twelve
explicit page requests, no per-result/preload calls, and 0 → 50 → 500 → 50 → 50 → 0 displayed
rows across the recorded milestones. Ten-page retained-data behavior remains covered by the
existing traversal unit tests. Heap/DOM/CDP listeners are descriptive evidence, not byte budgets
or leak proof. The approved budgets do not require all later traversal tasks to be <50 ms:
later task per-run maxima had **max 65 ms / p95 61 ms**, disclosed without asserting causation.
Mocked body wait per-run maxima were **1263.7 ms / p95 1263.2 ms**, separate from debounce/render.

Exact approved profile: Playwright 1.64.0 headless Chromium **156.0.8078.4**, Linux
**7.2.6-arch2-1**, AMD Ryzen 5 9600X; 12 exposed logical CPUs and 32,750,096,384 host memory
bytes, container cap **4 CPU equivalents** (`cpu.max=400000 100000`) and **8 GiB**
(`memory.max=8589934592`). Viewport **1280×720**, no CPU throttle/extensions, production static
build, Unicode DTO fixture, 50 rows/page, 12 pages, every mock response held 1200 ms, explicit
GC at six heap/DOM milestones. Fresh context does not imply a filesystem-cold browser/asset cache.

### Measurement interpretation

Input spans cover document-capture → window-bubble around synchronous app handling, not total
input-to-paint. Response-text → next rAF includes JSON parsing, contract validation, state/DOM
updates; it is a **pre-paint proxy**, not scan-out latency. The entire initial-input through
first-rAF window is checked conservatively for >50 ms tasks, without claiming attribution.
Continuation `afterLastInput` values are elapsed time since typing, not debounce samples.
GC/CDP instrumentation is invasive; memory reports V8 used heap rather than RSS/full renderer
memory. This bounded fixture/profile evidence is neither a large-catalog/backend benchmark nor
a guarantee across devices/payloads. No additional accessibility checks were run.

## Historical best-effort behavior coverage and fixes (`78bdd22`)

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

## Historical preliminary browser measurement (before budget agreement)

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

## F12 handoff

No F11b owner decision remains outstanding under the revised scope. F12 can consume the approved
Chromium-desktop statement, best-effort accessibility limitation and measured-profile evidence.
F12's final release checks and independent backend gate decisions are **not performed or closed**
by this package. Do not reintroduce manual AT acceptance as an F11b prerequisite or describe
historical automated checks as screen-reader success.
