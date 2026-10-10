# ADR 0002: SvelteKit static SPA with same-origin Go asset-directory hosting

Status: accepted by explicit user authorization on 2026-10-10.

## Context

Coldcat is a local, private catalog service with a JSON API, exclusive catalog ownership,
and no authentication or CORS configuration. The approved F0–F12 frontend needs navigation,
deep links, keyboard-friendly workflows, and a build independent of ordinary Go checks.
Frontend development is authorized in parallel with the unresolved backend acceptance gate;
this decision does not approve or waive that gate.

## Decision

Use current stable Svelte/SvelteKit, strict TypeScript, npm with exact dependency versions
and a committed lockfile, and adapter-static with client rendering (`ssr=false`). Build an
`index.html` SPA shell plus static assets in `frontend/build/`. Node is build/development
tooling only, not a production application server.

Development binds Vite to loopback and proxies only `/api/v1/*` and `/healthz` to an explicit
HTTP loopback origin (`COLDCAT_BACKEND`, default `http://127.0.0.1:8080`). Backend/proxy
failures are visible; there is no demo-response fallback or browser security workaround.

F10 implements Go same-origin hosting through the explicit `serve --assets` option. Reserve
API/readiness routes before static dispatch, serve the shell for recognized application
deep links, and return real 404s for missing assets. Production must test encoded paths,
traversal, HEAD, missing routes/assets, and reserved-route precedence. F0's Node static test
host demonstrates built-shell loading only; it is not the production server implementation.

The full initial scope and desktop-only disableable type-to-search default are approved.
The latter belongs to F3, not bootstrap. Imports remain CLI-only with the server stopped.

## Alternatives considered

- Bare Svelte: less framework tooling but would require custom routing/navigation conventions.
- SvelteKit SSR/Node runtime: adds a second persistent server without benefit for this local
  workflow; the Go service remains the sole API and catalog owner.
- Separate-origin production hosting/CORS: adds origin configuration and deployment concerns;
  same-origin hosting avoids that requirement.
- Embedding generated assets in Go: may help later packaging, but would couple frontend
  builds to Go distribution. Reconsider separately if packaging warrants it.

## Consequences

Builds require the pinned Node/npm toolchain and frontend checks. Deployment must supply
the built directory explicitly; without `--assets`, Go still serves JSON only. Deep-link reloads
require the documented shell dispatch contract rather than a generic file server.
Wire type generation/client work starts at F1; no Kit server routes or SQLite frontend
access. Browser support and UI performance budgets remain release decisions, not backend
latency acceptance. Loopback defaults do not make unauthenticated public exposure safe.
