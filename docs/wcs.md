# Web Coverage Service 2.1

neoserver exposes published raster coverages at:

```text
/workspaces/{workspace}/wcs
```

The endpoint implements WCS 2.1 core and preserves WCS 2.0.1 compatibility. `GetCapabilities`, `DescribeCoverage`, and `GetCoverage` use the GET/KVP binding by default. Optional protocol features are advertised only after they are enabled in the workspace WCS settings, so the existing WCS 2.0 core conformance profile remains stable.

## Raster sources

- `rasterfile` publishes GeoTIFF/COG, CF-NetCDF, and GRIB2 sources. NetCDF and GRIB arrays are discovered through GDAL's multidimensional API, including numeric range metadata and regular or irregular spatial, time, elevation, and other coordinate axes. `connection_info.variables` restricts publication to named arrays and `open_options` passes explicit GDAL driver options.
- `postgis` discovers regular PostGIS raster grids through `raster_columns`. Sources require a non-zero SRID and resolution, consistent alignment/bands, and no skew.
- `raster_mosaic` combines allowlisted GeoTIFF/COG granules. Explicit or persistently harvested time/elevation values become WCS axes as well as WMS/tile dimensions. A WCS mosaic request must slice each nonspatial axis to one value.

Every local or remote source is checked by `Datasource.AllowedPaths`. HTTPS files are downloaded through the bounded safe cache. S3 paths use GDAL's `/vsis3/` adapter only after exact bucket allowlisting.

Create a service, discover its arrays, and publish one coverage:

```bash
curl -X POST http://localhost:9000/api/v1/workspaces/demo/services \
  -H 'Authorization: Bearer TOKEN' -H 'Content-Type: application/json' \
  -d '{"name":"climate","type":"rasterfile","connection_info":{"path":"./data/climate.nc","variables":["/temperature"]}}'

curl -X POST http://localhost:9000/api/v1/workspaces/demo/services/climate/discover-coverages \
  -H 'Authorization: Bearer TOKEN'

curl -X POST http://localhost:9000/api/v1/workspaces/demo/services/climate/coverages \
  -H 'Authorization: Bearer TOKEN' -H 'Content-Type: application/json' \
  -d '{"source_coverage":"/temperature","public_id":"temperature","enabled":true}'
```

For multidimensional arrays, publication automatically derives time/elevation dimensions, their source-axis labels, defaults, units, and extents. Explicit `dimensions` may override that metadata. `source_axis` distinguishes the source coordinate name from the public `time` or `elevation` dimension name.

Coverage publication also accepts `wcs20_coverage_subtype` with the exact value `RectifiedGridCoverage` or `GridCoverage`. It defaults to `RectifiedGridCoverage` for backward compatibility. The setting controls only the WCS 2.0.1 CIS 1.0 representation: it does not change the source raster, WMS or tile portrayal, or the WCS 2.1 CIS 1.1 representation. Use `GridCoverage` when the publication intentionally needs the generic grid model; otherwise retain the more specific rectified-grid default.

## Workspace capabilities

Enable only the profiles and formats intended for a workspace:

```bash
curl -X PUT http://localhost:9000/api/v1/workspaces/demo/settings/wcs \
  -H 'Authorization: Bearer TOKEN' -H 'Content-Type: application/json' \
  -d '{
    "enabled": true,
    "public": true,
    "title": "Demo WCS",
    "extensions": ["xml-post","range-subsetting","scaling","crs","interpolation","multidimensional"],
    "output_formats": ["image/tiff","application/gml+xml","multipart/related","application/netcdf","image/jp2"],
    "interpolation_methods": ["nearest-neighbor","linear"],
    "allowed_subsetting_crs": ["http://www.opengis.net/def/crs/EPSG/0/4326"],
    "allowed_output_crs": ["http://www.opengis.net/def/crs/EPSG/0/4326","http://www.opengis.net/def/crs/EPSG/0/3857"]
  }'
```

