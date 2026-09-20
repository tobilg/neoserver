APP_NAME=neoserver
.DEFAULT_GOAL := build
OGC_TEST_URL ?= http://localhost:9000/workspaces/demo/ogc
WMS_TEST_URL ?= http://localhost:9000/workspaces/demo/wms
WFS_TEST_URL ?= http://localhost:9000/workspaces/demo/wfs
WCS_TEST_URL ?= http://localhost:9000/workspaces/demo/wcs
WFS_CACHE_TEST_IMAGE ?= neoserver:candidate
RUN_ARGS ?= serve
GO_APP_LDFLAGS ?=
GO_APP_LINK_FLAGS = $(GO_APP_LDFLAGS)

.PHONY: doctor dev test-dev-tools test-focused
doctor:
	node scripts/dev.mjs doctor

dev:
	node scripts/dev.mjs dev

test-dev-tools:
	node --test scripts/dev.test.mjs scripts/set-version.test.mjs

# Example: make test-focused PKG=./internal/mgmt TEST=TestConsole
PKG ?= ./internal/mgmt
TEST ?= .
test-focused:
	go test -ldflags='$(GO_APP_LINK_FLAGS)' -count=1 -run '$(TEST)' $(PKG)

# Select the native linker from each executable's inputs (including tests).
ifeq ($(shell command -v go >/dev/null 2>&1 && go env GOOS),darwin)
GO_APP_LINK_FLAGS += -extld="$(CURDIR)/scripts/native-linker.sh"
endif

.PHONY: build ui-build-optional
build: ui-build-optional
	go build -ldflags='$(GO_APP_LINK_FLAGS)' ./cmd/$(APP_NAME)

# Release builds must include the real UI and an explicit version.
.PHONY: release-build
release-build: ui-build
	@test -n "$(VERSION)" || { echo "VERSION is required (for example v0.1.0)" >&2; exit 1; }
	go build -trimpath -ldflags='$(GO_APP_LINK_FLAGS) -X github.com/tobilg/neoserver/internal/conf.setVersion=$(VERSION) -X github.com/tobilg/neoserver/internal/conf.setCommit=$(shell git rev-parse HEAD)' ./cmd/$(APP_NAME)

# Rewrite current-release versions in code, configuration, README and docs,
# then regenerate the OpenAPI document the server produces.
.PHONY: set-version
set-version:
	@test -n "$(VERSION)" || { echo "VERSION is required (for example v0.1.0)" >&2; exit 1; }
	node scripts/set-version.mjs $(VERSION)
	$(MAKE) ui-openapi

# What CI enforces: every version literal agrees, including generated openapi.json.
.PHONY: check-version
check-version:
	node scripts/set-version.mjs --check

# Run just the backend; use ui-dev in a second terminal for the console.
.PHONY: run
run:
	go run -ldflags='$(GO_APP_LINK_FLAGS)' ./cmd/$(APP_NAME) $(RUN_ARGS)

.PHONY: ui-install ui-dev ui-build ui-test ui-e2e ui-lint ui-openapi ui-check
ui-install:
	cd web/admin && npm ci

ui-dev:
	cd web/admin && npm run dev

ui-build:
	cd web/admin && npm ci && npm run build

ui-build-optional:
	@if command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then \
		$(MAKE) ui-build; \
	else \
		echo "warning: Node.js is unavailable; building neoserver with the embedded console placeholder"; \
	fi

ui-test:
	cd web/admin && npm run test

# Brings up a disposable neoserver + PostGIS + Keycloak and runs the console
# suite against the real server. Chromium maps the local Keycloak fixture to
# loopback while retaining the real issuer hostname; no host DNS edit is needed.
ui-e2e:
	./scripts/console/run-e2e.sh

ui-lint:
	cd web/admin && npm run lint && npm run format:check

ui-openapi:
	go run -ldflags='$(GO_APP_LINK_FLAGS)' ./cmd/$(APP_NAME) openapi-dump --output web/admin/openapi.json

ui-check:
	cd web/admin && npm run check:ui-provenance && npm run test && npm run build && npm run check:budget

.PHONY: clean
clean:
	go clean
	rm -f $(APP_NAME)

# npm dependencies ship their own Go sources under web/admin/node_modules, which
# `./...` would otherwise walk into. Keep them out of the build graph.
GO_PACKAGES = $(shell go list ./... | grep -v '/web/admin/node_modules/')

# CI passes GO_TEST_FLAGS=-count=1 to defeat the test cache.
GO_TEST_FLAGS ?=

.PHONY: test
test:
	go test -p 1 -ldflags='$(GO_APP_LINK_FLAGS)' $(GO_TEST_FLAGS) $(GO_PACKAGES)

.PHONY: vet
vet:
	go vet $(GO_PACKAGES)

.PHONY: test-race
test-race:
	go test -p 1 -race -ldflags='$(GO_APP_LINK_FLAGS)' ./internal/rbac ./internal/identity ./internal/cache ./internal/wms ./internal/mgmt ./internal/workspace ./internal/tilecache ./internal/tilejobs ./internal/tiles ./internal/wfs ./internal/mosaiccatalog

.PHONY: test-s3-lease-integration
test-s3-lease-integration:
	./scripts/test-s3-lease-integration.sh

