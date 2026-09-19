# Repository guide for coding agents

Guidance for coding agents working in this repository. `AGENTS.md` is a symlink to this file, so Claude Code and any agent that looks for `AGENTS.md` read the same instructions. Update this file and both stay in step.

## Project Overview

**neoserver** is a modern multi-workspace geospatial server written in Go (1.26.8+, CGO, GDAL). It publishes PostGIS, DuckDB, GeoParquet, GDAL vector-file, and GeoTIFF/COG raster data through OGC API - Features, WMS 1.3.0, WFS 2.0 (including transactions and locking), WCS 2.0.1/2.1, WMTS 1.0.0, and OGC API - Tiles. It is managed through a REST API backed by an encrypted DuckDB store and through an embedded React administration console (`web/admin`, served at `/admin`).

## Build and Development Commands

Prefer the `make` targets: on macOS they add a custom native linker (`scripts/native-linker.sh`), which a plain `go build` / `go test` does not use.

```bash
# Contributor setup
make doctor        # check Node/Go/GDAL/CGO toolchain
make dev           # isolated local catalog in .cache/dev (key abc123), backend + Vite dev server
make test-dev-tools

# Build (also builds the admin UI when node/npm are available)
make build
make release-build VERSION=v0.1.0   # requires the real UI build
make run RUN_ARGS=serve              # backend only

# Version literals (Go fallback, Dockerfile args, console package, CI smoke test)
make set-version VERSION=v0.1.0     # rewrite, then regenerate openapi.json
make check-version                  # CI enforces this

# Go tests and vet. Both run GO_PACKAGES, which is ./... minus
# web/admin/node_modules -- npm dependencies ship Go sources of their own.
make test                                            # whole suite, -p 1
make vet
make test GO_TEST_FLAGS=-count=1                     # uncached, as CI runs it
make test-race                                       # race tests for concurrent packages
make test-focused PKG=./internal/mgmt TEST=TestName  # single package/test

# Initialize the encrypted backing store (prints a one-time bootstrap JWT)
export NEOSRV_STORE_KEY="$(openssl rand -hex 32)"
./neoserver init --store-path ./data/neoserver.db

# Start the server (WMS/WFS/WCS/WMTS/Tiles/Importer/Auth are disabled by default; enable via config)
./neoserver serve
./neoserver serve --debug          # debug logging
./neoserver serve --config path.toml

# Other CLI subcommands
./neoserver create-token --role super_admin
./neoserver rotate-signing-key
./neoserver add-claim-mapping
./neoserver openapi-dump --output web/admin/openapi.json
./neoserver install-extensions   # download/load matching DuckDB extensions; no catalog needed
./neoserver version

# Docker compose (PostGIS + sample data + server)
make up        # docker compose up --build
make down
make reset-demo

# Smoke test (requires running server)
BASE_URL=http://localhost:9000 ./testing/smoke_test.sh
```

### Admin console (`web/admin`)

Requires Node 24 or 22.22+.

```bash
make ui-install    # npm ci
make ui-dev        # Vite dev server (run the backend separately)
make ui-build      # production build, embedded into the Go binary
make ui-test       # Vitest unit tests
make ui-lint       # ESLint + Prettier check
make ui-check      # shadcn provenance, tests, build, bundle budget (250 KiB gzipped initial JS)
make ui-e2e        # Playwright suite against disposable neoserver + PostGIS + Keycloak
make ui-openapi    # regenerate web/admin/openapi.json from the Go server
cd web/admin && npm run codegen   # regenerate orval React Query client + form schemas
```

When management API handlers or schemas change, run `make ui-openapi` and then `npm run codegen`. `npm run check:form-schemas` detects drift in the generated form schemas.

`make ui-e2e` rebuilds the server image and needs Docker plus free host ports 8080 (Keycloak, hard-coded in `docker-compose.console-e2e.yml`) and 19100 (`CONSOLE_HTTP_PORT`). When the local image already contains the current code, `CONSOLE_SKIP_BUILD=true` skips the rebuild; `KEEP_CONSOLE_ENVIRONMENT=true` leaves the stack running, and extra arguments pass through to Playwright (`./scripts/console/run-e2e.sh -g "pattern"`). Console wording lives in `src/lib/display.ts` and `src/lib/format.ts`; changing it breaks assertions in `e2e/` and `browser-tests/`, which the mocked suites do not catch.

