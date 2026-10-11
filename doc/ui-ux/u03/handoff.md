# U03 — Application shell and navigation handoff

**Complete: 2026-10-11.** Implements the owner-approved U01 specification on U02's foundation.
No repeated approval or new architecture decision is required. No U04–U14 implementation is
included; packages remain sequential. No blockers remain.

## Plan and outcome

1. Compose brand, native section links and connection status in one wrapping app bar.
2. Derive active sections from route identity, including nested resources, without requests.
3. Share workspace alignment/gutters and publish an opt-in constrained form body.
4. Preserve skip link, keyboard shortcut and existing connection/retry ownership.
5. Validate dimensions, behavior and screenshots; run frontend checks and focused tests.

All steps completed. Ready/checking bars are 57px including the border, versus U01's 143px.
Outage bars grow naturally: 63px at 1440/1024, 103px at 768. No sticky positioning, clipping,
collapsed navigation or new icon dependency. Readiness uses quiet secondary text; outage uses
explicit emphasized words, an error border and the unchanged Retry connection action.

## Files and interfaces

- `frontend/src/lib/components/Shell.svelte`: preserves optional `brand`, `navigation` and
  `children` snippets. New `shell-bar shell-frame` inner header and `shell-connection` group;
  `unavailable` class is presentation only. Status remains `role="status"`, polite live text.
  The shell still creates/provides one `Connection`, checks on mount, subscribes and disposes.
- `frontend/src/routes/+layout.svelte`: native links and unchanged `GlobalShortcut` placement.
  Active navigation is derived from `page.route.id`, not URL substring or feature reads.
  Search owns `/`; Contents owns content and observation routes; Disks owns disk, inventory
  and directory routes. Unknown routes highlight nothing. Root section links use
  `aria-current="page"`; nested resources use `aria-current="location"` on their owning link.
- `frontend/src/app.css`: shell-scoped composition/navigation/status styles; existing tokens
  and focus treatment retained. No feature-header/navigation styling added.
- `frontend/tests/unit/shell.test.ts`: outage assertions now check the complete semantic
  status text, including the emphasized child; readiness, failures and retry counts preserved.
- `frontend/tests/browser/shell.spec.ts`, `frontend/tests/fixtures/shell.ts`: route ownership,
  unknown routes, native in-app navigation, skip-link focus, Ctrl+K, pending health/catalog
  readiness and explicit retry request counts. Feature fixtures return an explicit 404;
  this is isolated shell behavior, not real-resource acceptance.
- `frontend/tests/visual/u03-shell.spec.ts`, `frontend/playwright.u03.config.ts`: isolated
  Chromium static-host capture and dimension checks. Test clocks freeze existing automatic
  retry scheduling only in tests; production backoff behavior is unchanged.
- `doc/coldcat-frontend-ui-ux.md`, this handoff and `screenshots/manifest.json`: progress and
  measured evidence; screenshot images are external under the artifact policy below.
- `frontend/AGENTS.md`: brief new inspection guidance left **unstaged**, as requested.
  Unrelated untracked root instructions/catalog/lock/measurement directory remain untouched.

### Shared layout contract

| Hook | Contract |
| --- | --- |
| `shell-frame` | Centered border-box frame, maximum 1600px content plus two gutters; header and main align |
| `--page-gutter` | 24px at 1440/1024; 16px at/below 900px, including 768 |
| `shell-main` | 16px vertical padding, main landmark and existing `id="main"` / `tabindex="-1"` |
| `form-body` | Opt-in native form/body class, width 100%, maximum 720px, shrinkable |
| `shell-navigation` | Wrapping native links; active background, weight and underline supplement color |

U06 and other feature owners should apply `form-body` to appropriate forms or form bodies.
Do not constrain whole table workspaces or apply the cap to every form globally: search and
filter toolbars need their workspace width. Existing feature form layouts were not rewritten.
The wide-cap test at 1920px confirms header/main frames are 1648px and centered, with a 720px
form body. Existing shared foundation interfaces remain unchanged.

## Tests and screenshots

From `frontend/`, validated:

- `npm run check`: passed, 0 errors/warnings.
- `npm run lint`: passed.
- `npm run build`: passed.
- `npm run test:unit`: 15 files / 143 passed.
- `npx playwright test --config playwright.u03.config.ts`: five passed, including nine
  state/width combinations and wide workspace/form caps. Bounds, gutters, full document
  width, visible outage action, page errors and shell request counts are asserted.
- `npx playwright test --config playwright.u01.config.ts`: one passed, all 18 representative
  page captures have no horizontal document overflow. U01's original baseline content is preserved.
  Temporary post-U03 full-page evidence was preserved in
  `/tmp/opencode/u03-representative-pages/` before the next suite; it is not a durable baseline.
- `npx playwright test tests/browser/shell.spec.ts tests/browser/smoke.spec.ts
  tests/browser/search.spec.ts tests/browser/inventories.spec.ts tests/browser/details.spec.ts
  tests/browser/directories.spec.ts tests/browser/disk-writes.spec.ts`: 55 passed.
  Existing IME/Back restoration, exact values, independent sections, retained pagination,
  uncertain writes and reconciliation assertions remain intact.

External shell captures are 240px-high viewport crops, Chromium 156.0.8078.4, en-US/UTC,
scale factor 1, from a 900px-high viewport. These are inspected evidence, not pixel goldens.
[Manifest](screenshots/manifest.json) records bounds, gutters and request-state dimensions.

The nine handoff-only images were removed from tracking after preservation at
`/tmp/opencode/ui-ux-artifact-cleanup-20261011/doc/ui-ux/u03/screenshots/`.
Filenames are `{state}-{width}.png`: states `ready`, `checking`, `unavailable`; widths 1440,
1024, 768. The manifest's `screenshot` values resolve under its external `imageRoot`.
See [artifact policy and inventory](../artifact-policy.md); the archive is environment-local,
not durable hosting. Reproduce after build with the U03 capture command above and preserve
ignored outputs externally, not as repository images.

## Constraints and follow-up

- No backend/API/schema/catalog changes, catalog preload, new feature requests or retries.
  All browser checks use intercepted disposable fixtures; no workspace catalog was opened.
- Connection automatic backoff, reconnect/revision invalidation and write ownership remain
  unchanged. Status styling does not create another request owner.
- Skip link targets the same focusable main; Ctrl+K still navigates to Search and focuses its
  query. Native links, browser Back, URL filters and literal resource context are preserved.
- U03 does not redesign search filters, disk lifecycle, feature tables or feedback/pagination.
  Form-body adoption belongs to subsequent feature tasks. Full deployed operation matrix
  remains U14's gate; no real-backend, performance, mobile or extended accessibility claims.
- The approved plan already proposes an optional ADR for shared UI conventions if they become
  architecture policy (decision, context, alternatives, consequences); none was auto-created.
