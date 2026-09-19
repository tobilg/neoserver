package rastergrid

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/airbusgeo/godal"
	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/datasource"
)

type EncodingOptions struct {
	TemporaryDirectory string
	MaxBytes           int64
}

var ErrUnsupportedNumericEncoding = errors.New("format cannot preserve numeric coverage samples")

// EncodeGrid writes a target-grid-aligned numeric result to a raw coverage
// format. It intentionally performs no portrayal or color mapping.
func EncodeGrid(grid *datasource.CoverageRenderGrid, info *datasource.CoverageInfo, format string) ([]byte, error) {
	return EncodeGridWithOptions(grid, info, format, EncodingOptions{})
}

// EncodeGridWithOptions applies temporary-storage and output ceilings while
// encoding formats whose GDAL drivers require a filesystem-backed dataset.
func EncodeGridWithOptions(grid *datasource.CoverageRenderGrid, info *datasource.CoverageInfo, format string, encoding EncodingOptions) ([]byte, error) {
	if grid == nil || info == nil || grid.Width < 1 || grid.Height < 1 || len(grid.Bands) == 0 {
		return nil, fmt.Errorf("coverage grid is empty")
	}
	format = strings.ToLower(strings.TrimSpace(format))
	var driver godal.DriverName
	var suffix string
	var options []godal.DatasetCreateOption
	switch format {
	case "image/tiff":
		driver, suffix = godal.GTiff, ".tif"
		options = []godal.DatasetCreateOption{godal.CreationOption("COMPRESS=DEFLATE", "TILED=YES")}
	case "application/netcdf":
		driver, suffix = godal.DriverName("netCDF"), ".nc"
		options = []godal.DatasetCreateOption{godal.CreationOption("FORMAT=NC4C", "COMPRESS=DEFLATE", "ZLEVEL=4")}
	case "image/jp2":
		driver, suffix = godal.DriverName("JP2OpenJPEG"), ".jp2"
		options = []godal.DatasetCreateOption{godal.CreationOption("REVERSIBLE=YES", "QUALITY=100")}
	default:
		return nil, fmt.Errorf("unsupported grid encoding %q", format)
	}
	if _, ok := godal.RasterDriver(driver); !ok {
		return nil, fmt.Errorf("GDAL driver %s is unavailable", driver)
	}
	dtype := commonDataType(grid.BandInfo)
	values, noData, err := numericSamples(grid, format == "application/netcdf")
	if err != nil {
		return nil, err
	}
	name := "/vsimem/neoserver-wcs-encode-" + uuid.NewString() + suffix
	if format == "image/jp2" {
		body, err := encodeJPEG2000(grid, info, name, driver)
		return enforceEncodingLimit(body, encoding.MaxBytes, err)
	}
	virtual := format != "application/netcdf"
	if !virtual {
		if encoding.TemporaryDirectory != "" {
			if err := os.MkdirAll(encoding.TemporaryDirectory, 0o750); err != nil {
				return nil, fmt.Errorf("create WCS temporary directory: %w", err)
			}
		}
		temporary, err := os.CreateTemp(encoding.TemporaryDirectory, "neoserver-wcs-encode-*"+suffix)
		if err != nil {
			return nil, fmt.Errorf("create temporary coverage: %w", err)
		}
		name = temporary.Name()
		if err := temporary.Close(); err != nil {
			return nil, err
		}
		if err := os.Remove(name); err != nil {
			return nil, err
		}
		defer os.Remove(name)
	} else {
		defer godal.VSIUnlink(name)
	}
	dataset, err := godal.Create(driver, name, len(grid.Bands), dtype, grid.Width, grid.Height, options...)
	if err != nil {
		return nil, fmt.Errorf("create %s coverage: %w", format, err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = dataset.Close()
		}
	}()
	if err := dataset.SetGeoTransform([6]float64{info.OriginX, info.ResolutionX, 0, info.OriginY, 0, info.ResolutionY}); err != nil {
		return nil, fmt.Errorf("set coverage geotransform: %w", err)
	}
	ref, err := godal.NewSpatialRefFromEPSG(info.SRID)
	if err != nil {
		return nil, fmt.Errorf("create coverage CRS: %w", err)
	}
	if err := dataset.SetSpatialRef(ref); err != nil {
		ref.Close()
		return nil, fmt.Errorf("set coverage CRS: %w", err)
	}
	ref.Close()
	bands := dataset.Bands()
	for index, written := range values {
		if noData != nil {
			if err := bands[index].SetNoData(*noData); err != nil {
				return nil, fmt.Errorf("set band %d no-data: %w", index+1, err)
			}
		}
		if index < len(grid.BandInfo) && grid.BandInfo[index].Name != "" {
			if err := bands[index].SetDescription(grid.BandInfo[index].Name); err != nil {
				return nil, fmt.Errorf("set band %d description: %w", index+1, err)
			}
		}
		if err := bands[index].Write(0, 0, written, grid.Width, grid.Height); err != nil {
			return nil, fmt.Errorf("write coverage band %d: %w", index+1, err)
		}
	}
	if err := dataset.Close(); err != nil {
		return nil, fmt.Errorf("finalize %s coverage: %w", format, err)
	}
	closed = true
	if !virtual {
		if encoding.MaxBytes > 0 {
			stat, err := os.Stat(name)
			if err != nil {
				return nil, err
			}
			if stat.Size() > encoding.MaxBytes {
				return nil, fmt.Errorf("encoded coverage exceeds the temporary byte limit")
			}
		}
		body, err := os.ReadFile(name)
		return enforceEncodingLimit(body, encoding.MaxBytes, err)
	}
	file, err := godal.VSIOpen(name)
	if err != nil {
		return nil, fmt.Errorf("open encoded coverage: %w", err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	return enforceEncodingLimit(body, encoding.MaxBytes, err)
}

func enforceEncodingLimit(body []byte, maxBytes int64, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("encoded coverage exceeds the temporary byte limit")
	}
	return body, nil
}

func encodeJPEG2000(grid *datasource.CoverageRenderGrid, info *datasource.CoverageInfo, name string, driver godal.DriverName) ([]byte, error) {
	defer godal.VSIUnlink(name)
	_, noData, err := numericSamples(grid, false)
	if err != nil {
		return nil, err
	}
	// This driver does not embed nodata in the returned single-file payload.
	// A PAM sidecar would be lost by the HTTP response. Fail explicitly.
	if noData != nil {
		return nil, fmt.Errorf("%w: image/jp2 cannot preserve missing-data validity; use image/tiff", ErrUnsupportedNumericEncoding)
	}
	// JPEG2000 cannot represent floating-point samples. Restrict the numeric
	// export to types whose entire range is supported without narrowing.
	switch commonDataType(grid.BandInfo) {
	case godal.Byte, godal.Int16, godal.UInt16:
	default:
		return nil, fmt.Errorf("%w: image/jp2 requires Byte, Int16 or UInt16 bands; use image/tiff", ErrUnsupportedNumericEncoding)
	}
	source, err := DatasetFromGrid(grid, info)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	encoded, err := source.Translate(name, nil, driver, godal.CreationOption("REVERSIBLE=YES", "QUALITY=100", "YCBCR420=NO"))
	if err != nil {
		return nil, fmt.Errorf("create image/jp2 coverage: %w", err)
	}
	if err := encoded.Close(); err != nil {
		return nil, err
	}
	file, err := godal.VSIOpen(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

// DatasetFromGrid creates an in-memory GDAL dataset for subsequent warp or
// translation operations. The caller owns the returned dataset.
func DatasetFromGrid(grid *datasource.CoverageRenderGrid, info *datasource.CoverageInfo) (*godal.Dataset, error) {
	if grid == nil || info == nil || grid.Width < 1 || grid.Height < 1 || len(grid.Bands) == 0 {
		return nil, fmt.Errorf("coverage grid is empty")
	}
	values, noData, err := numericSamples(grid, false)
	if err != nil {
		return nil, err
	}
	dataset, err := godal.Create(godal.Memory, "", len(grid.Bands), commonDataType(grid.BandInfo), grid.Width, grid.Height)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*godal.Dataset, error) {
		_ = dataset.Close()
		return nil, err
	}
	if err := dataset.SetGeoTransform([6]float64{info.OriginX, info.ResolutionX, 0, info.OriginY, 0, info.ResolutionY}); err != nil {
		return fail(err)
	}
	ref, err := godal.NewSpatialRefFromEPSG(info.SRID)
	if err != nil {
		return fail(err)
	}
	if err := dataset.SetSpatialRef(ref); err != nil {
		ref.Close()
		return fail(err)
	}
	ref.Close()
	for index, written := range values {
		if err := dataset.Bands()[index].Write(0, 0, written, grid.Width, grid.Height); err != nil {
			return fail(err)
		}
		if index < len(grid.BandInfo) {
			if err := dataset.Bands()[index].SetDescription(grid.BandInfo[index].Name); err != nil {
				return fail(err)
			}
		}
		if noData != nil {
			if err := dataset.Bands()[index].SetNoData(*noData); err != nil {
				return fail(err)
			}
		}
	}
	return dataset, nil
}

func commonDataType(fields []datasource.CoverageBand) godal.DataType {
	if len(fields) == 0 {
		return godal.Float64
	}
	selected := dataType(fields[0].DataType)
	for _, field := range fields[1:] {
		if dataType(field.DataType) != selected {
			return godal.Float64
		}
	}
	return selected
}

func dataType(value string) godal.DataType {
	switch strings.ToLower(value) {
	case "byte", "uint8":
		return godal.Byte
	case "int8":
		return godal.Int8
	case "uint16":
		return godal.UInt16
	case "int16":
		return godal.Int16
	case "uint32":
		return godal.UInt32
	case "int32":
		return godal.Int32
	case "float32":
		return godal.Float32
	default:
		return godal.Float64
	}
}
