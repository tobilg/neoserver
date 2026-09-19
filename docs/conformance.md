# Protocol testing and OGC conformance

neoserver separates ordinary correctness tests, native protocol regression,
unmodified official OGC suites, and locally adapted upstream suites. Results
from one category must not be presented as results from another.

| Category | Command | Meaning |
| --- | --- | --- |
| Unit and component | `make test` | Internal implementation correctness |
| Protocol integration | `make test-protocol-integration` | Native black-box regression against a live disposable neoserver fixture |
| Official conformance | `make test-conformance` | Unmodified digest-pinned OGC TEAM Engine execution |
| Official-derived | `make test-conformance-derived` | Published suite executed with a recorded compatibility adaptation |
| Complete assurance | `make test-assurance-all` | Failure-exhaustive execution of every category without merging their meaning |

Passing any local category is not, by itself, an OGC product certification.

## Native protocol integration

`make test-protocol-integration` starts one fresh Compose environment, creates a
disposable encrypted catalog, loads deterministic CITE vector and PostGIS raster
fixtures, and runs live OGC API Features, WMS, WFS, WCS 2.0.1/2.1, and OGC API
Tiles requests. It does not run TEAM Engine and does not rerun the ordinary
repository unit suite.

The runner is failure-exhaustive: every protocol group executes even when an
earlier group fails. Logs are stored under
`test-results/protocol-integration`.

These tests are product regression evidence. They provide focused diagnostics,
exercise neoserver-specific declaration gating and error behavior, and cover
published requirements for which no applicable official ETS exists. In
particular, WCS 2.1 and OGC API Features Part 3/CQL2 currently rely on native
integration evidence rather than an official ETS result.

The executable profile registry is under `testing/protocol`. The production
conformance-class catalog maps each advertised class to one of those profiles,
and tests reject missing, duplicate, or unexecuted mappings. WMS and WFS are
also registered as live executions even though their capabilities do not use
the OGC API `conformsTo` model.

## Stock official TEAM Engine execution

Run all stock suites or one isolated suite:

~~~bash
make test-conformance
make test-conformance-wms13
make test-conformance-wfs20
make test-conformance-wcs20
make test-conformance-wmts10
make test-conformance-ogcapi-features10
make test-conformance-ogcapi-tiles10
~~~

The manifest in `testing/officialets/versions.lock.json` is the source of truth
for suite image digests, profile selection, and arguments. Every official suite
gets a newly initialized catalog and database. This is required for mutating WFS
transaction/locking tests and prevents one suite from contaminating another.

The WMS profile selects Basic, Queryable, and Time but not the recommendations
checklist. WMTS exercises the KVP raster profile; optional WMTS MVT remains
disabled and OGC API vector tiles are tested separately. Stock WCS execution
includes Core, XML POST, range subsetting, scaling, and CRS.

Each stock profile writes below `test-results/conformance`:

- `result.xml`: the unmodified TEAM Engine response;
- `metadata.json`: evidence kind, immutable image, commit, arguments, timing,
  upstream totals, leaf/wrapper/infrastructure summaries, and categorized skip
  causes;
- `junit.xml`: one case per leaf assertion plus infrastructure cases, with the
  skip category in each skipped case's `type` attribute;
- `container.log` when orchestration or execution fails.

TestNG totals are preserved as upstream-reported values. CTL leaf assertions,
wrapper outcomes, and infrastructure errors are classified separately using
TEAM Engine call paths (and direct nesting when present), so a leaf failure and
its propagated parent failures are not reported as multiple failed
requirements. Skips remain visible and are classified as infrastructure,
profile-version, fixture-not-applicable, conditional-protocol-branch,
unclaimed-optional-capability, or unclassified. An unclassified skip is not
silently treated as a pass. Results from different engines and profiles are
never summed into a repository-wide requirement count.

## Official-derived WCS Interpolation

Run the adapted profile separately:

~~~bash
make test-conformance-derived
make test-conformance-derived-wcs20-interpolation
~~~

The published `ets-wcs20` 1.21 image passes 18 interpolation URIs as bare XPath
`select` expressions. TEAM Engine otherwise aborts before issuing the product
request. The derived image applies the narrow
`wcs20-interpolation-uri-xpath-v1` compatibility entrypoint, which verifies the
expected 18 expressions, quotes only those URI values, and refuses to start if
the pinned source changes.

Interpolation artifacts are stored under
`test-results/conformance-derived/wcs20/interpolation`. Metadata records
`official-derived`, the upstream image digest, derived image ID, patch-set name,
and patch SHA-256. This is useful regression evidence, but it is deliberately
not classified as stock official ETS execution or certification evidence.

When the upstream image pin changes, the stock Interpolation profile must be
re-evaluated. Remove the adaptation and reclassify the profile as official only
after the unmodified published suite executes successfully.

## Truthful declarations and evidence mapping

The canonical class catalog is in `internal/conformance`. Its fields have strict
meanings:

- `IntegrationProfile`: native live regression execution;
- `OfficialProfile`: exact unmodified ETS profile;
- `DerivedProfile`: explicitly adapted upstream profile.

An advertised class always maps to a live integration execution. Official and
derived mappings are optional when OGC has no applicable published suite. A
class cannot be mapped to both stock and derived evidence.

## CI

Native protocol integration runs for every pull request and main push. A
repository-owned conservative path selector chooses affected stock suites on
pull requests; unknown or shared production paths select all suites.
Documentation-only changes select none. WCS changes also select the derived
Interpolation job.

The complete stock and derived matrices run on `main`, version tags, manual
dispatch, and nightly at 02:17 UTC. Matrix jobs are failure-independent and
always upload their available evidence.

Related: [Development](development.md) · [WCS](wcs.md) ·
[OGC API - Tiles](ogc-api-tiles.md) · [WMTS](wmts.md)