### Integration and conformance tests

Require a running server with a configured `demo` workspace (see docs/getting-started.md):

```bash
make test-ogcapi-integration   # OGC API - Features suite
make test-wms-integration      # WMS suite
make test-wfs-integration      # WFS suite (incl. filter, paging, stored queries)
make test-wcs-integration      # Native live WCS 2.0.1 and 2.1 regression suite
make test-protocol-integration # All native live protocol suites in one fixture
make test-conformance          # Stock digest-pinned official OGC ETS suites
make test-conformance-derived  # Explicitly patched official-derived ETS profiles
make test-conformance-{wms13,wfs20,wcs20,wmts10,ogcapi-features10,ogcapi-tiles10}
make test-assurance-all
```

Official ETS execution pulls digest-pinned TEAM Engine images; `testing/officialets/versions.lock.json` is the source of truth for those digests and profiles. The suites themselves live in `testing/` (officialets, protocol, qualification, console, tutorial, and the per-protocol directories), and their checked-in fixtures under `testing/fixtures/`. Tests must read only from the repository, never from a local checkout of an upstream project.

## Local testing
When creating new access tokens locally, use abc123 as the NEOSRV_STORE_KEY

## Architecture

### Entry Point
- [cmd/neoserver/main.go](cmd/neoserver/main.go): CLI dispatch (`init`, `serve`, `create-token`, `rotate-signing-key`, `add-claim-mapping`, `openapi-dump`, `version`), config loading, and signal handling. Unknown or missing subcommands fall back to `serve`.

### Core Internal Packages

