# WMS 1.3.0

WMS is exposed at:

~~~text
/workspaces/{workspace}/wms
~~~

WMS.Enabled must be true in server configuration and the workspace's WMS settings must also be enabled.

## Operations

- GetCapabilities
- GetMap
- GetFeatureInfo
- GetLegendGraphic
- DescribeLayer

GET and XML POST requests are accepted at the same endpoint.

## Enable a workspace

~~~bash
curl -X PUT \
  http://localhost:9000/api/v1/workspaces/acme/settings/wms \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "public": false,
    "title": "ACME WMS",
    "max_width": 4096,
    "max_height": 4096,
    "max_pixels": 16777216
  }'
~~~

Workspace limits cannot exceed the global WMS ceilings.

## Capabilities

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities"
~~~

Capabilities include only enabled layers visible to the caller, including supported CRS, styles, bounds, queryability, and configured dimensions. The root `MaxWidth` and `MaxHeight` values are the effective workspace limits capped by the server configuration. GetMap exceptions truthfully advertise XML, `INIMAGE`, and `BLANK`; the latter two are accepted only for raster output formats.

## GetMap

~~~bash
curl -o map.png -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=buildings&STYLES=&CRS=EPSG:4326&BBOX=52.3,13.0,52.7,13.8&WIDTH=800&HEIGHT=600&FORMAT=image/png&TRANSPARENT=true"
~~~

GetMap advertises only formats the running process can produce:

| Format | Token | Behavior |
| --- | --- | --- |
| PNG / palette PNG | `image/png`, `image/png8` | Full-color or palette raster |
| JPEG / GIF | `image/jpeg`, `image/gif` | Standard raster output |
| TIFF / palette TIFF | `image/tiff`, `image/tiff8` | Plain or palette raster |
| GeoTIFF | `image/geotiff` | Georeferenced RGBA raster; advertised only when the linked GDAL GTiff driver is available |
| SVG | `image/svg+xml` | Self-contained hybrid output; vector geometry and labels remain SVG elements while portrayed coverages and composited intermediate layers are embedded images |
| PDF | `application/pdf` | Georeferenced rendered map; advertised only when the linked GDAL PDF driver is available |
| KML / KMZ | `application/vnd.google-earth.kml+xml`, `application/vnd.google-earth.kmz` | A rendered GroundOverlay; KML links to an equivalent PNG request and KMZ embeds `map.png` |
| MapML | `text/mapml` | A rendered image extent for WGS84/CRS:84 or Web Mercator only |
| UTFGrid | `application/json;type=utfgrid` | Interactive hit grid for exactly one visible vector layer, with stable feature keys and all returned properties |

`kml` and `kmz` are accepted as short aliases. GeoTIFF and PDF use the same process-wide Go/GDAL capability registry as WFS binary exports; a missing driver removes the format from capabilities and makes direct requests fail rather than advertising a broken encoder. The other formats do not depend on an optional GDAL output driver.

UTFGrid uses a four-pixel grid resolution, applies the normal feature/vertex/query ceilings, rejects coverage/group/multi-layer requests and rendering transformations, and is deliberately not cached. KML, MapML, and UTFGrid responses are also marked `no-store`; KMZ is cacheable because its image is embedded. Image exception modes are limited to raster formats that can safely carry them.

`WMS.MaxOutputBytes` is enforced for every encoder, including archive and document formats. WMS 1.3.0 follows the CRS axis order; EPSG:4326 uses latitude,longitude BBOX order, while CRS:84 uses longitude,latitude.

Published WCS coverages are also WMS layers. They can be mixed with feature layers in `LAYERS`, reprojected to any EPSG CRS available to GDAL/PROJ, queried with GetFeatureInfo for raw band values, and portrayed with RasterSymbolizer. WCS responses remain raw and unstyled.

Global controls cover width, height, total pixels, maximum rendered features and vertices, concurrency, queue time, and optional geometry simplification.

## DescribeLayer

`REQUEST=DescribeLayer&LAYERS=buildings,elevation` returns an SLD DescribeLayer response. Feature layers reference the workspace WFS endpoint and published feature type; coverages reference WCS. Invisible resources are reported as undefined rather than disclosed.

## GetFeatureInfo

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo&LAYERS=buildings&QUERY_LAYERS=buildings&CRS=EPSG:4326&BBOX=52.3,13.0,52.7,13.8&WIDTH=800&HEIGHT=600&I=400&J=300&INFO_FORMAT=application/json"
~~~

Feature info supports advertised XML, JSON, HTML, and text formats. The requested layer must be queryable and visible to the caller.

## Styles and SLD

Built-in styles can be defined in TOML under WMS.Styles. Workspace styles are created through the management API or console in SLD/SE, CSS, YSLD, or Mapbox form. All formats compile to one typed, painter-ordered portrayal model; alternative formats require the `dynamic-style` extension at server and workspace scope.

### Alternative style subsets

These are bounded server portrayal codecs, not complete GeoServer CSS/YSLD/MBStyle
or client-side Mapbox implementations. Revalidate existing styles before migration.

