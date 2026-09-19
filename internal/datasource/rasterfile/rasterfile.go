// Package rasterfile implements WCS coverage access for GeoTIFF and Cloud
// Optimized GeoTIFF files through GDAL.
package rasterfile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/airbusgeo/godal"
	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/datasource/rastergrid"
	"github.com/tobilg/neoserver/internal/gdalmd"
	"github.com/tobilg/neoserver/internal/store"
)

func init() {
	godal.RegisterAll()
	datasource.Register(store.ServiceTypeRasterFile, NewFromService)
}

type Config struct {
	Path        string            `json:"path"`
	Driver      string            `json:"driver,omitempty"`
	Variables   []string          `json:"variables,omitempty"`
	OpenOptions map[string]string `json:"open_options,omitempty"`
}

type DataSource struct {
	id        string
	path      string
	ds        *godal.Dataset
	md        *gdalmd.Dataset
	variables map[string]*mdCoverage
	mu        sync.Mutex
}

type mdCoverage struct {
	array      *gdalmd.Array
	info       *datasource.CoverageInfo
	descriptor *datasource.CoverageDescriptor
	xDimension int
	yDimension int
}

func NewFromService(svc *store.Service) (datasource.DataSource, error) {
	var cfg Config
	if err := json.Unmarshal(svc.ConnectionInfo, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal raster file config: %w", err)
	}
	resolved, err := pathpolicy.Resolve(context.Background(), cfg.Path)
	if err != nil {
		return nil, err
	}
	return newDataSource(svc.ID, resolved, cfg)
}

func New(id, path string) (*DataSource, error) {
	return newDataSource(id, path, Config{Path: path})
}

func newDataSource(id, path string, cfg Config) (*DataSource, error) {
	if cfg.Driver != "" && !strings.EqualFold(cfg.Driver, "GTiff") && !strings.EqualFold(cfg.Driver, "netCDF") && !strings.EqualFold(cfg.Driver, "GRIB") {
		return nil, fmt.Errorf("unsupported raster driver %q", cfg.Driver)
	}
	ext := strings.ToLower(filepath.Ext(path))
	openPath := path
	if strings.HasPrefix(strings.ToLower(path), "s3://") {
		openPath = "/vsis3/" + strings.TrimPrefix(path, "s3://")
	}
	if ext == ".nc" || ext == ".nc4" || ext == ".cdf" || ext == ".grib" || ext == ".grb" || ext == ".grib2" || ext == ".grb2" || strings.EqualFold(cfg.Driver, "netCDF") || strings.EqualFold(cfg.Driver, "GRIB") {
		drivers := []string{"netCDF", "GRIB"}
		if cfg.Driver != "" {
			drivers = []string{cfg.Driver}
		}
		md, err := gdalmd.OpenWithOptions(openPath, drivers, cfg.OpenOptions)
		if err != nil {
			return nil, fmt.Errorf("open multidimensional raster: %w", err)
		}
		result := &DataSource{id: id, path: path, md: md, variables: map[string]*mdCoverage{}}
		if err := result.discoverMultidimensional(cfg.Variables); err != nil {
			_ = md.Close()
			return nil, err
		}
		return result, nil
	}
	if ext != ".tif" && ext != ".tiff" {
		return nil, fmt.Errorf("raster file services accept GeoTIFF/COG, CF-NetCDF, and GRIB2 files")
	}
	ds, err := godal.Open(openPath, godal.RasterOnly(), godal.Drivers("GTiff"))
	if err != nil {
		return nil, fmt.Errorf("open GeoTIFF: %w", err)
	}
	result := &DataSource{id: id, path: path, ds: ds, variables: map[string]*mdCoverage{}}
	if _, err := result.coverageInfo(); err != nil {
		_ = ds.Close()
		return nil, err
	}
	return result, nil
}

func (ds *DataSource) Type() store.ServiceType { return store.ServiceTypeRasterFile }
func (ds *DataSource) ID() string              { return ds.id }
func (ds *DataSource) Health(context.Context) error {
	if ds.ds == nil && ds.md == nil {
		return fmt.Errorf("raster dataset is closed")
	}
	return nil
}
func (ds *DataSource) Close() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.ds == nil && ds.md == nil {
		return nil
	}
	var err error
	if ds.ds != nil {
		err = ds.ds.Close()
		ds.ds = nil
	}
	if ds.md != nil {
		if closeErr := ds.md.Close(); err == nil {
			err = closeErr
		}
		ds.md = nil
	}
	return err
}

