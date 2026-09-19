package rasterfile

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/gdalmd"
)

func TestETSWCS20GeoTIFFFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testing", "fixtures", "raster", "rectified-grid-coverage.tif")
	ds, err := New("fixture", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	items, err := ds.DiscoverCoverages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Info.Width != 40 || items[0].Info.Height != 30 || items[0].Info.SRID != 32611 {
		t.Fatalf("unexpected discovery: %+v", items)
	}
	window := datasource.CoverageWindow{XOff: 2, YOff: 3, Width: 5, Height: 4}
	data, err := ds.ExtractCoverage(context.Background(), "raster", window)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || string(data[:2]) != "II" && string(data[:2]) != "MM" {
		t.Fatalf("not a TIFF: %x", data[:min(8, len(data))])
	}
	raster, err := ds.ReadCoverage(context.Background(), "raster", window)
	if err != nil {
		t.Fatal(err)
	}
	if raster.Width != 5 || raster.Height != 4 || len(raster.Bands) == 0 || len(raster.Bands[0]) != 20 {
		t.Fatalf("unexpected raster: %+v", raster)
	}
	rendered, err := ds.RenderCoverage(context.Background(), "raster", datasource.CoverageRenderRequest{TargetCRS: "EPSG:32611", BBox: items[0].Info.Envelope, Width: 16, Height: 12, Bands: []int{1}, Resampling: "nearest"})
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Width != 16 || rendered.Height != 12 || len(rendered.Bands) != 1 || len(rendered.Bands[0]) != 192 {
		t.Fatalf("unexpected render grid: %+v", rendered)
	}
	valid := false
	for _, value := range rendered.Valid {
		valid = valid || value
	}
	if !valid {
		t.Fatal("expected valid pixels in rendered coverage")
	}
}

func TestRejectNonGeoTIFF(t *testing.T) {
	if _, err := New("bad", "fixture.vrt"); err == nil {
		t.Fatal("expected VRT rejection")
	}
}

