# OGC API - Tiles

OGC API - Tiles is exposed at:

~~~text
/workspaces/{workspace}/ogc-tiles
~~~

Tiles.Enabled must be true globally and OGC Tiles must be enabled in the workspace. The service provides Mapbox Vector Tiles, raster map tiles, tileset metadata, and TileJSON.

Feature collections retain both vector and rendered map tiles. Published coverages and persisted layer groups are added as rendered map tiles (`dataType: map`) and do not advertise vector tiles. Their supplemental map TileJSON documents contain no `vector_layers`.

## Enable globally

~~~toml
[Tiles]
Enabled = true
MinZoom = 0
MaxZoom = 22
TileSize = 4096
MaxFeatures = 50000
MaxVertices = 5000000
MaxTileBytes = 10485760
~~~

## Enable a workspace

~~~bash
curl -X PUT \
  http://localhost:9000/api/v1/workspaces/acme/settings/ogc-tiles \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "public": false,
    "title": "ACME Tiles",
    "versions": ["1.0.0"],
    "settings": {
      "tile_matrix_sets": ["WebMercatorQuad", "WorldCRS84Quad"],
      "vector_tiles": {
        "enabled": true,
        "formats": ["application/vnd.mapbox-vector-tile"]
      },
      "map_tiles": {
        "enabled": true,
        "formats": ["image/png", "image/jpeg", "image/webp"]
      },
      "cache_enabled": true,
      "persistent_cache_quota_bytes": 1073741824,
      "max_features": 50000,
      "max_vertices": 5000000,
      "max_tile_bytes": 10485760
    }
  }'
~~~

Workspace limits cannot exceed server ceilings. Vector format must be application/vnd.mapbox-vector-tile; map formats are PNG, JPEG, and WebP.

The conformance response is derived from these effective switches. Disabling
vector tiles removes the MVT class; disabling PNG or JPEG removes its format
class. WebP remains supported as an additive representation but is not
advertised as a made-up OGC requirement class.

## Discovery endpoints

| Endpoint | Description |
| --- | --- |
| / | Landing page |
| /api | Workspace OpenAPI JSON for every Tiles route |
| /conformance | Conformance declaration |
| /tileMatrixSets | Supported tile matrix sets |
| /tileMatrixSets/{id} | Tile matrix set definition |
| /collections | Tile-enabled visible collections |
| /collections/{id} | Collection metadata |
| /collections/{id}/tiles | Vector tilesets |
| /collections/{id}/map/tiles | Map tilesets |
| /collections/{id}/tilejson.json | TileJSON 3.0 metadata |

Supported matrix sets are WebMercatorQuad and WorldCRS84Quad. Use the tileset metadata to discover the combinations and formats available for a collection.

Tileset `item` links use the OGC API Tiles Core variables
`{tileMatrix}/{tileRow}/{tileCol}`. Their familiar XYZ equivalents are
`z/y/x`, so the concrete numeric URL shape has not changed.

## Vector tiles

~~~bash
curl -o tile.mvt -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/ogc-tiles/collections/buildings/tiles/WebMercatorQuad/10/343/550"
~~~

The path order is `tileMatrix/tileRow/tileCol` (equivalent to `z/y/x`). Vector tiles use the MVT extent configured by Tiles.TileSize and apply feature, vertex, output-size, and statement-timeout limits.

## Raster map tiles

~~~bash
curl -o tile.png -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/ogc-tiles/collections/buildings/map/tiles/WebMercatorQuad/10/343/550?f=png&style=blue-polygons"
~~~

The f parameter selects an advertised image format, and style selects a workspace SLD style. Raster rendering shares styling behavior and safety limits with WMS where applicable.

The same route also portrays WCS coverages. WebMercatorQuad and WorldCRS84Quad requests are reprojected from the source coverage CRS, and an omitted style uses a normalized grayscale or conventional RGB built-in portrayal.