func (ds *DataSource) DiscoverCoverages(context.Context) ([]*datasource.DiscoveredCoverage, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.md != nil {
		result := make([]*datasource.DiscoveredCoverage, 0, len(ds.variables))
		for source, variable := range ds.variables {
			result = append(result, &datasource.DiscoveredCoverage{SourceCoverage: source, Title: variable.array.Name, Description: variable.array.Attributes["long_name"], Info: *cloneInfo(variable.info), Descriptor: cloneDescriptor(variable.descriptor)})
		}
		slices.SortFunc(result, func(a, b *datasource.DiscoveredCoverage) int {
			return strings.Compare(a.SourceCoverage, b.SourceCoverage)
		})
		return result, nil
	}
	info, err := ds.coverageInfo()
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(ds.path), filepath.Ext(ds.path))
	return []*datasource.DiscoveredCoverage{{SourceCoverage: "raster", Title: name, Info: *info}}, nil
}

func (ds *DataSource) GetCoverageInfo(_ context.Context, source string) (*datasource.CoverageInfo, error) {
	if ds.md != nil {
		ds.mu.Lock()
		defer ds.mu.Unlock()
		variable := ds.variables[source]
		if variable == nil {
			return nil, fmt.Errorf("coverage %q not found", source)
		}
		return cloneInfo(variable.info), nil
	}
	if source != "raster" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.coverageInfo()
}

func (ds *DataSource) coverageInfo() (*datasource.CoverageInfo, error) {
	if ds.ds == nil {
		return nil, fmt.Errorf("raster dataset is closed")
	}
	structure := ds.ds.Structure()
	if structure.SizeX < 1 || structure.SizeY < 1 || structure.NBands < 1 {
		return nil, fmt.Errorf("raster must be a non-empty two-dimensional grid")
	}
	gt, err := ds.ds.GeoTransform()
	if err != nil {
		return nil, fmt.Errorf("read raster geotransform: %w", err)
	}
	if gt[1] == 0 || gt[5] == 0 || gt[2] != 0 || gt[4] != 0 {
		return nil, fmt.Errorf("raster must be an axis-aligned regular grid")
	}
	sr := ds.ds.SpatialRef()
	if sr == nil {
		return nil, fmt.Errorf("raster CRS is required")
	}
	authority, code := sr.AuthorityName(""), sr.AuthorityCode("")
	if authority == "" || code == "" {
		_ = sr.AutoIdentifyEPSG()
		authority, code = sr.AuthorityName(""), sr.AuthorityCode("")
	}
	if authority == "" || code == "" {
		return nil, fmt.Errorf("raster CRS authority is required")
	}
	srid, _ := strconv.Atoi(code)
	crs := "http://www.opengis.net/def/crs/" + authority + "/0/" + code
	x2 := gt[0] + float64(structure.SizeX)*gt[1]
	y2 := gt[3] + float64(structure.SizeY)*gt[5]
	minX, maxX := minmax(gt[0], x2)
	minY, maxY := minmax(gt[3], y2)
	bands := make([]datasource.CoverageBand, structure.NBands)
	for i, band := range ds.ds.Bands() {
		typeName := band.Structure().DataType.String()
		if strings.HasPrefix(typeName, "C") {
			return nil, fmt.Errorf("complex raster bands are unsupported")
		}
		name := band.Description()
		if name == "" {
			name = fmt.Sprintf("band%d", i+1)
		}
		bands[i] = datasource.CoverageBand{Band: i + 1, Name: name, DataType: typeName, ColorInterpretation: band.ColorInterp().Name()}
		if value, ok := band.NoData(); ok {
			bands[i].NilValues = []string{strconv.FormatFloat(value, 'g', -1, 64)}
		}
	}
	return &datasource.CoverageInfo{
		CRS: crs, SRID: srid, AxisLabels: [2]string{"x", "y"},
		Width: structure.SizeX, Height: structure.SizeY, OriginX: gt[0], OriginY: gt[3],
		ResolutionX: gt[1], ResolutionY: gt[5], Envelope: [4]float64{minX, minY, maxX, maxY}, Bands: bands,
	}, nil
}

func (ds *DataSource) discoverMultidimensional(allowed []string) error {
	names, err := ds.md.ArrayNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		if len(allowed) > 0 && !slices.Contains(allowed, name) {
			continue
		}
		array, err := ds.md.DescribeArray(name, 1_000_000)
		if err != nil {
			continue // Coordinate and string arrays are not publishable coverages.
		}
		xDimension, yDimension := -1, -1
		for index, dimension := range array.Dimensions {
			switch dimensionKind(dimension) {
			case datasource.CoverageAxisSpatialX:
				xDimension = index
			case datasource.CoverageAxisSpatialY:
				yDimension = index
			}
		}
		if xDimension < 0 || yDimension < 0 || xDimension == yDimension {
			continue
		}
		info, descriptor, err := multidimensionalMetadata(array, xDimension, yDimension)
		if err != nil {
			continue
		}
		ds.variables[array.FullName] = &mdCoverage{array: array, info: info, descriptor: descriptor, xDimension: xDimension, yDimension: yDimension}
	}
	if len(ds.variables) == 0 {
		return fmt.Errorf("NetCDF/GRIB dataset contains no publishable numeric array with spatial X and Y dimensions")
	}
	return nil
}

