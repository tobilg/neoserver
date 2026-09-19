package rastergrid

import (
	"fmt"
	"math"
	"strconv"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/datasource"
)

// numericSamples chooses one representable, collision-free sentinel for all
// bands (GeoTIFF has a dataset-wide nodata value). The input is never mutated.
// NetCDF requires an explicit sentinel even for all-valid data, otherwise the
// driver's implicit fill value can hide legitimate extrema.
func numericSamples(grid *datasource.CoverageRenderGrid, requireNoData bool) ([][]float64, *float64, error) {
	n := grid.Width * grid.Height
	if len(grid.Valid) != 0 && len(grid.Valid) != n {
		return nil, nil, fmt.Errorf("invalid coverage validity mask length")
	}
	valid := make([][]bool, len(grid.Bands))
	used := make(map[float64]bool)
	dtype := commonDataType(grid.BandInfo)
	minimum, maximum := integerRange(dtype)
	missing := false
	for band, samples := range grid.Bands {
		if len(samples) != n {
			return nil, nil, fmt.Errorf("coverage band %d has an invalid sample count", band+1)
		}
		valid[band] = make([]bool, n)
		var nils []float64
		if band < len(grid.BandInfo) {
			for _, raw := range grid.BandInfo[band].NilValues {
				if value, err := strconv.ParseFloat(raw, 64); err == nil {
					nils = append(nils, value)
				}
			}
		}
		for i, value := range samples {
			ok := true
			if len(grid.Valid) == n {
				ok = grid.Valid[i]
			} else {
				for _, nilValue := range nils {
					if value == nilValue || math.IsNaN(value) && math.IsNaN(nilValue) {
						ok = false
					}
				}
			}
			valid[band][i] = ok
			if ok {
				used[value] = true
				// Sentinel selection must account for native storage conversion:
				// interpolated samples can be fractional even on an integer band.
				if !math.IsNaN(minimum) {
					if math.IsNaN(value) {
						used[0] = true
					} else {
						used[math.Max(minimum, math.Min(maximum, math.Floor(value)))] = true
						used[math.Max(minimum, math.Min(maximum, math.Ceil(value)))] = true
					}
				} else if dtype == godal.Float32 {
					used[float64(float32(value))] = true
				}
			} else {
				missing = true
			}
		}
	}
	var sentinel *float64
	if missing || requireNoData {
		value := minimum
		// Retain the declared sentinel when it is representable and cannot
		// hide any valid sample, including samples in other bands.
		if len(grid.BandInfo) > 0 && len(grid.BandInfo[0].NilValues) > 0 {
			declared, err := strconv.ParseFloat(grid.BandInfo[0].NilValues[0], 64)
			if err == nil && !math.IsNaN(declared) && !math.IsInf(declared, 0) && !used[declared] {
				if !math.IsNaN(minimum) && declared >= minimum && declared <= maximum && math.Trunc(declared) == declared {
					value = declared
				}
			}
		}
		if math.IsNaN(value) {
			// A finite sentinel avoids classifying an explicitly valid NaN as missing.
			value = -math.MaxFloat32
			for used[value] {
				value = float64(math.Nextafter32(float32(value), 0))
			}
		} else {
			for used[value] && value <= maximum {
				value++
			}
			if value > maximum {
				return nil, nil, fmt.Errorf("%w: no unused integer nodata value; use a floating-point source/encoding", ErrUnsupportedNumericEncoding)
			}
		}
		sentinel = &value
	}
	output := make([][]float64, len(grid.Bands))
	for band, samples := range grid.Bands {
		output[band] = append([]float64(nil), samples...)
		if sentinel != nil {
			for i, ok := range valid[band] {
				if !ok {
					output[band][i] = *sentinel
				}
			}
		}
	}
	return output, sentinel, nil
}

func integerRange(dtype godal.DataType) (float64, float64) {
	switch dtype {
	case godal.Byte:
		return 0, 255
	case godal.Int8:
		return -128, 127
	case godal.UInt16:
		return 0, 65535
	case godal.Int16:
		return -32768, 32767
	case godal.UInt32:
		return 0, 4294967295
	case godal.Int32:
		return -2147483648, 2147483647
	default:
		return math.NaN(), math.NaN()
	}
}
