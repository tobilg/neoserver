package rastergrid

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/datasource"
)

func TestJPEG2000NumericFidelity(t *testing.T) {
	if _, ok := godal.RasterDriver(godal.DriverName("JP2OpenJPEG")); !ok {
		t.Skip("JP2OpenJPEG unavailable")
	}
	info := &datasource.CoverageInfo{SRID: 4326, OriginX: 7, OriginY: 51, ResolutionX: 1, ResolutionY: -1}
	for _, dtype := range []string{"float32", "float64", "int32", "uint32"} {
		t.Run(dtype, func(t *testing.T) {
			grid := &datasource.CoverageRenderGrid{Width: 2, Height: 2, Bands: [][]float64{{-12.5, .25, 70000, 42.75}}, BandInfo: []datasource.CoverageBand{{DataType: dtype}}}
			body, err := EncodeGrid(grid, info, "image/jp2")
			if err == nil || len(body) != 0 || !strings.Contains(err.Error(), "use image/tiff") {
				t.Fatalf("unsafe encoding accepted: %d bytes, %v", len(body), err)
			}
			if dtype == "float32" || dtype == "float64" {
				assertNumericRoundTrip(t, grid, info, "image/tiff")
			}
		})
	}
	for _, tc := range []struct {
		dtype  string
		values []float64
	}{
		{"byte", []float64{0, 1, 254, 255}},
		{"int16", []float64{-32768, -12, 0, 32767}},
		{"uint16", []float64{0, 1, 32768, 65535}},
	} {
		t.Run(tc.dtype, func(t *testing.T) {
			grid := &datasource.CoverageRenderGrid{Width: 2, Height: 2, Bands: [][]float64{tc.values, tc.values}, BandInfo: []datasource.CoverageBand{{DataType: tc.dtype}, {DataType: tc.dtype}}}
			assertNumericRoundTrip(t, grid, info, "image/jp2")
		})
	}
}

func assertNumericRoundTrip(t *testing.T, grid *datasource.CoverageRenderGrid, info *datasource.CoverageInfo, format string) {
	t.Helper()
	body, err := EncodeGrid(grid, info, format)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "coverage")
	if err := os.WriteFile(name, body, 0600); err != nil {
		t.Fatal(err)
	}
	ds, err := godal.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	for i, band := range ds.Bands() {
		actual := make([]float64, grid.Width*grid.Height)
		if err := band.Read(0, 0, actual, grid.Width, grid.Height); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, grid.Bands[i]) {
			t.Fatalf("%s band %d: %v != %v", format, i, actual, grid.Bands[i])
		}
	}
}
