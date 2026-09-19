#!/bin/sh
# Go chooses a C++ external linker if any package includes C++. DuckDB's
# Darwin bindings already supply -lc++, so clang++ would add it a second time.
# Choose from actual link inputs, not the package name: GDAL-only tests still
# need the C++ driver's implicit runtime. No linker warnings are suppressed.
set -eu
for argument in "$@"; do
  case "$argument" in
    -lc++|-lstdc++|*/libc++.dylib|*/libc++.1.dylib|*/libstdc++.so*)
      exec "${NEOSRV_LINK_CC:-clang}" "$@" ;;
  esac
done
exec "${NEOSRV_LINK_CXX:-clang++}" "$@"
