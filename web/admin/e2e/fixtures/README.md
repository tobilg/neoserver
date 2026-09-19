# Import fixture

`two-layers.gpkg` contains two synthetic point layers, `first_source` and
`second_source`. It has no external data or licensing dependencies. The checked-in
fixture lets the live browser suite test multi-layer imports without requiring
GDAL tools on the browser runner. It targets GeoPackage 1.3, which is supported by
both the container's GDAL 3.6 and newer developer installations.

To regenerate it with GDAL, from this directory (remove only the old generated
`two-layers.gpkg` first):

```sh
ogr2ogr -f GPKG two-layers.gpkg point.geojson -dsco VERSION=1.3 -nln first_source
ogr2ogr -update two-layers.gpkg point.geojson -nln second_source
```
