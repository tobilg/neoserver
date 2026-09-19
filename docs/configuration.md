# Configuration

neoserver loads defaults, an optional TOML file, environment overrides, and command-line mode flags.

## Configuration sources

Without --config, the server searches for neoserver.toml in:

1. ./config
2. /config
3. /etc

Pass an explicit file with:

~~~bash
neoserver serve --config /etc/neoserver/production.toml
~~~

Copy [config/neoserver.toml.example](../config/neoserver.toml.example) for a complete annotated configuration.

Environment variables use the NEOSRV_ prefix and replace TOML dots with underscores:

~~~bash
export NEOSRV_SERVER_HTTPPORT=9000
export NEOSRV_SERVER_URLBASE=https://geo.example.com
export NEOSRV_STORE_PATH=/data/neoserver.db
export NEOSRV_STORE_KEY=secret
export NEOSRV_WMS_ENABLED=true
~~~

DATABASE_URL is accepted as a compatibility alias when Database.DatabaseURL is otherwise empty. AUTH_USERS accepts comma-separated user:password pairs for basic auth.

## CLI

~~~text
neoserver init [--config PATH] [--store-path PATH]
neoserver serve [--config PATH] [--debug] [--devel]
neoserver create-token [--config PATH] [--store-path PATH] [--role ROLE]
                       [--subject SUBJECT] [--expires DURATION]
neoserver rotate-signing-key [--config PATH] [--store-path PATH] [--force]
neoserver add-claim-mapping [--config PATH] [--store-path PATH] [--workspace ID]
                            --claim NAME --value VALUE --role ROLE
                            [--priority N]
neoserver version
~~~

The default store path is ./data/neoserver.db. Durations accept Go duration syntax such as 24h and the create-token command also accepts day values such as 7d.

All administration commands use the same configuration-file search and store environment variables as `serve`. Store-path precedence is `--store-path`, then `NEOSRV_STORE_PATH`, then `[Store].Path` in the selected configuration, then the default. `--config` selects a file explicitly; a missing explicit file is an error. The container's `NEOSRV_STORE_PATH=/data/neoserver.db` therefore also applies to `init`, token creation, signing-key rotation and claim mappings. Use the same `--config` when administering a server with a non-default configuration. Administration reads only store configuration and does not require unrelated HTTP settings to be valid.

## Server and store

The Server section controls the bind address, public URL, route prefix, CORS, timeouts, maximum request body, trusted proxies, debug logging, and console availability.

Important settings:

| Setting | Purpose |
| --- | --- |
| Server.HttpHost / HttpPort | Bind address |
| Server.UrlBase | Public origin used in generated links |
| Server.BasePath | Optional route prefix |
| Server.CORSOrigins | Comma-separated origins |
| Server.MaxBodyBytes | Maximum request body |
| Server.TrustedProxyCIDRs | Peers allowed to set forwarding headers |
| Server.AdminUI | Serve the embedded administration console at `/admin` (default true) |
| Server.DisableUI | Disable all browser UI, including the administration console |
| Store.Path | Encrypted DuckDB backing store |
| Store.EncryptionKey | Store key; normally supplied as NEOSRV_STORE_KEY |

Server.UrlBase must be an absolute HTTP(S) URL and is required for non-loopback binds.

## Datasource policy and database defaults

Datasource.AllowedPaths is a deny-by-default doublestar glob allowlist for local files and remote URLs. RemoteCachePath, RemoteMaxBytes, and RemoteTimeoutSec control downloads. MosaicMaxGranules bounds managed raster-mosaic service initialization; WMS.MaxMosaicGranulesPerRender separately limits each portrayed selection.

Database settings provide defaults and pool sizing for database-backed operation: URL, schemas, table includes/excludes, open and idle connection counts, and connection lifetimes. Per-workspace PostGIS services carry their own connection information.

See [Data sources](data-sources.md).

## Paging and metadata

Paging.LimitDefault and Paging.LimitMax control OGC API feature pages; Paging.MaxOffset optionally rejects deep offset scans. Paging.CountTimeoutMS (default 5000) bounds the exact count used for `numberMatched`; when the deadline is exceeded, the page is returned without that optional member. Metadata.Title and Metadata.Description populate server metadata. Website.BasemapUrl configures the UI basemap.

Workspace OGC settings can override paging and metadata within the server ceilings.

