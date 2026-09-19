# Data sources

A workspace contains services, and each service describes one datasource connection. Discover source layers through the management API, then publish selected layers with stable public IDs.

## Capability overview

| Type | Local | HTTP/S3 | Discovery | SQL views | WFS writes |
| --- | --- | --- | --- | --- | --- |
| PostGIS | Network | No | Geometry tables/views | Yes | Yes |
| DuckDB | Yes | No | Spatial tables | Yes | No |
| GeoParquet | Yes | Yes | File geometry | Yes | No |
| Vector file | Yes | Yes | Driver layers | Yes | No |
| Raster file | Yes | Yes | GeoTIFF/COG, CF-NetCDF, and GRIB2 coverage arrays | No | No |
| Raster mosaic | Yes | Via explicit remote granules | Managed GeoTIFF/COG mosaic with optional persistent harvest catalog | No | No |

SQL-view layers are always read-only, including on PostGIS.

## PostGIS

~~~json
{
  "name": "production",
  "type": "postgis",
  "connection_info": {
    "host": "postgres.example.com",
    "port": 5432,
    "database": "geodata",
    "user": "geo_reader",
    "password": "secret",
    "sslmode": "require",
    "schemas": ["public", "boundaries"]
  }
}
~~~

The field is user, not username. Discovery scans configured schemas for spatial tables and views. Give the account SELECT access for serving features. WFS transactions additionally require INSERT, UPDATE, and DELETE rights on published tables.

## DuckDB Spatial

~~~json
{
  "name": "analytics",
  "type": "duckdb",
  "connection_info": {
    "path": "/data/analytics.duckdb",
    "srid": 4326
  }
}
~~~

The server loads DuckDB's spatial extension and exposes spatial tables. The configured path must pass Datasource.AllowedPaths and be readable by the process.

`srid` declares the **native CRS of the stored coordinates**, not the desired response CRS. Set it explicitly for ordinary DuckDB tables without CRS metadata; unknown CRS is no longer silently treated as EPSG:4326. For a database with different CRSs, use `layer_srids`, for example `{"geographic_points":4326,"projected_roads":3857}`. The console exposes the native SRID; per-table overrides are available in advanced connection JSON. Single-column primary keys, including custom names, take precedence over conventional `id`/`fid` names for feature identity.

New managed imports persist native CRS metadata inside their encrypted database and generate a discoverable primary key when necessary. Older managed imports recover CRS metadata from their original persisted import plan. Import reprojection follows XY (longitude/easting first) order. GeoParquet imports accept native geometry and binary WKB columns.

