# Deployment

neoserver can run as a native binary or a container. Every deployment must initialize and persist its encrypted backing store before starting the server.

## Runtime requirements

Native builds require Go 1.26.8, a C toolchain for DuckDB's CGO bindings, the GDAL development/runtime libraries, and access to the configured datasource systems. Build with:

~~~bash
make build
~~~

Output capabilities are runtime-accurate. WMS GeoTIFF/PDF and WFS GeoPackage/SHAPE-ZIP are registered and probed once through the linked GDAL library. A missing optional driver disables and removes only its corresponding format from service capabilities; it does not trigger a request-time driver installation or a DuckDB Spatial fallback. Verify the target image's advertised capabilities after changing its GDAL package set.

The container image uses Ubuntu 26.04 and runs as UID/GID 65532. The native build uses pinned GDAL 3.13.3; the runtime installs the packages its libraries require and keeps the netCDF, JP2OpenJPEG and PDF plugins. Mount writable data directories with permissions that allow this user to create the store and remote-file cache.

DuckDB 1.5.5 spatial and httpfs extensions are bundled in the runtime user's
home. Initialization, startup and local DuckDB/GeoParquet queries work without
network access. Remote sources and identity providers still need their own
network connections. For native installations, `neoserver install-extensions`
installs and loads the matching extensions in the invoking user's home and
prints their version, platform and paths as JSON; no catalog or encryption key
is required.

### Datum-shift grids

The base image omits PROJ datum grids. GDAL raster reprojection continues to
work, but transformations that need these grids (such as NAD27 to NAD83) may
use a less accurate fallback. Install the grids for your data's area of use
when precise datum conversion is required. PostGIS vector transformations use
the database server's PROJ installation. DuckDB spatial has its own embedded
PROJ database; its default vector transformations do not use the image's
system grid directory.

The image keeps `projsync`, `projinfo`, `gdalinfo` and `ogrinfo`. Choose one of
these three grid installation methods:

1. **Mount a grid directory.** Prepare a writable directory for UID 65532,
   then download the needed grids. For example, for the contiguous US:

   ```sh
   mkdir -p proj-grids
   sudo chown 65532:65532 proj-grids
   docker run --rm --entrypoint projsync \
     -v "$PWD/proj-grids:/proj-grids" tobilg/neoserver:0.1.1 \
     --file us_noaa_conus.tif --target-dir /proj-grids
   ```

   Mount it read-only in the server container with
   `-v "$PWD/proj-grids:/proj-grids:ro"` and set
   `PROJ_DATA=/usr/local/gdal-internal/share/proj:/proj-grids`.
   Keep the first path: it contains the required `proj.db`.

2. **Download on demand.** Set `PROJ_NETWORK=ON` and
   `PROJ_USER_WRITABLE_DIRECTORY=/data/proj-cache`. Allow outbound HTTPS to
   `cdn.proj.org` and keep `/data` writable. This method needs network access
   when a required grid is not cached.

3. **Build an image with your grids.** Download the grids first, then build:

   ```dockerfile
   FROM tobilg/neoserver:0.1.1
   COPY proj-grids/ /usr/local/gdal-internal/share/proj/
   ```

Verify the active operation inside your configured container:

```sh
projinfo -s EPSG:4267 -t EPSG:4269 --bbox -100,30,-99,31 \
  --spatial-test intersects --grid-check discard_missing --hide-ballpark -o PROJ
```

A grid-based operation names a file in `+grids=...`. Choose a source/target
pair and area matching your data when checking other transformations.
With `PROJ_NETWORK=ON`, omit `--grid-check discard_missing` to include grids
available from the CDN; that flag explicitly restricts the check to local files.

## Initialization lifecycle

Initialization is an explicit operator action:

~~~bash
export NEOSRV_STORE_KEY="$(openssl rand -hex 32)"
./neoserver init --store-path /srv/neoserver/neoserver.db
~~~

This creates the encrypted store, generates an internal ECDSA signing key, and prints a one-time bootstrap token. Starting the server never creates security state implicitly:

~~~bash
./neoserver serve --config /etc/neoserver/neoserver.toml
~~~

