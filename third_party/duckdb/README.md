# Bundled DuckDB extensions

The container build downloads the unmodified, signed DuckDB 1.5.5 `spatial`
and `httpfs` binaries using `neoserver install-extensions`, running as the
runtime user. The final image contains those binaries in
`/home/nonroot/.duckdb/extensions`, allowing startup without a download.
No extension source archive is built or published by neoserver.

`extensions.lock.json` pins the expected engine and extension revisions and
records upstream dependency references. The build refuses different revisions;
`/usr/share/neoserver/extensions.json` records the actual versions and paths.

`NOTICES.txt` contains upstream license and attribution texts, including GEOS's
LGPL 2.1 license. Dependency licenses remain applicable independently of
neoserver's MIT license. Modification for your own use and reverse engineering
to debug those modifications are permitted.

Upstream sources and build instructions:

- [spatial eb1e57c](https://github.com/duckdb/duckdb-spatial/tree/eb1e57c9d92c0f3f76eb03eaa52c315090f328cc)
- [httpfs 827222f](https://github.com/duckdb/duckdb-httpfs/tree/827222fb45a043a7a852d1f7aae46901492a3cda)
- [DuckDB 1.5.5](https://github.com/duckdb/duckdb/tree/v1.5.5)

The spatial build recipes pin GDAL 3.8.5, PROJ 9.1.1, and GEOS 3.14.1. These
are independent of the system GDAL/PROJ libraries used for raster operations.
The build recipes and dependency source references are available upstream.
