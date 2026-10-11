# U06 — Disk creation/editing lifecycle

**Complete: 2026-10-11.** Implements the owner-approved U01 disk lifecycle direction using
U02/U03 controls/form width, U04 exact capacity entry and U05 metadata display. U06 only;
no backend/API/schema changes, workspace-catalog operations or extended audits. No blockers.

## Executed plan

1. Preserve the existing form-owned write/recovery requests and connection invalidation.
2. Move creation behind an explicit action after the disk list; add guarded closing and
   deliberate dirty discard to create/edit panels.
3. Associate required-field errors, retain saving/verified success, and replace reconciliation
   jargon with a focused saved-change check.
4. Validate focused browser/real lifecycles and shared frontend checks; capture agreed widths
   externally, publish this handoff and commit only U06 files.

## Files and interfaces

- `frontend/src/routes/disks/+page.svelte`: default list-first browsing and header Create disk
  action. Opening mounts the panel **after** the existing list; only one create owner can exist.
  The header action is absent while the panel is open, including pending/unresolved outcomes.
  Existing verified refresh/list-version behavior is retained.
- `frontend/src/lib/features/disks/DiskForm.svelte`: props remain `initial?: Disk` and
  `refreshed(disk)`, with required **`closed(): void`** added. Hosts unmount only through this
  guarded callback, not when connection/read state changes. Opening focuses/scrolls the heading.
  `panel form-body`, shared wrapping actions and stable Saving… button labels are composed
  directly on native elements; no modal/state-management framework is added.
- `frontend/src/lib/features/disks/DiskDetail.svelte`: `closed` retires the editing snapshot,
  allowing a new explicit edit only when metadata is freshly verified. Closing does not modify
  cached/server metadata or initiate a request. Success remains visible until explicit Close.
- `frontend/tests/browser/disk-writes.spec.ts`: intentional open/check action changes plus five
  new cases for list-first/validation/discard, edit completion, pending/uncertain Cancel,
  approved-width captures and recovery after a previously verified creation is invalidated.
- `frontend/tests/real/{writes,lifecycle}.spec.ts`: explicit creation opening and renamed
  Check whether saved selectors only; committed-response-loss, exact payload, no duplicate
  writes, conflict, null clear, revision and search invalidation assertions are retained.

### Close/discard contract

Cancel closes pristine/semantically unchanged drafts immediately. Changed drafts require
**Discard changes**; **Keep editing** retains all fields, including capacity amount/unit.
Edit dirtiness compares converted canonical bytes, so equivalent unit representations do not
cause a PATCH or unnecessary discard. Creation treats even a unit-only change as a draft.
Invalid capacities are dirty, not silently treated as unchanged. Saved edits offer Close;
any subsequent unsaved changes still require discard. Creation success keeps the verified
disk link and disables further submissions from that owner.

Cancel/Close is disabled **and handler-guarded** while busy or blocked. It never aborts a
dispatched write or claims to undo it. Unknown outcomes remain in the same mounted form;
closing/reopening cannot grant permission for a second POST. Connection invalidation also
retires any open discard confirmation. Recovery remains reachable if a previously successful
creation is invalidated; the recovery panel is not hidden merely because `created` is true.

### Validation/recovery contract

Required label/capacity precede optional serial/notes. Label and capacity errors use unique IDs,
`aria-invalid`, associated descriptions and alerts, revealed on interaction/submit rather than
untouched mount. The form uses `novalidate` so both custom field errors can appear together;
native required semantics remain. Literal label/serial/notes text is not trimmed or normalized.
U04 conversion, exact-byte preview and int64 limits are unchanged; empty optional fields send null.