- **[internal/server/](internal/server/)**: HTTP server setup with the chi router. Mounts `/api/v1`, `/health`, `/ready`, protected pprof, `/admin`, managed assets, and workspace routes via `WorkspaceRouter` ([workspace_router.go](internal/server/workspace_router.go)). Also wires the persistent tile cache.
- **[internal/conf/](internal/conf/)**: Configuration loading via Viper (TOML + env vars with the `NEOSRV_` prefix). Holds the defaults and build version info.
- **[internal/mgmt/](internal/mgmt/)**: Management REST API handlers (`/api/v1/*`, route tree in [api.go](internal/mgmt/api.go)), the generated OpenAPI spec, Swagger UI, and console login/session endpoints.
- **[internal/admin/](internal/admin/)**: Embedded administration console assets, SPA routing, and browser security headers.
- **[internal/workspace/](internal/workspace/)**: Runtime workspace registry and workspace middleware. Handles per-layer visibility (`Layer.VisibleToRole` / `Workspace.VisibleLayers`).
- **[internal/store/](internal/store/)**: Encrypted DuckDB backing store (DuckDB native `ATTACH ... ENCRYPTION_KEY`). Stores workspaces, services, layers, coverages, layer groups, styles, style assets, imports, API keys, JWT signing keys, sessions, claim mappings, and RBAC state.
- **[internal/cataloglifecycle/](internal/cataloglifecycle/)**: Coordinates referentially complete publication and deletion (deletion plans, retryable deletion operations).
- **[internal/identity/](internal/identity/)**: Authentication (API key, self-signed JWT, basic/static auth, OIDC, browser sessions) and identity middleware.
- **[internal/rbac/](internal/rbac/)**: Casbin-based role-based access control at the workspace level. Per-layer read access is enforced separately in the protocol handlers via the workspace visibility helpers. These helpers block direct access to restricted layers and filter them out of listings and capabilities.
- **[internal/audit/](internal/audit/)**: Audit event log with retention.
- **[internal/protocolrequest/](internal/protocolrequest/)**: Resolves protocol routing semantics once, before authentication.
- **[internal/ogc/](internal/ogc/)**: OGC API - Features handlers.
- **[internal/wms/](internal/wms/)**: WMS 1.3.0 handlers: GetCapabilities, GetMap, GetFeatureInfo, GetLegendGraphic. Supports layer groups.
- **[internal/wfs/](internal/wfs/)**: WFS 2.0 handlers: GetCapabilities, DescribeFeatureType, GetFeature, GetPropertyValue, stored queries, Transactions, and feature locking. Includes the FES filter compiler.
- **[internal/wcs/](internal/wcs/)**: WCS 2.0.1/2.1 GET/KVP coverage service.
- **[internal/wmts/](internal/wmts/)**: WMTS 1.0.0 KVP and REST bindings (backed by the tiles engine).
- **[internal/tiles/](internal/tiles/)**: OGC API - Tiles: Mapbox Vector Tiles, raster map tiles, TileJSON, and tile matrix sets.
- **[internal/tilecache/](internal/tilecache/)**: Persistent tile cache (local/S3 with leases).
- **[internal/tilejobs/](internal/tilejobs/)**: Durable, resumable tile-cache seed/maintenance jobs.
- **[internal/importer/](internal/importer/)**: Durable, bounded managed-vector upload/import jobs (plan → preview → publish/rollback). Publication is asynchronous.
- **[internal/mosaiccatalog/](internal/mosaiccatalog/)**: Optional operational index for managed raster mosaics (granules, harvest jobs).
- **[internal/filter/](internal/filter/)**: CQL2 text filter parser. Compiles to parameterized SQL (PostGIS and DuckDB dialects).
- **[internal/query/](internal/query/)**: bbox and CRS parameter parsing.
- **[internal/crs/](internal/crs/)**: CRS utilities.
- **[internal/sld/](internal/sld/)**: SLD (Styled Layer Descriptor) XML parser for WMS styling.
- **[internal/stylegraphics/](internal/stylegraphics/)**: Resolves managed and allowlisted remote graphics for styles.
- **[internal/renderer/](internal/renderer/)**: Image rendering for WMS GetMap and raster tiles (uses fogleman/gg).
- **[internal/cache/](internal/cache/)**: In-memory caching using ristretto for capabilities, collections, features, and tiles.
- **[internal/gdalcap/](internal/gdalcap/), [internal/gdalmd/](internal/gdalmd/)**: Optional GDAL driver capability checks and a minimal binding to GDAL's multidimensional C API.
- **[internal/conformance/](internal/conformance/)**: Canonical OGC requirement-class identifiers.
- **[internal/observability/](internal/observability/)**: OpenTelemetry traces and metrics (OTLP export).
- **[internal/httputil/](internal/httputil/)**: Shared HTTP helpers.

### Data Source Packages

Service types (`store.ServiceType`): `postgis`, `duckdb`, `geoparquet`, `vectorfile`, `rasterfile`, `raster_mosaic`.

- **[internal/datasource/](internal/datasource/)**: Common interfaces ([source.go](internal/datasource/source.go)) and the factory (`datasource.CreateFromService`). `DataSource` covers vector features; `CoverageDataSource` covers WCS raster data.
- **[internal/datasource/postgis/](internal/datasource/postgis/)**: PostGIS data source (PostgreSQL with geometry columns).
- **[internal/datasource/duckdb/](internal/datasource/duckdb/)**: DuckDB spatial data source.
- **[internal/datasource/duckdbsqlview/](internal/datasource/duckdbsqlview/)**: SQL view layers on DuckDB (see docs/sql-view-security.md).
- **[internal/datasource/geoparquet/](internal/datasource/geoparquet/)**: GeoParquet files (local and remote via httpfs).
- **[internal/datasource/vectorfile/](internal/datasource/vectorfile/)**: GDAL-based vector files (Shapefile, GeoPackage, GeoJSON, etc.).
- **[internal/datasource/rasterfile/](internal/datasource/rasterfile/)**: GeoTIFF / Cloud Optimized GeoTIFF coverages.
- **[internal/datasource/rastermosaic/](internal/datasource/rastermosaic/)**: Managed collections of compatible GeoTIFF granules.
- **[internal/datasource/rastergrid/](internal/datasource/rastergrid/)**: Shared GDAL target-grid operations for raster backends.
- **[internal/datasource/pathpolicy/](internal/datasource/pathpolicy/)**: File path allowlisting and remote (HTTPS) fetch policy with local caching.

