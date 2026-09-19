# Security policy

Do not post credentials, private data, or working exploit details in public issues.
Use [private vulnerability reporting](https://github.com/tobilg/neoserver/security/advisories/new).
If private reporting is unavailable, open a minimal issue requesting a private contact without disclosing the vulnerability.

The latest tagged release is the supported security-update line. Until the first
public release, use the current main branch and treat deployments as pre-release.
Security fixes may require upgrading; older releases do not have an LTS guarantee.

Deployment requirements: TLS outside loopback, a random catalog encryption key
stored separately from backups, least-privilege datasource credentials, explicit
file allowlists, and private-by-default publications. See
[deployment and release guidance](docs/releasing.md) and
[SQL-view security](docs/sql-view-security.md).

Maintainers: confirm private reporting is enabled before announcing a release.
Reproduce reports in isolated fixtures, prepare regression tests, coordinate a
patched release and advisory, and describe credential rotation/migration needs.
Run scheduled dependency scans as well as scans of the exact release image.
