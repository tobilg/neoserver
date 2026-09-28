# Release notes

## Unreleased

**Breaking:** self-signed JWTs for `admin`, `editor`, and `viewer` are now
bound to one workspace. `create-token` requires `--workspace ID|NAME` for these
roles, and tokens minted by 0.1.2 or earlier for them are rejected because they
granted the role in every workspace. `super_admin` tokens are unchanged.
Console sessions opened with such a token keep their roles until they expire;
revoke them, or rotate the signing key, to end them sooner.

- The catalog serves reads from a connection pool beside its single serialized
  writer. `Store.MaxConnections` (default 10, maximum 100) sets the total.
- The HTTPS datasource cache has a size quota (`Datasource.RemoteCacheMaxBytes`,
  default 10 GiB) and an optional idle-age limit (`RemoteCacheMaxAgeSec`).
  Downloads held by open datasources are never evicted. An unreachable origin
  or a 5xx response now serves the cached copy instead of failing.
- Every DuckDB datasource now disables external access and locks its
  configuration. Local GeoParquet and vector-file sources can read only their
  own file or directory.
- Outbound datasource and remote-graphic fetches refuse more non-public
  addresses: carrier-grade NAT (including `100.100.100.200`), benchmarking,
  documentation and reserved ranges, multicast, Teredo, and NAT64/6to4
  addresses whose embedded IPv4 address is non-public. Remote style graphics
  now use the same connect-time address check as datasources.
- An expired or revoked browser session no longer causes 401 on public,
  read-only protocol requests; they continue anonymously.
- HTTP Basic credentials are throttled on every endpoint, sharing the console
  sign-in failure budget. A username is blocked only after failures from many
  addresses, so one client cannot lock another user out.
- Session cookies are marked `Secure` behind a trusted TLS-terminating proxy,
  and the console's OIDC redirect URI no longer trusts `X-Forwarded-Proto` from
  untrusted peers.
- DuckDB map rendering no longer transforms the bbox of a layer with an unknown
  SRID to `EPSG:0`, and binds bbox coordinates as parameters.
- `serve` warns when the store key holds fewer than 32 bytes, unless it runs
  with `--devel` or `NEOSRV_SERVER_DEVEL=true`.

**Breaking:** workspace administrators can no longer point PostGIS services at
arbitrary hosts. Creating a PostGIS service, testing a connection, or changing
a service's host or port now requires `super_admin` unless the endpoint is
listed in the new `Datasource.DatabaseHosts` setting (`host` or `host:port`).
Workspace administrators can still edit existing services that keep their host
and port. Add your database endpoints to `DatabaseHosts` if workspace
administrators create their own PostGIS services.

**Breaking:** the native default for `Datasource.AllowedPaths` is now
`["./data/sources/**", "./data/imports/**"]`, matching the container image. The
previous `./data/**` also exposed the server's own catalog, audit, tile-cache
and mosaic databases. Move source files directly under `./data` into
`./data/sources`, or set `AllowedPaths` explicitly.

- The repository's `docker-compose.yml` sets `NEOSRV_AUTH_REQUIREHTTPS=false`.
  Behind Docker's port proxy every authenticated plain-HTTP request previously
  failed with 426, which broke the README quickstart and console sign-in.
- New `Server.ExportWriteTimeoutSec` (default 600) replaces
  `Server.WriteTimeoutSec` for WFS GetFeature, GetFeatureWithLock and
  GetPropertyValue, WCS GetCoverage, and OGC API Features items, so large
  downloads to slow clients are no longer cut off after 30 seconds.
- The workspace OGC API documentation page (`/ogc/api.html`) no longer loads
  Swagger UI from unpkg; the Content-Security-Policy blocked it and the page
  was blank. It now uses the same-origin assets of the management API page.
- WMS PDF output is now written by neoserver itself instead of GDAL's PDF
  driver, so it is always available. The container image no longer includes
  that driver's plugin or its GPL-licensed poppler dependency (about 12 MB with
  gpgme, NSS and lcms2), and therefore cannot read PDF files; no datasource
  accepted them. PDFs remain georeferenced (ISO 32000) at 96 DPI.
- The management OpenAPI document's `servers` URL comes from `Server.UrlBase`
  (relative when unset) instead of the request's `Host` and
  `X-Forwarded-Proto` headers.
- Workspace OGC API and OGC API - Tiles OpenAPI documents declare their bearer
  and API-key security schemes for non-public workspaces even when
  `Auth.Enabled` is false, since those workspaces require credentials either way.

## 0.2.0 — unreleased

## 0.1.2 — 21 September 2026

