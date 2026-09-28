package gdalcap

import "testing"

func TestGetReportsLinkedDriversOnce(t *testing.T) {
	first := Get()
	// GeoTIFF, GeoPackage and Shapefile are core GDAL drivers that the WMS, WCS
	// and WFS encoders rely on; a runtime without them is misbuilt.
	if !first.GeoTIFF || !first.GeoPackage || !first.Shapefile {
		t.Fatalf("core drivers missing: %+v", first)
	}
	if Get() != first {
		t.Fatal("capabilities changed between calls")
	}
}