func multidimensionalMetadata(array *gdalmd.Array, xDimension, yDimension int) (*datasource.CoverageInfo, *datasource.CoverageDescriptor, error) {
	x := array.Dimensions[xDimension]
	y := array.Dimensions[yDimension]
	xOrigin, xResolution, xRegular, err := regularDimension(x)
	if err != nil {
		return nil, nil, err
	}
	yOrigin, yResolution, yRegular, err := regularDimension(y)
	if err != nil {
		return nil, nil, err
	}
	if !xRegular || !yRegular {
		return nil, nil, fmt.Errorf("spatial dimensions must be regular")
	}
	crsValue := array.CRS
	if crsValue == "" && longitudeDimension(x) && latitudeDimension(y) {
		crsValue = "http://www.opengis.net/def/crs/EPSG/0/4326"
	}
	if crsValue == "" {
		return nil, nil, fmt.Errorf("multidimensional array has no resolvable CRS")
	}
	srid := 0
	parts := strings.Split(strings.TrimRight(crsValue, "/"), "/")
	if len(parts) > 0 {
		srid, _ = strconv.Atoi(parts[len(parts)-1])
	}
	if srid == 0 {
		return nil, nil, fmt.Errorf("multidimensional array CRS has no EPSG code")
	}
	xOuter1, xOuter2 := xOrigin-xResolution/2, xOrigin+(float64(x.Size)-.5)*xResolution
	yOuter1, yOuter2 := yOrigin-yResolution/2, yOrigin+(float64(y.Size)-.5)*yResolution
	band := datasource.CoverageBand{Band: 1, Name: array.Name, Description: array.Attributes["long_name"], DataType: array.DataType, Definition: array.Attributes["standard_name"], UOM: array.Unit}
	if band.UOM == "" {
		band.UOM = array.Attributes["units"]
	}
	if array.NoData != nil {
		band.NilValues = []string{strconv.FormatFloat(*array.NoData, 'g', -1, 64)}
	}
	info := &datasource.CoverageInfo{
		CRS: crsValue, SRID: srid, AxisLabels: [2]string{x.Name, y.Name}, Width: int(x.Size), Height: int(y.Size),
		OriginX: xOuter1, OriginY: yOuter1, ResolutionX: xResolution, ResolutionY: yResolution,
		Envelope: [4]float64{math.Min(xOuter1, xOuter2), math.Min(yOuter1, yOuter2), math.Max(xOuter1, xOuter2), math.Max(yOuter1, yOuter2)},
		Bands:    []datasource.CoverageBand{band},
	}
	descriptor := &datasource.CoverageDescriptor{ID: array.FullName, Title: array.Attributes["long_name"], NativeCRS: crsValue, Formats: []string{"image/tiff", "application/gml+xml", "multipart/related", "application/netcdf"}}
	for _, dimension := range array.Dimensions {
		axis, err := coverageAxis(dimension, crsValue)
		if err != nil {
			return nil, nil, err
		}
		descriptor.Axes = append(descriptor.Axes, axis)
	}
	field := datasource.CoverageRangeField{Name: band.Name, SourceIndex: 1, DataType: band.DataType, Definition: band.Definition, Unit: band.UOM, NilValues: append([]string(nil), band.NilValues...)}
	if array.Scale != nil {
		field.Scale = *array.Scale
	}
	if array.Offset != nil {
		field.Offset = *array.Offset
	}
	descriptor.RangeFields = []datasource.CoverageRangeField{field}
	return info, descriptor, nil
}

func dimensionKind(dimension gdalmd.Dimension) datasource.CoverageAxisKind {
	typeName := strings.ToUpper(dimension.Type)
	direction := strings.ToUpper(dimension.Direction)
	name := strings.ToLower(dimension.Name)
	switch {
	case strings.Contains(typeName, "HORIZONTAL_X"), direction == "EAST", direction == "WEST", name == "x", name == "lon", name == "longitude":
		return datasource.CoverageAxisSpatialX
	case strings.Contains(typeName, "HORIZONTAL_Y"), direction == "NORTH", direction == "SOUTH", name == "y", name == "lat", name == "latitude":
		return datasource.CoverageAxisSpatialY
	case strings.Contains(typeName, "TEMPORAL"), name == "time", name == "forecast_reference_time", name == "valid_time":
		return datasource.CoverageAxisTime
	case strings.Contains(typeName, "VERTICAL"), direction == "UP", direction == "DOWN", name == "elevation", name == "height", name == "depth", name == "level", name == "isobaric":
		return datasource.CoverageAxisElevation
	default:
		return datasource.CoverageAxisOther
	}
}

func longitudeDimension(dimension gdalmd.Dimension) bool {
	name := strings.ToLower(dimension.Name)
	return name == "x" || name == "lon" || name == "longitude" || strings.EqualFold(dimension.Direction, "EAST")
}

