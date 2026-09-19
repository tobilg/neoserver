# Raster test fixture

`rectified-grid-coverage.tif` is a 40×30 single-band GeoTIFF in EPSG:32611. Go
tests for the raster file source, raster mosaics, the mosaic catalog and durable
recovery read it directly, so it has to exist in a fresh clone.

## Provenance

It is `responseGetCoverage_multipart_part_2.tif` from the OGC WCS 2.0 executable
test suite, taken from `src/main/resources/wcs/2.0/examples/RectifiedGridCoverage`
in [ets-wcs20](https://github.com/opengeospatial/ets-wcs20).

Copyright 2014 Open Geospatial Consortium, licensed under the Apache License,
Version 2.0. The file is redistributed unmodified.

```
sha256  b7bcd80377526f5da5727528d9ee7291c59ba4b192f90bca0fd7d9f52c4a101a
```

The tests previously read this file from a local `ets-wcs20` checkout, which is
not part of the repository, so they failed for anyone who had not fetched that
suite separately. Keep the fixture checked in rather than pointing tests back at
a local working copy.