## WMS, WFS, WCS, Tiles, and WMTS

The global Enabled setting controls whether a protocol's routes are mounted at all. The corresponding service must also be enabled in each workspace.

- WMS: image dimensions, pixel/feature/vertex and encoded-output ceilings, raster and process working-memory ceilings, render concurrency, queue timeout, simplification, default styles, authored-style/asset limits, managed and remote graphic policy, font paths, rendering-extension allowlist, ENV/contour limits, layer-group depth, and per-render mosaic granules
- WFS: counts, offset and timeout limits, CRS and namespace, locks, version history, CSV/GeoPackage/SHAPE-ZIP output and temporary-byte ceilings, export concurrency/timeouts, cleanup quotas, and the guarded anonymous-mutation test switch
- WCS: grid-cell/output ceilings, dimension and axis-value limits, source-granule and temporary-byte limits, a private temporary directory, extraction concurrency, queue timeout, and processing timeout
- Tiles: zoom range, MVT extent, feature/vertex/output ceilings, render concurrency, statement timeout, and server ceilings for per-layer metatile, gutter, and parameter-cardinality policy
- WMTS: global route activation and the GetFeatureInfo result ceiling; matrix sets, raster formats, styles, and caching reuse the workspace OGC Tiles settings, while WMTS vector tiles require a separate per-workspace opt-in

Protocol-specific behavior is documented in [WMS](wms.md), [WFS](wfs.md), [WCS](wcs.md), [OGC API - Tiles](ogc-api-tiles.md), and [WMTS](wmts.md).

`WFS.AllowAnonymousMutations` defaults to `false`. It exists for isolated
conformance/development fixtures that expose a public WFS endpoint and must run
transactions, locking, and stored-query management without credentials. It is
effective only for public WFS workspaces while `Auth.Enabled=false`; startup
rejects the unsafe combination with authentication enabled. Do not enable it
on an Internet-facing or shared deployment.

WCS extensions are enabled per workspace. Supported names are `xml-post`, `range-subsetting`, `scaling`, `crs`, `interpolation`, and `multidimensional`. Output formats and CRS allowlists are workspace settings; the process-wide WCS section supplies hard resource ceilings rather than silently enabling protocol claims for every tenant.

Advanced portrayal extensions are disabled by default. An extension must be present in the server `WMS.Extensions` array and in the workspace WMS `extensions` array. Valid names are `dynamic-raster`, `dynamic-style`, `advanced-labels`, `rendering-transformations`, `compositing`, `z-order`, and `remote-graphics`. `dynamic-style` gates alternative style languages and feature/environment expressions; `remote-graphics` additionally requires an exact HTTPS origin in `ExternalGraphicAllowedOrigins`. Managed `asset:` graphics do not require network access. This two-level gate lets operators make the compiler available without enabling its CPU-expensive behavior for every tenant.

`WMS.StyleAssetPath` and `WMS.ExternalGraphicCachePath` contain binary payloads only. Managed asset metadata remains in the encrypted catalog. Writes are atomic, names are path-safe, asset/style bodies and decoded dimensions are bounded, and remote redirects are rechecked against the origin allowlist. Process feature, cell, memory, and time ceilings bound rendering transformations independently from ordinary GetMap limits.

## Managed imports and audit history

`Importer.Enabled` opts into durable vector import jobs. `Root` contains one encrypted DuckDB database per published import; `TemporaryDirectory` contains uploads and bounded archive expansion only. Upload, source, expanded-byte, archive-file, layer, feature, worker, retry, transform, and shutdown limits are independent. URI sources are restricted to local allowlisted paths and HTTPS resources handled by the bounded remote fetch policy; object-store URIs are not accepted because their byte size cannot be enforced before DuckDB reads them. Keep both paths on private persistent storage and include `Importer.Root` in backup and restore drills.

Uploads and extracted archives are retained until publication or cancellation,
including while previewing or correcting a failed plan. They do not expire
automatically. `Importer.MaxRetainedSourceBytes` defaults to 10 GiB and bounds
aggregate files in `TemporaryDirectory`; zero uses that default. Budget errors
ask the operator to publish or cancel unused jobs. Startup/acquisition logs
report retained bytes and the limit. Include a separately configured temporary
directory in backups; protect it as source data. Use filesystem quotas and
disk monitoring for staged and published databases, independently of this
source-retention budget. Existing local/remote URI sources must still exist
when replanning; unavailable sources are rejected before changing the plan.