func TestDiscoverAndReadNetCDFThroughMultidimensionalAPI(t *testing.T) {
	if _, ok := godal.RasterDriver(godal.DriverName("netCDF")); !ok {
		t.Skip("GDAL netCDF driver is unavailable")
	}
	path := filepath.Join(t.TempDir(), "grid.nc")
	dataset, err := godal.Create(godal.DriverName("netCDF"), path, 1, godal.Float32, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := godal.NewSpatialRefFromEPSG(4326)
	if err != nil {
		t.Fatal(err)
	}
	if err := dataset.SetSpatialRef(ref); err != nil {
		t.Fatal(err)
	}
	ref.Close()
	if err := dataset.SetGeoTransform([6]float64{10, 1, 0, 20, 0, -1}); err != nil {
		t.Fatal(err)
	}
	if err := dataset.Bands()[0].SetDescription("temperature"); err != nil {
		t.Fatal(err)
	}
	values := []float32{0, 1, 2, 3, 10, 11, 12, 13, 20, 21, 22, 23}
	if err := dataset.Bands()[0].Write(0, 0, values, 4, 3); err != nil {
		t.Fatal(err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatal(err)
	}

	source, err := New("netcdf", path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	items, err := source.DiscoverCoverages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Descriptor == nil || len(items[0].Descriptor.Axes) != 2 {
		t.Fatalf("unexpected NetCDF discovery: %+v", items)
	}
	raster, err := source.ReadCoverage(context.Background(), items[0].SourceCoverage, datasource.CoverageWindow{XOff: 1, YOff: 1, Width: 2, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	// The netCDF driver normalizes the Y indexing variable to ascending order,
	// so the second selected row contains the original northernmost values.
	if len(raster.Bands) != 1 || len(raster.Bands[0]) != 4 || raster.Bands[0][0] != 11 || raster.Bands[0][3] != 2 {
		t.Fatalf("unexpected NetCDF values: %+v", raster)
	}
}

func TestMultidimensionalNetCDFDiscoverySliceAndNativeEncoding(t *testing.T) {
	ncgen, err := exec.LookPath("ncgen")
	if err != nil {
		t.Skip("ncgen is unavailable")
	}
	directory := t.TempDir()
	cdlPath, dataPath := filepath.Join(directory, "climate.cdl"), filepath.Join(directory, "climate.nc")
	cdl := `netcdf climate {
dimensions:
  time = 2 ;
  lat = 2 ;
  lon = 3 ;
variables:
  double time(time) ;
    time:standard_name = "time" ;
    time:axis = "T" ;
    time:units = "hours since 2026-01-01 00:00:00" ;
  double lat(lat) ;
    lat:standard_name = "latitude" ;
    lat:axis = "Y" ;
    lat:units = "degrees_north" ;
  double lon(lon) ;
    lon:standard_name = "longitude" ;
    lon:axis = "X" ;
    lon:units = "degrees_east" ;
  int crs ;
    crs:grid_mapping_name = "latitude_longitude" ;
    crs:longitude_of_prime_meridian = 0. ;
    crs:semi_major_axis = 6378137. ;
    crs:inverse_flattening = 298.257223563 ;
  float temperature(time, lat, lon) ;
    temperature:standard_name = "air_temperature" ;
    temperature:long_name = "Air temperature" ;
    temperature:units = "K" ;
    temperature:grid_mapping = "crs" ;
    temperature:_FillValue = -9999.f ;
data:
  time = 0, 6 ;
  lat = 50, 51 ;
  lon = 7, 8, 9 ;
  temperature = 270, 271, 272, 273, 274, 275,
                280, 281, 282, 283, 284, 285 ;
}
`
	if err := os.WriteFile(cdlPath, []byte(cdl), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(ncgen, "-4", "-o", dataPath, cdlPath).CombinedOutput(); err != nil {
		t.Fatalf("ncgen: %v: %s", err, output)
	}

	source, err := New("climate", dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	items, err := source.DiscoverCoverages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var item *datasource.DiscoveredCoverage
	for _, candidate := range items {
		if candidate.Descriptor != nil && len(candidate.Descriptor.Axes) == 3 {
			item = candidate
			break
		}
	}
	if item == nil {
		t.Fatalf("expected a three-dimensional array, discovered %+v", items)
	}
	if item.Descriptor.Axes[0].Kind != datasource.CoverageAxisTime {
		t.Fatalf("expected temporal first axis, got %+v", item.Descriptor.Axes)
	}

	instant := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	result, err := source.ExecuteCoverage(context.Background(), item.SourceCoverage, datasource.CoverageQuery{
		Format: "image/tiff",
		DomainSubsets: []datasource.CoverageDomainSubset{{
			Axis: item.Descriptor.Axes[0].Label, Low: datasource.CoverageAxisValue{Time: &instant}, High: datasource.CoverageAxisValue{Time: &instant}, Slice: true,
		}},
		TargetGrid: &datasource.CoverageTargetGrid{CRS: item.Info.CRS, BBox: item.Info.Envelope, Width: item.Info.Width, Height: item.Info.Height, Bands: []int{1}, Resampling: "nearest"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Grid == nil || len(result.Grid.Bands) != 1 || result.Grid.Bands[0][0] < 279 {
		t.Fatalf("unexpected temporal slice: %+v", result.Grid)
	}

	native, err := source.ExecuteCoverage(context.Background(), item.SourceCoverage, datasource.CoverageQuery{Format: "application/netcdf"})
	if err != nil {
		t.Fatal(err)
	}
	defer native.Body.Close()
	prefix, err := io.ReadAll(io.LimitReader(native.Body, 8))
	if err != nil {
		t.Fatal(err)
	}
	if native.ContentType != "application/netcdf" || len(prefix) < 4 || string(prefix[:3]) != "CDF" && string(prefix[:4]) != "\x89HDF" {
		t.Fatalf("unexpected native NetCDF response: type=%q prefix=%x", native.ContentType, prefix)
	}
}

func TestDiscoverGRIB2Coverage(t *testing.T) {
	if _, ok := godal.RasterDriver(godal.DriverName("GRIB")); !ok {
		t.Skip("GDAL GRIB driver is unavailable")
	}
	directory := t.TempDir()
	tiffPath, gribPath := filepath.Join(directory, "source.tif"), filepath.Join(directory, "forecast.grib2")
	dataset, err := godal.Create(godal.GTiff, tiffPath, 1, godal.Float32, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := godal.NewSpatialRefFromEPSG(4326)
	if err != nil {
		t.Fatal(err)
	}
	if err := dataset.SetSpatialRef(ref); err != nil {
		t.Fatal(err)
	}
	ref.Close()
	if err := dataset.SetGeoTransform([6]float64{7, 1, 0, 52, 0, -1}); err != nil {
		t.Fatal(err)
	}
	if err := dataset.Bands()[0].Write(0, 0, []float32{1, 2, 3, 4, 5, 6}, 3, 2); err != nil {
		t.Fatal(err)
	}
	translated, err := dataset.Translate(gribPath, nil, godal.DriverName("GRIB"), godal.ErrLogger(func(category godal.ErrorCategory, _ int, message string) error {
		if category >= godal.CE_Failure {
			return errors.New(message)
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := translated.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatal(err)
	}

	source, err := New("forecast", gribPath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	items, err := source.DiscoverCoverages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Descriptor == nil || items[0].Info.SRID != 4326 || items[0].Info.Width != 3 || items[0].Info.Height != 2 {
		t.Fatalf("unexpected GRIB2 discovery: %+v", items)
	}
}

func TestDimensionIndexForLargeRegularAxis(t *testing.T) {
	dimension := gdalmd.Dimension{
		Name: "level", Size: 2_000_000, Coordinates: []float64{1000, 999.5, -998999.5},
	}
	index, err := dimensionIndex(dimension, "500")
	if err != nil || index != 1000 {
		t.Fatalf("index=%d err=%v", index, err)
	}
	if _, err := dimensionIndex(dimension, "500.1"); err == nil {
		t.Fatal("off-grid slice should be rejected")
	}
}