- CSS supports the implemented flat mark, stroke, fill, text and raster-opacity
  properties and simple selectors/filters. It is not a browser CSS engine.
- YSLD uses `feature-styles` → `rules` → `symbolizers`, with `mark`, `line`, `fill`,
  `text` and `raster` entries and their supported flat property mappings.
- Mapbox supports circle, line, fill, symbol/text and raster-opacity layers,
  supported min/max zoom and comparison filters. Literal paint values are typed
  and range-checked. Colors are `#RGB`, `#RRGGBB` or the renderer's supported
  named colors. Property-driven vector/text values use `["get", "property"]`;
  radius is converted to diameter for both literal and property-driven circles.
  `line-dasharray` accepts a nonempty non-negative numeric array with at least
  one positive entry. Raster opacity is literal. `interpolate`, `step`, `zoom`,
  arithmetic expression arrays and other unsupported constructs fail validation
  with a property path. They are never flattened into a default style.

The console provides point/line/polygon/raster starters for all six persisted
format identifiers. Save alternative styles before named WMS preview; unsaved
draft preview remains SLD/SE-only. Creating a style does not enable its rendering
extension or bind it to a publication automatically.

Layers and coverages may bind a `default_style` and advertised alternate `styles`. Raster styles support opacity, gray/RGB channel selection, ramp/interval/value color maps, normalize and histogram contrast enhancement, configurable normalization algorithms, gamma, and NoData transparency. Multiple symbolizers are rendered in document order. Unsupported constructs are rejected when a style is created or updated.

The optional portrayal extensions add:

- request-safe raster expressions through `env` functions and the URL-encoded `ENV=key:value;...` parameter;
- priority-based label collision handling, line following/repetition, displacement, wrapping, groups, partial-label control, and related TextSymbolizer vendor options;
- `Heatmap`, `Contour`, `Rasterize`, `PointStacker`, `GroupCandidateSelection`, `Barnes`, `RasterAsPointCollections`, and bounded `RasterAlgebra` transformations;
- Porter-Duff composition and multiply, screen, overlay, darken, lighten, color-dodge/burn, hard/soft-light, difference, and exclusion blending with opacity and composite-base isolation;
- stable feature z-order using a FeatureTypeStyle `sortBy` or `z-order` vendor option.

Point `ExternalGraphic`, line/polygon `GraphicStroke`, and polygon `GraphicFill` are supported. Use `asset:name` for a workspace-managed graphic. Remote URLs are HTTPS-only and require the `remote-graphics` two-level gate plus an exact configured origin; redirects are checked again. Raster graphics support PNG/JPEG/GIF, and the bounded SVG mark renderer accepts basic geometric SVG elements. Invalid or unsupported graphics are rejected or omitted without granting arbitrary filesystem/network access.

Numeric, color, font, label, rotation, opacity, and width properties can use bounded `env`, `property`, arithmetic, string case/concatenation, and common numeric functions. Dynamic feature expressions require `dynamic-style`; normal `<PropertyName>` labels remain part of the standards path. Label placement also honors obstacle geometries, priority/collision settings, line following/repetition, displacement, grouping, wrapping, partial-label control, and polygon alignment options.

`ENV` names and values are bounded by the configured limits, are included in map cache keys, and are evaluated only as numeric/string portrayal expressions. They are never expanded into XML or SQL.

Use the style name in STYLES or request a legend:

~~~bash
curl -o legend.png -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetLegendGraphic&LAYER=buildings&STYLE=blue-polygons&FORMAT=image/png"
~~~

GetMap and GetLegendGraphic accept inline SLD XML through `SLD_BODY`. Remote `SLD=` URL retrieval is not implemented and is rejected with `InvalidParameterValue`; use `SLD_BODY` or a stored workspace style.

## Dimensions

Published feature layers may bind time and elevation dimensions to `source_property` and an optional interval `end_property`. GetMap and GetFeatureInfo apply exact values, comma-separated values when enabled, intervals, defaults, and `current` as parameterized backend filters. Metadata-only dimensions created before these bindings remain advertised, but an explicit request is rejected until a source property is configured.

Published raster mosaics use coverage dimensions to select granules by TIME or ELEVATION. WMTS and OGC API map-tile requests use the same selection and cache each selection independently.

## Layer groups

`/api/v1/workspaces/{workspace}/layer-groups` manages ordered, reusable map compositions. A member references a feature layer, coverage, or another group and may select a style, opacity, and blend mode. Creation/update rejects missing members, cycles, excessive nesting, and resource/style references outside the workspace. Groups are advertised as WMS and WMTS layers and as OGC API map-tile collections. They never advertise vector tiles. See [Layer groups](layer-groups.md).

## Caching and errors

Capabilities and rendered map responses use the configured capabilities/tile caches. Relevant service, layer, style, and settings updates invalidate cached results.

WMS errors use service exception XML or the requested image exception format. Resource ceilings produce request errors instead of unbounded rendering.

Related: [Management API](management-api.md) · [Performance](performance.md) · [Authentication](authentication.md)
