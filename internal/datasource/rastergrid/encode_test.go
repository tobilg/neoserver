package rastergrid

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/datasource"
)

func TestEncodeGridCoverageFormats(t *testing.T) {
	grid := &datasource.CoverageRenderGrid{
		Width: 2, Height: 2,
		Bands:    [][]float64{{1, 2, 3, 4}},
		Valid:    []bool{true, true, true, false},
		BandInfo: []datasource.CoverageBand{{Band: 1, Name: "value", DataType: "int16", NilValues: []string{"-9999"}}},
	}
	info := &datasource.CoverageInfo{CRS: "EPSG:4326", SRID: 4326, Width: 2, Height: 2, OriginX: 7, OriginY: 51, ResolutionX: 1, ResolutionY: -1}
	tests := []struct {
		format, driver string
		prefixes       [][]byte
	}{
		{"image/tiff", "GTiff", [][]byte{{'I', 'I'}, {'M', 'M'}}},
		{"application/netcdf", "netCDF", [][]byte{{'C', 'D', 'F'}, {0x89, 'H', 'D', 'F'}}},
		{"image/jp2", "JP2OpenJPEG", [][]byte{{0, 0, 0, 12, 'j', 'P'}}},
	}
	for _, test := range tests {
		t.Run(test.format, func(t *testing.T) {
			if _, ok := godal.RasterDriver(godal.DriverName(test.driver)); !ok {
				t.Skip(test.driver + " driver unavailable")
			}
			body, err := EncodeGrid(grid, info, test.format)
			if test.format == "image/jp2" {
				if !errors.Is(err, ErrUnsupportedNumericEncoding) {
					t.Fatalf("masked JPEG2000 must fail explicitly: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(body) < len(test.prefixes[0]) {
				t.Fatalf("short %s response: %x", test.format, body)
			}
			matched := false
			for _, prefix := range test.prefixes {
				if len(body) < len(prefix) {
					continue
				}
				matched = true
				for index := range prefix {
					matched = matched && body[index] == prefix[index]
				}
				if matched {
					break
				}
			}
			if !matched {
				t.Fatalf("unexpected %s signature: %x", test.format, body[:len(test.prefixes[0])])
			}
		})
	}
}

func TestEncodeGridHonorsTemporaryDirectoryAndLimit(t *testing.T) {
	if _, ok := godal.RasterDriver(godal.DriverName("netCDF")); !ok {
		t.Skip("netCDF driver unavailable")
	}
	grid := &datasource.CoverageRenderGrid{
		Width: 2, Height: 2,
		Bands:    [][]float64{{1, 2, 3, 4}},
		Valid:    []bool{true, true, true, true},
		BandInfo: []datasource.CoverageBand{{Band: 1, Name: "value", DataType: "float32"}},
	}
	info := &datasource.CoverageInfo{CRS: "EPSG:4326", SRID: 4326, OriginX: 7, OriginY: 51, ResolutionX: 1, ResolutionY: -1}
	temporaryDirectory := filepath.Join(t.TempDir(), "wcs")
	_, err := EncodeGridWithOptions(grid, info, "application/netcdf", EncodingOptions{TemporaryDirectory: temporaryDirectory, MaxBytes: 1})
	if err == nil || !strings.Contains(err.Error(), "temporary byte limit") {
		t.Fatalf("expected byte-limit error, got %v", err)
	}
	entries, err := os.ReadDir(temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary encoding files were not cleaned up: %v", entries)
	}
}
