# Development

neoserver is a Go application with workspace-scoped protocol handlers, a management API, datasource adapters, and an encrypted DuckDB configuration store.

## One-command contributor startup

Install Node 24 (or 22.22+), npm, Go 1.26.8+, a C/C++ toolchain and GDAL development files. On macOS, use `xcode-select --install` and `brew install gdal`; on Debian/Ubuntu, install `build-essential libgdal-dev gdal-bin`. Then from the repository root:

```bash
make doctor
make dev
```

`doctor` reports missing dependencies and fixes without starting services. `dev` installs UI dependencies if absent, builds the backend using the platform linker, initializes a separate `.cache/dev/neoserver.db` if needed, prints a 24-hour super-admin JWT, waits for backend readiness, and starts Vite at **http://localhost:5173/admin/**. Paste the JWT into Token sign-in. The first build and extension download need network access and may take several minutes. Re-run `make ui-install` after dependency changes.

This local-only runner uses `abc123`, loopback ports 9000/5173, the explicit `testing/tutorial/development.toml`, and managed uploads under `.cache/dev/imports`. It ignores inherited `NEOSRV_*` / `DATABASE_URL` settings to avoid touching an operator's catalog or services; it sets Vite's native development environment variables for the proxy, not browser secrets. PostGIS is optional: start with the supplied `testing/tutorial/places.geojson` upload and the console's getting-started guide. The [UI tutorial](getting-started-ui.md) explains the workflow. For PostGIS inside Docker, run its Compose profile and use `localhost` as the store host from this native backend (`db` only works between containers).

Ctrl-C stops both processes without deleting data. The next `make dev` reuses the catalog and issues a fresh JWT **before** starting the backend. Occupied ports fail with an explanation; no existing process is killed. An interrupted/crashed runner may leave `.cache/dev/running`; verify no runner/backend remains, then remove only that empty lock directory with `rmdir .cache/dev/running`. Do not delete the database to resolve a lock.

For an existing operator configuration, use the separate `make run` / `make ui-dev` commands below instead. Windows contributors should use WSL2 or the Docker tutorial.

### Focused feedback loop

```bash
make test-dev-tools
make test-focused PKG=./internal/mgmt TEST=TestConsole
cd web/admin
npm test -- src/features/endpoints src/features/workspaces
npm run lint
npm run test:browser -- onboarding.spec.ts
# Full live fixture (Docker + Playwright Chromium required):
cd ../..
make ui-e2e
```

The browser-mock tests exercise interactions without Docker; live tests prove publication and external key access against a real backend. Human usability and assistive-technology evaluation are tracked separately in [the usability release gate](usability-testing.md).

## Package map

| Package | Responsibility |
| --- | --- |
| cmd/neoserver | CLI and process lifecycle |
| internal/server | HTTP middleware and route mounting |
| internal/mgmt | Management handlers and OpenAPI |
| internal/workspace | Runtime workspace registry |
| internal/store | Encrypted persistence and security state |
| internal/datasource | Datasource interfaces and adapters |
| internal/ogc | OGC API - Features |
| internal/wms | WMS 1.3.0 |
| internal/wfs | WFS 2.0 |
| internal/tiles | OGC API - Tiles |
| internal/identity and internal/rbac | Authentication and authorization |
| internal/cache | Response caches |
| internal/renderer and internal/sld | Map rendering and styling |
| internal/observability | OpenTelemetry integration |

## Build and run

~~~bash
make build
# Go-only build (no UI rebuild):
go build ./cmd/neoserver
~~~

Use the supported Go toolchain declared in `go.mod` (currently Go 1.26.8).
On macOS, use the Make targets or pass
`-ldflags=-extld=$PWD/scripts/native-linker.sh` to a Go-only build/test.
The wrapper selects the C driver when DuckDB already links `libc++`, and
the C++ driver when GDAL-only tests need an implicit runtime. GDAL linkage is
owned by `godal`; our multidimensional bridge imports only GDAL's compiler
flags through `gdal-headers.pc`. No linker warnings are suppressed.

For local startup:

~~~bash
export NEOSRV_STORE_KEY=abc123
./neoserver init --store-path ./data/neoserver.db
./neoserver serve --debug
~~~

The repository's local-testing convention permits abc123 as NEOSRV_STORE_KEY for disposable development stores. Never use it in production.