### Admin Console (`web/admin/`)

The console is built with React 19, Vite, TypeScript, Tailwind 4, shadcn/Radix, TanStack Query/Table, react-router, react-hook-form + zod, MapLibre (lazy-loaded), and CodeMirror.
- `src/api/`: `client.ts` (the `apiFetch` mutator), session handling, and `generated/` (orval output from `openapi.json`; do not edit by hand).
- `src/app/`: App shell, router, and providers.
- `src/features/`: Feature areas: workspaces, catalog, imports, styles, tiles, cache, endpoints, security, settings, operations, viewer, shared.
- `src/components/`: Shared components. `components/ui` holds the shadcn components, tracked by `components-manifest.json` and checked by `check:ui-provenance`.
- Tests: Vitest + Testing Library + MSW (`*.test.tsx`), Playwright specs in `e2e/` and `browser-tests/` (including axe accessibility checks).

### Request Flow

1. `server.New()` initializes the chi router, preloads DuckDB extensions (spatial, httpfs), and mounts `/api/v1`, `/health`, `/ready`, pprof, `/admin`, and the workspace router.
2. The management API (or the admin console) creates workspaces, services (data sources), layers, coverages, layer groups, styles, imports, and API keys in the encrypted backing store.
3. The workspace router serves `/workspaces/{workspaceId}/ogc`, `/wms`, `/wfs`, `/wcs`, `/wmts`, and `/ogc-tiles`. `workspace.Middleware` loads the workspace into the request context.
4. The data source factory (`datasource.CreateFromService()`) creates the appropriate data source for a service.
5. `filter.Compile()` / `filter.CompileForDuckDB()` translate CQL2 text filters into parameterized SQL. The WFS FES compiler does the same for XML filters.

### Key Dependencies

- **chi**: HTTP router
- **pgx/v5**: PostgreSQL driver
- **duckdb-go**: DuckDB driver (spatial + httpfs extensions; also the encrypted backing store)
- **godal**: GDAL bindings (vector files, rasters)
- **viper**: Configuration management
- **kin-openapi**: OpenAPI schema generation
- **gg**: 2D graphics library for WMS/tile image rendering
- **orb**: Geometry types and MVT encoding
- **go-oidc / go-jose**: OpenID Connect and JWT
- **casbin**: Role-based access control
- **ristretto**: In-memory response cache
- **aws-sdk-go-v2 (S3)**: Remote tile cache storage
- **pg_query_go**: SQL parsing for SQL view validation
- **opentelemetry-go**: Traces and metrics

## Configuration

Configuration comes from a TOML file (`neoserver.toml`, searched in `./config/`, `/config/`, and `/etc/`, or set with the `--config` flag) or from environment variables with the `NEOSRV_` prefix:

```bash
export NEOSRV_STORE_KEY="..."           # encryption key for the backing store (required)
export NEOSRV_SERVER_HTTPPORT=9000
export NEOSRV_SERVER_URLBASE="http://localhost:9000"
export NEOSRV_SERVER_BASEPATH="/api"
export NEOSRV_WMS_ENABLED=true
export NEOSRV_WFS_ENABLED=true
export NEOSRV_WCS_ENABLED=true
export NEOSRV_WMTS_ENABLED=true
export NEOSRV_TILES_ENABLED=true
export NEOSRV_IMPORTER_ENABLED=true
```

Notes:
- WMS, WFS, WCS, WMTS, OGC API - Tiles, Importer, MosaicCatalog, and Auth are **disabled by default**. OGC API - Features is always on.
- `DATABASE_URL` is accepted as a back-compat alias for `Database.DatabaseURL`.
- Per-collection overrides use the format `[Collections."schema.table"]` in TOML.
- File-based data sources are restricted to `Datasource.AllowedPaths` (default `./data/**`). Managed imports live under `Importer.Root` (default `./data/imports`).

See [docs/configuration.md](docs/configuration.md) for the full reference.

## API Routes