The container runtime now uses a minimal Ubuntu 26.04 image with the GDAL
libraries and plugins neoserver needs. It bundles matching DuckDB 1.5.5 spatial
and httpfs extensions, so initialization and local-data queries work offline.
The image keeps netCDF, JPEG 2000 and PDF support, and the `projsync`, `projinfo`,
`gdalinfo` and `ogrinfo` tools. Other GDAL tools, Python and Java are omitted.

PROJ datum grids are no longer included. Grid-dependent raster reprojection
can be less accurate until grids are provided through a mount, network fetching,
or a derived image. See [deployment](deployment.md#datum-shift-grids) for all
three methods. Default DuckDB vector transformations use the spatial extension's
own PROJ database; PostGIS vector transformations run in PostgreSQL.

**Upgrading from 0.1.0:** the catalog upgrades transactionally from schema 25
to 26. Tile-cache and mosaic schemas already match the supported versions;
the existing audit log gains version metadata. Back up the complete consistency
set before upgrading: DuckDB 1.5.5
can change encrypted-file storage on write, and DuckDB 1.4.3 cannot reopen those
rewritten files. To roll back, restore the pre-upgrade catalog and audit files.

**Upgrading from pre-release builds:** open older catalogs once with 0.1.0,
which contains the historical upgrades, or initialize a new catalog. Older
tile-cache indexes can be discarded together with their cached payloads. The
new baseline guards refuse unsupported files without modifying them.

`neoserver install-extensions` is also available for native deployments. It
installs and loads the embedded engine's required extensions, reporting their
versions and paths as JSON without opening a catalog.

The image includes extension license notices and upstream source references
alongside the pre-downloaded binaries. See
[third-party licenses](../THIRD-PARTY-LICENSES.md).

## 0.1.0 — 19 September 2026

The first public release of neoserver: a multi-workspace geospatial server,
written in Go, that publishes vector and raster data through the OGC service
standards. It is managed through a REST API and an embedded administration
console, and keeps its configuration in an encrypted catalog. Your data stays
in its source systems.

### Features

**Data sources**

- PostGIS tables and rasters, DuckDB databases, and GeoParquet files, local or
  fetched over HTTPS
- Shapefile, GeoPackage, GeoJSON and FlatGeobuf files
- GeoTIFF and Cloud Optimized GeoTIFF rasters, plus managed mosaics of
  compatible GeoTIFF granules
- SQL views on PostGIS and DuckDB. PostGIS views must be a single read-only
  SELECT and may only call allowlisted functions
- Discovery of publishable layers and coverages in a connected store

**OGC services**

- **OGC API - Features**: CRS negotiation, bbox and temporal queries,
  queryables, CQL2 text filters with basic spatial predicates, property
  selection, sorting and linked paging
- **WMS 1.3.0**: GetMap, GetFeatureInfo and GetLegendGraphic, SLD, time and
  elevation dimensions, raster and document output formats, MapML and UTFGrid
- **WFS 2.0**: GML, GeoJSON, CSV, GeoPackage and SHAPE-ZIP output, FES filters,
  stored queries, transactions and feature locking
- **WCS 2.1**, compatible with WCS 2.0.1: GeoTIFF/COG and PostGIS raster
  sources, GML and GeoTIFF output, 2D subsetting, scaling, range subsetting and
  CRS handling
- **OGC API - Tiles** and **WMTS 1.0.0** (KVP and REST): Mapbox Vector Tiles,
  raster map tiles, TileJSON, the standard tile matrix sets and custom ones
- Layer groups that publish several layers as one map layer

**Styling**

- SLD 1.0 and SLD/SE 1.1, validated by the same compiler the renderer uses
- CSS, YSLD and Mapbox styles, available once `dynamic-style` is enabled
- Managed graphic assets for markers, and an allowlist for remote graphics

**Tiles and caching**

- A persistent tile cache on the local filesystem or S3-compatible storage,
  with quotas
- Durable, resumable seed, reseed and truncate jobs
- An in-memory response cache for capabilities, collections, features and tiles

**Data import**

- Managed vector imports from an upload, a URL or a server path: plan,
  preview, publish and roll back, with publication running in the background

**Administration console** (at `/admin`)

- Stores, layers, layer groups, styles and imports, with a live map preview
- A style editor with geometry templates and a WMS preview of unsaved SLD
  drafts
- Ready-to-use connection examples for QGIS and other clients
- API keys, roles and policies, OIDC claim mappings, sessions, the audit log,
  caching and deletions
- Light and dark themes, keyboard navigation, and automated accessibility
  checks in Chromium, Firefox and WebKit

**Management API**

- A REST API under `/api/v1` covering everything the console does, described by
  an OpenAPI document with Swagger UI

**Security**

- Isolated workspaces, each with its own sources, layers, styles, credentials
  and service settings
- API keys, self-signed JWTs, static and basic authentication, and OIDC,
  including browser sign-in with Authorization Code + PKCE and group claim
  mappings
- Workspace role-based access control with custom roles and policies, and
  per-layer read restrictions enforced across every protocol
- Browser sessions with CSRF protection, an encrypted catalog, and an audit log
  with retention

**Operations**

- `/health` and `/ready` probes, OpenTelemetry traces and metrics, and
  protected pprof endpoints
- Configurable resource limits for queries, renders, uploads and caches
- A catalog integrity check with repair, and retryable deletion operations
- A non-root container image (UID/GID 65532, state under `/data`)

### Standards conformance

Every official OGC executable test suite that applies was run unmodified and
digest-pinned against this release's server code, with no failures. The release
workflow runs them again on the tagged commit before anything is published, and
records the results in the release's `qualification.json`.

| Suite | Passed | Failed | Skipped |
| --- | ---: | ---: | ---: |
| OGC API - Features 1.0 | 1665 | 0 | 80 |
| WFS 2.0 | 913 | 0 | 68 |
| WMS 1.3.0 | 187 | 0 | 0 |
| WCS 2.0 (core, POST, CRS, scaling, range subsetting) | 96 | 0 | 0 |
| WMTS 1.0 | 40 | 0 | 11 |
| OGC API - Tiles 1.0 | 15 | 0 | 1 |

WCS 2.0 Interpolation passes 10 of 10 in a separately labelled profile with one
documented compatibility patch to the suite. WCS 2.1 and OGC API - Features
Part 3 (CQL2) have no official suite; they are covered by neoserver's native
protocol integration tests. Passing these suites is evidence, not OGC product
certification. See [Protocol testing and OGC conformance](conformance.md).

### Install

~~~bash
docker pull tobilg/neoserver:0.1.0
~~~

The GitHub release also carries the image archive, a CycloneDX SBOM,
`SHA256SUMS`, and a `qualification.json` recording every gate this build
passed. Start with the [UI-first](getting-started-ui.md) or
[API-first](getting-started.md) tutorial.

WMS, WFS, WCS, WMTS, OGC API - Tiles, the importer and authentication are off
by default. OGC API - Features is always on. Enable the others in
[configuration](configuration.md).

### Known limitations

- **One active server process per store.** The catalog and cache indexes are
  held with exclusive locks, and the S3 tile cache uses an ownership lease. See
  [Deployment](deployment.md#single-active-node).
- **Linux amd64 is the supported platform.** Native builds need GDAL and are not
  standalone binaries; macOS arm64 is used for development and testing only.
- **Vector file input** is limited to Shapefile, GeoPackage, GeoJSON and
  FlatGeobuf. Convert other formats before importing.
- **CSS, YSLD and Mapbox styles** render only when `dynamic-style` is enabled at
  both server and workspace level.
- **No GeoServer compatibility layer.** There is no GeoServer REST API emulation
  or data-directory importer; catalogs are recreated through the API or console.
- **Accessibility** is tested automatically; screen-reader and moderated
  usability sessions have not been run yet.

### Upgrading from pre-release builds

New installations can skip this section. If you ran a development build, back up
the complete consistency set first, then follow the
[upgrade checklist](deployment.md#upgrades).

- Catalog schema 25 and persistent-cache schema 2 need a compatible binary, and
  downgrading upgraded state is unsupported.
- S3 ownership markers use schema 3. Older binaries cannot read them, and older
  active markers need an exact-owner takeover. See
  [tile-cache recovery](tile-cache.md#prefix-ownership-guard).
- Containers run as UID/GID 65532 under `/data`, and Compose uses a named
  volume. Existing bind-mounted state is not migrated automatically.
- Managed imports reprojected before 2026-09-14 may hold incorrectly transformed
  coordinates; follow the
  [recovery procedure](deployment.md#managed-import-upgrade-recovery).
- GML output follows the axis order of its EPSG URN, and WFS KVP and FES
  geometries honour formal EPSG URN/URL axis order. Remove any client workaround
  for the earlier XY output. GeoJSON, `EPSG:code` and CRS84 stay XY.
- The per-workspace viewer at `/workspaces/{id}/ui` and `Server.AssetsPath` are
  gone; use the console preview at `/admin/workspaces/{workspace}/preview`.
- `GET /api/v1/workspaces` requires `super_admin`. Workspace administrators find
  their workspaces through `GET /api/v1/auth/me`.

After restoring traffic, reapply revocations made after a restored backup,
review private service settings, and check representative client requests.