To run the backend from source against an already initialized store:

~~~bash
NEOSRV_STORE_KEY=abc123 make run
# Optional CLI arguments:
NEOSRV_STORE_KEY=abc123 make run RUN_ARGS='serve --debug'
~~~

`make run` handles the macOS linker selection automatically and does not
rebuild the UI. The direct macOS equivalent is
`NEOSRV_STORE_KEY=abc123 go run -ldflags=-extld=$PWD/scripts/native-linker.sh ./cmd/neoserver serve`.
Plain `go run` still selects the C++ driver on macOS and duplicates
DuckDB's `-lc++`; use the explicit flag when bypassing Make.

## Unit tests

~~~bash
make test
make test-race
go test -v ./internal/filter/...
~~~

Use package-focused tests while developing and the full suite before handoff. Tests create temporary stores and datasource fixtures where possible.

## Administration console

The React 19/Vite console lives in `web/admin` and builds into `internal/admin/dist`, which Go embeds in the binary. Use `nvm use` in `web/admin` for Node 24, matching the container and CI. Node 22.22+ within the 22.x line is also supported; odd/current Node releases are not the reproducible test target.

~~~bash
make ui-install
make ui-dev
make ui-build
make ui-test
make ui-lint
~~~

The development server proxies API requests to the configured local neoserver. The production Vite configuration uses a relative base, so the console works below `Server.BasePath` without injected runtime variables. `make build` builds the UI first when Node/npm are available; a Go-only environment embeds the tracked placeholder page instead.

### Sign in locally

With the backend running on port 9000, run `nvm use` and `npm ci` in
`web/admin`, then `npm run dev`. Open `http://localhost:5173/admin/`, choose
**Token / API key**, and paste the bootstrap JWT printed by `neoserver init`
without a `Bearer` prefix. `NEOSRV_STORE_KEY` encrypts the database; it is **not**
a login credential.

If the store already exists and you no longer have a valid JWT, stop the backend
first (DuckDB permits only one process to open this store), then from the repository root:

~~~bash
NEOSRV_STORE_KEY=abc123 make run RUN_ARGS='create-token --store-path ./data/neoserver.db --role super_admin'
NEOSRV_STORE_KEY=abc123 make run
~~~

Use the token printed by the first command to sign in. Both commands must use
the same store path, configuration, and encryption key as initialization. Do not
reinitialize an existing store or change its encryption key to resolve a login
failure. `abc123` is only for disposable local development stores.

### Inspect the UI bundle

Run `npm run analyze` in `web/admin` to build and print raw/gzip sizes for each
JavaScript chunk, classified as initial or lazy using Vite's manifest dependency
graph. For machine-readable output from an existing build, use
`node scripts/analyze-bundle.mjs --json`. `npm run check:budget` independently
enforces the initial-load budget; lazy map code is not counted as initial code.

For another backend port or `Server.BasePath`, create `web/admin/.env.local`
using `.env.example` as a reference:

~~~dotenv
NEOSRV_DEV_API_TARGET=http://localhost:9100
NEOSRV_DEV_BASE_PATH=/geo
~~~

Restart Vite, then open `http://localhost:5173/geo/admin/`. These are Vite
configuration variables, not injected browser globals or production settings.
The prefix must match the running server; omit it for the default `/admin/`.

Management types are generated from the server-owned OpenAPI document:

~~~bash
make ui-openapi
git diff --exit-code -- web/admin/openapi.json
~~~

The drift check fails when the committed `web/admin/openapi.json` differs from the generated API. Generated client files are not committed. `npm run codegen` also derives committed form metadata from the OpenAPI schemas. `npm run check:form-schemas` detects stale metadata; labels, selectors, and editable-field curation live separately in `src/lib/schema-fields.ts` and `src/lib/resource-schemas.ts`. Forms preserve settings they do not expose, and advanced JSON remains optional.

This check is load-bearing rather than cosmetic: **every console screen calls the
generated hooks**, so the OpenAPI document is the source of truth for request
shapes, response types and query keys. Two rules follow.

- When a management handler gains or changes a field, update the schema in
  `internal/mgmt/openapi_schemas.go` (or `openapi_console_schemas.go`), then run
  `make ui-openapi` and `npm run codegen`. A field the server sends but the spec
  omits is invisible to the console and will surface as a missing property in
  TypeScript.