### Management API (`/api/v1`, authenticated)
- `/api/v1/auth/{login,me,logout,refresh,sessions}`, `/api/v1/console/config`: Console/browser sessions
- `/api/v1/workspaces`: CRUD for workspaces (list/create/delete are super_admin only). Also `/summary`, `/roles`, `/deletion-plan`.
- `/api/v1/workspaces/{workspace}/services`: CRUD for services (data sources), plus `test-connection` and `deletion-plan`
- `/api/v1/workspaces/{workspace}/services/{service}/discover`, `/discover-coverages`: Layer/coverage discovery (POST)
- `/api/v1/workspaces/{workspace}/services/{service}/validate-sql`: SQL view validation (POST)
- `/api/v1/workspaces/{workspace}/services/{service}/layers`: CRUD for published layers
- `/api/v1/workspaces/{workspace}/services/{service}/coverages`: CRUD for published WCS coverages
- `/api/v1/workspaces/{workspace}/services/{service}/mosaic/{granules,harvest-jobs}`: Raster mosaic management
- `/api/v1/workspaces/{workspace}/imports`: Managed uploads (`plan`, `preview`, `publish`, `retry`, `rollback`, `history`)
- `/api/v1/workspaces/{workspace}/layer-groups`: CRUD for layer groups
- `/api/v1/workspaces/{workspace}/styles`, `/style-assets`: CRUD for SLD styles and graphic assets
- `/api/v1/workspaces/{workspace}/apikeys`: API key management
- `/api/v1/workspaces/{workspace}/claim-mappings`: Workspace OIDC claim mappings
- `/api/v1/workspaces/{workspace}/settings/{wms|wfs|wcs|wmts|ogcapi|ogc-tiles}`: Service settings (GET/PUT)
- `/api/v1/workspaces/{workspace}/tile-matrix-sets`: Tile matrix sets available to the workspace
- `/api/v1/workspaces/{workspace}/cache/clear`: Clear workspace cache (POST)
- `/api/v1/workspaces/{workspace}/tile-cache/{stats,jobs}`: Persistent tile cache stats and seed jobs
- `/api/v1/cache/stats`, `/api/v1/cache/clear`, `/api/v1/cache/clear/{cacheType}`: Global cache admin (super_admin only)
- `/api/v1/tile-matrix-sets`: Custom tile matrix sets (global)
- `/api/v1/deletions`: Deletion operations (list/get/retry)
- `/api/v1/catalog/integrity` (+ `/repair`): Catalog integrity check (super_admin only)
- `/api/v1/audit`: Audit events and retention
- `/api/v1/claim-mappings`: Global OIDC claim mappings (super_admin only)
- `/api/v1/roles`: Role and policy management (super_admin only)
- `/api/v1/api`: Management OpenAPI spec (JSON)
- `/api/v1/api.html`: Swagger UI

### OGC Services (workspace-scoped; `{workspaceId}` is a workspace name or UUID)
- `/workspaces/{workspaceId}/ogc/`: OGC API - Features landing page
- `/workspaces/{workspaceId}/ogc/collections`: List collections
- `/workspaces/{workspaceId}/ogc/collections/{id}/items`: Query features (supports `bbox`, `limit`, `offset`, `filter`, `crs`, `sortby`, and property selection)
- `/workspaces/{workspaceId}/wms`: WMS 1.3.0 endpoint
- `/workspaces/{workspaceId}/wfs`: WFS 2.0 endpoint
- `/workspaces/{workspaceId}/wcs`: WCS 2.0.1/2.1 endpoint
- `/workspaces/{workspaceId}/wmts`: WMTS 1.0.0 endpoint (KVP + REST)
- `/workspaces/{workspaceId}/ogc-tiles`: OGC API - Tiles endpoint

### Other
- `/admin`: Embedded administration console
- `/health`, `/ready`: Liveness and readiness checks
- `/api/v1/debug/pprof/*`: pprof endpoints (protected, super_admin only)

If `Server.BasePath` is configured, prepend it to every path.

## Documentation

User and operator docs live in `docs/`: protocol guides, configuration, authentication, deployment, conformance, development, and the `getting-started.md` / `getting-started-ui.md` tutorials. Keep `docs/` to documentation a user or operator needs; review and remediation records belong in `plans/` (untracked) or in the git history, not in the published docs. Product plans live in `plans/`.
