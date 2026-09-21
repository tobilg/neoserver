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
Interpolation job; WFS changes select the derived WFS 2.0.2 job.

The complete stock and derived matrices run on `main`, version tags, manual
dispatch, and nightly at 02:17 UTC. Matrix jobs are failure-independent and
always upload their available evidence.

## Conformance dashboard

The public dashboard is hosted by GitHub Pages at
[conformance.neoserver.cloud](https://conformance.neoserver.cloud/). It includes
the six stock official suites and the separately labelled derived WCS
Interpolation and WFS 2.0.2 profiles. Native protocol integration remains a separate CI lane.

`/latest/` shows the most recent eligible main-branch conformance run, including
failures. `/releases/<tag>/` retains evidence from the successful release workflow
that published that tag. Prereleases are labelled. The recorded server image ID
identifies the image used by the suite; it does not identify the release image
built later in the release workflow.

Profile pages provide searchable test executions, outcome and skip-category
filters, build provenance, and downloadable ZIP archives. The archives retain
the original XML, metadata, and available diagnostic logs and TEAM Engine session
records. Repeated assertions remain separate executions. Missing or malformed
expected evidence appears as **Incomplete**, unselected PR suites as **Not run**,
and successful profiles containing skips as **Passed with skips**. Counts remain
per profile, with assertions, wrappers, infrastructure, and upstream totals
identified separately.

Each suite writes a short Actions summary, including on failure. The final
`conformance-dashboard` artifact contains `site/` (open `site/index.html`) and
`inputs/` (the source manifest, run context, and original evidence). These reports
work locally without a web server; filtering is optional JavaScript. Reporting
errors do not change test outcomes or become release qualification gates.

### Generate a report locally

After collecting conformance evidence:

~~~bash
make conformance-report
# Open test-results/conformance-dashboard/site/index.html

# Existing download-artifact directories are supported too:
make conformance-report CONFORMANCE_REPORT_INPUT=/path/to/downloaded-artifacts \
  CONFORMANCE_REPORT_OUTPUT=.cache/conformance-dashboard

# Re-render a downloaded bundle using its original manifest and run context:
go run ./testing/officialets/cmd/etsreport \
  --input /path/to/bundle/inputs \
  --manifest /path/to/bundle/inputs/manifest.json \
  --context /path/to/bundle/inputs/run.json \
  --output .cache/conformance-rebuilt

make test-conformance-report
# Requires the existing console dependencies and Playwright Chromium:
node --test scripts/conformance/report-browser.test.mjs
~~~

The generator marks its output directory so subsequent runs can replace its
generated files safely. It refuses to overwrite an unrelated nonempty directory.
Keep output separate from input evidence. Local evidence may describe a dirty
checkout; the profile provenance retains that original value.

### Configure GitHub Pages and Cloudflare

1. In the **tobilg account settings → Pages**, verify
   `conformance.neoserver.cloud`, unless an existing verification of
   `neoserver.cloud` already covers it. GitHub supplies the TXT record name and
   value to add in Cloudflare. Retain that TXT record after verification.
2. In **tobilg/neoserver → Settings → Pages**, choose **GitHub Actions** as the
   source, then set **Custom domain** to `conformance.neoserver.cloud`.
   Configure the repository domain before adding the routing record.
3. In the Cloudflare `neoserver.cloud` zone, add:

   | Field | Value |
   | --- | --- |
   | Type | `CNAME` |
   | Name | `conformance` |
   | Target | `tobilg.github.io` |
   | Proxy status | **DNS only** |
   | TTL | **Auto** |

4. Once GitHub validates DNS and provisions the certificate, enable **Enforce
   HTTPS** in the repository Pages settings. GitHub manages the certificate;
   routine deployments require no Cloudflare credentials or DNS changes.
5. Ensure the `github-pages` environment permits deployments from `main`, and
   repository rules permit the publishing workflow to update the generated
   `conformance-pages` branch with its `GITHUB_TOKEN`.

GitHub Actions deployments use the repository Pages domain configuration, not
a `CNAME` file. See GitHub's [custom-domain instructions](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site/managing-a-custom-domain-for-your-github-pages-site)
and [domain verification instructions](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site/verifying-your-custom-domain-for-github-pages).
DNS-only mode sends traffic directly to GitHub Pages; see
[Cloudflare proxy status](https://developers.cloudflare.com/dns/proxy-status/).

Verify DNS and HTTPS after propagation:

~~~bash
dig +short CNAME conformance.neoserver.cloud
curl -I https://conformance.neoserver.cloud/
curl -I http://conformance.neoserver.cloud/
~~~

The CNAME should resolve to `tobilg.github.io`, HTTPS should have a valid
certificate, and HTTP should redirect to HTTPS. Check `/latest/`, a retained
release report, its assets, and its evidence download after the first deployment.

### Publishing, history, and retries

The **Publish conformance dashboard** workflow runs independently after the
conformance or release workflow finishes. It executes trusted default-branch
code, downloads evidence only from the verified source run, and rebuilds HTML
from that evidence. PR runs and manual release candidates never update Pages.
Successful jobs retained from an earlier attempt of the same CI run remain valid
inputs when failed jobs are rerun.

The `conformance-pages` branch contains the deployed report tree and publication
state. Main reports replace `/latest/`; published release reports are retained.
Newer commits take precedence over reruns of older commits. Concurrent publishing
is serialized, and each eligible invocation reconciles unarchived release and
main completions since reporting was enabled. Cancelled runs leave the previous
report in place. Existing releases are not automatically backfilled.

To retry publishing or explicitly backfill an available historical run, dispatch
**Publish conformance dashboard** with its `source_run_id`. The run must be an
eligible main conformance run or a successful tag-triggered release run with an
existing published GitHub release. No tests are rerun. Release evidence must
still be available; expired or missing release artifacts produce an error.
An already retained release is immutable. Manual retries also redeploy unchanged
history after a Pages deployment failure.

Publication validates the entire staged tree before committing it. The site
budget is 900 MiB, below GitHub Pages' 1 GB limit, and individual generated files
must be below 95 MiB. Exceeding a budget fails publication and preserves release
history; it does not silently prune reports. Reporting errors appear in the
publishing workflow's logs and summary. Inspect that workflow independently of
the source test or release outcome.

Related: [Development](development.md) · [WCS](wcs.md) ·
[OGC API - Tiles](ogc-api-tiles.md) · [WMTS](wmts.md)

### Fixture coverage and reviewed skips

Official runs prepare isolated data for each suite before publishing layers.
WFS adds populated numeric and temporal properties and actual NULL samples.
OGC API Features retains all 16 feature types and adds geometries in the suite's
five fixed bbox regions, including the antimeridian and polar regions. WMTS
uses a time-and-elevation layer and opts into published tile-matrix limits.
The WMS and WCS data are unchanged by these preparations.

`testing/officialets/coverage-policy.json` records minimum assertion counts and
reviewed residual skips. Both official and official-derived runners write a
`coverage-check.json` beside their unchanged raw results, metadata and JUnit,
and fail on unreviewed skips or lost assertions. WFS point exclusions and
unclaimed joins/versioning (including dependent setup/cleanup) remain explicitly
accounted for. The Tiles fixture configures a curated workspace map and requires
the dataset-tilesets assertion to pass without a skip allowance. CTL messages are
retained in normalized results. Rendering assessment remains advisory.

### WFS 2.0.2 compatibility profile

`make test-conformance-derived-wfs20-core202` runs WFS 2.0.2 against the pinned
WFS ETS image with two corrections. The locking test reads the lock ID from
`LockFeatureResponse`. The stock test incorrectly looks for `FeatureCollection`,
throws a null-pointer exception, and leaves the acquired lock out of its cleanup
list. This defect was also reproduced in the latest published Docker image,
`1.43-teamengine-6.0.0-RC2` (digest
`sha256:101ff2737a36b1b8f7ac6e6bc2347fc4e0132b48ef66c5ab349bb122aeac1878`),
and is tracked in [upstream issue #288](https://github.com/opengeospatial/ets-wfs20/issues/288).

The destructive transaction group runs last. Its delete tests restore features
by inserting them with new IDs, but the suite retains the original sampled IDs.
Running locking tests afterward can therefore randomly select a deleted feature.
Moving the intact transaction group after all consumers of that snapshot avoids
this stale-data failure, tracked in
[upstream issue #289](https://github.com/opengeospatial/ets-wfs20/issues/289).

The `wfs202-locking-v2` patch verifies the original class and suite XML checksums,
changes the response element constant, and moves the transaction group.
Assertions, test selection, and skip accounting are preserved.
CI and the dashboard label these results **official-derived**, record the base
image and patch identity, and keep them separate from stock WFS 2.0.0 evidence.
The 2.0.2-only assertion remains an explicit version-applicability skip in the
stock 2.0.0 profile and executes in `core202`.

Both regular profiles retain ETS 1.42 for now: a local evaluation of 1.43 also
reported new temporal parsing failures in `afterPeriod` and `beforePeriod`.
Adopting that image requires resolving those failures independently; the latest
image evaluation is not represented as passing conformance evidence.