- Invalidate caches with the generated key helpers
  (`getListServicesQueryKey(ws)`), never a hand-built array. A wrong key
  compiles cleanly and silently stops a list refreshing after a mutation.

Files under `web/admin/src/components/ui` must be created only by the shadcn CLI. Inspect the registry entry first, add it with `npx shadcn@latest add <component>`, and update `components-manifest.json` through the repository's provenance workflow. Do not hand-author, reconstruct, or copy a component into that directory; project-specific components belong in `src/components`. `npm run check:ui-provenance` makes this a build failure rather than a review convention.

### Management API docs

`/api/v1/api.html` renders Swagger UI. The server sends a restrictive
Content-Security-Policy (`default-src 'self'; script-src 'self'`), so the page
cannot load a CDN bundle or run an inline initialiser. Instead:

- `web/admin/scripts/copy-swagger-assets.mjs` copies Swagger UI's runtime out of
  `node_modules` into the console build output after `vite build`, where it is
  embedded in the binary and served from `/admin/vendor/swagger/`.
- The initialiser is served separately from `/api/v1/api.js`.

Because the runtime ships with the console, the docs page needs the console to
have been built and left enabled. A binary built without Node, or one running
with `Server.AdminUI=false`, shows an explanatory message with a link to the raw
OpenAPI document at `/api/v1/api` rather than a blank page.

Do not "fix" a blank docs page by loosening the CSP or pointing the page at a
CDN; run `make ui-build`.

### TypeScript 7 and the ESLint bridge

The console compiles with TypeScript 7, but typescript-eslint 8 still targets the
TypeScript 6 compiler API. Rather than hold the application back, both versions
are installed: `typescript` (7.x) builds the app, and `typescript-eslint-ts` — an
alias for `typescript@6` — is used only by the linter.

`npm run lint` therefore runs ESLint through
`scripts/eslint-typescript-bridge.mjs`, a small `node:module` hook that redirects
`import "typescript"` to the aliased 6.x copy, but only for requests originating
inside `node_modules`. Application code is untouched.

Remove all three pieces — the alias dependency, the bridge script, and the
indirection in the `lint` script — as soon as typescript-eslint supports the
TypeScript 7 compiler API. Verify by running `npm run lint` with the bridge
bypassed (`npx eslint .`); if it succeeds, the workaround is obsolete.

The same version split is why `web/admin/.npmrc` sets `legacy-peer-deps=true`:
typescript-eslint 8 peers on TypeScript below 6.1 while the app compiles with
TypeScript 7. That file must be copied into the container **before** `npm ci`
runs, which is why the Dockerfile copies it alongside `package*.json`. Drop it
at the same time as the bridge.

### Console end-to-end tests

The fast browser regression suite uses mocked management APIs and runs without
Docker, a server, tokens, or changes to local catalog data:

~~~bash
cd web/admin
npm ci
npx playwright install chromium
npm run test:browser
~~~

It covers rendered switch/sidebar state, accessibility in both themes and
sidebar sizes, dirty editors, failed reads and writes, workspace cache refresh,
5,000-source discovery, structured import planning, and actual map rendering.
The fixtures reject unexpected routes and validate JSON requests and responses
against the committed OpenAPI schemas, including timestamp formats. Session
changes, API-key expiry/revocation, SQL/raster publication, invalid table links,
and opened operational dialogs have dedicated regressions. Common middleware
errors use the server's shared error schema even where an older operation omits
an explicit error response declaration.
CI runs both development and production-asset browser suites alongside unit tests, schema/provenance checks, and the unchanged
250 KiB initial-JavaScript budget. Maps and larger workflow routes load lazily.
The budget check walks static imports in Vite's build manifest and counts shared
chunks once; lazy chunks are not classified by their filenames.

To run the same regressions against bundled assets, use `npm run build` then
`UI_TEST_BUILD=true npm run test:browser`. The Vite preview server mirrors the
Go console handler's nested relative-asset routing. This still mocks the APIs;
it does not replace the live server suite below.

The separate live console suite runs against a real neoserver — not the Vite dev server and
not route mocks — so a passing `publish-layer` means a layer genuinely reached
the catalog and the workspace's OGC API serves it.

