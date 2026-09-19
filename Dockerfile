FROM node:24-alpine AS ui

WORKDIR /ui
# .npmrc carries legacy-peer-deps, which npm ci needs while typescript-eslint 8
# still peers on TypeScript < 6.1 and the app builds on TypeScript 7. Without
# it the install fails outright, so it must be copied before npm ci -- not
# alongside the rest of the sources.
COPY web/admin/package*.json web/admin/.npmrc ./
RUN npm ci --ignore-scripts
COPY web/admin/*.ts web/admin/*.json web/admin/index.html web/admin/eslint.config.js ./
COPY web/admin/src ./src
COPY web/admin/public ./public
COPY web/admin/scripts ./scripts
RUN npm run codegen && npm run build

# Compile against the pinned OSGeo build; the closure stage extracts only the
# application runtime. Its Ubuntu release must match the final runtime stage.
FROM ghcr.io/osgeo/gdal:ubuntu-full-3.13.3@sha256:2dd0f81ef927ff4c3d4dbe4f73c029dc86d4073974c0564f8e196f6e1412e2e0 AS native

USER root
# Use the application's allocator; do not inherit the GDAL CLI image's preload.
ENV LD_PRELOAD=""

FROM golang:1.26.8-bookworm AS go-toolchain
FROM native AS build
COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /src

# GDAL already provides matching headers/libraries. Installing the distro's
# libgdal-dev here would replace them with a different, older GDAL version.
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY config ./config
COPY --from=ui /internal/admin/dist ./internal/admin/dist

# Build with CGO enabled for DuckDB support
ARG VERSION=0.1.1
ARG COMMIT=unknown
# Bound compiler concurrency on small/emulated builders. This only affects the
# build processes, not the server's runtime GOMAXPROCS or container CPU limits.
ARG GO_BUILD_PARALLELISM=2
RUN GOMAXPROCS=${GO_BUILD_PARALLELISM} CGO_ENABLED=1 go build -p ${GO_BUILD_PARALLELISM} -trimpath -ldflags="-s -w -X github.com/tobilg/neoserver/internal/conf.setVersion=${VERSION} -X github.com/tobilg/neoserver/internal/conf.setCommit=${COMMIT}" -o /out/neoserver ./cmd/neoserver

FROM native AS closure
COPY --from=build /out/neoserver /app/neoserver
COPY scripts/container/runtime-closure.py /build/runtime-closure.py
COPY third_party/duckdb /licenses
RUN groupadd -g 65532 nonroot \
    && useradd -u 65532 -g 65532 -m -d /home/nonroot -s /bin/false nonroot
USER nonroot
# Install with this binary's DuckDB and the runtime user's actual home.
RUN /app/neoserver install-extensions > /tmp/installed-extensions.json
USER root
RUN python3 /build/runtime-closure.py

FROM ubuntu:26.04@sha256:da6fc2be547864451aa253836dd926da33623312df4a9a243e35dc877c378a78 AS runtime

ARG VERSION=0.1.1
ARG COMMIT=unknown
LABEL org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$COMMIT \
      org.opencontainers.image.source="https://github.com/tobilg/neoserver" \
      org.opencontainers.image.licenses="MIT"

RUN --mount=type=bind,from=closure,source=/closure/packages.txt,target=/tmp/packages.txt \
    apt-get update \
    && DEBIAN_FRONTEND=noninteractive xargs apt-get install -y --no-install-recommends < /tmp/packages.txt \
    && find /usr/share/proj -type f \( -iname '*.tif' -o -iname '*.gsb' -o -iname '*.gtx' -o -iname '*.byn' -o -iname '*.lla' -o -iname '*.gvb' -o -iname '*.ct2' \) -delete \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 65532 nonroot \
    && useradd -u 65532 -g 65532 -m -d /home/nonroot -s /bin/false nonroot \
    && mkdir -p /data \
    && chown 65532:65532 /data

COPY --from=closure /closure/rootfs/ /
COPY third_party/duckdb /usr/share/doc/neoserver/duckdb
COPY third_party/native /usr/share/doc/neoserver/native
COPY LICENSE THIRD-PARTY-LICENSES.md /usr/share/doc/neoserver/
COPY scripts/container/check-runtime.sh /usr/local/bin/neoserver-check-runtime
RUN chown -R 65532:65532 /home/nonroot/.duckdb \
    && rm -f /usr/bin/pebble \
    && chmod 755 /usr/local/bin/neoserver-check-runtime \
    && ldconfig

WORKDIR /data

ENV PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/gdal-internal/bin" \
    LD_PRELOAD="" \
    NEOSRV_STORE_PATH=/data/neoserver.db \
    NEOSRV_DATASOURCE_ALLOWEDPATHS=/data/sources/**,/data/imports/** \
    NEOSRV_DATASOURCE_REMOTECACHEPATH=/data/remote-cache \
    NEOSRV_IMPORTER_ROOT=/data/imports \
    NEOSRV_IMPORTER_TEMPORARYDIRECTORY=/data/staging \
    NEOSRV_WMS_STYLEASSETPATH=/data/style-assets \
    NEOSRV_WMS_EXTERNALGRAPHICCACHEPATH=/data/external-graphic-cache \
    NEOSRV_MOSAICCATALOG_DATABASEPATH=/data/mosaic-index.duckdb \
    NEOSRV_AUDIT_DATABASEPATH=/data/audit.duckdb \
    NEOSRV_PERSISTENTCACHE_DATABASEPATH=/data/tile-cache.duckdb \
    NEOSRV_PERSISTENTCACHE_FILESYSTEM_ROOT=/data/tile-cache

USER nonroot
RUN neoserver-check-runtime

EXPOSE 9000

ENTRYPOINT ["/app/neoserver"]
CMD ["serve"]