Imports reprojected before the 2026-09-14 candidate remediation may contain incorrectly transformed coordinates. Metadata recovery does **not** repair those coordinates. Follow the [managed-import upgrade recovery](deployment.md#managed-import-upgrade-recovery) procedure before relying on affected imports.

## GeoParquet

~~~json
{
  "name": "buildings",
  "type": "geoparquet",
  "connection_info": {
    "path": "/data/buildings.parquet"
  }
}
~~~

GeoParquet may be local or remote when allowed by policy. Geometry metadata and CRS are read from the file where available.

## Vector files

Vector files are read through DuckDB Spatial's GDAL integration. Supported inputs are Shapefile, GeoPackage, GeoJSON, and FlatGeobuf. Every vector open—including upload discovery, import conversion, metadata reads, and subsequent queries—restricts GDAL to these actual drivers. Renaming an unsupported file does not enable its driver.

VRT and indirect/XML source formats (including GML and KML inputs) are not accepted. Convert them to a supported, self-contained dataset in a trusted offline environment first. This restriction concerns input datasets, not the WFS/WMS output formats. Raster sources similarly restrict actual opens to GTiff, netCDF, or GRIB; a client cannot select the VRT driver.

~~~json
{
  "name": "natural-earth",
  "type": "vectorfile",
  "connection_info": {
    "path": "/data/natural-earth.gpkg",
    "layer": "ne_10m_admin_0_countries",
    "geometry_column": "geom",
    "id_column": "id",
    "srid": 4326,
    "open_options": {}
  }
}
~~~

Omit layer during discovery to enumerate a multi-layer container such as GeoPackage. The geometry column, ID column, and SRID fields override detection.

`open_options` accepts only `FLATTEN_NESTED_ATTRIBUTES`, `NESTED_ATTRIBUTE_SEPARATOR`, `ARRAY_AS_STRING`, `DATE_AS_STRING`, `ADJUST_GEOM_TYPE`, `ADJUST_TYPE`, and `LIST_ALL_TABLES`. These become GDAL `KEY=VALUE` open options. File/schema-bearing and unknown options are rejected.

The driver policy closes indirect dataset references; it is not an operating-system sandbox for native parser vulnerabilities. For hostile data, also isolate the deployment with least-privilege mounts and network egress controls. Do not allowlist the catalog, keys, other tenants' files, or a broad data root containing them.

Managed DuckDB imports are server-owned bindings, not an escape hatch for ordinary paths. `managed_import_id` cannot be supplied or changed through store create/update/test-connection. A managed source opens only when the published import, service, asset, and workspace all agree; staged, rolled-back, or foreign assets are denied, including after restart.

An enabled store's connection update is prepared and health-checked before it is saved. Failed updates retain the previous durable configuration and active connection; retrying tries the candidate again. Preparation has a 30-second ceiling (or the caller's shorter deadline) and bounded concurrency. Legacy native constructors may finish later, but their abandoned handles are closed and do not block unrelated workspace administration.

## Local and remote path policy

File-based services are deny-by-default. Add patterns to Datasource.AllowedPaths:

~~~toml
[Datasource]
AllowedPaths = [
  "/srv/geodata/**",
  "https://data.example.com/public/**",
  "s3://approved-bucket/**"
]
RemoteCachePath = "/var/cache/neoserver/remote"
RemoteMaxBytes = 1073741824
RemoteTimeoutSec = 60
~~~

Patterns use doublestar glob syntax. A path or URL that matches no entry is rejected. Remote responses are bounded by size and timeout before being cached locally. Supply remote-system credentials through the runtime environment supported by the underlying HTTP/S3 client; never place secrets in a public URL.

## Raster files

Use `rasterfile` for GeoTIFF/Cloud Optimized GeoTIFF grids and for
multidimensional CF-NetCDF or GRIB2 arrays:

```json
{
  "name": "terrain",
  "type": "rasterfile",
  "connection_info": { "path": "./data/terrain.tif" }
}
```

For multidimensional containers, discovery returns one source coverage per
compatible numeric array. Use `connection_info.variables` to allowlist array
names and `connection_info.open_options` for driver-specific GDAL open options.
Published nonspatial axes are exposed through WCS slicing/trimming; retained
multidimensional NetCDF requests can be returned without flattening the source
array.

Raster services use `/discover-coverages` and `/coverages`; they do not publish
feature layers. See [WCS 2.1](wcs.md).

## Raster mosaics

Use `raster_mosaic` to manage compatible GeoTIFF/COG granules as one coverage. Explicit granule metadata supports time/elevation selection and deterministic overlap priority; a local directory plus glob may add granules during service initialization.

```json
{
  "name": "imagery-series",
  "type": "raster_mosaic",
  "connection_info": {
    "name": "Imagery series",
    "directory": "./data/imagery",
    "pattern": "*.tif",
    "granules": [
      {"path":"./data/imagery/2026-01-01.tif","time":"2026-01-01T00:00:00Z","elevation":0,"priority":10}
    ]
  }
}
```

