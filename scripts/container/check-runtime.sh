#!/bin/sh
set -eu
test "$(dpkg --print-architecture)" = amd64
test "$(getconf GNU_LIBC_VERSION)" = "$(cat /usr/share/neoserver/builder-glibc.txt)"
while IFS= read -r binary; do
  test -f "$binary"
  output=$(ldd "$binary")
  case "$output" in *'not found'*) printf '%s\n%s\n' "$binary" "$output" >&2; exit 1;; esac
done < /usr/share/neoserver/runtime-elf.txt
test "$(find /usr/lib/x86_64-linux-gnu/gdalplugins -name '*.so' | wc -l)" -eq 3
test -z "$(find /usr/local/gdal-internal/share/proj /usr/share/proj -type f \( -iname '*.tif' -o -iname '*.gsb' -o -iname '*.gtx' -o -iname '*.byn' -o -iname '*.lla' -o -iname '*.gvb' -o -iname '*.ct2' \))"
test ! -e /usr/bin/pebble
test ! -e /usr/bin/python3
test ! -e /usr/bin/java
gdalinfo --version
ogrinfo --formats >/dev/null
projinfo EPSG:4326 >/dev/null
# PROJ's help intentionally exits 1. Require its usage text, not a loader error.
help=$(projsync --help 2>&1) || test "$?" -eq 1
case "$help" in *usage:*projsync*) ;; *) printf '%s\n' "$help" >&2; exit 1;; esac
