# U05 — Shared readable size and date presentation

**Complete: 2026-10-11.** Follows the owner-approved U01 specification and U02 foundation,
coordinated with U04's separate capacity parser. No backend/API/catalog changes, new requests,
write lifecycle changes, extended audits or other task packages. No blockers remain for U06.

## Executed plan

1. Add exact-integer readable formatting, retaining the existing exact-byte helpers.
2. Compose a native size disclosure and compact date disclosure; integrate shared presentation
   without changing page hierarchy, request owners, filters or comparisons.
3. Test unknown/zero/maximum/precision, unit thresholds and keyboard reachability; capture the
   agreed widths using intercepted disposable fixtures and publish interfaces/constraints.
4. Update plan status and commit only U05 files. Unrelated instruction changes, workspace
   catalogs and `.million-distributions-*` remain unstaged.

## Shared interfaces

- `frontend/src/lib/format/decimal.ts` exports `ByteUnits = 'binary' | 'decimal'` and
  `readableBytes(value: string | null, units = 'binary', locale?)`, returning
  `{ text, approximate, exactBytes, scaled }`. Input is canonical nonnegative signed-64-bit
  decimal text or null. `exactBytes` is localized exact integer text **including B**, not an API
  payload; retain the original canonical input for writes/copy-as-canonical needs.
- Unit selection, round-half-up to at most two decimal places, promotion at rounded unit
  boundaries and approximation detection use BigInt only. Fractional trailing zeros disappear.
  Binary units run KiB through EiB; decimal units KB through EB. Scaled display uses an ASCII
  decimal point; exact-byte grouping still uses `Intl.NumberFormat` on BigInt. `text` alone does
  not mark approximation: consumers must honor `approximate`, preferably through `SizeValue`.
  `9007199254740992` is exactly 8 PiB; one more byte is approximately 8 PiB. Int64 maximum is
  approximately 8 EiB / 9.22 EB, with all exact digits retained in the disclosure.
- `frontend/src/lib/components/SizeValue.svelte`: props `{ value: string | null,
  units?: ByteUnits }`. Defaults to binary file/catalog units. Use `units="decimal"` for
  declared capacity. Scaled values are native `<details>` with the readable value as summary;
  rounded values visibly show `≈` and have an accessible name beginning “Approximately”. The
  accessible summary also names “exact bytes”; expanding shows selectable exact integer bytes.
  Unscaled values, including 0 B, are already exact and need no extra disclosure. Unknown is
  plain text, not zero and not an empty control. No hover dependency or clipboard permission.
- `frontend/src/lib/format/date.ts`: the existing `presentDate(value, meaning, locale?,
  timeZone?)` interface stays intact. Default zone is now explicit UTC, not ambient browser
  local time; optional caller-selected zones remain explicit in text. Display includes calendar
  date and 24-hour time through seconds. `exactUTC` preserves the original RFC3339 string and
  all 1–9 fractional digits; malformed/non-UTC timestamps still fail explicitly.
- `frontend/src/lib/components/DateValue.svelte`: unchanged `{ value, meaning }` props.
  Each known date is **one** native disclosure whose summary includes “Captured at”, “Imported
  at” or “Source mtime”, the date/time, and UTC. The accessible name also says “exact UTC”. Its
  expanded text retains full original precision; `<time datetime>` retains the original string.
  Unknown has its meaning label and no disclosure. Dates no longer have a second separate
  “Exact UTC” summary row. `.date-value` / `.size-value` are useful test hooks, not request owners.

`MAX_INT64`, `decimal`, `capacityInput`, `formatBytes`, `formatCount`, percentage and other-count
interfaces remain unchanged. `formatBytes` deliberately remains **exact**, for non-component
consumers; readable presentation is a separate API. U04's `format/capacity.ts` and `DiskForm`
are untouched, including exact previews, lossless edit initialization and canonical payloads.

## Integration and semantic ownership

Presentation-only substitutions integrate `SizeValue` into search results, content lists,
identity and locations, observations, disk metadata/cataloged size, recursive directory sizes,
entries and coverage. Search capture dates now use `DateValue` instead of tooltip-only precision.
Existing inventory/snapshot/directory dates inherit the compact shared presentation.

**Completeness belongs to the caller, not the size component.** Supply null for an incomplete
total, and render a separately labeled known subtotal plus unknown-size counts/completeness.
This remains visible in disk, directory and comparison summaries. Disk known-byte labeling and
comparison source known-byte labeling explicitly say subtotal. A known subtotal of 0 B can
coexist with an Unknown total: do not turn it into a complete zero. Logical file bytes, unique
content bytes, declared capacity, actual usage and free space remain separate concepts.

