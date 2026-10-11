# U04 — Exact unit-aware capacity input handoff

**Complete: 2026-10-11.** Implements the approved amount + unit interaction on U02/U03's
foundation. No new approval, backend change or architecture decision. No blockers remain for
U05/U06; packages continue sequentially.

## Plan and outcome

1. Isolate decimal-string/BigInt capacity arithmetic from display formatting.
2. Add amount/unit controls, immediate exact-byte preview and capacity-associated validation.
3. Preserve canonical payloads, lossless unchanged edits, optional clears and write recovery.
4. Test arithmetic, disk writes, recovery and approved-width layout; preserve screenshots.
5. Publish interfaces and constraints for U05/U06, then commit only U04-owned files.

All implementation/validation steps completed. The selector reinterprets the current amount;
it does not convert the amount to preserve the old byte value. This is explained beside the
controls. `1.5` TB previews/sends `1500000000000`; `1` TiB previews/sends `1099511627776`.
Zero and `9223372036854775807` B are valid. Invalid drafts display no stale byte preview.

## Files and interfaces

- `frontend/src/lib/format/capacity.ts`: exports `CAPACITY_UNITS`, `CapacityUnit` and
  `capacityBytes(amount: string, unit: CapacityUnit): string`. The result is canonical decimal
  byte text in the inclusive range 0–9223372036854775807. Parsing/conversion uses strings and
  BigInt only; scale, multiplication, divisibility and range checks never round or use Number.
  Decimal units use 1000 powers; binary units use 1024 powers. Trailing fractional zeros and
  leading integer zeros are accepted and canonicalized. A decimal point requires digits on
  both sides. Signs, whitespace, exponent notation, commas, underscores, suffixes and
  fractional-byte results fail explicitly. No suffix-entry interaction is added.
- `frontend/src/lib/features/disks/DiskForm.svelte`: controls named **Capacity amount** and
  **Capacity unit**, decimal input mode, reactive **Exact bytes** preview and field error.
  Component-unique IDs link help/preview/error through `aria-describedby`; invalid drafts
  set `aria-invalid`. Validation appears on input/unit interaction or submit, not untouched
  initial empty forms. Local capacity validation does not report a connection failure.
- `frontend/tests/unit/capacity.test.ts`: 44 new cases covering every unit, zero, decimal and
  binary fractions, long exact representations, values beyond Number precision, int64
  boundaries, rejected syntax and fractional-byte results.
- `frontend/tests/browser/disk-writes.spec.ts`: 17 disk-write tests, including four stated
  acceptance examples, reinterpretation, field association, no invalid POST, maximum/equivalent
  no-op edits, canonical writes, remote rebase, uncertain PATCH and layout captures.
- `frontend/tests/real/writes.spec.ts`: renamed capacity selector only; precise B payload,
  conflict, maximum, optional-clear and catalog-revision assertions retained.
- `frontend/tests/real/lifecycle.spec.ts`: committed POST response-loss case now uses `1.5` TB
  and verifies retained amount/unit plus stored canonical bytes, still exactly one creation.
- Plan, this handoff and three screenshots are durable U04 evidence. Existing root instructions,
  frontend instruction edits, workspace catalogs and measurement directories are not staged.

### U05 integration

`format/decimal.ts` remains unchanged, including `MAX_INT64`, `capacityInput`, `formatBytes`
and the API patch adapter's canonical-byte validation. U04's new helper imports only
`MAX_INT64`; U05 can evolve readable size presentation without modifying input arithmetic.
Do not replace the exact preview with rounded/approximate display text. This input is declared
capacity, not cataloged bytes, measured usage or free space. An ADR for shared unit conventions
remains an optional proposal from the approved plan, not an automatically created record.

### U06 lifecycle integration

The form still accepts optional `initial` and `refreshed(disk)`; no new write owner or request
is introduced. Existing capacities always open in B with their exact canonical byte amount.
Dirty-capacity comparisons use converted bytes, not amount spelling or selected unit: an
equivalent representation sends no PATCH. Units/amount are retained when verification leaves
capacity unchanged. If an untouched/equivalent capacity changes remotely during reconciliation,
both controls rebase together to exact remote bytes in B. Changed or invalid local drafts stay
retained. Empty serial/notes still clear with null; text is otherwise literal.

Pending/uncertain writes remain blocked until explicit reconciliation. Recovery does not
retry writes, reconnect does not grant permission to retry, and catalog-context invalidation
retains drafts. Preserve these invariants when adding Cancel/Close, dirty checks and focused
recovery UX. Include both amount and unit in draft retention/discard behavior; determine
semantic unchanged capacity using `capacityBytes`. U06 still owns list-first creation panels,
explicit editing, cancellation, other field errors and adoption of `form-body`.

## Validation and screenshots

From `frontend/`, all final checks passed:

- `npm run check`: 0 errors/warnings.
- `npm run lint`: passed.
- `npm run test:unit`: 16 files / 187 tests (44 new capacity tests).
- `npm run api:check`: no generated contract drift.
- `npm run build`: passed.
- `npx playwright test tests/browser/disk-writes.spec.ts tests/browser/inventories.spec.ts`:
  25 passed (17 write, eight inventory). Disposable intercepted fixtures, no catalog opened.
- `npx playwright test --config playwright.real.config.ts tests/real/writes.spec.ts
  tests/real/lifecycle.spec.ts --grep 'real disk create|committed POST response loss'`:
  two passed. Existing owned harness used disposable offline-imported catalogs and cleaned up
  its Go/Vite processes; no workspace catalog was used.

Initial check/lint runs identified Svelte's top-level `$props.id()` initializer requirement
and keyed-each rule; both were fixed in code, without suppression. Review also strengthened
the invalid-input browser fixture to record unexpected POSTs rather than relying on an absent
POST handler. Final checks above include these fixes.

Form-region screenshots from the deterministic browser capacity test, Chromium desktop,
900px-high viewports. No horizontal document overflow at any agreed width; native controls
and exact preview remain visible. Captures were inspected and preserved before later suites;
they are evidence, not pixel goldens, and do not replace U01's baseline.

| 1440 | 1024 | 768 |
| --- | --- | --- |
| [Capacity form](screenshots/capacity-1440.png) | [Capacity form](screenshots/capacity-1024.png) | [Capacity form](screenshots/capacity-768.png) |

## Remaining issues and limits

- No U04 blockers or confirmed implementation defects remain.
- Creation still precedes the disk list, forms retain their existing section layout, and
  recovery still uses the existing presentation. These are U06/U08 scope, not U04 omissions.
- Suffix parsing, localized number separators and implicit unit conversion are intentionally
  unsupported. U05's readable display must not broaden this exact-entry grammar silently.
- No backend/API/schema/catalog changes, performance claims, extended accessibility audit,
  full deployed operation matrix or whole browser suite. U14 retains integration gates.
