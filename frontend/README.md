# Coldcat frontend foundation

F0 foundation and F1 wire/domain helpers: no catalog feature UI or Go asset hosting yet. Backend acceptance remains
open. Architecture: [ADR 0002](../doc/adr/0002-static-browser-frontend.md).

## Toolchain and commands

Use Node **24.21.0** and npm **11.19.0** (enforced by `.npmrc`/engines). From `frontend/`:

```sh
npm ci
npm run check
npm run lint
npm run test:unit
npm run api:check
npm run test:api-generation
npm run test:proxy
npm run build
npx playwright install chromium
npm run test:e2e
```

Linux also needs Playwright's Chromium system libraries. They are available in the current
image: project-local `npm ci`, `npx playwright install chromium`, and `npm run test:e2e`
passed (2 smoke tests). No global browser or executable override was needed.
Earlier launches failed on missing `libnspr4.so`; an authorized system dependency install
failed at `su` authentication. Those are historical results, not a current blocker.
For a different image without system libraries, provision them with the authorized
`npx playwright install-deps chromium` using appropriate system privileges. Installing the
browser alone does not supply system libraries; a launch failure is not a browser pass.
Chromium is the bootstrap test target, not a finalized supported-browser policy.

`test:unit` runs Vitest/jsdom unit/component tests. `test:proxy` owns a disposable loopback
HTTP fixture and Vite process in-process, verifies real forwarding and connection-refusal
502, and shuts both down. The expected ECONNREFUSED log is evidence of the tested outage.
`test:e2e` builds first and owns a test-only static HTTP host on `127.0.0.1:4173` with no
reused server. Its health responses are intercepted or unavailable; it does not start Go,
open a catalog, or prove real-backend workflows (F11a). Browser fixtures never touch the
workspace catalog.

## Wire client and domain helpers (F1 handoff)

`npm run api:generate` regenerates `src/lib/api/generated/{wire,contract}.ts` from the
local `../doc/openapi.json`. Commit both outputs with contract changes. `npm run api:check`
regenerates in memory and fails on drift without writing. `test:api-generation` checks
determinism, source/output drift, read-only failure and unsupported schema assertions in
owned temporary directories; it does not modify the real contract or generated files.

The dependency-free generator supports this contract's schema subset, not general OpenAPI.
Unknown assertion keywords, reference siblings and unsupported operations fail explicitly.
The generated descriptors power runtime shape validation, avoiding handwritten DTO schemas.
`openapi-typescript@7.13.0` was not installed: its TypeScript 5 peer conflicts with the pinned
TypeScript 6 toolchain. No dependency/lockfile change or forced peer override was needed.

- `src/lib/api/client.ts`: `createClient(fetcher?)` returns explicit wrappers for all 19
  operations. Exported `Client`, `Query<operation>` and `Result<operation>` types use generated
  `operations`; generated `components['schemas'][name]` supplies wire DTOs. Reads accept an
  optional `AbortSignal` (after the query, where applicable); writes also accept one.
- Calls use same-origin relative URLs only, declared query keys, literal empty roots and
  repeated allow/block parameters. Unknown keys fail locally. Glob byte/count limits are
  checked without interpreting patterns; backend query-combination validation stays backend-owned.
- `ApiError.kind`: `request`, `cancelled`, `transport`, `contract`, or `http`. Structured
  HTTP errors retain `status`, `code`, and literal `message`; branch on status/code. Non-JSON
  502/504 proxy errors are transport failures with status; unexpected HTML/JSON/status is a
  contract error. Raw bodies are not error messages. `cause` retains diagnostic context.
  `uncertainWrite` marks post-dispatch transport/cancellation/malformed write responses:
  reconcile by reloading before resubmitting. The client never retries or supplies mock fallbacks.
- Runtime checks enforce required shapes, nullability, enums, decimal strings/range, UTC dates,
  page items/cursor presence and directory-entry kind consistency. Returned DTOs remain JSON
  serializable and unchanged. This is not full domain-semantic or cross-resource validation.
- `buildDiskPatch(before, draft)` emits only changed metadata, canonicalizes capacity and
  treats null/empty optional text as equivalent clearing. An unchanged draft returns `{}`;
  do not submit it (the wrapper rejects empty PATCH). Changed text is never trimmed.
- `src/lib/format/`: exact BigInt arithmetic/locale counts and byte text (`Unknown` vs `0 B`),
  signed-64-bit capacity validation, safe percentages (null for an empty denominator),
  zero-safe **content-level** other counts, labeled dates retaining exact UTC fractional
  seconds, slash-only breadcrumbs/containing directories and encoded directory URLs.
  `otherCount` is not observation-specific replica subtraction.
- `tests/fixtures/api.ts` exports fresh generated-type disk/search fixtures. OpenAPI examples
  are separately tested endpoint shapes, not a linked catalog or real-backend workflow.

F2 must add request generations, traversal/revision invalidation and connection lifecycle;
passing a signal alone does not solve response ordering. No UI, search state, pagination cache
or Go hosting is introduced by F1. The F0 shell remains unchanged until F2 integrates the client.

## Development

Start the backend separately with a disposable catalog, following
[backend integration](../doc/backend-integration.md). Stop it before CLI catalog operations;
never remove a lock file. Then:

```sh
npm run dev
```

Vite binds loopback, normally port 5173, and fails if its chosen port is occupied. Only
`/api/v1` paths and exact `/healthz` are proxied, preserving path/query. To select another
backend loopback origin:

```sh
COLDCAT_BACKEND=http://127.0.0.1:8081 npm run dev
```

Non-loopback targets, credentials, query/fragment, and non-root target paths fail configuration.
The shell checks `/healthz` once on mount and offers explicit retry on failure; no catalog
requests, polling, cached/demo responses, or automatic retry. Connection lifecycle will be
replaced by F2's shared primitives rather than expanded here.

## Built route and hosting contract (F10 handoff)

- `npm run build` writes `build/index.html` and `build/_app/` assets. Serve at origin root;
  a subpath deployment is not configured. No runtime Node service is required.
- Client rendering is global; no prerendered resource pages or server endpoints.
- The host must dispatch `/api/v1/*` and `/healthz` to the backend first, never to HTML.
- Serve existing static files with correct MIME types. Missing assets, including `_app/*`,
  must be real 404s, not HTML 200. Traversal/encoded paths and HEAD require F10 tests.
- Application navigation routes from the approved plan are `/`, `/contents`,
  `/contents/[id]`, `/observations/[id]`, `/disks`, `/disks/[id]`, `/snapshots/[id]`, and
  `/snapshots/[id]/directory`. Return `index.html` for their direct document requests;
  query strings do not change asset dispatch. Arbitrary directory paths stay in `?path=`.
- Only `/` is implemented in F0. A built nested navigation loads the shell and displays
  the client “Page not found” boundary until that feature route exists. Unknown routes
  are not approved production fallback destinations.
- `tests/serve-built.ts` is a test-only reference host with broader document fallback to
  exercise future nested URLs. Do not ship it or copy it as the production security handler.

Manual screen-reader/keyboard audits and browser support/measurement thresholds remain
F11b/F12 work. No backend benchmark or full Go race check is needed for pure frontend F0/F1.
