# WMTS 1.0

WMTS is exposed at:

~~~text
/workspaces/{workspace}/wmts
~~~

`WMTS.Enabled` must be true globally and WMTS must be enabled for the workspace. WMTS is additive: it does not replace or remove OGC API - Tiles, raster map tiles, MVT, or TileJSON.

## Enable the service

~~~toml
[WMTS]
Enabled = true
MaxFeatureInfoResults = 10
~~~

~~~bash
curl -X PUT \
  http://localhost:9000/api/v1/workspaces/acme/settings/wmts \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "public": false,
    "title": "ACME WMTS",
    "feature_info_enabled": true,
    "vector_tiles_enabled": false,
    "provider_name": "ACME Maps",
    "provider_site": "https://maps.example.com",
    "contact_name": "Map Operations",
    "contact_email": "maps@example.com"
  }'
~~~

The workspace OGC API Tiles settings remain authoritative for enabled tile matrix sets, formats, styles, render limits, and cache activation. WMTS advertises and accepts raster image formats by default. Set `vector_tiles_enabled` to `true` to expose MVT through WMTS as an explicit extension; this does not affect OGC API - Tiles MVT publication.
GetCapabilities derives its operations, per-resource formats, FeatureInfo
metadata, and matrix-set links from those same effective settings, so it does
not advertise disabled behavior.

`provider_name` defaults to `neoserver`. Provider site and contact fields are
optional and populate the required OWS ServiceProvider section. KVP
GetCapabilities accepts `SECTIONS=All` or a comma-separated selection of
`ServiceIdentification`, `ServiceProvider`, `OperationsMetadata`, `Contents`,
and `Themes`.

## Operations and bindings

The KVP binding supports GetCapabilities, GetTile, and optional GetFeatureInfo:

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/wmts?SERVICE=WMTS&REQUEST=GetCapabilities&VERSION=1.0.0"

curl -o tile.png -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0&LAYER=buildings&STYLE=default&FORMAT=image%2Fpng&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=10&TILEROW=343&TILECOL=550"
~~~

The REST binding publishes capabilities at `/1.0.0/WMTSCapabilities.xml`. GetCapabilities advertises exact ResourceURL templates for tile and FeatureInfo resources. Supported raster tile formats are PNG, JPEG, and WebP when enabled for the resource. MVT is advertised for feature resources only when both OGC API vector tiles and the WMTS-specific opt-in are enabled. Published coverages and layer groups advertise raster map formats only.

Coverage time/elevation dimensions are advertised in capabilities. KVP GetTile accepts `TIME` and `ELEVATION`; REST tiles accept lowercase query parameters. Each value is part of the canonical cache identity. Layer groups are rendered in persisted member order and use the same authorization and nesting rules as WMS.

GetFeatureInfo accepts JSON, GeoJSON, XML, HTML, and plain text. It applies the same layer visibility and vector/coverage identify model as WMS. SOAP and XML POST bindings are not implemented.

## Access and caching

Private WMTS workspaces require the same identity and workspace access as the other OGC services. Public WMTS still applies each resource's `public` and `allowed_roles` rules.

WMTS and OGC API - Tiles call one shared tile engine. The canonical cache identity contains workspace UUID and rendering revision, resource UUID and generation, map or vector type, tile matrix set, matrix, row, column, style digest, and output format. This prevents protocol-specific duplicate durable entries and makes setting, style, and resource changes miss old generations safely. Official WMTS conformance evidence is collected with the raster-default profile; enabling the WMTS MVT extension is outside that verified profile.

Related: [OGC API - Tiles](ogc-api-tiles.md) · [Persistent tile caching](tile-cache.md) · [Authentication](authentication.md)

The official WMTS TEAM Engine target and its evidence format are documented in
[Specification verification and OGC conformance](conformance.md).