Every path is checked by `Datasource.AllowedPaths`. Granules must have compatible CRS and band schemas. The service builds a GDAL VRT mosaic and exposes source coverage `mosaic`. Time/elevation selections are shared by WCS, WMS, WMTS, and OGC API raster map tiles; WCS treats a mosaic dimension as a slice axis and rejects attempts to flatten several time/elevation positions into one raster.

When `MosaicCatalog.Enabled=true`, workspace administrators can submit append or synchronize harvest jobs at `/api/v1/workspaces/{workspace}/services/{service}/mosaic/harvest-jobs`. The standalone encrypted DuckDB index stores the active generation, source URI, CRS, grid/band signature, dimensions, size/timestamp, bounding-box footprint WKB, and durable job progress. A synchronize job scans the configured directory/glob or request granules into an inactive generation; any invalid/incompatible granule leaves the previous generation live. Successful activation refreshes the runtime VRT without changing service configuration. Granules and footprints can be queried or removed through `/mosaic/granules`; removing a granule also creates and activates a new generation.

`Datasource.MosaicMaxGranules` is the runtime initialization ceiling, `connection_info.max_granules` may lower it for one service, `MosaicCatalog.MaxGranulesPerJob` limits harvest work, and `WMS.MaxMosaicGranulesPerRender` bounds each portrayed selection. With the catalog disabled, the original explicit-granule and startup directory-scan behavior remains unchanged.

## Create, discover, and publish

Create a service:

~~~bash
curl -X POST http://localhost:9000/api/v1/workspaces/acme/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @service.json
~~~

Discover layers:

~~~bash
curl -X POST \
  http://localhost:9000/api/v1/workspaces/acme/services/production/discover \
  -H "Authorization: Bearer $TOKEN"
~~~

Publish a result:

~~~bash
curl -X POST \
  http://localhost:9000/api/v1/workspaces/acme/services/production/layers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_layer": "public.buildings",
    "public_id": "buildings",
    "title": "Building footprints",
    "crs_default": 4326
  }'
~~~

Service and layer updates invalidate relevant caches.

## SQL views

SQL views publish a SELECT query as a virtual read-only layer. First validate and inspect a query:

~~~bash
curl -X POST \
  http://localhost:9000/api/v1/workspaces/acme/services/production/validate-sql \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "sql": "SELECT id, name, geom FROM public.buildings WHERE active = true"
  }'
~~~

Create the SQL-view layer using the discovered geometry metadata:

~~~bash
curl -X POST \
  http://localhost:9000/api/v1/workspaces/acme/services/production/layers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "public_id": "active-buildings",
    "title": "Active buildings",
    "sql_view": {
      "sql": "SELECT id, name, geom FROM public.buildings WHERE active = true",
      "geometry_column": "geom",
      "geometry_type": "Polygon",
      "srid": 4326,
      "id_column": "id"
    }
  }'
~~~

Only SELECT statements are accepted. SQL views are validated again when loaded; invalid stored views are disabled rather than executed.

Every newly published SQL view must have a stable, unique, non-null feature-ID column returned by its query. If `id_column` is omitted, discovery may suggest one; publication fails with an actionable error if no suitable column is selected or uniqueness/null checks fail. In the console, choose **Feature ID column** after validating SQL. Keep that identity stable when changing the underlying data.

Individual items, WFS `GetFeatureById`, and `GetPropertyValue` execute within the authored view, just like feature lists; they cannot read rows or columns excluded by the view. The server owns the SQL-view `source_layer` placeholder (`_sql_view_`) and ignores a supplied physical source. Existing publications with physical source names are also read through their view. Older views without an ID must be republished with one for item lookup; duplicate matching IDs fail rather than returning an arbitrary row.

## Per-service cache overrides

A service may override feature and tile caching:

~~~json
{
  "cache_settings": {
    "features_enabled": true,
    "features_ttl_sec": 120,
    "tiles_enabled": true,
    "tiles_ttl_sec": 600
  }
}
~~~

Related: [Management API](management-api.md) · [Performance](performance.md)
