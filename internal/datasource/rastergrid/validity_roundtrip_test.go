package rastergrid

import (
	"errors"
	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/datasource"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIntegerRasterValidityRoundTrip(t *testing.T) {
	info := &datasource.CoverageInfo{SRID: 4326, OriginX: 7, OriginY: 51, ResolutionX: 1, ResolutionY: -1}
	for _, format := range []string{"image/tiff", "image/jp2", "application/netcdf"} {
		t.Run(format, func(t *testing.T) {
			grid := &datasource.CoverageRenderGrid{Width: 2, Height: 2, Bands: [][]float64{{0, 7, 9, 255}}, Valid: []bool{true, false, true, true}, BandInfo: []datasource.CoverageBand{{Name: "value", DataType: "byte"}}}
			body, err := EncodeGrid(grid, info, format)
			if format == "image/jp2" {
				if !errors.Is(err, ErrUnsupportedNumericEncoding) {
					t.Fatalf("missing-data JPEG2000 must be rejected: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "output"+map[string]string{"image/tiff": ".tif", "image/jp2": ".jp2", "application/netcdf": ".nc"}[format])
			if err = os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			ds, err := godal.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer ds.Close()
			band := ds.Bands()[0]
			values := make([]float64, 4)
			if err = band.Read(0, 0, values, 2, 2); err != nil {
				t.Fatal(err)
			}
			mask := make([]byte, 4)
			if err = band.MaskBand().Read(0, 0, mask, 2, 2); err != nil {
				t.Fatal(err)
			}
			nodata, has := band.NoData()
			gt, _ := ds.GeoTransform()
			if !reflect.DeepEqual(mask, []byte{255, 0, 255, 255}) || values[0] != 0 || values[2] != 9 || values[3] != 255 || !has || values[1] != nodata || gt != [6]float64{7, 1, 0, 51, 0, -1} {
				t.Fatalf("%s: values=%v mask=%v nodata=%v transform=%v", format, values, mask, nodata, gt)
			}
		})
	}
}

func TestIntegerNoDataSelectionAcrossBands(t *testing.T) {
	for _, dtype := range []string{"byte", "int16", "uint16", "int32", "uint32"} {
		t.Run(dtype, func(t *testing.T) {
			grid := &datasource.CoverageRenderGrid{Width: 2, Height: 2, Bands: [][]float64{{0, 1, 2, 3}, {4, 5, 6, 7}}, Valid: []bool{true, false, true, true}, BandInfo: []datasource.CoverageBand{{DataType: dtype, NilValues: []string{"0"}}, {DataType: dtype}}}
			values, nodata, err := numericSamples(grid, true)
			if err != nil || nodata == nil {
				t.Fatalf("prepare: %v %v", nodata, err)
			}
			for band := range grid.Bands {
				for sample, valid := range grid.Valid {
					if valid {
						if values[band][sample] != grid.Bands[band][sample] || values[band][sample] == *nodata {
							t.Fatalf("valid value lost: %v", values)
						}
					} else if values[band][sample] != *nodata {
						t.Fatal("missing mask")
					}
				}
			}
			ds, err := DatasetFromGrid(grid, &datasource.CoverageInfo{SRID: 4326, ResolutionX: 1, ResolutionY: -1})
			if err != nil {
				t.Fatal(err)
			}
			defer ds.Close()
			for _, band := range ds.Bands() {
				mask := make([]byte, 4)
				if err := band.MaskBand().Read(0, 0, mask, 2, 2); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(mask, []byte{255, 0, 255, 255}) {
					t.Fatalf("intermediate validity: %v", mask)
				}
			}
		})
	}
	// There is no safe sentinel when valid samples occupy the entire Byte
	// domain. Return a typed error rather than hiding a legitimate sample.
	all := make([]float64, 257)
	valid := make([]bool, 257)
	for i := range 256 {
		all[i] = float64(i)
		valid[i] = true
	}
	_, _, err := numericSamples(&datasource.CoverageRenderGrid{Width: 257, Height: 1, Bands: [][]float64{all}, Valid: valid, BandInfo: []datasource.CoverageBand{{DataType: "byte"}}}, false)
	if !errors.Is(err, ErrUnsupportedNumericEncoding) {
		t.Fatalf("full domain accepted: %v", err)
	}
}

func TestNoDataDoesNotCollideAfterNativeConversion(t *testing.T) {
	grid := &datasource.CoverageRenderGrid{Width: 2, Height: 1, Bands: [][]float64{{0.25, 7}}, Valid: []bool{true, false}, BandInfo: []datasource.CoverageBand{{DataType: "byte"}}}
	ds, err := DatasetFromGrid(grid, &datasource.CoverageInfo{SRID: 4326, ResolutionX: 1, ResolutionY: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	mask := make([]byte, 2)
	if err := ds.Bands()[0].MaskBand().Read(0, 0, mask, 2, 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mask, []byte{255, 0}) {
		t.Fatalf("converted valid sample hidden: %v", mask)
	}
}
