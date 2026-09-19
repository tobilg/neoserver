# Persistent tile caching

neoserver can place a durable L2 cache below its existing in-memory tile cache. It stores canonical OGC API - Tiles and WMTS map/MVT payloads; it does not store arbitrary WMS GetMap images.

## Backends

Persistent caching is disabled by default. Filesystem mode writes payloads atomically below `PersistentCache.Filesystem.Root` and keeps tile metadata, LRU usage, and durable jobs in a standalone DuckDB database at `PersistentCache.DatabasePath`:

~~~toml
[PersistentCache]
Enabled = true
Backend = "filesystem"
DatabasePath = "/data/tile-cache.duckdb"
MaxBytes = 10737418240

[PersistentCache.Filesystem]
Root = "/data/tiles"
~~~

S3-compatible mode uses the standard AWS credential chain. Endpoint and path-style options support compatible object stores. Server-side encryption may be `AES256` or `aws:kms`.

~~~toml
[PersistentCache]
Enabled = true
Backend = "s3"
DatabasePath = "/data/tile-cache.duckdb"
MaxBytes = 107374182400

[PersistentCache.S3]
Bucket = "maps"
Region = "eu-central-1"
Prefix = "neoserver/tiles"
UsePathStyle = false
ServerSideEncryption = "AES256"
~~~

The cache database is encrypted with the same `Store.EncryptionKey`/`NEOSRV_STORE_KEY` as the catalog, but it is a separate file and DuckDB instance. S3 mode supports one active neoserver node. The local cache database is authoritative, so multiple active nodes must not share a prefix.

### Prefix ownership guard

S3 mode enforces the single-active constraint with conditional object writes. Startup first verifies that the endpoint honors `If-None-Match` and `If-Match` on PutObject; an incompatible implementation is rejected. It then creates `<Prefix>/.neoserver-owner.json` atomically or conditionally replaces a gracefully released generation. The marker remains immutable while the process owns it. Once per `PersistentCache.S3.OwnershipCheckIntervalSec` (default 60), the owner performs one read-only marker verification against its original owner UUID and ETag. Graceful release conditionally replaces that generation with a `released: true` marker; it never deletes the shared key. Thus an old process cannot release a replacement owner, including on MinIO versions that ignore conditional-delete headers.

There is no timed or same-host takeover. When an unreleased marker exists, startup reports its owner UUID and refuses to start. After confirming that the reported process is dead, authorize exactly that marker for one recovery invocation:

~~~bash
./neoserver serve --config /etc/neoserver/neoserver.toml \
  --take-over-tile-cache-owner 6d8f392c-34da-43d2-920d-44d6089f0f69
~~~

The command fails if the marker is absent, already released, or its owner UUID changed. Never use takeover while the old process might still be running. A corrupt marker must likewise be removed manually only after proving that no owner is active.

Marker schema 3 adds the release state. Existing schema 1/2 active markers still
require the exact-owner recovery procedure and are upgraded by takeover. Older
binaries cannot read schema 3; do not downgrade in place or remove an active
marker to bypass the guard. The catalog/cache database schemas are unchanged.

Any failed or mismatched ownership verification permanently fences persistent-cache payload, metadata, maintenance, and job mutations in that process until restart. Rendered and cached reads may continue, but `/ready` returns 503. Set `OwnershipEnabled = false` only when an external orchestrator already supplies equivalent single-writer fencing.

## Upgrade from the development SQLite index

The former SQLite index is not migrated. Before starting this version, remove the old local index and cached payload directory. If development used S3, clear the old cache prefix as well. Normal requests or seed jobs will populate the new DuckDB cache.

## Quotas and eviction

`PersistentCache.MaxBytes` is the global hard quota. The workspace OGC Tiles setting `persistent_cache_quota_bytes` and layer/coverage field `tile_cache_quota_bytes` add narrower quotas. Zero inherits the parent quota. A write applies resource, workspace, and global LRU eviction in that order. A single tile larger than an applicable quota is served but not stored.

Usage and eviction counters are available from:

~~~text
GET /api/v1/cache/stats
GET /api/v1/workspaces/{workspace}/tile-cache/stats
GET /api/v1/workspaces/{workspace}/tile-cache/stats?resource=buildings
~~~

## Parameter filters, metatiles, and gutters

Feature-layer create/update payloads accept `tile_cache_parameters`:

~~~json
{
  "styles": ["default", "night"],
  "times": ["2026-08-01", "2026-08-02"],
  "elevations": ["0", "10"],
  "metatile_factor": 2,
  "gutter_pixels": 8
}
~~~

Time/elevation values are exact bounded allowlists. A map tile outside a configured allowlist is still rendered but returns `X-Cache: BYPASS` and is written to neither memory nor persistent cache. With no explicit policy, only the default (empty) time/elevation selection is cacheable; styles remain bounded by the catalog style set. `Tiles.MaxParameterCombinations` bounds the allowlist product, while `MaxMetatileFactor` and `MaxGutterPixels` are hard server ceilings.

For feature map tiles, a factor greater than one aligns a larger render to the tile grid, expands it by the gutter, and crops the requested 256-pixel tile after rendering. This reduces label and symbol seams. Vector tiles are not metatiled, and coverage/group portrayal currently continues to render one tile at a time.

Custom global matrix sets are managed at `/api/v1/tile-matrix-sets`. They are persisted with revision/digest metadata and shared by OGC API Tiles, WMTS, and seed validation. Built-ins are immutable; custom grids currently use a top-left origin and EPSG:3857 or CRS84 so native and WGS84 query bounds remain deterministic.

## Seed, reseed, and truncate jobs

Create a seed job. Bounds are optional when publication persisted a native extent; neoserver transforms EPSG-referenced native extents to the selected tile matrix set. Explicit bounds require an EPSG:4326 or EPSG:3857 CRS.

~~~bash
curl -X POST \
  http://localhost:9000/api/v1/workspaces/acme/tile-cache/jobs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "operation": "seed",
    "resource": "buildings",
    "tile_type": "map",
    "tile_matrix_set": "WebMercatorQuad",
    "format": "image/png",
    "style": "default",
    "min_zoom": 0,
    "max_zoom": 12,
    "bounds": {"bbox": [5.8, 47.2, 15.1, 55.1], "crs": "EPSG:4326"}
  }'
~~~

`seed` skips existing durable entries. `reseed` renders and replaces matching entries. `truncate` removes matching entries; `{"operation":"truncate","all_resources":true}` clears the workspace's durable tile entries. A bounded truncate resolves exact current-generation tile identities, while an unbounded truncate removes all matching resource generations.

Unbounded style-scoped truncation uses the logical style name (including `default`), not the full rendering fingerprint. It covers matching matrix-set, dimension, and group-dependency variants subject to the other supplied filters; unrelated styles remain cached.

List, inspect, and cancel jobs:

~~~text
GET    /api/v1/workspaces/{workspace}/tile-cache/jobs
GET    /api/v1/workspaces/{workspace}/tile-cache/jobs/{job}
DELETE /api/v1/workspaces/{workspace}/tile-cache/jobs/{job}
~~~

Jobs report queued, running, cancelling, cancelled, succeeded, or failed state plus tile and byte counters. Chunks checkpoint their next tile after every attempt. Running jobs return to the queue after restart; user-cancelled jobs remain terminal. `PersistentCache.Jobs.MaxTilesPerJob` rejects overly broad requests before they enter the queue.

Workers atomically claim the oldest eligible queued job independently of the paginated history view. Terminal history cannot hide older queued work, and concurrent workers cannot claim the same job twice.

## Recovery and invalidation

Payload writes use pending metadata followed by an atomic filesystem rename or S3 PutObject and a ready transition. Startup removes interrupted pending entries. Missing objects found during reads or seed checks are repaired as cache misses. Resource publication changes increment a resource generation, workspace tile-render or data-service changes increment a workspace revision, and style bodies contribute a digest. Changed resources, settings, services, and styles therefore cannot serve stale canonical entries. Old generations remain eligible for LRU eviction or truncate jobs.

WFS transactions establish a durable workspace data-write barrier before changing the source. Tiles bypass both cache tiers while it is pending; completion advances the persisted data revision included in vector, map, and dependent-group identities. Rollback also advances the revision conservatively. If completion bookkeeping fails after a source commit, the barrier stays pending and caching remains bypassed. Startup recovery advances the revision before clearing interrupted barriers, so pre-edit tiles cannot reappear after a restart. External database edits do not pass through this barrier: refresh/truncate the affected cache explicitly after out-of-band writes.

Related: [WMTS](wmts.md) · [OGC API - Tiles](ogc-api-tiles.md) · [Management API](management-api.md)