Coverage mosaics accept `datetime` (or the `time` alias) and `elevation` query parameters. Feature map tiles use the same parameters when their published dimensions have source-property bindings. Persisted layer groups use the same route and preserve their member styles, order, opacity, and optional blend modes.

## TileJSON

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/ogc-tiles/collections/buildings/tilejson.json"
~~~

TileJSON includes templated tile URLs, bounds, zoom range, attribution metadata where available, and vector layer fields for MVT sources. TileJSON retains its own conventional `{z}/{y}/{x}` template variables; these are intentionally distinct from the OGC API Tiles variables in tileset metadata.

## Caching and access

Workspace settings can disable all tile caching even when the global in-memory or persistent cache is enabled. When PersistentCache is enabled globally, `persistent_cache_quota_bytes` optionally limits this workspace; each layer, coverage, or layer group can additionally set `tile_cache_quota_bytes`. Zero inherits the parent limit. LRU eviction enforces resource, workspace, then global quotas.

OGC API - Tiles keeps all existing vector, rendered-map, and TileJSON routes. Its map tiles and MVT payloads share canonical generation-aware durable entries with WMTS, so a tile filled through either protocol can be served through the other. Arbitrary WMS GetMap responses are not written to the durable tile cache.

Public workspace Tiles settings allow anonymous access, but per-layer public/allowed_roles rules still filter collections and direct tile requests.

Related: [WMTS](wmts.md) · [Persistent tile caching](tile-cache.md) · [WMS](wms.md) · [Performance](performance.md) · [Authentication](authentication.md)

For native Go verification and the digest-pinned official OGC API Tiles suite,
see [Specification verification and OGC conformance](conformance.md).

## Workspace map tilesets

A workspace can publish one curated map directly from its OGC API – Tiles
landing page. Create a layer group with the desired ordered members and styles,
then select it under **Settings → OGC API – Tiles → Workspace map**. The group
can contain feature layers, coverages, and nested groups supported by the map
renderer. Existing compositing requirements still apply.

The management settings field `settings.dataset_map_layer_group_id` holds the
group UUID. Set it using the existing
`PUT /api/v1/workspaces/{workspace}/settings/ogc-tiles` operation, retaining the
other settings from GET. For example, add this field to `settings`:

```json
"dataset_map_layer_group_id": "<layer-group UUID>"
```

An empty string clears the selection; omitting the field on PUT preserves it.
Existing workspaces default to no selection. The group can be configured while
disabled, but publication requires OGC API – Tiles, map tiles, and the group to
be enabled, with an available matrix set and image format.

Clients follow the landing page's `tilesets-map` link to:

| Path beneath the Tiles endpoint | Purpose |
| --- | --- |
| `/map/tiles` | List workspace map tilesets |
| `/map/tiles/{tileMatrixSetId}` | Describe a map tileset |
| `/map/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}` | Retrieve a map tile |

The routes accept the same tile formats, styles, and time/elevation parameters as
collection map tiles. They use the configured group's exact composition and
share its cache entries with equivalent collection and WMTS requests. Existing
group cache jobs also warm the workspace map. Client-selected `collections`
combinations and dataset-level vector tiles are not supported.

Map tileset metadata lists `tileMatrixSetLimits` for the configured zoom range.
Each matrix covers its full row and column range, including empty tiles outside
the published data extent.

Every member must be visible to the caller. A public Tiles endpoint does not
bypass group or member permissions. Inaccessible, missing, or disabled groups
are absent from discovery and return 404; cached tiles follow the same checks.
The dataset-tilesets conformance class is advertised only when the workspace map
is available to the caller. Datasource outages retain the normal rendering error
behavior.

Renaming the selected group preserves the selection. Before deleting it, or
recursively deleting a store whose resources belong to it, clear or change the
workspace map selection. The deletion preview lists this blocker; deleting the
entire workspace remains supported. Catalog integrity checks report broken
selections, and explicit integrity repair clears those references.
