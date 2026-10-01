# SpatioTemporal Asset Catalog

STAC publishes searchable metadata about geospatial assets and existing neoserver datasets. Each workspace has its own catalog at `/workspaces/{workspace}/stac/`. There is no deployment-wide catalog or cross-workspace search. Prepend `Server.BasePath` when configured.

The API implements STAC API 1.0.0 Core, Collections, Features and Item Search. Generated documents use STAC 1.1.0; imported 1.0.0 and 1.1.0 documents retain their version and extension fields. See the [OGC STAC standard](https://docs.ogc.org/cs/25-004/25-004.html) and [STAC specification overview](https://stacspec.org/en/about/stac-spec/).

## Enable a workspace

Enable the server feature in the configuration and restart:

```toml
[STAC]
Enabled = true
DatabasePath = "./data/stac.duckdb"
MaxItems = 1000000
MaxUploadBytes = 1073741824
WorkerCount = 1
RefreshIntervalSec = 900
```

The database must be separate from other neoserver databases. It uses the original `NEOSRV_STORE_KEY`. The Item limit applies across all workspaces. Each workspace may have at most 8 unpublished imports, and staged imports across all workspaces are bounded to twice the Item limit. Source refreshes are limited to one active job per Collection and are never blocked by staged imports. Individual metadata records are limited to 8 MiB.

In the administration console, select a workspace, open **STAC → Settings**, and enable its catalog. Catalogs are private by default. Anonymous access requires both the workspace catalog and the Collection to be public. Linked Collections also inherit the current source layer or coverage visibility.

The equivalent management request is:

```bash
curl -X PUT "$BASE_URL/api/v1/workspaces/demo/settings/stac" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  --data '{"enabled":true,"public":false,"title":"Demo assets","description":"Published datasets and acquisitions"}'
```

## Publish existing data

Create a Collection in **STAC → Collections**, then use **Publish existing data** to select a layer or coverage from the same workspace. Preview the mapping before publishing. Publication runs as a durable job; follow its progress in **Jobs**.

| Mode | Sources | Result |
| --- | --- | --- |
| Dataset discovery | All published vector layers, SQL views and raster coverages | A Collection describing the dataset, with links to its enabled OGC services; no synthetic Items |
| Mapped asset records | PostGIS, DuckDB, GeoParquet, GDAL vector layers, PostGIS/DuckDB SQL views | One Item per source row, using explicit mappings for stable ID, acquisition time and asset URLs |
| Raster assets | GeoTIFF/COG coverage or managed raster mosaic | One Item per file or granule, with a geographic footprint and existing asset access |

A row mapping requires an explicit stable, unique ID property, a genuine acquisition instant or start/end interval, and at least one asset URL. A spatial feature is not automatically an asset record. SQL view mappings query the published view, including its selected fields and filters.

The console supports property selectors and additional mapping JSON. Values select a source property or a constant:

```json
{
  "id_property": "scene_id",
  "datetime": {"property": "acquired_at"},
  "assets": {
    "data": {
      "href": {"property": "asset_url"},
      "type": "image/tiff; application=geotiff; profile=cloud-optimized",
      "roles": ["data"]
    }
  },
  "properties": {"platform": {"constant": "example-satellite"}}
}
```

For an interval, leave `datetime` unset and map both `start_datetime` and `end_datetime`. Timestamps use RFC 3339. Typed source timestamps without a timezone are interpreted as UTC. File modification times are never substituted for acquisition times. A GeoTIFF without acquisition metadata requires an explicit mapping constant; mosaic granules can use their recorded temporal dimensions.

Publishing adds metadata and indexes. It does not copy datasets, export database tables, or generate downloadable snapshots. Raster Item IDs derive from the binding and source URI, so replacing a granule's catalog row does not change its STAC identity.

Changes made through neoserver trigger reconciliation. Startup and a 60-second reconciliation interval recover missed events; external source changes are polled at the binding's refresh interval (900 seconds by default). Each published generation records a fingerprint of its source, binding and generated service links, so only Collections whose source changed are refreshed, including after a restart. Feature writes are tracked per workspace, so a write to any vector layer refreshes the workspace's linked vector Collections. A refresh validates every record before atomically activating its generation. Failed or interrupted refreshes retain the last successful generation. Source removal removes its linked STAC publication once the main catalog confirms the layer or coverage was deleted; source disablement immediately hides it. Deleting STAC metadata does not delete source files. To switch a linked Collection to a different source or mode, create a separate Collection; the original publication retains its source access rules.

Linked Item identity, geometry, time and assets are source-owned. Descriptive and custom property edits are stored as separate overrides and survive refresh. Unedited properties continue following the source.

## Import metadata

In **Import metadata**, select a standalone Collection and upload a Collection, Item, FeatureCollection or NDJSON file. Review the staged sample and record count, then publish. Metadata remains invisible to clients until publication succeeds. Existing IDs are rejected unless **upsert** is explicitly selected. Duplicate IDs within an upload are rejected.

Relative asset references require an explicit `base_url`; the importer does not crawl links or download assets. Extension declarations and unknown fields are retained, while offline, pinned core schemas validate each document. Extension schemas are not fetched or claimed as validated.

```bash
curl -X POST "$BASE_URL/api/v1/workspaces/demo/stac/imports?collection_id=scenes" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/x-ndjson' \
  --data-binary @items.ndjson
```

Use the returned job ID with `GET .../stac/imports/{id}/preview` and `POST .../stac/imports/{id}/publish`. Publication accepts `{"upsert":false}`. Fully staged jobs survive restart and can still be reviewed and published for 24 hours; after that they are cancelled and their staging is discarded. Interrupted uploads must be uploaded again. Failed refresh jobs can be retried. Cancellation discards unpublished staging; published jobs cannot be cancelled. Terminal job history is retained for 30 days, plus the latest job for each Collection and job kind.

## Search and clients

| Route beneath the workspace STAC root | Methods | Purpose |
| --- | --- | --- |
| `/` | GET | Catalog and discovery links |
| `/conformance` | GET | Advertised conformance classes |
| `/api`, `/api.html` | GET | OpenAPI definition and API guide |
| `/collections` | GET | Visible Collections |
| `/collections/{id}` | GET | Collection metadata |
| `/collections/{id}/items` | GET | Collection Items |
| `/collections/{id}/items/{item}` | GET | One Item |
| `/search` | GET, POST | Workspace Item Search |

Search supports `collections`, `ids`, `bbox`, `datetime`, `intersects` and `limit`. GET uses comma-separated lists and a JSON-encoded geometry; POST uses JSON arrays and an object. `bbox` and `intersects` are mutually exclusive. Coordinates use longitude/latitude in CRS84. Four- and six-coordinate bounding boxes, antimeridian crossing, point/line bounding boxes, polygon holes and open temporal intervals are supported. Spatial filtering uses geometry intersection, not just bounding-box overlap. Following OGC Features semantics, `bbox` also includes Items without a geometry; `intersects` requires a geometry.

```bash
curl "$BASE_URL/workspaces/demo/stac/search?collections=scenes&datetime=2026-01-01T00:00:00Z/..&limit=100" \
  -H "Authorization: Bearer $TOKEN"
```

Pages default to 100 Items, and limits above 1000 are clamped to 1000. Follow the response's `next` link, including its method and body for POST pagination. Tokens are bound to their workspace and search parameters; restart the search after a server restart. Collection/Item IDs are scoped to the workspace.

```python
from pystac_client import Client

catalog = Client.open(
    "https://maps.example.org/workspaces/demo/stac/",
    headers={"Authorization": "Bearer YOUR_TOKEN"},
)
for item in catalog.search(
    collections=["scenes"],
    bbox=[7, 46, 10, 49],
    datetime="2026-01-01T00:00:00Z/..",
    method="POST",
).items():
    print(item.id, item.assets)
```

The console's **Items** tab provides search, footprint visualization, pagination, JSON inspection and standalone Item editing. STAC Filter/CQL2, Query, Sort and Fields extensions are not advertised. The source binding's CQL2 filter controls publication; it is not a public STAC Filter endpoint.

## Assets and access control

External assets use absolute HTTP(S) or object-store URLs without embedded credentials. Relative filesystem paths, passwords and signed/token-bearing URLs are rejected as public asset references. Clients authenticate separately to external services.

Local asset bindings expose an existing allowlisted file through a workspace/Collection/Item endpoint. Binding requires explicit authorization for the whole file. A super administrator must authorize arbitrary files or multi-resource containers; a workspace administrator may authorize the single GeoTIFF belonging to its published coverage. Encrypted import databases and server state databases cannot be exposed. Source visibility is checked again on every download. Local asset responses support HEAD, Range and conditional requests, including from cross-origin browser clients such as STAC Browser.

Management mutations require workspace administration rights. Public POST Item Search is a read operation. Authenticated browser session POSTs retain CSRF protection. Listings, search and direct asset requests apply current workspace and source policies; metadata access does not grant access to an otherwise restricted source.

## Operation and validation

Persist `STAC.DatabasePath` on private storage alongside the main catalog. For backup or restore, stop neoserver and capture both databases together with any local asset files and the other enabled state components. Keep the original encryption key separately. Copying a live DuckDB database is not a supported backup procedure. Main catalog schema 27 adds workspace STAC settings; the independent STAC database is at schema 2 and upgrades schema 1 automatically.

Readiness includes the STAC store. OpenTelemetry metrics record processed source rows, refresh failures and refresh duration (`neoserver.stac.rows_processed`, `neoserver.stac.refresh_failures`, `neoserver.stac.refresh_duration_seconds`). Job errors and logs identify failed publications.

`make test-conformance-stac` runs four separately reported checks: the STAC API validator, core document validation, PySTAC client interoperability, and the stock official OGC API Features ETS against the STAC workspace root. Tool versions and Python dependency hashes are pinned; raw results are retained under `test-results/conformance`. A successful Features ETS run establishes its tested Features requirements, not full STAC certification. The STAC API validator itself covers only part of the API specification. The [conformance dashboard](conformance.md#conformance-dashboard) preserves these distinctions and makes the original logs downloadable.

For an opt-in inventory qualification run:

```bash
NEOSRV_STAC_BENCH_ITEMS=1000000 make test \
  GO_PACKAGES=./internal/staccatalog \
  GO_TEST_FLAGS='-count=1 -v -run=TestSTACInventoryQualification'
```

This records ingestion time, query-plan index use, Go heap size and warm selective-search p95 with four concurrent clients. Qualification requires adequate temporary disk space; assess results on the deployment's intended hardware. The acceptance target is p95 at most one second for a 100-Item selective search on a 4-vCPU, 16-GiB reference machine.

### Advisory warnings

The API validator can exit successfully while reporting advisory warnings.
They remain visible as **Passed with warnings** and are not counted as failed
assertions or merged into the inherited OGC suite's skips.

- **Metadata recommendations:** `stac-check` recommends Collection `summaries`
  and human-readable link titles. Summaries help clients describe available
  values without fetching every Item. Its suggestion to include `eo:bands`
  is generic: add band metadata only when it describes the actual assets.
  These are recommendations rather than required core schema fields. See the
  [STAC Collection summaries](https://docs.ogc.org/cs/25-004/25-004.html#summaries)
  and [Link Object](https://docs.ogc.org/cs/25-004/25-004.html#link-object).
- **Test coverage limitation:** the pinned validator searches the bounding box
  `20,20,21,21` to obtain an Item for checking that `ids` does not override other
  search filters. If the fixture has no Item there, that check cannot complete.
  This warning does not demonstrate an implementation failure, but it also
  provides no passing evidence for that specific interaction. The conformance
  fixture includes `scene-0294` inside this box so the upstream check can run.
  The PySTAC lane also checks matching and non-intersecting boxes for GET and
  POST, recording the outcomes in its log and failing if the probe is missing
  or the filters are not applied together.

Repeated Collection warnings are retained as separate upstream occurrences.
Inspect the warning text and original log rather than interpreting the count
as a count of distinct defects.