func latitudeDimension(dimension gdalmd.Dimension) bool {
	name := strings.ToLower(dimension.Name)
	return name == "y" || name == "lat" || name == "latitude" || strings.EqualFold(dimension.Direction, "NORTH")
}

func regularDimension(dimension gdalmd.Dimension) (float64, float64, bool, error) {
	if dimension.Size < 1 || len(dimension.Coordinates) == 0 {
		return 0, 0, false, fmt.Errorf("dimension %s has no numeric indexing variable", dimension.Name)
	}
	if dimension.Size == 1 {
		return dimension.Coordinates[0], 1, true, nil
	}
	resolution := dimension.Coordinates[1] - dimension.Coordinates[0]
	if resolution == 0 {
		return 0, 0, false, fmt.Errorf("dimension %s has duplicate coordinates", dimension.Name)
	}
	if len(dimension.Coordinates) == int(dimension.Size) {
		for index, coordinate := range dimension.Coordinates[2:] {
			expected := dimension.Coordinates[0] + float64(index+2)*resolution
			if math.Abs(coordinate-expected) > math.Max(math.Abs(resolution)*1e-9, 1e-12) {
				return dimension.Coordinates[0], resolution, false, nil
			}
		}
	} else if len(dimension.Coordinates) >= 3 {
		expected := dimension.Coordinates[0] + float64(dimension.Size-1)*resolution
		if math.Abs(dimension.Coordinates[len(dimension.Coordinates)-1]-expected) > math.Max(math.Abs(resolution)*1e-9, 1e-12) {
			return dimension.Coordinates[0], resolution, false, nil
		}
	}
	return dimension.Coordinates[0], resolution, true, nil
}

func coverageAxis(dimension gdalmd.Dimension, crsValue string) (datasource.CoverageAxis, error) {
	kind := dimensionKind(dimension)
	axis := datasource.CoverageAxis{Label: dimension.Name, Kind: kind, Unit: dimension.Unit, GridHigh: int64(dimension.Size - 1)}
	if kind == datasource.CoverageAxisSpatialX || kind == datasource.CoverageAxisSpatialY {
		axis.CRS = crsValue
	}
	if origin, resolution, regular, _ := regularDimension(dimension); regular {
		axis.Regular = true
		axis.Origin = numericAxisValue(origin, dimension.Unit, kind)
		value := resolution
		axis.Resolution = datasource.CoverageAxisValue{Number: &value}
		return axis, nil
	}
	if !dimension.CoordinatesComplete {
		return datasource.CoverageAxis{}, fmt.Errorf("irregular dimension %s exceeds the coordinate discovery limit", dimension.Name)
	}
	for _, coordinate := range dimension.Coordinates {
		axis.Coordinates = append(axis.Coordinates, numericAxisValue(coordinate, dimension.Unit, kind))
	}
	return axis, nil
}

func numericAxisValue(value float64, unit string, kind datasource.CoverageAxisKind) datasource.CoverageAxisValue {
	if kind == datasource.CoverageAxisTime {
		if instant, ok := cfTime(value, unit); ok {
			return datasource.CoverageAxisValue{Time: &instant}
		}
	}
	return datasource.CoverageAxisValue{Number: &value}
}

func cfTime(value float64, unit string) (time.Time, bool) {
	parts := strings.SplitN(strings.TrimSpace(unit), " since ", 2)
	if len(parts) != 2 {
		return time.Time{}, false
	}
	baseText := strings.TrimSpace(parts[1])
	base, err := time.Parse(time.RFC3339Nano, baseText)
	if err != nil {
		base, err = time.Parse("2006-01-02 15:04:05", baseText)
	}
	if err != nil {
		base, err = time.Parse("2006-01-02", baseText)
	}
	if err != nil {
		return time.Time{}, false
	}
	var duration time.Duration
	switch strings.ToLower(strings.TrimSpace(parts[0])) {
	case "second", "seconds", "sec", "s":
		duration = time.Second
	case "minute", "minutes", "min":
		duration = time.Minute
	case "hour", "hours", "h":
		duration = time.Hour
	case "day", "days", "d":
		duration = 24 * time.Hour
	default:
		return time.Time{}, false
	}
	return base.Add(time.Duration(value * float64(duration))), true
}

