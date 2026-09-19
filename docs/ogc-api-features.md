# OGC API - Features

Each workspace exposes OGC API - Features below:

~~~text
/workspaces/{workspace}/ogc
~~~

The service is mounted globally and enabled for new workspaces by default. Workspace settings can disable it, make it public, customize metadata, and set paging limits.

## Endpoints

| Endpoint | Description |
| --- | --- |
| / | Landing page |
| /conformance | Conformance declaration |
| /collections | Visible collections |
| /collections/{id} | Collection metadata |
| /collections/{id}/queryables | Queryables JSON Schema |
| /collections/{id}/items | Feature collection |
| /collections/{id}/items/{featureId} | Single feature |
| /api | Workspace OpenAPI JSON |
| /api.html | Workspace Swagger UI |

Listings and direct reads apply per-layer role visibility. Restricted layers return not found rather than revealing their existence.

## Query features

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/ogc/collections/buildings/items?limit=10&offset=0"
~~~

Supported item parameters:

| Parameter | Purpose |
| --- | --- |
| limit / offset | Paging |
| properties | Comma-separated output properties |
| bbox | Spatial bounds |
| crs | Output CRS |
| bbox-crs | CRS of bbox coordinates |
| datetime | RFC 3339 date/timestamp, bounded or half-bounded interval (`..`), including ISO 8601 durations |
| filter | CQL2 text expression |
| filter-lang | Filter encoding; omitted defaults to `cql2-text` |
| filter-crs | CRS used by spatial filter literals |
| sortby | Comma-separated properties; prefix descending fields with minus |

Server and workspace limits cap paging and may reject deep offsets. Feature
collections include `numberReturned`; when another page exists, the `next` link
preserves the active non-authentication query parameters and advances `offset`.
Query-string API keys are never echoed into cacheable links. The server uses a
one-feature lookahead for paging and reports an RFC 3339 `timeStamp` plus an
exact `numberMatched` count over the same filters. `Paging.CountTimeoutMS`
bounds that independent count; if its deadline expires, the page is still
returned and only the optional `numberMatched` member is omitted.

## Spatial and CRS queries

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/ogc/collections/buildings/items?bbox=13.0,52.3,13.8,52.7"

curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/ogc/collections/buildings/items?crs=EPSG:3857"
~~~

GeoJSON defaults to CRS84 even when the source layer uses another storage CRS.
Responses include `Content-Crs: <URI>`. Explicit `crs`, `bbox-crs`, and
`filter-crs` values must identify a CRS advertised by the collection.
Collection metadata exposes a fresh native spatial extent in CRS84, transforming
it from the storage CRS when necessary. A stale or untransformable extent is
omitted rather than advertised incorrectly.

## Temporal queries

`datetime` accepts an RFC 3339 date/timestamp or interval. Either interval
endpoint can be open using `..`, for example `../2026-01-01T00:00:00Z`, and one
endpoint can be an ISO 8601 duration, for example `2026-01-01/P2D`. A date by
itself selects the complete UTC calendar day. A completely open interval is
invalid.

Temporal filtering uses the first published layer dimension named `time` that
has a `source_property`. If `end_property` is also configured, features are
treated as intervals; a null end is treated as an instant at the source value.
Features whose source value is null match all temporal selections, as required
for features without a temporal association. Collections without a bound time
dimension validate `datetime` but remain untimed, so no features are excluded.
The configured dimension extent is exposed as the collection temporal extent.

~~~json
{
  "name": "time",
  "units": "ISO8601",
  "source_property": "observed_at",
  "end_property": "valid_until",
  "extent": "2020-01-01T00:00:00Z/2026-01-01T00:00:00Z"
}
~~~

## CQL2 filtering and sorting

URL-encode filter expressions:

~~~bash
curl -G -H "Authorization: Bearer $TOKEN" \
  --data-urlencode "filter=height > 50 AND category = 'office'" \
  --data-urlencode "sortby=-height,name" \
  "http://localhost:9000/workspaces/acme/ogc/collections/buildings/items"
~~~

Filters are parsed and compiled into parameterized datasource SQL rather than concatenated into queries.

The service advertises CQL2 Basic, CQL2 Text, and Basic Spatial Functions.
Standard `S_INTERSECTS` with `POINT` and `BBOX` geometry literals is supported,
including CRS84 bounding boxes that cross the antimeridian. Existing additional
operators remain available as compatibility extensions but are not advertised
as additional CQL2 conformance classes. `cql2-json` is not supported.

## Queryables

`/collections/{id}/queryables` returns JSON Schema draft 2020-12 with media type
`application/schema+json`. Scalar database types are mapped to JSON Schema
types, date/timestamp properties receive `date` or `date-time` formats, and the
geometry property uses the applicable `geometry-*` format. The schema sets
`additionalProperties` to false, so it is also the allowlist used to validate
CQL2 property references. Queryables obey the same collection visibility and
role rules as collection and item endpoints.

Unknown query parameters, invalid RFC 3339 values, unsupported filter languages,
invalid CQL2, unknown filter properties, and unadvertised CRSs return HTTP 400.

## Public and authenticated access

Set public: true in the workspace OGC API settings for anonymous service access. Layer public/allowed_roles settings still determine which layers are visible.

Authenticate with Authorization: Bearer or X-API-Key. An invalid or expired credential returns 401 even on a public service; omit credentials for anonymous access.

## Configuration

~~~bash
curl -X PUT \
  http://localhost:9000/api/v1/workspaces/acme/settings/ogcapi \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "public": true,
    "title": "ACME Features",
    "abstract": "Public feature collections",
    "limit_default": 10,
    "limit_max": 1000,
    "max_offset": 100000
  }'
~~~

Related: [Getting started](getting-started.md) · [Authentication](authentication.md) · [Data sources](data-sources.md)
