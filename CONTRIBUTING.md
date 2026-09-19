# Contributing

Install the Go toolchain from `go.mod`, a C/C++ compiler, GDAL development
libraries, and Node 22 or 24 with npm. macOS needs the Xcode command-line tools
and Homebrew GDAL. Linux CI installs `libgdal-dev`.

Start with [development.md](docs/development.md). Use `make run` and `make ui-dev`
for backend/frontend development. The catalog key is not a login credential;
initialize a disposable catalog and sign in with its issued JWT.

Before opening a PR:

```sh
make test
make test-race
go vet ./...
make ui-openapi
make ui-install
make ui-lint
make ui-check
make ui-e2e  # Docker required; creates a disposable fixture
```

Update the backend OpenAPI source, then regenerate the snapshot and clients;
do not hand-edit generated clients. Include regression tests for bugs, cover
anonymous and scoped identities for authorization changes, and document schema
migrations. Do not commit `data/`, credentials, build outputs or fixture sessions.
Report security issues privately as described in [SECURITY.md](SECURITY.md).

CI includes native Go tests, race checks, UI unit/browser/live workflow tests,
contract drift, recovery, dependency scanning and non-root container smoke tests.
Docker-dependent checks are not replaced by mocked browser tests.