func (ds *DataSource) ExtractCoverage(_ context.Context, source string, window datasource.CoverageWindow) ([]byte, error) {
	if ds.md != nil {
		grid, info, err := ds.readMultidimensionalGrid(source, window, "", "")
		if err != nil {
			return nil, err
		}
		return rastergrid.EncodeGrid(grid, info, "image/tiff")
	}
	if source != "raster" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	info, err := ds.coverageInfo()
	if err != nil {
		return nil, err
	}
	if window.XOff < 0 || window.YOff < 0 || window.Width < 1 || window.Height < 1 ||
		window.XOff+window.Width > info.Width || window.YOff+window.Height > info.Height {
		return nil, fmt.Errorf("coverage window is outside the grid")
	}
	name := "/vsimem/neoserver-wcs-" + uuid.NewString() + ".tif"
	defer godal.VSIUnlink(name)
	out, err := ds.ds.Translate(name, []string{"-srcwin", strconv.Itoa(window.XOff), strconv.Itoa(window.YOff), strconv.Itoa(window.Width), strconv.Itoa(window.Height)}, godal.GTiff, godal.CreationOption("COMPRESS=DEFLATE"))
	if err != nil {
		return nil, fmt.Errorf("extract GeoTIFF window: %w", err)
	}
	if err := out.Close(); err != nil {
		return nil, fmt.Errorf("finalize GeoTIFF: %w", err)
	}
	file, err := godal.VSIOpen(name)
	if err != nil {
		return nil, fmt.Errorf("open generated GeoTIFF: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read generated GeoTIFF: %w", err)
	}
	return data, nil
}

func (ds *DataSource) ReadCoverage(_ context.Context, source string, window datasource.CoverageWindow) (*datasource.CoverageRaster, error) {
	if ds.md != nil {
		grid, _, err := ds.readMultidimensionalGrid(source, window, "", "")
		if err != nil {
			return nil, err
		}
		return &datasource.CoverageRaster{Width: grid.Width, Height: grid.Height, Bands: grid.Bands}, nil
	}
	if source != "raster" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	info, err := ds.coverageInfo()
	if err != nil {
		return nil, err
	}
	if window.XOff < 0 || window.YOff < 0 || window.Width < 1 || window.Height < 1 || window.XOff+window.Width > info.Width || window.YOff+window.Height > info.Height {
		return nil, fmt.Errorf("coverage window is outside the grid")
	}
	result := &datasource.CoverageRaster{Width: window.Width, Height: window.Height, Bands: make([][]float64, len(info.Bands))}
	for i, band := range ds.ds.Bands() {
		values := make([]float64, window.Width*window.Height)
		if err := band.Read(window.XOff, window.YOff, values, window.Width, window.Height, godal.Window(window.Width, window.Height)); err != nil {
			return nil, fmt.Errorf("read raster band %d: %w", i+1, err)
		}
		result.Bands[i] = values
	}
	return result, nil
}

func (ds *DataSource) RenderCoverage(_ context.Context, source string, request datasource.CoverageRenderRequest) (*datasource.CoverageRenderGrid, error) {
	if ds.md != nil {
		ds.mu.Lock()
		variable := ds.variables[source]
		ds.mu.Unlock()
		if variable == nil {
			return nil, fmt.Errorf("coverage %q not found", source)
		}
		window, ok, err := rastergrid.NativeWindow(variable.info, request)
		if err != nil {
			return nil, err
		}
		if !ok {
			return rastergrid.EmptyGrid(request, []int{1}, variable.info.Bands), nil
		}
		grid, sourceInfo, err := ds.readMultidimensionalGrid(source, window, request.Time, request.Elevation)
		if err != nil {
			return nil, err
		}
		dataset, err := rastergrid.DatasetFromGrid(grid, sourceInfo)
		if err != nil {
			return nil, err
		}
		defer dataset.Close()
		return rastergrid.WarpDataset(dataset, request, sourceInfo.Bands)
	}
	if source != "raster" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	info, err := ds.coverageInfo()
	if err != nil {
		return nil, err
	}
	return rastergrid.WarpDataset(ds.ds, request, info.Bands)
}