On restore, retained uploads and extracted archives are rebound to the configured
temporary root using their owned relative identity. Legacy jobs without that
identity are reconciled using the job-specific extraction directory or an
unambiguous owned upload directory, not a parent whose name happens to start
with `upload-`. Ambiguous legacy paths fail startup with the affected import ID;
restore that job's owned source directory from the same consistency-set backup
before retrying. Do not point recovery at an unrelated old mount.

Each revision builds a separate encrypted staging database. Only a successful,
closed candidate replaces the selected asset and encryption key. Failed
revisions keep the previous successful file; correct the displayed plan and
revalidate before publishing. Cancellation finishes only after source/staging
cleanup and metadata removal; interrupted cancellation resumes at startup.

Authenticated multipart uploads have an independent transport budget:
`Importer.UploadTimeoutSec=900` limits total upload time and
`Importer.UploadIdleTimeoutSec=60` limits pauses between reads. Zero selects
these defaults; negative values are rejected. The normal API read/write
timeouts are unchanged. Upload size caps remain enforced, and proxy limits
must allow the same size and duration.

`Audit.Enabled` records security failures and successful/failed change requests in the separate encrypted DuckDB file at `Audit.DatabasePath`; ordinary reads are included only with `RecordReads=true`. Mutation classification uses the effective protocol operation, including WFS GET mutations and XML POSTs. Request bodies, query strings, credential secrets, and raw claims are never stored. API-key requests and key-backed sessions include the non-secret `credential_id`, separately from the optional owner/principal name. Filter using `GET /api/v1/audit?credential_id=KEY_ID` or the console's API-key ID filter. An additive migration preserves old events and queued payloads; historical events without attribution retain an empty credential ID rather than inventing an owner. `RetentionDays` defaults to 90, cleanup runs on `CleanupIntervalSec`, and `MaxFieldBytes` bounds every recorded string. The audit file uses `Store.EncryptionKey` but must not be the same path as the catalog.

Audit delivery has an explicit durable fail-open policy. A request keeps its application response when the direct audit write fails; the bounded/redacted event is synchronously placed in an encrypted catalog outbox and retried oldest-first. `/ready` reports the audit component failed while delivery or retention is degraded, while `/health` remains a process-only liveness check. `WriteTimeoutSec`, `RetryIntervalSec`, and `RetryBatchSize` bound delivery work. If both the audit write and catalog handoff fail, readiness remains latched failed for the process and the loss counter requires operator investigation. Pending outbox events are not included in `GET /api/v1/audit` until delivered. The application mutation and this post-response audit handoff are not one transaction; deployments requiring zero-gap auditability must use a selected-operation fail-closed design or transactionally coupled mutation staging.

Audit capture includes authentication and CSRF rejections before management/workspace handlers run. Login, refresh, and logout are security events even with `RecordReads=false`; successful logins carry verified actor attribution. Rejected tokens do not contribute unverified subjects or raw claims.

`GET /api/v1/audit` returns `events` and an optional `next_cursor`. Follow that opaque cursor with the same filters to retrieve older matching events, ordered by timestamp and ID. `limit` accepts 1–1000 (default 100); `q` searches retained attribution, operation, path, and status fields, while `workspace`, `principal`, and `credential_id` are exact filters. `since` is RFC3339. `outcome=failed` keeps responses with status 400 or higher, `outcome=succeeded` the rest. Invalid cursors, outcome values, limits, and unsupported `offset` pagination return 400. New events inserted ahead of a page do not shift subsequent pages.

## Cache

Cache.Enabled controls all in-memory response caches. Cache.Profile may be balanced, feature-heavy, or map-heavy. Explicit sub-cache budgets override the profile.

Managed style-asset changes automatically invalidate workspace rendering. Map and tile identities include the committed asset manifest, including shared/group consumers; no manual cache clear is required. The manifest is reconstructed at startup, so previous asset versions cannot be reused from persistent caches after restart.

Subsections configure capabilities, collections, features, counts, and tiles. Each has an enabled flag, TTL, and memory budget; response caches also support maximum entry sizes. Enabled sub-budgets must fit within Cache.MaxMemoryMB.

