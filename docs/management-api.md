# Management API

The management REST API configures neoserver at runtime. Its base path is /api/v1 and all administrative requests require an authenticated identity with sufficient role permissions.

## Live reference

- OpenAPI JSON: /api/v1/api
- Swagger UI: /api/v1/api.html

The server URL in the generated document reflects the current request, configured base path, and forwarded scheme.

## Resource model

~~~
Workspaces
  +-- Services
  |   +-- Layers
  |   +-- Coverages
  |   +-- Mosaic granules and harvest jobs
  +-- Layer groups
  +-- Styles
  +-- API keys
  +-- Claim mappings
  +-- WMS, WFS, WCS, OGC API, OGC Tiles, and WMTS settings
  +-- Persistent tile-cache jobs and usage
  +-- Durable managed imports

Global resources
  +-- Roles
  +-- Service/operation policies
  +-- Custom tile matrix sets
  +-- Audit history
  +-- Claim mappings
  +-- Cache administration
~~~

Workspace and service route parameters accept their names or UUIDs. Layer and style routes accept their public identifiers/names or IDs where supported.

## Authentication and roles

Use a self-signed or OIDC bearer token:

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/api/v1/workspaces
~~~

Or use a stored API key:

~~~bash
curl -H "X-API-Key: nsk_..." \
  http://localhost:9000/api/v1/workspaces
~~~

Listing, creating, or deleting workspaces, global cache operations, role management, and global claim mappings require super_admin. Workspace administrators discover only their permitted console workspaces through `/auth/me`; this prevents global workspace-name disclosure. See [Authentication and authorization](authentication.md).

## Endpoint groups

| Area | Path |
| --- | --- |
| Workspaces | /workspaces |
| Browser authentication | /console/config, /auth/login, /auth/me, /auth/logout, /auth/refresh, and /auth/sessions |
| Services | /workspaces/{workspace}/services |
| Discovery | /workspaces/{workspace}/services/{service}/discover and /discover-coverages |
| SQL validation | /workspaces/{workspace}/services/{service}/validate-sql |
| Layers | /workspaces/{workspace}/services/{service}/layers |
| Coverages | /workspaces/{workspace}/services/{service}/coverages |
| Mosaic catalog | /workspaces/{workspace}/services/{service}/mosaic/{granules,harvest-jobs} |
| Layer groups | /workspaces/{workspace}/layer-groups |
| API keys | /workspaces/{workspace}/apikeys |
| Styles | /workspaces/{workspace}/styles |
| Claim mappings | /workspaces/{workspace}/claim-mappings and /claim-mappings |
| Roles | /roles |
| Settings | /workspaces/{workspace}/settings/{wms,wfs,wcs,wmts,ogcapi,ogc-tiles} |
| Workspace cache | /workspaces/{workspace}/cache/clear |
| Persistent tile cache | /workspaces/{workspace}/tile-cache/stats and /tile-cache/jobs |
| Global cache | /cache/stats and /cache/clear |
| Catalog deletion | /workspaces/{workspace}/deletion-plan, service deletion-plan, and /deletions/{operation} |
| Catalog integrity | /catalog/integrity and /catalog/integrity/repair |
| Managed imports | /workspaces/{workspace}/imports |
| Custom tile matrix sets | /tile-matrix-sets |
| Service/operation grants | /roles/{roleId}/policies |
| Audit history | /audit and /audit/retention |

Collection endpoints support GET and POST; item endpoints support GET, PUT, and DELETE as appropriate. Consult Swagger for request and response schemas.

Mosaic catalog routes return `503` when the opt-in `MosaicCatalog` subsystem is disabled. Harvest jobs accept `mode` (`append` or `synchronize`), an optional allowlisted `directory` and doublestar `pattern`, and/or explicit granules with time, elevation, and priority metadata. Job GET responses expose durable progress and generation numbers; DELETE requests cancellation. Granule listing supports native-CRS `bbox`, exact `time`/`elevation`, `limit`, and `offset` filters and returns the derived footprint. Deleting the final active granule is rejected because an activated generation must remain renderable.

## Referentially complete deletion

