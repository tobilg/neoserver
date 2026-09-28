# Security policy

Do not post credentials, private data, or working exploit details in public issues.
Use [private vulnerability reporting](https://github.com/tobilg/neoserver/security/advisories/new).
If private reporting is unavailable, open a minimal issue requesting a private contact without disclosing the vulnerability.

## Supported versions

Only the latest tagged [release](https://github.com/tobilg/neoserver/releases)
receives security fixes. neoserver is pre-1.0: fixes ship in a new release rather
than as backports, and may require upgrading and following the upgrade notes in
the [release notes](docs/release-notes.md). Older releases have no LTS guarantee.

## What to expect

Confirmed vulnerabilities are fixed in a new release and disclosed through a
[GitHub security advisory](https://github.com/tobilg/neoserver/security/advisories)
that names affected versions and any credential rotation or migration needed.

## Deploying securely

Deployment requirements: TLS outside loopback, a random catalog encryption key
stored separately from backups, least-privilege datasource credentials, explicit
file allowlists that exclude the server's own state, PostGIS host allowlisting
for workspace administrators, and private-by-default publications. See
[deployment and release guidance](docs/releasing.md) and
[SQL-view security](docs/sql-view-security.md).

## Maintainer checklist

Confirm private reporting is enabled before announcing a release.
Reproduce reports in isolated fixtures, prepare regression tests, coordinate a
patched release and advisory, and describe credential rotation/migration needs.
Run scheduled dependency scans as well as scans of the exact release image.