See [Performance and observability](performance.md).

## Persistent tile cache

PersistentCache enables an opt-in durable L2 cache below the existing in-memory tile cache. It is disabled by default. Backend may be filesystem or s3. DatabasePath stores the standalone DuckDB cache database, including tile metadata, LRU usage, and durable job checkpoints. The database reuses Store.EncryptionKey but is separate from the catalog. MaxBytes is the global hard quota, and the maintenance intervals control batched access timestamps and recovery checks.

Filesystem.Root must be on persistent storage. S3 configuration uses the standard AWS credential chain and supports custom endpoints, path-style requests, AES256, and KMS encryption. S3 mode is intentionally single-active-node because the local cache database is authoritative. `S3.OwnershipEnabled` (default true) enables an atomic conditional marker and `S3.OwnershipCheckIntervalSec` (60) controls a single read-only ownership verification. The marker is not periodically rewritten. Automatic stale or same-host takeover is not supported; recovery uses the one-shot `serve --take-over-tile-cache-owner <owner-uuid>` option after the operator confirms the previous process is dead. See [Persistent tile caching](tile-cache.md).

PersistentCache.Jobs controls durable seed, reseed, and truncate workers. MaxTilesPerJob rejects unexpectedly broad jobs before enqueueing. Jobs checkpoint tile chunks and resume after a clean or unclean restart. See [Persistent tile caching](tile-cache.md).

## Mosaic catalog

`MosaicCatalog` enables the optional operational index for `raster_mosaic` services. It is disabled by default. `DatabasePath` is a standalone DuckDB database that reuses `Store.EncryptionKey`; it is not attached to the catalog or tile-cache metadata file. Harvest workers inspect allowlisted GeoTIFF/COG granules, persist their dimensions and footprint, and activate completed generations atomically. Job size, worker concurrency, retry, progress-checkpoint batch size, and shutdown settings bound ingestion work. Static mosaics continue to use their service `connection_info` when the catalog is disabled or has no active generation.

## Observability

Observability.OTel enables OTLP/HTTP traces and metrics and sets the service name and shutdown timeout. Standard OpenTelemetry environment variables configure the exporter.

Observability.Pprof enables protected endpoints below /api/v1/debug/pprof.

## Authentication

Auth.Enabled activates the configured server authentication method: none, apikey, basic, or oidc. Self-signed JWTs and stored API keys are always available to the identity middleware.

Key settings include HTTPS enforcement, the static API key, basic users, DefaultRole, query-string key policy, OIDC issuer/client, required scopes, and issuer verification.

OIDC discovery and JWKS HTTP calls have five-second timeouts. Discovery failures
are not permanent: readiness probes and bearer requests retry with exponential
backoff from one to 30 seconds, sharing one initialization attempt. `/ready`
reports the `oidc` dependency unavailable until discovery succeeds; `/health`
remains liveness-only. OIDC bearer authentication fails closed with HTTP 503
during discovery outages, while configured API keys and self-signed JWTs remain
usable. Recovery requires no server restart. Successful discovery is cached;
readiness is not a continuous upstream identity-provider availability probe.

`Auth.Session.TTLSec` (default 43200), `IdleTimeoutSec` (default 3600), and `CleanupIntervalSec` (default 300) control encrypted browser sessions used by the administration console. `Auth.OIDC.BrowserLoginEnabled`, `BrowserScopes`, and `GroupClaims` control the browser Authorization Code + PKCE flow and the exact or dotted claims available to role mappings. `RequiredScopes` fails closed for API bearer tokens; it is not imposed on browser ID tokens. Basic/static sessions re-evaluate the currently configured `DefaultRole` on each request. See [Authentication](authentication.md).

See [Authentication and authorization](authentication.md).

## Collection overrides

Auto-discovered collections can be customized by source ID:

~~~toml
[Collections."public.buildings"]
CollectionId = "buildings"
Title = "Building footprints"
Hidden = false
IDColumn = "id"
GeometryColumn = "geom"
PropertiesInclude = ["name", "height"]
CrsDefault = 4326
CrsAllowed = [4326, 3857]
~~~

Includes restrict the exposed properties before excludes are applied. Use collection overrides for legacy auto-discovery behavior; management-created workspace layers store their publishing configuration in the backing store.

Related: [Deployment](deployment.md) · [Management API](management-api.md)