func (ds *DataSource) DescribeCoverage(_ context.Context, source string) (*datasource.CoverageDescriptor, error) {
	if ds.md == nil {
		info, err := ds.GetCoverageInfo(context.Background(), source)
		if err != nil {
			return nil, err
		}
		return datasource.DescriptorFromInfo(source, info), nil
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	variable := ds.variables[source]
	if variable == nil {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	return cloneDescriptor(variable.descriptor), nil
}

func (ds *DataSource) ExecuteCoverage(ctx context.Context, source string, query datasource.CoverageQuery) (*datasource.CoverageResult, error) {
	if ds.md == nil {
		if query.TargetGrid == nil {
			return nil, fmt.Errorf("coverage target grid is required")
		}
		grid, err := ds.RenderCoverage(ctx, source, datasource.CoverageRenderRequest{
			TargetCRS: query.TargetGrid.CRS, BBox: query.TargetGrid.BBox, Width: query.TargetGrid.Width, Height: query.TargetGrid.Height,
			Bands: query.TargetGrid.Bands, Resampling: query.TargetGrid.Resampling,
		})
		if err != nil {
			return nil, err
		}
		descriptor, _ := ds.DescribeCoverage(ctx, source)
		return &datasource.CoverageResult{Descriptor: descriptor, Grid: grid}, nil
	}
	ds.mu.Lock()
	variable := ds.variables[source]
	ds.mu.Unlock()
	if variable == nil {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	retained := retainedNonSpatialAxes(variable.descriptor, query.DomainSubsets)
	if query.Format == "application/netcdf" && retained {
		if query.OutputCRS != "" && !sameEPSG(query.OutputCRS, variable.info.CRS) {
			return nil, fmt.Errorf("output CRS reprojection requires all nonspatial axes to be sliced")
		}
		if query.Scaling.Mode != datasource.CoverageScaleNone {
			return nil, fmt.Errorf("scaling a retained multidimensional axis is unsupported; slice nonspatial axes first")
		}
		temporaryDirectory := query.TemporaryDirectory
		if temporaryDirectory != "" {
			if err := os.MkdirAll(temporaryDirectory, 0o700); err != nil {
				return nil, fmt.Errorf("create WCS temporary directory: %w", err)
			}
		}
		temporary, err := os.CreateTemp(temporaryDirectory, "neoserver-wcs-md-*.nc")
		if err != nil {
			return nil, fmt.Errorf("create multidimensional output: %w", err)
		}
		path := temporary.Name()
		if err := temporary.Close(); err != nil {
			return nil, err
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		defer os.Remove(path)
		if err := ds.md.Translate(path, "netCDF", source, querySubsetStrings(query.DomainSubsets, ""), "", nil); err != nil {
			return nil, err
		}
		if query.MaxTemporaryBytes > 0 {
			if stat, statErr := os.Stat(path); statErr != nil {
				return nil, statErr
			} else if stat.Size() > query.MaxTemporaryBytes {
				return nil, fmt.Errorf("multidimensional output exceeds the temporary-byte limit")
			}
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &datasource.CoverageResult{Descriptor: cloneDescriptor(variable.descriptor), Body: io.NopCloser(bytes.NewReader(body)), ContentType: "application/netcdf", ContentLength: int64(len(body))}, nil
	}
	if retained {
		return nil, fmt.Errorf("format %s cannot represent retained multidimensional axes", query.Format)
	}
	if query.TargetGrid == nil {
		return nil, fmt.Errorf("coverage target grid is required")
	}
	timeValue, elevationValue := queryDimensionSelections(query.DomainSubsets, variable.descriptor)
	grid, err := ds.RenderCoverage(ctx, source, datasource.CoverageRenderRequest{
		TargetCRS: query.TargetGrid.CRS, BBox: query.TargetGrid.BBox, Width: query.TargetGrid.Width, Height: query.TargetGrid.Height,
		Bands: []int{1}, Resampling: query.TargetGrid.Resampling, Time: timeValue, Elevation: elevationValue,
	})
	if err != nil {
		return nil, err
	}
	return &datasource.CoverageResult{Descriptor: cloneDescriptor(variable.descriptor), Grid: grid}, nil
}

func (ds *DataSource) readMultidimensionalGrid(source string, window datasource.CoverageWindow, timeValue, elevationValue string) (*datasource.CoverageRenderGrid, *datasource.CoverageInfo, error) {
	ds.mu.Lock()
	variable := ds.variables[source]
	ds.mu.Unlock()
	if variable == nil {
		return nil, nil, fmt.Errorf("coverage %q not found", source)
	}
	if window.XOff < 0 || window.YOff < 0 || window.Width < 1 || window.Height < 1 || window.XOff+window.Width > variable.info.Width || window.YOff+window.Height > variable.info.Height {
		return nil, nil, fmt.Errorf("coverage window is outside the grid")
	}
	start := make([]uint64, len(variable.array.Dimensions))
	count := make([]uint64, len(variable.array.Dimensions))
	for index := range count {
		count[index] = 1
		dimension := variable.array.Dimensions[index]
		switch dimensionKind(dimension) {
		case datasource.CoverageAxisSpatialX:
			start[index], count[index] = uint64(window.XOff), uint64(window.Width)
		case datasource.CoverageAxisSpatialY:
			start[index], count[index] = uint64(window.YOff), uint64(window.Height)
		case datasource.CoverageAxisTime:
			selected, err := dimensionIndex(dimension, timeValue)
			if err != nil {
				return nil, nil, err
			}
			start[index] = selected
		case datasource.CoverageAxisElevation:
			selected, err := dimensionIndex(dimension, elevationValue)
			if err != nil {
				return nil, nil, err
			}
			start[index] = selected
		}
	}
	values, err := ds.md.ReadFloat64(source, start, count)
	if err != nil {
		return nil, nil, err
	}
	rowMajor := make([]float64, window.Width*window.Height)
	strides := make([]int, len(count))
	stride := 1
	for index := len(count) - 1; index >= 0; index-- {
		strides[index] = stride
		stride *= int(count[index])
	}
	for y := 0; y < window.Height; y++ {
		for x := 0; x < window.Width; x++ {
			sourceIndex := x*strides[variable.xDimension] + y*strides[variable.yDimension]
			rowMajor[y*window.Width+x] = values[sourceIndex]
		}
	}
	valid := make([]bool, len(rowMajor))
	for index, value := range rowMajor {
		valid[index] = !math.IsNaN(value) && (variable.array.NoData == nil || value != *variable.array.NoData)
	}
	info := cloneInfo(variable.info)
	info.OriginX += float64(window.XOff) * info.ResolutionX
	info.OriginY += float64(window.YOff) * info.ResolutionY
	info.Width, info.Height = window.Width, window.Height
	info.Envelope = [4]float64{
		math.Min(info.OriginX, info.OriginX+float64(info.Width)*info.ResolutionX),
		math.Min(info.OriginY, info.OriginY+float64(info.Height)*info.ResolutionY),
		math.Max(info.OriginX, info.OriginX+float64(info.Width)*info.ResolutionX),
		math.Max(info.OriginY, info.OriginY+float64(info.Height)*info.ResolutionY),
	}
	grid := &datasource.CoverageRenderGrid{Width: window.Width, Height: window.Height, BandNumbers: []int{1}, Bands: [][]float64{rowMajor}, Valid: valid, BandInfo: append([]datasource.CoverageBand(nil), info.Bands...)}
	return grid, info, nil
}

func dimensionIndex(dimension gdalmd.Dimension, selector string) (uint64, error) {
	if dimension.Size == 0 {
		return 0, fmt.Errorf("dimension %s is empty", dimension.Name)
	}
	if selector == "" {
		return 0, nil
	}
	parts := strings.Split(selector, "/")
	low, high := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		high = strings.TrimSpace(parts[1])
	}
	if dimensionKind(dimension) == datasource.CoverageAxisTime {
		lowTime, lowErr := parseTimeCoordinate(low)
		highTime, highErr := parseTimeCoordinate(high)
		if lowErr != nil || highErr != nil {
			return 0, fmt.Errorf("invalid time subset")
		}
		if origin, resolution, regular, _ := regularDimension(dimension); regular && !dimension.CoordinatesComplete {
			originTime, originOK := cfTime(origin, dimension.Unit)
			nextTime, nextOK := cfTime(origin+resolution, dimension.Unit)
			if originOK && nextOK {
				return regularCoordinateIndex(0, nextTime.Sub(originTime).Seconds(), dimension.Size,
					lowTime.Sub(originTime).Seconds(), highTime.Sub(originTime).Seconds())
			}
		}
		for index, coordinate := range dimension.Coordinates {
			if instant, ok := cfTime(coordinate, dimension.Unit); ok && !instant.Before(lowTime) && !instant.After(highTime) {
				return uint64(index), nil
			}
		}
		return 0, fmt.Errorf("time subset is outside the coverage domain")
	}
	lowNumber, lowErr := strconv.ParseFloat(low, 64)
	highNumber, highErr := strconv.ParseFloat(high, 64)
	if lowErr != nil || highErr != nil {
		return 0, fmt.Errorf("invalid numeric dimension subset")
	}
	if origin, resolution, regular, _ := regularDimension(dimension); regular && !dimension.CoordinatesComplete {
		return regularCoordinateIndex(origin, resolution, dimension.Size, lowNumber, highNumber)
	}
	for index, coordinate := range dimension.Coordinates {
		if coordinate >= lowNumber && coordinate <= highNumber {
			return uint64(index), nil
		}
	}
	return 0, fmt.Errorf("dimension subset is outside the coverage domain")
}

func regularCoordinateIndex(origin, resolution float64, size uint64, low, high float64) (uint64, error) {
	if size == 0 || resolution == 0 || low > high {
		return 0, fmt.Errorf("dimension subset is outside the coverage domain")
	}
	var candidate float64
	if resolution > 0 {
		candidate = math.Ceil((low-origin)/resolution - 1e-12)
	} else {
		candidate = math.Ceil((origin-high)/-resolution - 1e-12)
	}
	if candidate < 0 {
		candidate = 0
	}
	if candidate >= float64(size) {
		return 0, fmt.Errorf("dimension subset is outside the coverage domain")
	}
	index := uint64(candidate)
	coordinate := origin + float64(index)*resolution
	tolerance := math.Max(math.Abs(resolution)*1e-10, 1e-12)
	if coordinate < low-tolerance || coordinate > high+tolerance {
		return 0, fmt.Errorf("dimension subset is outside the coverage domain")
	}
	return index, nil
}

func parseTimeCoordinate(value string) (time.Time, error) {
	if result, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return result, nil
	}
	return time.Parse("2006-01-02", value)
}

func retainedNonSpatialAxes(descriptor *datasource.CoverageDescriptor, subsets []datasource.CoverageDomainSubset) bool {
	for _, axis := range descriptor.Axes {
		if axis.Kind == datasource.CoverageAxisSpatialX || axis.Kind == datasource.CoverageAxisSpatialY {
			continue
		}
		foundSlice := false
		for _, subset := range subsets {
			if strings.EqualFold(axis.Label, subset.Axis) && subset.Slice {
				foundSlice = true
				break
			}
		}
		if !foundSlice {
			return true
		}
	}
	return false
}

func queryDimensionSelections(subsets []datasource.CoverageDomainSubset, descriptor *datasource.CoverageDescriptor) (string, string) {
	var timeValue, elevationValue string
	for _, subset := range subsets {
		var kind datasource.CoverageAxisKind
		for _, axis := range descriptor.Axes {
			if strings.EqualFold(axis.Label, subset.Axis) {
				kind = axis.Kind
				break
			}
		}
		value := axisValueText(subset.Low)
		if !subset.Slice {
			value += "/" + axisValueText(subset.High)
		}
		if kind == datasource.CoverageAxisTime {
			timeValue = value
		} else if kind == datasource.CoverageAxisElevation {
			elevationValue = value
		}
	}
	return timeValue, elevationValue
}

func querySubsetStrings(subsets []datasource.CoverageDomainSubset, skipAxis string) []string {
	result := make([]string, 0, len(subsets))
	for _, subset := range subsets {
		if strings.EqualFold(subset.Axis, skipAxis) {
			continue
		}
		low, high := axisValueText(subset.Low), axisValueText(subset.High)
		if subset.Low.Time != nil {
			low = `"` + low + `"`
		}
		if subset.High.Time != nil {
			high = `"` + high + `"`
		}
		if subset.Slice {
			result = append(result, subset.Axis+"("+low+")")
		} else {
			result = append(result, subset.Axis+"("+low+","+high+")")
		}
	}
	return result
}

func axisValueText(value datasource.CoverageAxisValue) string {
	if value.Number != nil {
		return strconv.FormatFloat(*value.Number, 'g', -1, 64)
	}
	if value.Time != nil {
		return value.Time.UTC().Format(time.RFC3339Nano)
	}
	return value.Text
}

func sameEPSG(first, second string) bool {
	firstParts, secondParts := strings.Split(strings.TrimRight(first, "/"), "/"), strings.Split(strings.TrimRight(second, "/"), "/")
	return len(firstParts) > 0 && len(secondParts) > 0 && firstParts[len(firstParts)-1] == secondParts[len(secondParts)-1]
}

func cloneInfo(info *datasource.CoverageInfo) *datasource.CoverageInfo {
	copy := *info
	copy.Bands = append([]datasource.CoverageBand(nil), info.Bands...)
	return &copy
}

func cloneDescriptor(descriptor *datasource.CoverageDescriptor) *datasource.CoverageDescriptor {
	if descriptor == nil {
		return nil
	}
	copy := *descriptor
	copy.Axes = append([]datasource.CoverageAxis(nil), descriptor.Axes...)
	for index := range copy.Axes {
		copy.Axes[index].Coordinates = append([]datasource.CoverageAxisValue(nil), descriptor.Axes[index].Coordinates...)
	}
	copy.RangeFields = append([]datasource.CoverageRangeField(nil), descriptor.RangeFields...)
	copy.Formats = append([]string(nil), descriptor.Formats...)
	return &copy
}

// Feature methods intentionally return no layers: rasterfile is a coverage-only service.
func (ds *DataSource) DiscoverLayers(context.Context) ([]*datasource.DiscoveredLayer, error) {
	return nil, nil
}
func (ds *DataSource) Query(context.Context, string, datasource.QueryParams) ([]json.RawMessage, error) {
	return nil, fmt.Errorf("raster file service does not support features")
}
func (ds *DataSource) QueryWKB(context.Context, string, datasource.QueryParams) ([]datasource.RenderFeature, error) {
	return nil, fmt.Errorf("raster file service does not support features")
}
func (ds *DataSource) QueryByID(context.Context, string, string, int) (json.RawMessage, bool, error) {
	return nil, false, fmt.Errorf("raster file service does not support features")
}
func (ds *DataSource) Count(context.Context, string, datasource.QueryParams) (int, error) {
	return 0, fmt.Errorf("raster file service does not support features")
}
func (ds *DataSource) GetLayerInfo(context.Context, string) (*datasource.LayerInfo, error) {
	return nil, fmt.Errorf("raster file service does not support features")
}

func minmax(a, b float64) (float64, float64) {
	if a < b {
		return a, b
	}
	return b, a
}

var _ datasource.DataSource = (*DataSource)(nil)
var _ datasource.CoverageDataSource = (*DataSource)(nil)
var _ datasource.CoverageRenderDataSource = (*DataSource)(nil)
var _ datasource.CoverageQueryDataSource = (*DataSource)(nil)
