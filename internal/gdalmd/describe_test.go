package gdalmd_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/tobilg/neoserver/internal/gdalmd"
)

func TestDescribeAndReadNetCDFFixture(t *testing.T) {
	dataset, err := gdalmd.Open(filepath.Join("..", "..", "testing", "fixtures", "raster", "runtime-grid.nc"), "netCDF")
	if err != nil {
		t.Fatal(err)
	}
	if dataset.Driver() != "netCDF" {
		t.Fatalf("driver = %q", dataset.Driver())
	}
	names, err := dataset.ArrayNames()
	if err != nil || len(names) == 0 {
		t.Fatalf("array names: %v %v", names, err)
	}
	var described *gdalmd.Array
	for _, name := range names {
		if array, err := dataset.DescribeArray(name, 1_000); err == nil && len(array.Dimensions) >= 2 {
			described = array
			break
		}
	}
	if described == nil {
		t.Fatalf("no numeric grid among %v", names)
	}
	start, count, cells := make([]uint64, len(described.Dimensions)), make([]uint64, len(described.Dimensions)), uint64(1)
	for i, dimension := range described.Dimensions {
		if dimension.Size == 0 || !dimension.CoordinatesComplete || uint64(len(dimension.Coordinates)) != dimension.Size {
			t.Fatalf("dimension %+v was not fully described", dimension)
		}
		count[i], cells = dimension.Size, cells*dimension.Size
	}
	// Above the limit only the first two and the last coordinate are read:
	// enough to derive a regular axis without reading a long coordinate array.
	limited, err := dataset.DescribeArray(described.FullName, 2)
	if err != nil {
		t.Fatal(err)
	}
	truncated := false
	for i, dimension := range limited.Dimensions {
		full := described.Dimensions[i].Coordinates
		if len(full) <= 2 {
			if !dimension.CoordinatesComplete || !slices.Equal(dimension.Coordinates, full) {
				t.Fatalf("axis %s within the limit was not read completely: %v", dimension.Name, dimension.Coordinates)
			}
			continue
		}
		truncated = true
		want := []float64{full[0], full[1], full[len(full)-1]}
		if dimension.CoordinatesComplete || !slices.Equal(dimension.Coordinates, want) {
			t.Fatalf("limited dimension %s = %v (complete %v), want %v", dimension.Name, dimension.Coordinates, dimension.CoordinatesComplete, want)
		}
	}
	if !truncated {
		t.Fatal("fixture has no axis longer than the coordinate limit")
	}
	values, err := dataset.ReadFloat64(described.FullName, start, count)
	if err != nil || uint64(len(values)) != cells {
		t.Fatalf("read %d of %d cells: %v", len(values), cells, err)
	}
	if _, err := dataset.DescribeArray("/does-not-exist", 0); err == nil {
		t.Fatal("described a missing array")
	}

	if err := dataset.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := dataset.ArrayNames(); err == nil {
		t.Fatal("a closed dataset still answered")
	}
}

func TestOpenRejectsMissingAndDisallowedDrivers(t *testing.T) {
	if _, err := gdalmd.Open(filepath.Join(t.TempDir(), "missing.nc")); err == nil {
		t.Fatal("opened a missing file")
	}
	fixture := filepath.Join("..", "..", "testing", "fixtures", "raster", "runtime-grid.nc")
	if _, err := gdalmd.Open(fixture, "GRIB"); err == nil {
		t.Fatal("opened a NetCDF file with only the GRIB driver allowed")
	}
}