U06–U12 should reuse these components when restructuring pages, not derive new floating-point
formatters or put readable strings in forms/API payloads. Do not conceal unknown counts or
completeness in later statistics disclosures. Keep current/history and capture/import/mtime
meaning labels. U04's selector reinterprets the amount; display formatting does not change it.
There are no automatic loads, retries, comparison execution or clipboard actions here.

## Validation

From `frontend/`, final checks passed:

- `npm run check`: 0 errors/warnings.
- `npm run lint`: ESLint and Prettier passed, without suppression.
- `npm run test:unit`: 17 files / 224 tests, including 37 new readable-value cases and all
  44 U04 capacity cases. Every unit, unknown/zero, int64 maximum, values beyond Number precision,
  exact versus rounded fractions, half-up rounding, unit carry, malformed/overflow input,
  explicit timezones/date rollover, seconds and nanoseconds are covered.
- `npm run api:check`: passed, no wire-contract drift.
- `npm run build`: passed.
- `npx playwright test tests/browser/readable-values.spec.ts tests/browser/search.spec.ts
  tests/browser/details.spec.ts tests/browser/directories.spec.ts tests/browser/coverage.spec.ts
  tests/browser/inventories.spec.ts tests/browser/contents.spec.ts
  tests/browser/disk-writes.spec.ts`: **72 passed**, including four new value tests and all
  17 disk-write/reconciliation tests. New tests cover keyboard Enter/Space and Tab focus,
  accessible approximation names, exact byte visibility, maximum/zero/unknown, binary catalog
  versus decimal capacity, incomplete zero subtotal, all date meanings, epoch zero and precision.
- After making the screenshot disk fixture internally consistent with its latest complete
  inventory, the four new browser tests passed again. Coverage's exact-byte assertion targets
  the covered subtotal's disclosure, not another equal source subtotal. Relevant coverage tests
  were rerun after that assertion refinement.

Initial validation caught an incorrect half-up test expectation (1005 B rounds to approximately
1.01 KB, not exactly), Svelte stripping a literal trailing space, and the lint rule against a
string-literal interpolation. The expectation was corrected; the approximation span uses a
nonbreaking space. No validation was disabled. Existing date tests now open the single date
summary, and the real core test's declared-capacity assertion opens exact bytes instead of
requiring raw bytes as the entire cell text. Real/backend/deployed suites were **not run**;
the adapted real assertion remains for U14's integration gate.

## Screenshots

Nine deterministic Chromium desktop main-region captures, viewport height 900, at the agreed
widths. Captures use fixture-only browser tests, not a catalog. Compact versus expanded states
show that exact bytes and UTC nanoseconds are reachable; subtotal views distinguish declared
decimal capacity, binary known subtotal and Unknown total. Tests assert document width equals
viewport width for all nine captures. Narrow screenshots were inspected; these are evidence,
not pixel goldens and do not replace U01's approved pre-redesign baseline.

| State | 1440 | 1024 | 768 |
| --- | --- | --- | --- |
| Compact dates/size | [PNG](screenshots/compact-1440.png) | [PNG](screenshots/compact-1024.png) | [PNG](screenshots/compact-768.png) |
| Expanded precision | [PNG](screenshots/precision-1440.png) | [PNG](screenshots/precision-1024.png) | [PNG](screenshots/precision-768.png) |
| Declared capacity / incomplete subtotal | [PNG](screenshots/subtotal-1440.png) | [PNG](screenshots/subtotal-1024.png) | [PNG](screenshots/subtotal-768.png) |

Reproduce after `npm run build` using `npx playwright test tests/browser/readable-values.spec.ts`.
Copy its `compact-*.png`, `precision-*.png` and `subtotal-*.png` from `test-results/readable-values-*`
before another Playwright suite clears outputs. Only inspected captures should replace U05 evidence.

## Limits / next owners

- No U05 blockers. Feature hierarchy, tables, filters and disclosure grouping beyond individual
  values remain U06–U12 scope. This is not a full page-density or deployed-operation gate.
- No localized decimal-fraction grammar, relative date arithmetic, unit-entry changes or
  new clipboard behavior. Exact values remain selectable through native disclosures.
- UTC is the shared component's display convention; optional caller-selected zones belong to
  the helper only. No timezone preference UI is added.
- U14 owns full integration/deployed checks; no performance claims or extended accessibility
  audits. Backend/catalog/race/benchmark work was neither needed nor performed.
- The approved plan already proposes an optional ADR for shared unit/UI conventions (decision,
  context, alternatives, consequences); none is created automatically by U05.
