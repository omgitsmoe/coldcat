# Coldcat frontend foundation

F0 bootstrap only: no catalog feature UI or Go asset hosting yet. Backend acceptance remains
open. Architecture: [ADR 0002](../doc/adr/0002-static-browser-frontend.md).

## Toolchain and commands

Use Node **24.21.0** and npm **11.19.0** (enforced by `.npmrc`/engines). From `frontend/`:

```sh
npm ci
npm run check
npm run lint
npm run test:unit
npm run test:proxy
npm run build
npx playwright install chromium
npm run test:e2e
```

Linux also needs Playwright's Chromium system libraries. On this host browser launch fails
because `libnspr4.so` is absent. With system-install approval, provision them using
`npx playwright install-deps chromium`, then rerun `npm run test:e2e`; installing the browser
alone does not supply system libraries. Do not treat a launch failure as a browser pass.
Chromium is the bootstrap test target, not a finalized supported-browser policy.

`test:unit` runs Vitest/jsdom unit/component tests. `test:proxy` owns a disposable loopback
HTTP fixture and Vite process in-process, verifies real forwarding and connection-refusal
502, and shuts both down. The expected ECONNREFUSED log is evidence of the tested outage.
`test:e2e` builds first and owns a test-only static HTTP host on `127.0.0.1:4173` with no
reused server. Its health responses are intercepted or unavailable; it does not start Go,
open a catalog, or prove real-backend workflows (F11a). Browser fixtures never touch the
workspace catalog. `api:generate`/`api:check` are intentionally pending F1.

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
F11b/F12 work. No backend benchmark or full Go race check is needed for pure frontend F0.