Reuse the same store path and NEOSRV_STORE_KEY on every start. A missing store or wrong key causes startup to fail.

WFS feature locks and feature-version metadata are persisted in the catalog store, so a restart does not invalidate held WFS lockIds (they remain valid until their expiry). In-memory response caches are rebuilt after a restart.

## Single active node

neoserver requires exactly one active server process per store. This is enforced at runtime:

- The catalog, the persistent tile-cache index, and the mosaic index are DuckDB files held with exclusive OS file locks. A second process that tries to open one fails at startup with a message naming the contested path. The same applies to admin CLI commands (`create-token`, `rotate-signing-key`, `add-claim-mapping`): stop the server before running them against its store.
- In S3 tile-cache mode, a conditional ETag lease refuses startup whenever an unreleased ownership marker exists; see [Persistent tile caching](tile-cache.md) for fencing, graceful release and owner-ID-scoped manual takeover.

OS file locks are released automatically when a process dies. S3 ownership markers do not expire automatically: after confirming the old process is dead, restart once with `--take-over-tile-cache-owner <owner-uuid>` using the UUID printed by the blocked startup.

Available recovery commands:

~~~bash
./neoserver create-token --store-path /srv/neoserver/neoserver.db \
  --role super_admin --expires 24h

./neoserver rotate-signing-key --store-path /srv/neoserver/neoserver.db
~~~

Rotating the signing key invalidates all self-signed JWTs but does not change the store encryption key.

## Docker and Compose

Build and start the application:

~~~bash
export NEOSRV_STORE_KEY="a-persistent-secret-value"
export NEOSRV_SERVER_URLBASE="https://geo.example.com"
docker compose run --rm server init --store-path /data/neoserver.db
docker compose up --build
~~~

