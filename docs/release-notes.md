# Release notes

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