Workspace and service deletion is non-recursive by default. Deleting a target that owns or is referenced by catalog or operational state returns `409 Conflict` without changing anything. The response contains a stable dependency plan covering catalog children, transitive layer-group references, workspace policies, persistent tile entries/jobs, mosaic state, and managed style assets.

Inspect the same plan without attempting deletion:

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/api/v1/workspaces/acme/deletion-plan

curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/api/v1/workspaces/acme/services/postgis/deletion-plan
~~~

An empty target is deleted synchronously with `204 No Content`. Explicit recursive deletion starts a durable operation and returns `202 Accepted` plus a `Location` header:

~~~bash
curl -i -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  'http://localhost:9000/api/v1/workspaces/acme/services/postgis?recurse=true'

curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/api/v1/deletions/OPERATION_ID
~~~

Once a recursive deletion is accepted, its workspace or service is tombstoned and is no longer published. The operation cancels and drains tile and mosaic jobs, removes persistent cache payloads and quota rows, removes mosaic indexes without deleting source rasters, stages workspace style assets, commits all primary-catalog child removal in one DuckDB transaction, clears WFS locks/version state and process caches, and then removes staged files. A recursive service deletion also removes the transitive closure of layer groups that depend on its layers or coverages; shared workspace styles remain.

Operations are idempotent and resume after restart. A failure leaves the target unavailable and records its phase and error. Workspace administrators can inspect and retry their operations with `POST /api/v1/deletions/{operation}/retry`; global workspace deletion still requires `super_admin`. A target name cannot be reused until the operation reaches `completed`. Source feature tables, source raster files, global roles, and signing keys are never deleted.

Upgrades from releases that allowed parent-only deletion can be audited explicitly by a super administrator:

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/api/v1/catalog/integrity
~~~

The report covers primary-catalog rows, persistent tile owners, mosaic owners, and managed asset directories. Repair never runs implicitly, including at startup. After reviewing the report, run `POST /api/v1/catalog/integrity/repair?confirm=true`. Repair is idempotent and removes only state whose owning workspace/service/resource no longer exists; it refuses unsafe managed-asset paths and never deletes source data.

## Typical publication workflow

1. POST /workspaces
2. POST /workspaces/{workspace}/services
3. POST /workspaces/{workspace}/services/{service}/discover
4. POST /workspaces/{workspace}/services/{service}/layers
5. PUT the desired workspace service settings
6. Create styles and API keys as needed

The [getting started guide](getting-started.md) contains runnable commands for this complete flow.

## Durable managed imports

When `Importer.Enabled=true`, create an import with either JSON `{"source_uri":"...","name":"..."}` or multipart fields `file` and optional `name` at `POST /workspaces/{workspace}/imports`. URI sources are local allowlisted paths or HTTPS resources acquired through the bounded remote fetcher; uploads, ZIP expansion, discovery, transformation, and worker concurrency use the independent importer ceilings.

Jobs first discover source layers and stop at `awaiting_plan`. Submit a plan to `PUT /imports/{import}/plan`, inspect bounded GeoJSON with `GET /imports/{import}/preview?layer=...`, and explicitly publish with `POST /imports/{import}/publish`. Publication moves one encrypted DuckDB database into managed storage and commits its service, layers, asset ownership, and job state in one catalog transaction. `DELETE` requests cancellation, `POST /retry` retries a failed job within the configured limit, and `POST /rollback` starts the normal durable service-deletion lifecycle. Publishing and rollback jobs resume after restart; rollback responses expose `rollback_operation_id` so their linked catalog deletion remains inspectable. Direct recursive deletion of a managed service uses the same staged-file cleanup.

## Fine-grained service and operation grants

The existing Casbin policy model now accepts service resources and operation resources; it is not a second authorization system. Manage them at `/roles/{roleId}/policies` with `workspace`, `service`, optional `operation`, and `action` (`read`, `write`, `delete`, or `manage`). Supported services are `ogcapi`, `wms`, `wfs`, `wcs`, `ogc-tiles`, and `wmts`. Grants are checked from operation to service to the existing workspace wildcard. They are additive because the model remains allow-only. Built-in roles retain their documented broad defaults; use a custom role without a workspace wildcard for least-privilege protocol profiles.