Add --profile postgis when using the repository's PostGIS service. Compose uses
the `serverdata` named volume at `/data` and a separate `pgdata` volume for
PostGIS. Protect and back up both. `make down` preserves them; only the explicitly
confirmed `make reset-demo CONFIRM_DELETE_DEMO_DATA=yes` removes both volumes.
Existing `./data` bind mounts are not migrated automatically; see the
[container upgrade instructions](releasing.md#container-storage-and-upgrade).

In an orchestrator, run init as a one-time job against the same persistent volume used by the application. Do not run concurrent initialization jobs. Retrieve the bootstrap token from the job output and move ongoing administration to OIDC or scoped API keys. When enabled, place `Importer.Root` and `Audit.DatabasePath` on private persistent storage too; the audit database must be a different file from the catalog.

## Persistent state

Persist:

- The encrypted DuckDB store configured by Store.Path
- The standalone encrypted persistent tile-cache index and its filesystem/S3 payloads when `PersistentCache.Enabled=true`
- The standalone encrypted mosaic catalog when `MosaicCatalog.Enabled=true`
- The managed import root (published encrypted DuckDB files) when `Importer.Enabled=true`
- `Importer.TemporaryDirectory`, including retained uploads and extracted inputs, when pending/revisable imports must survive restore
- The standalone encrypted audit history when `Audit.Enabled=true`
- The managed style-asset directory configured by `WMS.StyleAssetPath`
- Local datasource files
- Datasource.RemoteCachePath when cached remote files should survive restarts
- External databases independently

The store contains workspaces, service connection details, layers, styles, API-key hashes, claim mappings, RBAC state, signing keys, import lifecycle links, and the pending audit-delivery outbox. Feature data remains in its datasource.

Back up a quiescent consistency set containing the main catalog, managed style assets, managed import root, audit history, mosaic catalog, persistent tile index and payload store, and the configuration needed to locate them. The tile payload cache is rebuildable, but its durable jobs and quota index must either be backed up with matching payloads or deliberately discarded and reseeded. Store the encryption key separately in a secret manager; a backup without its original key cannot be opened.

Include retained import inputs from `Importer.TemporaryDirectory` in that set. Catalog schema v24 records their owned relative paths; startup reconciles both staged/published output and retained inputs beneath the configured replacement roots. Pre-v24 managed upload/extract paths are reconciled using their owned directory names. Arbitrary URI/local datasource paths are not relocated. Restoring outputs without retained inputs may preserve a staged result but cannot support revision; restore the missing input or reupload it. The recovery drill also transforms a restored pending source before declaring success.

WFS feature locks now persist stable source keys independently of publication names. PostGIS aliases in a workspace share locks when their configured endpoint, database and qualified table match, even across services with different credentials. Use one canonical configured endpoint for a database; distinct DNS/proxy aliases are not automatically identified as the same database. Pre-upgrade locks lack source keys, so their feature IDs are conservatively protected across the workspace until their original expiry or explicit release. Back up before upgrading the catalog; do not run an older binary against an upgraded catalog without a tested rollback procedure.

Do not copy DuckDB files while neoserver is writing them. Stop the server cleanly (or otherwise quiesce all writers), capture every enabled state component, and restore them together before starting the replacement process. The catalog and audit database form one consistency set because pending audit events live in the catalog and delivery to the audit database is idempotent. Pending catalog deletion, import publication, import rollback, and audit-delivery operations resume after restore. Managed import paths are reconciled beneath the configured restored `Importer.Root` before workspace data sources open. Test restoration by polling pending operations, checking readiness, and exercising representative OGC requests. Source data stores require their own consistent backup policy.

`make test-recovery` runs the repository's deterministic restore drill. It closes all writers; copies the encrypted catalog, managed imports, audit history, filesystem tile cache, mosaic catalog, and style assets; and reopens them at new paths with the original key. It verifies a surviving published import, interrupted publication and rollback reconciliation, audit outbox delivery and retention, a custom tile matrix definition, a resumed tombstoned deletion, representative OGC requests, and source-raster survival. Run an equivalent drill against deployment-specific paths and copy the complete S3 payload prefix when S3 caching is enabled. A wrong key or missing main catalog must fail before serving traffic.

## Public URLs and reverse proxies

Server.UrlBase must be the externally visible absolute HTTP or HTTPS origin. It is required when the server binds to a non-loopback address because generated OGC links and OpenAPI server URLs depend on it.

Use Server.BasePath when the complete application is mounted below a prefix:

~~~toml
[Server]
HttpHost = "0.0.0.0"
HttpPort = 9000
UrlBase = "https://geo.example.com"
BasePath = "/maps"
TrustedProxyCIDRs = ["10.0.0.0/8"]
~~~

The resulting management API begins at /maps/api/v1 and workspace services at /maps/workspaces/{workspace}/....

Only peers listed in TrustedProxyCIDRs may supply forwarding headers. Configure the proxy to replace, not append untrusted client values, and set X-Forwarded-Proto to https. Loopback proxies are trusted automatically for local development.

## HTTPS and authentication

Authenticated requests are rejected over insecure transport when Auth.RequireHTTPS is true. Terminate TLS either in neoserver's trusted upstream proxy or in infrastructure that preserves the forwarded scheme.

Production recommendations:

- Keep NEOSRV_STORE_KEY, datasource passwords, static API keys, and basic-auth passwords in a secret manager.
- Prefer OIDC or scoped workspace API keys over a shared static credential.
- Keep Auth.DefaultRole at viewer unless elevated global access is intentional.
- Use an explicit CORS origin list for browser clients.
- Leave query-string API keys disabled.
- Restrict local datasource paths and remote URL patterns.

See [Authentication and authorization](authentication.md).

## Health, shutdown, and diagnostics

`GET`/`HEAD /health` is unauthenticated and returns 200 when the HTTP process is alive; it deliberately performs no dependency checks. `GET`/`HEAD /ready` checks the encrypted catalog and every enabled durable coordinator/cache with a two-second bound. It returns 200 with `status=ready` or 503 with `status=not_ready`. Response checks expose only `ok`, `failed`, or `disabled`; detailed errors stay in logs. Publication datasources are intentionally excluded. Apply `Server.BasePath` to both paths.

Audit delivery is fail-open for the completed request but fail-ready for operations: a direct audit failure queues the event durably in the catalog and changes the audit readiness check to `failed` until the backlog drains. Graceful shutdown retries pending delivery within `Audit.ShutdownTimeoutSec` and returns an error without deleting undelivered events when that deadline expires. Alert immediately on `neoserver.audit.events_lost`; alert on sustained `neoserver.audit.degraded`, `pending_events`, `write_failures`, or `retention_failures` for longer than the configured retry interval.

SIGINT and SIGTERM trigger graceful HTTP shutdown with a ten-second timeout. Allow at least that period before forcefully stopping a container.

OpenTelemetry export and pprof are opt-in. pprof is mounted below /api/v1/debug/pprof and requires a super_admin identity and secure transport. See [Performance and observability](performance.md).

## Upgrades

Read the versioned [release notes](release-notes.md) before changing the active
deployment, then work through this checklist.

1. Stop writes, capture the complete consistency set, retain the original
   encryption key separately, and test restore.
2. The catalog schema is **25**, persistent-cache schema **2**. Never force an
   older binary to open newer state by editing `schema_info`. Restore a compatible
   backup, review integrity, and reapply revocations made after it.
3. Containers use UID/GID **65532** and `/data`. Compose uses a named volume;
   previous `./data` contents are not migrated or deleted automatically. Preserve
   all state and provision ownership before switching mounts.
4. Review reprojected managed imports created before 2026-09-14. Metadata
   recovery does not repair coordinates. Follow [import recovery](#managed-import-upgrade-recovery).
5. Retain import inputs/outputs, style assets, audit history, mosaic index and
   matching persistent-cache index/payloads. Never copy live DuckDB files.
6. Convert unsupported vector input drivers offline; retest style subsets,
   gates, authentication, CRS/axis order, writes and representative renders.

### Managed-import upgrade recovery

New managed imports persist native CRS metadata in `__neoserver.layers`.
Older imports recover known CRS from their original catalog plan. This metadata
recovery does not rewrite already incorrect geometry.

For reprojected imports created before 2026-09-14:

1. Back up the complete consistency set and preserve original source files/plans.
2. Inspect jobs whose source and target SRIDs differ. Verify non-symmetric control
   coordinates against the original source. Do not blindly swap coordinates:
   authority-axis order varies between CRS pairs.
3. Reimport suspect datasets from trustworthy originals with the corrected
   server, initially under different service/publication names. Check coordinates,
   extents, IDs, response CRS and rendered placement.
4. Reapply style/access/publication settings, then coordinate client URL cutover.
   Remove or roll back old imports only after verification and operator approval.

Ordinary DuckDB tables without usable CRS metadata require the true native
`connection_info.srid` or `connection_info.layer_srids`, not the desired output
CRS. Implicit EPSG:4326 guessing is intentionally unsupported.

### General upgrade procedure

Before upgrading:

1. Back up the store and record the running version.
2. Keep the existing NEOSRV_STORE_KEY unchanged.
3. Review configuration additions in config/neoserver.toml.example.
4. Replace the binary or image, then start against the existing persistent store.
5. Verify `/health`, `/ready`, the management workspace list, and representative OGC endpoints.

A binary refuses a state database whose schema is older than its baseline or newer than it supports; it never modifies a database it refuses. Avoid rolling back to an older binary without a compatible backup.

Version 0.1.0 already uses the supported catalog (25), tile-cache (2), and
mosaic (1) schemas. No schema upgrade is required for 0.1.1. Its unversioned
audit log is recognized and stamped at baseline 1. DuckDB 1.5.5 can update
the storage format of encrypted files on write, however: rolling back to
0.1.0 requires restoring the pre-upgrade catalog and audit backup.

After upgrading from a release that predates referentially complete deletion, inspect `GET /api/v1/catalog/integrity`. No orphan is removed automatically. Review the super-admin report before invoking the explicitly confirmed repair endpoint described in the [Management API](management-api.md).

## Production checklist

- Store initialized once and mounted persistently
- Encryption key stored separately and recoverably
- External URL, base path, and trusted proxies correct
- HTTPS enforced for authenticated traffic
- Datasource network and path access minimized
- Memory, render, tile, paging, and WFS limits sized
- Backups and restoration tested
- Separate liveness (`/health`) and readiness (`/ready`) probes, logs, metrics, and shutdown grace configured

Related: [Configuration](configuration.md) · [Performance](performance.md) · [Getting started](getting-started.md)