Supported extension names are:

- `xml-post`: secure `application/xml` or `text/xml` POST requests for all three operations. DTDs and XML directives are rejected, and POST is normalized through the same validation/execution path as KVP.
- `range-subsetting`: ordered field selection through `RANGESUBSET`; intervals preserve the published range-field order.
- `scaling`: `SCALEFACTOR`, `SCALEAXES`, `SCALESIZE`, or `SCALEEXTENT` (mutually exclusive).
- `crs`: allowlisted `SUBSETTINGCRS` and `OUTPUTCRS` transformation.
- `interpolation`: nearest-neighbor or linear interpolation.
- `multidimensional`: nonspatial trims and slices over published source axes.

## Requests and formats

KVP names are case-insensitive. Spatial and nonspatial subsets may be repeated:

```text
?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=temperature&FORMAT=image/tiff&SUBSET=lon(7,9)&SUBSET=lat(50,51)&SUBSET=time("2026-01-01T06:00:00Z")
```

Extension examples:

```text
&RANGESUBSET=red:blue
&SCALESIZE=lon(1024),lat(512)
&OUTPUTCRS=http://www.opengis.net/def/crs/EPSG/0/3857
&INTERPOLATION=linear
```

The core formats are `image/tiff` (default), `application/gml+xml`, and `multipart/related`. The multipart representation contains GML metadata referencing a GeoTIFF MIME part. `application/netcdf` and `image/jp2` are available when configured and when the corresponding GDAL driver is present. A retained multidimensional NetCDF subset is encoded with GDAL's multidimensional translator; GeoTIFF, JPEG 2000, GML, and multipart results require all nonspatial axes to be sliced to a two-dimensional grid.

JPEG2000 numeric exports require homogeneous Byte, Int16, or UInt16 bands. Floating-point, 32-bit integer, unknown, or mixed band types are rejected with an `InvalidParameterValue` exception for `format`; use `image/tiff` to preserve those samples. The server does not silently clamp, round, or rescale unsupported values to UInt16.

Target-grid numeric exports also preserve missing-data validity. TIFF and NetCDF select a representable nodata value that does not collide with valid samples across bands; valid zeroes and extrema remain valid. A declared integer sentinel is retained when safe. If the integer domain has no unused value, the server rejects the encoding instead of hiding valid samples; use a floating-point source for that case. JPEG2000 target-grid exports containing missing samples are rejected with a `format` exception and a recommendation to use TIFF, because the returned single-file JPEG2000 cannot preserve that validity metadata. All-valid supported integer JPEG2000 exports remain available. These rules also apply to intermediate grids used for reprojection; source files are not modified.

WCS 2.1 descriptions use CIS 1.1 `GeneralGridCoverage`, enumerating regular and irregular axes and ordered range fields. WCS 2.0.1 responses use the coverage publication's CIS 1.0 `RectifiedGridCoverage` or generic `GridCoverage` representation. Existing two-dimensional sources continue through the original extraction path unless an extension requires target-grid rendering.

## Limits and conformance

Process-wide WCS settings cap grid cells, output bytes, dimensions, retained axis values, source granules, temporary bytes, concurrency, queue time, and processing time. Workspace values inherit from and cannot exceed those ceilings. Native multidimensional encoding uses `WCS.TemporaryDirectory`; generated files are removed after each request.

WCS 2.0.1 and 2.1 are exercised by native black-box protocol integration tests.
WCS 2.1 uses that evidence because no applicable official WCS 2.1 ETS is
published. Stock official WCS 2.0.1 execution covers Core, XML POST, range
subsetting, scaling, and CRS. Interpolation uses the separately identified
official-derived compatibility profile. Run `make test-wcs-integration`,
`make test-conformance-wcs20`,
`make test-conformance-derived-wcs20-interpolation`, or
`make test-wcs-extensions` for focused source/extension tests. See
[Protocol testing and OGC conformance](conformance.md).
