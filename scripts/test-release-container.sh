#!/bin/sh
# Disposable resources only. Never uses the developer's Compose project/data.
set -eu
image=${1:-neoserver:candidate}
version=${2:-0.1.2}
fixture="neoserver-release-smoke-$$"
container="$fixture-server"
volume="$fixture-data"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker volume rm "$volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM
docker volume create "$volume" >/dev/null
docker run --rm "$image" version | grep -F "$version"
docker run --rm --entrypoint sh "$image" -c 'test "$(id -u)" = 65532 && test "$(id -g)" = 65532 && test "$PWD" = /data'
docker run --rm --entrypoint sh "$image" -ec '
  test ! -e /usr/bin/pebble
  neoserver-check-runtime
  for driver in GTiff COG netCDF GRIB JP2OpenJPEG PDF GPKG; do
    gdalinfo --format "$driver" >/dev/null
  done
  for driver in GeoJSON GPKG "ESRI Shapefile" FlatGeobuf; do
    ogrinfo --format "$driver" >/dev/null
  done
'
docker run --rm -v "$volume:/data" -e NEOSRV_STORE_KEY=abc123 \
  "$image" init --store-path /data/neoserver.db >/dev/null
docker run -d --name "$container" -p 127.0.0.1::9000 -v "$volume:/data" \
  -e NEOSRV_STORE_KEY=abc123 -e NEOSRV_SERVER_HTTPHOST=0.0.0.0 \
  -e NEOSRV_SERVER_URLBASE=http://localhost:9000 \
  -e NEOSRV_IMPORTER_ENABLED=true -e NEOSRV_AUDIT_ENABLED=true \
  -e NEOSRV_MOSAICCATALOG_ENABLED=true -e NEOSRV_PERSISTENTCACHE_ENABLED=true \
  -e NEOSRV_PERSISTENTCACHE_MAXBYTES=67108864 \
  "$image" >/dev/null
port=$(docker port "$container" 9000/tcp | sed 's/.*://')
wait_ready() {
  attempt=0
  until curl --fail --silent "http://127.0.0.1:$port/ready" >/dev/null; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then docker logs "$container"; return 1; fi
    sleep 1
  done
}
wait_ready
curl --fail --silent "http://127.0.0.1:$port/admin/" | grep -q '<script'
docker exec "$container" sh -c 'test -s /data/neoserver.db && test -s /data/audit.duckdb && test -s /data/mosaic-index.duckdb && test -d /data/imports && test -d /data/staging && touch /data/restart-marker'
docker restart "$container" >/dev/null
port=$(docker port "$container" 9000/tcp | sed 's/.*://')
wait_ready
docker exec "$container" test -f /data/restart-marker