.PHONY: test-wfs-cache-container
test-wfs-cache-container:
	node scripts/test-wfs-cache-container.mjs $(WFS_CACHE_TEST_IMAGE)

.PHONY: test-recovery
test-recovery:
	go test -ldflags='$(GO_APP_LINK_FLAGS)' ./internal/server -run '^TestRestoreConsistencySetRecoversDurableState$$' -count=1 -v

.PHONY: fixture-formats
fixture-formats:
	go run -ldflags='$(GO_APP_LINK_FLAGS)' ./testing/fixtures/generate

.PHONY: docker
docker:
	docker build -t $(APP_NAME):dev .

.PHONY: up
up:
	docker compose up --build

.PHONY: down
down:
	docker compose down

# Explicit opt-in: the normal stop/start workflow must preserve source data.
.PHONY: reset-demo
reset-demo:
	@test "$(CONFIRM_DELETE_DEMO_DATA)" = "yes" || { echo "Deletes Compose serverdata and pgdata: catalog, imports, caches, and PostGIS data. Back up first; rerun with CONFIRM_DELETE_DEMO_DATA=yes." >&2; exit 1; }
	docker compose down -v

.PHONY: test-ogcapi
test-ogcapi:
	OGC_TEST_URL=$(OGC_TEST_URL) go test -ldflags='$(GO_APP_LINK_FLAGS)' -v ./testing/ogcapi/...

.PHONY: test-ogcapi-integration
test-ogcapi-integration:
	OGC_TEST_URL=$(OGC_TEST_URL) OGC_INTEGRATION_TEST=true go test -ldflags='$(GO_APP_LINK_FLAGS)' -v ./testing/ogcapi/...

.PHONY: test-wms
test-wms:
	go test -ldflags='$(GO_APP_LINK_FLAGS)' -v ./testing/wms/...

.PHONY: test-wms-integration
test-wms-integration:
	WMS_TEST_URL=$(WMS_TEST_URL) go test -ldflags='$(GO_APP_LINK_FLAGS)' -v ./testing/wms/...

.PHONY: test-wfs
test-wfs:
	go test -ldflags='$(GO_APP_LINK_FLAGS)' -v ./testing/wfs/...

.PHONY: test-wfs-integration
test-wfs-integration:
	WFS_TEST_URL=$(WFS_TEST_URL) go test -ldflags='$(GO_APP_LINK_FLAGS)' -v ./testing/wfs/...

.PHONY: test-wcs-integration
test-wcs-integration:
	WCS_TEST_URL="$(WCS_TEST_URL)" go test -ldflags='$(GO_APP_LINK_FLAGS)' -v ./testing/wcs

.PHONY: test-wcs-extensions
test-wcs-extensions:
	go test -ldflags='$(GO_APP_LINK_FLAGS)' -v ./internal/wcs ./internal/datasource/rasterfile ./internal/datasource/rastermosaic ./internal/datasource/rastergrid ./internal/mosaiccatalog

# Native live protocol regression tests against an isolated neoserver/PostGIS
# fixture. This target does not invoke TEAM Engine.
.PHONY: test-protocol-integration
test-protocol-integration:
	./scripts/conformance/run-protocol-integration.sh

# Official OGC conformance is a separate lane and executes only the published,
# digest-pinned TEAM Engine container for the named suite.
.PHONY: test-conformance test-conformance-wms13 test-conformance-wfs20 test-conformance-wcs20
.PHONY: test-conformance-wmts10 test-conformance-ogcapi-features10 test-conformance-ogcapi-tiles10
test-conformance:
	./scripts/conformance/run-official.sh all

test-conformance-wms13:
	./scripts/conformance/run-official.sh wms13

test-conformance-wfs20:
	./scripts/conformance/run-official.sh wfs20

test-conformance-wcs20:
	./scripts/conformance/run-official.sh wcs20

test-conformance-wmts10:
	./scripts/conformance/run-official.sh wmts10

test-conformance-ogcapi-features10:
	./scripts/conformance/run-official.sh ogcapi-features10

test-conformance-ogcapi-tiles10:
	./scripts/conformance/run-official.sh ogcapi-tiles10

.PHONY: test-conformance-derived test-conformance-derived-wcs20-interpolation test-conformance-derived-wfs20-core202
test-conformance-derived:
	./scripts/conformance/run-derived.sh all

test-conformance-derived-wcs20-interpolation:
	./scripts/conformance/run-derived.sh wcs20-interpolation

test-conformance-derived-wfs20-core202:
	./scripts/conformance/run-derived.sh wfs20-core202

# Render previously collected evidence without starting Docker or TEAM Engine.
CONFORMANCE_REPORT_INPUT ?= test-results
CONFORMANCE_REPORT_OUTPUT ?= test-results/conformance-dashboard
.PHONY: conformance-report test-conformance-report
conformance-report:
	go run ./testing/officialets/cmd/etsreport --input "$(CONFORMANCE_REPORT_INPUT)" --output "$(CONFORMANCE_REPORT_OUTPUT)"

test-conformance-report:
	go test -ldflags='$(GO_APP_LINK_FLAGS)' -count=1 ./testing/officialets/report ./testing/officialets/cmd/etsreport
	python3 -B -m unittest discover -s scripts/conformance -p '*_test.py'

.PHONY: test-assurance-all
test-assurance-all:
	./scripts/conformance/run-assurance-all.sh
