# neoserver

neoserver is a modern, multi-workspace geospatial server written in Go. It publishes PostGIS, DuckDB, GeoParquet, vector-file, and raster data through OGC API - Features, WMS 1.3.0, WFS 2.0, WCS 2.1/2.0.1, OGC API - Tiles, a management REST API, and an embedded administration console.

It is designed as a lightweight, API-driven alternative to GeoServer, with isolated workspaces, configurable service metadata, role-based access control, and an encrypted DuckDB backing store.

## Features

- Workspace-isolated data sources, layers, styles, credentials, and OGC settings
- PostGIS, DuckDB Spatial, GeoParquet, Shapefile, GeoPackage, GeoJSON, and FlatGeobuf
- OGC API - Features with CRS, bbox, temporal queries, Queryables, CQL2 text/basic spatial filters, property selection, sorting, and linked paging
- WMS 1.3.0 with GetMap, GetFeatureInfo, GetLegendGraphic, SLD, time/elevation dimensions, raster/document formats, MapML, and UTFGrid
- WFS 2.0 with GML/GeoJSON/CSV/GeoPackage/SHAPE-ZIP output, FES filters, stored queries, transactions, and feature locking
- WCS 2.1 with WCS 2.0.1 compatibility, GeoTIFF/COG and PostGIS raster sources, GML/GeoTIFF output, and 2D subsetting
- OGC API - Tiles and WMTS 1.0 with Mapbox Vector Tiles, raster map tiles, TileJSON, standard tile matrix sets, and a shared persistent cache
- Management API and generated Swagger UI
- Embedded React administration console for catalog, security, imports, caches, styles, settings, and map preview
- API keys, self-signed JWTs, static/basic authentication, OIDC, workspace RBAC, and per-layer read controls
- Configurable response caching, resource limits, OpenTelemetry export, and protected pprof endpoints
- Filesystem or S3-compatible persistent tile caching with quotas and resumable seed/reseed/truncate jobs

## Architecture

~~~
Management API
    |
    +-- Workspace
        +-- Services (datasource connections)
        |   +-- Published layers and SQL views
        +-- Styles
        +-- API keys and claim mappings
        +-- OGC service settings

Published endpoints:
  /workspaces/{workspace}/ogc
  /workspaces/{workspace}/wms
  /workspaces/{workspace}/wfs
  /workspaces/{workspace}/ogc-tiles
  /workspaces/{workspace}/wcs
  /workspaces/{workspace}/wmts
  /admin
~~~

The server stores its configuration and security state in an encrypted DuckDB file. Feature data remains in the configured source systems.

## Install

Released images are published to Docker Hub for Linux amd64:

~~~bash
docker pull tobilg/neoserver:latest     # or a pinned version, e.g. :0.1.0
~~~

Each [GitHub release](https://github.com/tobilg/neoserver/releases) also carries
the image archive, a CycloneDX SBOM, `SHA256SUMS` and a `qualification.json`
recording the gates that build passed.

## Quickstart with Docker Compose

Choose your path:

- [UI-first tutorial](docs/getting-started-ui.md): Docker + browser, from an empty catalog to a private layer and a working client. No management API commands.
- [API-first tutorial](docs/getting-started.md): the same workflow using scripted HTTP requests.
- [Contributor setup](docs/development.md#one-command-contributor-startup): `make doctor` checks dependencies; `make dev` starts the backend and hot-reloading console with an isolated local catalog.

Requirements: Docker Compose, curl, jq, and OpenSSL.

For this local tutorial, use the repository's development key. Generate and retain a strong key (`openssl rand -hex 32`) for any deployment:

~~~bash
export NEOSRV_STORE_KEY=abc123 # Local only; never use in production.
export NEOSRV_SERVER_URLBASE="http://localhost:9000"
~~~

Initialize the backing store. The command prints a one-time bootstrap JWT; save it as TOKEN.

~~~bash
docker compose build server
docker compose run --rm --no-deps server init --store-path /data/neoserver.db
export TOKEN="paste-bootstrap-token-here"
~~~

Start neoserver and the seeded PostGIS database:

~~~bash
docker compose --profile postgis up --build
~~~

Verify the server:

~~~bash
curl http://localhost:9000/health
curl http://localhost:9000/ready
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/api/v1/workspaces
~~~

The store and PostGIS volumes are persistent. On later starts, reuse the same NEOSRV_STORE_KEY and run only the compose up command. Continue with the [complete getting started tutorial](docs/getting-started.md) to create a workspace, publish the included sample data, and access every service.

## Deployment

For a native development build, install GDAL 3 development headers first:

~~~bash
make build
export NEOSRV_STORE_KEY="$(openssl rand -hex 32)"
./neoserver init --store-path ./data/neoserver.db
./neoserver serve
~~~

For containers and production deployments, persist the backing-store path, supply the original encryption key through a secret manager, configure Server.UrlBase for the externally visible URL, and terminate authenticated traffic with HTTPS. See [Deployment](docs/deployment.md) for initialization, persistence, proxy, backup, and upgrade guidance.

## Documentation

### Start and operate

- [UI-first tutorial](docs/getting-started-ui.md) — publish and connect using the console
- [API-first tutorial](docs/getting-started.md) — end-to-end Compose and PostGIS HTTP workflow
- [Deployment](docs/deployment.md) — native, container, persistence, proxy, and production operation
- [Configuration](docs/configuration.md) — TOML, environment variables, CLI, and setting reference
- [Authentication and authorization](docs/authentication.md) — credentials, OIDC, RBAC, and layer access
- [Administration console](docs/admin-console.md) — browser sign-in, operator workflows, and deployment
- [Performance and observability](docs/performance.md) — cache, limits, OpenTelemetry, and pprof

### Publish and manage data

- [Data sources](docs/data-sources.md) — connection formats, discovery, files, remote data, and SQL views
- [WCS 2.1](docs/wcs.md) — raster publication, subsetting, formats, and compatibility
- [Management API](docs/management-api.md) — task-oriented administration and API reference links

### Consume OGC services

- [OGC API - Features](docs/ogc-api-features.md)
- [WMS 1.3.0](docs/wms.md)
- [WFS 2.0](docs/wfs.md)
- [WCS 2.1](docs/wcs.md)
- [OGC API - Tiles](docs/ogc-api-tiles.md)
- [WMTS 1.0](docs/wmts.md)
- [Persistent tile caching](docs/tile-cache.md)

### Contribute

- [Development](docs/development.md) — architecture, builds, tests, and conformance suites
- [Protocol testing and OGC conformance](docs/conformance.md) — native integration, stock official TEAM Engine, and explicitly derived evidence
- [Release notes](docs/release-notes.md) — breaking changes and upgrade notes

## API reference and UI

After startup:

- Liveness: http://localhost:9000/health
- Readiness: http://localhost:9000/ready
- Management OpenAPI JSON: http://localhost:9000/api/v1/api
- Management Swagger UI: http://localhost:9000/api/v1/api.html
- Workspace OGC API docs: http://localhost:9000/workspaces/demo/ogc/api.html
- Administration console: http://localhost:9000/admin/

Replace demo with a workspace name or UUID for workspace endpoints. If Server.BasePath is configured, prepend it to every path.

## License

neoserver is distributed under the [MIT License](LICENSE). It bundles
third-party software under its own terms; see
[third-party licenses and notices](THIRD-PARTY-LICENSES.md).