## Audit and custom grids

Super administrators can query the durable audit view at `GET /audit?workspace=&principal=&since=&limit=` and trigger the configured retention boundary with `POST /audit/retention`. The API never returns request bodies or query strings because they are not stored. A failed audit-database write preserves the original request response and queues the bounded event in the encrypted catalog; the event becomes queryable after retry delivery, while `/ready` exposes the interim degraded state.

`PUT /tile-matrix-sets/{id}` installs a globally reusable OGC TileMatrixSet definition; GET/list and DELETE use the same collection. Built-in `WebMercatorQuad` and `WorldCRS84Quad` are immutable. The current renderer accepts bounded top-left EPSG:3857 or CRS84 grids. Definitions carry revision and digest metadata and become visible to OGC API Tiles and WMTS immediately.

## Workspace settings

Global configuration mounts WMS, WFS, and Tiles routes. Per-workspace settings then enable a service, control whether it is public, customize metadata, and set limits no higher than server ceilings.

~~~bash
curl -X PUT \
  http://localhost:9000/api/v1/workspaces/acme/settings/ogcapi \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "public": false,
    "title": "ACME Features",
    "limit_default": 20,
    "limit_max": 1000
  }'
~~~

OGC API - Features is enabled for a newly created workspace by default. WMS, WFS, and OGC API - Tiles are disabled per workspace by default.

## Styles

Styles store an authored document and canonical format at workspace scope. Supported formats are `sld_1.0.0`, `sld_1.1.0`, `se_1.1.0`, `css`, `ysld`, and `mapbox`; every format compiles to the same ordered typed portrayal model. `sld_body` remains a backwards-compatible alias for `body` on SLD/SE styles.

~~~bash
curl -X POST \
  http://localhost:9000/api/v1/workspaces/acme/styles \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "blue-polygons",
    "title": "Blue polygons",
    "format": "sld_1.0.0",
    "body": "<StyledLayerDescriptor>...</StyledLayerDescriptor>"
  }'
~~~

See [WMS](wms.md) for use in map requests.

Feature layers and coverages accept `default_style` and `styles` fields containing workspace style names. Coverages additionally accept `resampling` (`nearest`, `bilinear`, or `cubic`). Style writes are compiled and return structured diagnostics. Referenced styles cannot be renamed or deleted until their bindings are removed.

Workspace graphic assets are managed at `/api/v1/workspaces/{workspace}/style-assets`. Upload a PNG, JPEG, GIF, or bounded SVG with `PUT /style-assets/{name}` and its image media type, list metadata with `GET /style-assets`, or retrieve/delete one by name. Reference it from SLD as `asset:name` or `asset://name`. Deletion returns `409` while a stored style still references the asset.

## Layer access

Published layers can be public or restricted to roles:

~~~json
{
  "source_layer": "public.parcels",
  "public_id": "parcels",
  "allowed_roles": ["admin", "editor"]
}
~~~

An empty allowed_roles list permits any principal with workspace access. public: true permits anonymous reading when the workspace service is also public. super_admin always has access.

## Cache administration

Get global statistics:

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/api/v1/cache/stats
~~~

Clear a workspace or a global cache type:

~~~bash
curl -X POST \
  http://localhost:9000/api/v1/workspaces/acme/cache/clear \
  -H "Authorization: Bearer $TOKEN"

curl -X POST \
  http://localhost:9000/api/v1/cache/clear/features \
  -H "Authorization: Bearer $TOKEN"
~~~

Valid global types are capabilities, collections, features, tiles, and counts.

The global clear endpoints affect the in-memory response cache. Use persistent truncate jobs for durable tile removal. Seed, reseed, truncate, quotas, progress, cancellation, and restart behavior are documented in [Persistent tile caching](tile-cache.md).

## Errors

Management errors use JSON with a status-oriented code, message, and optional detail. Common statuses are:

- 400 for invalid JSON, validation, or server-ceiling violations
- 401 for missing or rejected credentials
- 403 for insufficient role permissions
- 404 for unknown resources
- 409 for duplicate names or identifiers
- 503 when a configured datasource is unavailable

Related: [Data sources](data-sources.md) · [Configuration](configuration.md)
