# Runtime format fixtures

These synthetic files were created for neoserver and carry the repository's
MIT license. Regenerate them, together with the raster runtime fixtures, with
`make fixture-formats` (native GDAL and DuckDB spatial required).

Each file contains one point at longitude 8, latitude 51 (EPSG:4326), with
integer `id=1` and `name=München`. The Shapefile DBF uses Windows-1252, declared
in its `.cpg`; its umlaut exercises code-page conversion. The DuckDB table is
`points`, and the geometry column in DuckDB and GeoParquet is `geom`.

`runtime-nad27.*` is a separate Shapefile with a point at (-100,30) in EPSG:4267,
used to compare default vector reprojection before and after removing system
datum grids. Its DBF is UTF-8.

The files are checked in so container acceptance needs no host GDAL installation.
