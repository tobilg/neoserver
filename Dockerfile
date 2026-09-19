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

# Keep the patched GDAL runtime and build ABI identical. The full official
# variant retains NetCDF/GRIB, PostGIS raster and export-driver support.
FROM ghcr.io/osgeo/gdal:ubuntu-full-3.13.3@sha256:2dd0f81ef927ff4c3d4dbe4f73c029dc86d4073974c0564f8e196f6e1412e2e0 AS native

USER root
# Pebble is an upstream service supervisor, not a GDAL dependency; neoserver
# runs directly as the container entrypoint and does not use that supervisor.
# Do not ship its obsolete Go runtime in the application image.
RUN test -f /usr/bin/pebble && rm /usr/bin/pebble \
    && apt-get update && apt-get upgrade -y \
    && rm -rf /var/lib/apt/lists/*
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
ARG VERSION=0.1.0
ARG COMMIT=unknown
# Bound compiler concurrency on small/emulated builders. This only affects the
# build processes, not the server's runtime GOMAXPROCS or container CPU limits.
ARG GO_BUILD_PARALLELISM=2
RUN GOMAXPROCS=${GO_BUILD_PARALLELISM} CGO_ENABLED=1 go build -p ${GO_BUILD_PARALLELISM} -trimpath -ldflags="-s -w -X github.com/tobilg/neoserver/internal/conf.setVersion=${VERSION} -X github.com/tobilg/neoserver/internal/conf.setCommit=${COMMIT}" -o /out/neoserver ./cmd/neoserver

FROM native AS runtime

ARG VERSION=0.1.0
ARG COMMIT=unknown
LABEL org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$COMMIT \
      org.opencontainers.image.source="https://github.com/tobilg/neoserver" \
      org.opencontainers.image.licenses="MIT"

RUN useradd -r -u 65532 -m -d /home/nonroot -s /bin/false nonroot \
    && mkdir -p /data \
    && chown 65532:65532 /data

WORKDIR /data
COPY --from=build /out/neoserver /app/neoserver

ENV NEOSRV_STORE_PATH=/data/neoserver.db \
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

EXPOSE 9000

ENTRYPOINT ["/app/neoserver"]
CMD ["serve"]