**Check whether saved** reads metadata or one bounded disk-label page, never retries a write.
The focused panel explains retained drafts and why Cancel cannot undo a sent write. Continuation
is explicit, revision-checked and still limited to 50 items per page. Full no-match traversal
still requires **I reviewed the disks; allow another creation**; reconnect and failed checks do
not silently approve resubmission. Refreshed comparison metadata uses a native disclosure;
creation/recovered creation exposes it with the Open verified disk link. PATCH checks retain
the explicit instruction to compare refreshed metadata with retained inputs before saving.
Untouched fields rebase to remote values, changed/invalid local drafts stay retained, equivalent
capacity representation stays intact unless remote bytes changed. No automatic retry or
catalog-wide preload is introduced.

## Validation

From `frontend/`, final checks:

- `npm run check`: 0 errors/warnings.
- `npm run lint`: ESLint/Prettier passed; no suppressions. Initial lint caught an unqualified
  DOM type; using `globalThis.HTMLHeadingElement` fixed it without configuration changes.
- `npm run build`: passed.
- `npm run test:unit`: 17 files / 224 tests passed.
- `npm run api:check`: passed, no contract drift.
- `npx playwright test tests/browser/disk-writes.spec.ts tests/browser/inventories.spec.ts`:
  **30 passed** (22 write/lifecycle, eight inventory). Original 17 disk-write protections remain.
  An initial capture expectation used an unrelated label; it now references the actual fixture.
- `npx playwright test --config playwright.real.config.ts tests/real/writes.spec.ts
  tests/real/lifecycle.spec.ts --grep 'real disk create|committed (POST|PATCH) response loss|successful metadata-only write'`:
  **four passed**. Includes committed POST and PATCH loss with real backend outages, explicit
  checks, exactly one write and search-label invalidation; also conflict, exact capacity and null
  clearing. Existing owned disposable-catalog harness cleaned up its processes/catalogs.

Playwright suites ran sequentially; browser captures were preserved before real tests.
No full deployed matrix/full browser suite/backend race/benchmark checks are claimed; U14 owns
integration. No workspace `coldcat.sqlite`, lock or `.million-distributions-*` was touched.

## Screenshot evidence and policy

**No U06 images are committed.** Inspection artifacts are available in this environment at
`/tmp/opencode/u06-screenshots/`, not durable repository links:

- `list-{1440,1024,768}.png`: list-first default, explicit creation action.
- `discard-{1440,1024,768}.png`: retained create draft and deliberate discard choice.
- `recovery-{1440,1024,768}.png`: unresolved outcome and focused saved-change check.
- `edit-{1440,1024,768}.png`: explicit edit panel with exact maximum-capacity input.
- `capacity-{1440,1024,768}.png`: U04 amount/unit/preview in the new panel.

Fifteen deterministic fixture-only Chromium main/form-region captures, 900px viewport height.
Tests assert document width equals viewport width at all three sizes. Representative list,
narrow edit/discard/recovery images were visually inspected; no horizontal clipping was seen.
These are temporary handoff evidence, not pixel regression baselines. Reproduce with the focused
browser command above after build; copy matching PNGs from ignored `frontend/test-results/`
before another suite clears them. The subsequent bounded U01–U05 cleanup is recorded in the
[artifact policy and inventory](../artifact-policy.md): handoff-only images were removed,
and needed U01 before-references were preserved externally because Git LFS is unavailable.
U06's existing external images remain unchanged. No actual regression image baseline was
needed by U06; Git LFS was neither configured nor claimed to work.

## Constraints / next owners

- U07 is next sequential package; U08 still owns compact disk/history rows and detail regrouping.
  Existing disk articles remain deliberately unchanged; list-first does not imply U08 is done.
- U08 must preserve the added `closed` host contract, verified refresh and connection-retained
  editing owner. Do not remount the form when reads/revisions change or hide recovery on success.
- Optional metadata remains directly editable after required fields; no new upload/import,
  delete, undo, server-side cancellation or across-route draft persistence is introduced.
  Native navigation remains native; Cancel is a local draft lifecycle action, not an undo API.
- No new architecture decision beyond the approved UI direction; the plan's optional shared
  UI/unit ADR proposal remains available, not automatically created.
- Unrelated frontend/root AGENTS changes, catalogs and measurement directories remain unstaged.
