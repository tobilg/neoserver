# STAC validation

Run `make test-conformance-stac` for an isolated Docker fixture and four separately reported checks:

- The STAC project's `stac-api-validator` checks API Core, Collections, Features, Item Search and pagination.
- `stac-validator` checks core documents against pinned, vendored STAC schemas.
- `pystac-client` exercises GET and POST pagination over all 300 fixture Items, checks limit clamping, and verifies that `ids` and `bbox` filters apply together.
- The stock, digest-pinned OGC Features ETS runs against `/workspaces/demo/stac/`. This establishes tested OGC Features requirements; it is not a STAC-specific certification suite.

Python dependencies are pinned with hashes in `requirements.lock`. The OGC image and profile are pinned in `testing/officialets/versions.lock.json`. Raw validator logs, exit codes and OGC reports are retained under `test-results/conformance`. Advisory STAC validator warnings are retained alongside errors.

Runtime schemas live in [`internal/stacmodel/schemas`](../../internal/stacmodel/schemas), with names such as `stac-1.1.0-item.json` and `geojson-geometry.json`. They cover Items and Collections in STAC 1.0.0 and 1.1.0, plus their dependencies and query geometries. The generated STAC 1.1 Catalog is validated using the test-only schema in [`schemas`](schemas); Catalog schemas are not embedded in the server. Each directory's `manifest.json` maps the original schema URLs to local files and records their source URLs and SHA-256 hashes. The document validator loads both manifests. Schema contents and `$ref` URLs remain unchanged from upstream; the STAC license files are retained with the runtime schemas.

The checked-in `testing/fixtures/stac` inventory contains one Collection with 300 dated footprints. It covers the stock ETS bounding boxes around Europe, South America, the antimeridian and both poles. This allows its spatial assertions to execute without an empty-fixture skip. The profile has its own assertion baseline because it has one Collection and does not advertise the optional OGC CRS extension. Its reviewed skips cover that unclaimed extension and the optional `numberMatched` and response `timeStamp` fields, which the API omits. Unexpected skip reasons or additional skipped executions fail coverage checks.

`scene-0294` has a footprint at `[20.2,20.2,20.4,20.4]`, inside the API validator's fixed `[20,20,21,21]` probe box. This lets the upstream validator exercise its check that `ids` does not override a non-intersecting `bbox`. The PySTAC lane also requires a probe Item and records matching and excluded results for both GET and POST in `pystac-client.log`; a missing probe or incorrect filtering fails that lane.

To validate an existing public fixture with the pinned Python environment:

```bash
STAC_URL=http://localhost:9000/workspaces/demo/stac/ \
STAC_RESULTS=test-results/stac python testing/stac/run.py
```

Source adapter regressions run with the Go suite. A real PostGIS table/SQL-view boundary check can additionally use a disposable database (the test creates and removes its own schema):

```bash
NEOSRV_STAC_TEST_POSTGIS='postgres://postgres:postgres@localhost:5432/postgis?sslmode=disable' \
  make test-focused PKG=./internal/stacsource TEST=TestPostGISPublicationAndSQLViewBoundary
```

See [the STAC guide](../../docs/stac.md#operation-and-validation) for the opt-in million-Item qualification and its hardware target.