~~~bash
echo "127.0.0.1 keycloak" | sudo tee -a /etc/hosts   # one-time, see below
make ui-e2e
~~~

`make ui-e2e` runs [`scripts/console/run-e2e.sh`](../scripts/console/run-e2e.sh),
which brings up `docker-compose.console-e2e.yml` (PostGIS with the CITE
fixtures, a freshly initialised encrypted catalog, the server on port 19100, and
Keycloak with an imported realm), provisions workspace and API-key fixtures
through the management API, runs Playwright, and tears the environment down.
Set `KEEP_CONSOLE_ENVIRONMENT=true` to leave it running for debugging.

**The `/etc/hosts` entry is required.** neoserver pins both the OIDC issuer and
the audience, so the issuer URL the browser sees on the host and the one the
server verifies inside the compose network must be byte-identical. Mapping
`keycloak` to `127.0.0.1` achieves that. Do not work around it by enabling
`Auth.OIDC.SkipIssuerCheck` — that would remove the very property the test
exists to verify. Without the entry the script stops with instructions rather
than running a weakened suite.

This fixture is deliberately separate from `docker-compose.conformance.yml`,
which backs the pinned official ETS evidence and must not be perturbed.

## Live integration tests

Start the Compose server and seeded PostGIS database, then configure a demo workspace as described in [Getting started](getting-started.md).

~~~bash
make test-ogcapi-integration
make test-wms-integration
make test-wfs-integration
~~~

The Makefile targets use:

- OGC_TEST_URL=http://localhost:9000/workspaces/demo/ogc
- WMS_TEST_URL=http://localhost:9000/workspaces/demo/wms
- WFS_TEST_URL=http://localhost:9000/workspaces/demo/wfs

Package-level integration tests can also be run directly with those environment variables.
The native OGC API suite includes Core `datetime`, strict parameter handling,
lookahead paging, complete OpenAPI discovery, Part 3 Queryables, `filter-lang`,
and CQL2 Basic Spatial coverage. Without `OGC_TEST_URL`, it runs against its
self-contained mock server.

## Protocol integration and conformance tests

Native live protocol regression is self-contained:

~~~bash
make test-protocol-integration
~~~

It starts a disposable Compose environment and runs OGC API Features, WMS,
WFS, WCS 2.0.1/2.1, and OGC API Tiles black-box tests. Ordinary unit tests stay
in `make test`; this target does not invoke TEAM Engine.

Stock official OGC conformance is intentionally separate:

~~~bash
make test-conformance
# or one suite, for example:
make test-conformance-wmts10
~~~

Those targets run digest-pinned official TEAM Engine containers for WMS 1.3,
WFS 2.0, stock WCS 2.0.1 profiles, WMTS 1.0, OGC API Features 1.0, and
OGC API Tiles 1.0. The WCS Interpolation compatibility profile is explicitly
separate:

~~~bash
make test-conformance-derived-wcs20-interpolation
~~~

`make test-assurance-all` runs unit, protocol integration, stock official, and
official-derived categories failure-exhaustively. Detailed semantics, immutable
version pins, artifacts, and truthful-declaration rules are in
[Protocol testing and OGC conformance](conformance.md).

## Smoke testing

testing/smoke_test.sh predates workspace-scoped routing in some commands. For current manual smoke coverage, verify:

~~~bash
curl -fsS http://localhost:9000/health
curl -fsS http://localhost:9000/ready
curl -fsS -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/workspaces/demo/ogc/collections
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/demo/wms?SERVICE=WMS&REQUEST=GetCapabilities"
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/demo/wfs?SERVICE=WFS&REQUEST=GetCapabilities"
~~~

## Documentation maintenance

The implementation is the source of truth:

- Route mounts: internal/server/workspace_router.go and internal/mgmt/api.go
- Request/response types: management handlers and internal/store/types.go
- Global settings: internal/conf/config.go and config/neoserver.toml.example
- Protocol parameters: their workspace handlers

Keep README examples short, move detailed explanations into docs, use /workspaces/{workspace}/... consistently, and update generated management OpenAPI whenever management routes or payloads change.

Run link checks, OpenAPI validation, docker compose config, and relevant Go tests for documentation examples that depend on code.

Related: [Configuration](configuration.md) · [Management API](management-api.md)
