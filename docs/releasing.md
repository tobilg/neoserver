# Building and releasing

Build with Go 1.26.8 or newer in a supported Go release line. The previous Go
1.25.12 toolchain has reachable standard-library advisories, and Go 1.25 is now
outside upstream's two-release support window. See the
[Go release policy and fixes](https://go.dev/doc/devel/release).

Initial release artifacts are the source tag and a Linux amd64 container image.
Native builds require GDAL and a matching C/C++ runtime; they are not portable
standalone binaries. macOS arm64 is a development/test platform. Other OS/CPU
targets are not release-certified until their native and container suites run.

`make release-build VERSION=v0.1.0` requires the real console build and stamps
the native binary. Container builds accept `VERSION` and `COMMIT` build args.
`neoserver version`, console config and image labels expose release identity.

A release build takes its version from those flags, so the literals in the tree
are what an unstamped development build reports. Keep them in step with:

```sh
make set-version VERSION=v0.1.0   # rewrites the literals, then regenerates openapi.json
make check-version                # what CI enforces
```

That covers the Go fallback, both Dockerfile build args, the console package and
the release-container smoke test. It deliberately leaves documentation alone and
instead lists the prose that mentions the old version: some of it records the
version a build was actually tested at and must not be rewritten. Update the
release notes by hand.

The release-candidate workflow builds, tests and scans the exact image, exports
a CycloneDX SBOM, image archive, copyright notices, runtime evidence and SHA256
checksums.

**Pushing a `v*` tag publishes.** Once CI, security, conformance and the
stamped-image acceptance and scan have all passed for that tag, the workflow
pushes the qualified image to `docker.io/tobilg/neoserver` and creates the
GitHub release with the archive, SBOM, `SHA256SUMS` and `qualification.json`
attached. It publishes the image the gates accepted -- loaded from that
artifact, never rebuilt -- so what users pull is what was tested. A version
with a prerelease suffix (`v0.1.0-rc.1`) publishes only its exact tag and is
marked as a prerelease; `latest` moves for stable versions only. A manual
`workflow_dispatch` run qualifies an artifact for review and publishes nothing.

Because a tag publishes, do the review before tagging, not after: download and
check the candidate artifacts, review dependency licenses (including native
GDAL/DuckDB/image libraries) against
[third-party licenses and notices](../THIRD-PARTY-LICENSES.md), resolve scanner
findings and verify checksums. All CI/security/live protocol/recovery jobs must
pass, private security reporting must be enabled, and release notes must
describe migrations and known limitations. Workflow artifact retention is 30
days; the GitHub release is the durable copy.

Publishing requires `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` repository
secrets, holding a Docker Hub access token with write scope for that
repository. Without them the publish job fails at login, after the gates have
already passed, so the qualified artifacts remain available for a manual
push.

Candidate export now depends on reusable CI, security, and conformance workflows
executed for the candidate's exact SHA, including manual runs. CI includes
recovery tests; conformance selects every official suite for candidate runs.
Any failed, cancelled, or skipped prerequisite blocks export. The stamped image
also runs non-root smoke, live WFS integrity/durable-cache checks, and the live
console/OIDC suite before scanning and export. `qualification.json` records the
source SHA, version, Linux amd64 image ID, workflow evidence URL, prerequisite
results, exact-image checks, and SHA256 digests of the exported artifacts.
Review that manifest with `SHA256SUMS`; local smoke results or another SHA's CI
run are not substitute release qualifications.

Candidate builds pull fresh base images without build-layer reuse and apply
available OS package updates. The image-security gate retains `image-security.json`
as a separate 30-day artifact even when the scan fails. That failure still blocks
candidate export; retaining diagnostics is not a vulnerability exception.

The native builder uses the pinned full GDAL 3.13.3 image. The runtime uses
digest-pinned Ubuntu 26.04 with a matching glibc, installing its shared-library
package closure and copying only the non-package libraries, required data,
three plugins and four documented tools. It omits datum grids and does not
preload the upstream allocator. Both build stages check for unresolved libraries.

The build downloads signed spatial/httpfs extensions through the compiled
server's own DuckDB engine as UID 65532. It checks their engine, platform and
source revisions before packaging the binaries and notices. Extension sources
are not included in release artifacts.

The release workflow records capability parity against digest-pinned 0.1.0,
decoded format responses, offline startup and queries, all three grid methods,
and the image size budget in `runtime-evidence.tar.gz`. It verifies every
installed Ubuntu package against the scanner SBOM and exports the native-library
inventory and `image-copyright.txt`. Libraries built outside dpkg and statically
linked extension dependencies remain outside the package scanner's coverage;
see [third-party notices](../THIRD-PARTY-LICENSES.md). The image size ceiling in
`scripts/container/image-budget.json` is fixed at 10% above the initial measured
minimal image; increases require deliberate review.

Container compilation defaults to two Go compiler workers. On small or
emulated builders, `--build-arg GO_BUILD_PARALLELISM=1` lowers build concurrency
without changing runtime CPU limits. Reserve scratch space for native linking,
image export and scanner databases; do not run these alongside capacity tests.

To test an already built image in the live console fixture, use
`CONSOLE_TEST_IMAGE=neoserver:candidate CONSOLE_SKIP_BUILD=true ./scripts/console/run-e2e.sh`.
The init and server containers both use that image without rebuilding.
Set a unique `CONSOLE_PROJECT_NAME` to avoid another fixture's volumes. On
Apple Silicon, `CONSOLE_KEYCLOAK_PLATFORM=linux/arm64` runs the identity provider
natively while the candidate remains Linux amd64. Test ports bind to loopback.

Native, stock ETS and derived runners also accept
`CONFORMANCE_TEST_IMAGE=neoserver:candidate CONFORMANCE_SKIP_BUILD=true` and
`CONFORMANCE_PROJECT_NAME`. Each stock suite still gets fresh disposable state;
skipping a rebuild never skips fixture setup or protocol assertions.

## Container storage and upgrade

The image runs as UID 65532 with `/data` as its working directory. Catalog,
imports/staging, audit, mosaic index, style assets and persistent caches all use
absolute paths under `/data`. File sources are allowed only in `/data/sources/**`
and `/data/imports/**`; credential databases are not exposed as data sources.
Use a named volume, whose initial ownership is copied from the image. Bind
mounts must be provisioned by the host operator with UID/GID 65532 write access;
the image deliberately does not start as root or recursively chown host files.

Compose now uses `serverdata:/data` instead of `./data:/data`. Existing `./data`
is **not deleted or migrated automatically**. Before upgrading: stop writes,
back up the whole consistency set and encryption key separately, then either
retain the old bind mount in a Compose override with correct ownership, or copy
the complete backed-up data into the named volume and preserve UID/GID 65532.
Do not merge a live old catalog into a newly initialized one. Test restore in a
separate volume and keep the backup until the upgraded service is verified.

Each state database records its schema version: the catalog is at 25, the
persistent-cache index at 2, the mosaic index at 1 and the audit log at 1. These
are the baselines; a database at an older version is refused rather than
upgraded, and so is one newer than the binary, including by `serve` and the
administration commands. A future schema change adds an upgrade step from the
current version. Use a compatible binary or restore a consistent backup; never
edit a version table to bypass the check.

Released 0.1.0 catalogs, cache indexes and mosaics are already at these baselines.
The unversioned 0.1.0 audit schema is validated and stamped as version 1. Encrypted
DuckDB files can change storage format on write with DuckDB 1.5.5; restore the
pre-upgrade consistency set when rolling back to 0.1.0.

An assigned role cannot be deleted; inspect
`/api/v1/roles/{roleId}/deletion-plan` and remove assignments first. Successful
deletion removes policies and permanently retires the ID, so old credentials
cannot gain access through a recreated role. Restore backups only with a
current binary, review integrity, and reapply revocations made after the
backup; backups are not a revocation history.

A pending WFS write barrier bypasses cached tiles; startup recovery advances its
durable data generation before clearing it. This covers the crash window between
source commit and catalog bookkeeping.

Vector input support is now explicitly limited to GeoJSON, GeoPackage,
Shapefile, and FlatGeobuf. Review existing GDAL sources and convert unsupported
indirect/XML inputs offline before upgrading; changing the extension is not
sufficient. See [Data sources](data-sources.md).

Docker builds exclude host dependencies, local environment files, catalogs,
and generated output from the context. The UI stage installs Linux dependencies
and copies an explicit source whitelist, then regenerates API clients. Run the
container smoke lane from a dirty checkout as well as a clean checkout before
publishing a release image.

Run `make test-wfs-cache-container WFS_CACHE_TEST_IMAGE=neoserver:candidate`
with Docker and Node.js available. It creates isolated PostGIS and server
containers, verifies WFS inserts/attribute updates/deletes against real vector,
map, group, and WMTS tiles, and verifies persistent hits after each restart.
All fixture containers, data volumes, and the network are removed afterward;
it never connects to a developer's source database or catalog. This focused
cache regression is not a complete WFS conformance suite.

The same check now verifies lock enforcement beyond 10,000 rows, XML action
ordering, typed/scalar XML round trips, geometry updates, inherited input CRS,
coordinate transformations, and transaction rollback against actual PostGIS.

`make down` preserves both catalog and PostGIS volumes. Destructive demo reset
requires `make reset-demo CONFIRM_DELETE_DEMO_DATA=yes` and deletes **both**
volumes, including all imports/caches and source tables. Back up first.
Compose ports bind only to loopback; use a configured TLS proxy for public access.

## Uploads and native development

`Importer.UploadTimeoutSec` defaults to 900 seconds; the idle read deadline is
`Importer.UploadIdleTimeoutSec` (60 seconds). These apply only after upload
authorization. Normal API timeouts and upload byte limits remain in force.
Configure proxy request size and upload timeouts consistently; interrupted
multipart temporary files are removed by the parser/handler.

Use `make test`, `make test-race`, `make run` and `make build` on macOS. The
input-aware native linker selects clang when DuckDB already supplies libc++,
and clang++ when GDAL-only executables need an implicit C++ runtime. Raw
`go test`/`go run` still use Go's default linker selection unless passed
`-ldflags=-extld=$PWD/scripts/native-linker.sh`; no warnings are suppressed.
