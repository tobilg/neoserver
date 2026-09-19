// Package rastermosaic provides a managed collection of compatible GeoTIFF
// granules as one coverage. Time/elevation dimensions select granules for
// portrayal and are exposed as slice axes by the generalized WCS interface.
package rastermosaic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/airbusgeo/godal"
	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/datasource/rastergrid"
	"github.com/tobilg/neoserver/internal/store"
)

func init() {
	defaultMaxGranules.Store(100000)
	defaultMaxRenderGranules.Store(256)
	godal.RegisterAll()
	datasource.Register(store.ServiceTypeRasterMosaic, NewFromService)
}

var (
	defaultMaxGranules       atomic.Int64
	defaultMaxRenderGranules atomic.Int64
	catalogMu                sync.RWMutex
	managedCatalog           GranuleCatalog
)

// GranuleCatalog is implemented by the optional, separate mosaic index. The
// static connection-info path remains the fallback when no active generation
// has been harvested.
type GranuleCatalog interface {
	ListRasterMosaicGranules(ctx context.Context, serviceID string) ([]store.RasterMosaicGranule, error)
}

func SetCatalog(value GranuleCatalog) {
	catalogMu.Lock()
	managedCatalog = value
	catalogMu.Unlock()
}

func SetMaxGranules(value int) {
	if value > 0 {
		defaultMaxGranules.Store(int64(value))
	}
}

func SetMaxRenderGranules(value int) {
	if value > 0 {
		defaultMaxRenderGranules.Store(int64(value))
	}
}

type granule struct {
	store.RasterMosaicGranule
	resolved string
}

type DataSource struct {
	id, name string
	granules []granule
	all      *godal.Dataset
	allName  string
	info     *datasource.CoverageInfo
	mu       sync.Mutex
}

func NewFromService(service *store.Service) (datasource.DataSource, error) {
	var config store.RasterMosaicConnectionInfo
	if err := json.Unmarshal(service.ConnectionInfo, &config); err != nil {
		return nil, fmt.Errorf("unmarshal raster mosaic config: %w", err)
	}
	items := append([]store.RasterMosaicGranule(nil), config.Granules...)
	indexedActive := false
	catalogMu.RLock()
	index := managedCatalog
	catalogMu.RUnlock()
	if index != nil {
		indexed, err := index.ListRasterMosaicGranules(context.Background(), service.ID)
		if err != nil {
			return nil, fmt.Errorf("load indexed mosaic granules: %w", err)
		}
		if len(indexed) > 0 {
			items = indexed
			indexedActive = true
		}
	}
	if !indexedActive && config.Directory != "" {
		pattern := config.Pattern
		if pattern == "" {
			pattern = "*.tif"
		}
		matches, err := filepath.Glob(filepath.Join(config.Directory, pattern))
		if err != nil {
			return nil, fmt.Errorf("invalid mosaic pattern: %w", err)
		}
		known := make(map[string]bool)
		for _, item := range items {
			known[item.Path] = true
		}
		for _, match := range matches {
			if !known[match] {
				items = append(items, store.RasterMosaicGranule{Path: match})
			}
		}
	}
	maximum := config.MaxGranules
	if maximum <= 0 {
		maximum = int(defaultMaxGranules.Load())
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("raster mosaic contains no granules")
	}
	if len(items) > maximum {
		return nil, fmt.Errorf("raster mosaic exceeds %d granules", maximum)
	}
	result := &DataSource{id: service.ID, name: config.Name}
	if result.name == "" {
		result.name = service.Name
	}
	for _, item := range items {
		resolved, err := pathpolicy.Resolve(context.Background(), item.Path)
		if err != nil {
			result.Close()
			return nil, fmt.Errorf("resolve mosaic granule: %w", err)
		}
		if strings.HasPrefix(strings.ToLower(resolved), "s3://") {
			resolved = "/vsis3/" + strings.TrimPrefix(resolved, "s3://")
		}
		result.granules = append(result.granules, granule{RasterMosaicGranule: item, resolved: resolved})
	}
	sort.SliceStable(result.granules, func(i, j int) bool { return result.granules[i].Priority < result.granules[j].Priority })
	paths := result.paths(result.granules)
	name := "/vsimem/neoserver-mosaic-" + uuid.NewString() + ".vrt"
	dataset, err := godal.BuildVRT(name, paths, nil)
	if err != nil {
		return nil, fmt.Errorf("build raster mosaic: %w", err)
	}
	result.all, result.allName = dataset, name
	result.info, err = datasetInfo(dataset)
	if err != nil {
		result.Close()
		return nil, err
	}
	return result, nil
}

func (d *DataSource) Type() store.ServiceType { return store.ServiceTypeRasterMosaic }
func (d *DataSource) ID() string              { return d.id }
func (d *DataSource) Health(context.Context) error {
	if d.all == nil {
		return fmt.Errorf("mosaic is closed")
	}
	return nil
}
func (d *DataSource) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.all == nil {
		return nil
	}
	err := d.all.Close()
	godal.VSIUnlink(d.allName)
	d.all = nil
	return err
}
func (d *DataSource) DiscoverCoverages(context.Context) ([]*datasource.DiscoveredCoverage, error) {
	return []*datasource.DiscoveredCoverage{{SourceCoverage: "mosaic", Title: d.name, Info: *d.info, Descriptor: d.coverageDescriptor()}}, nil
}
func (d *DataSource) GetCoverageInfo(_ context.Context, source string) (*datasource.CoverageInfo, error) {
	if source != "mosaic" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	copy := *d.info
	copy.Bands = append([]datasource.CoverageBand(nil), d.info.Bands...)
	return &copy, nil
}

func (d *DataSource) DescribeCoverage(_ context.Context, source string) (*datasource.CoverageDescriptor, error) {
	if source != "mosaic" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	return d.coverageDescriptor(), nil
}

func (d *DataSource) ExecuteCoverage(_ context.Context, source string, query datasource.CoverageQuery) (*datasource.CoverageResult, error) {
	if source != "mosaic" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	if query.TargetGrid == nil {
		return nil, errors.New("coverage target grid is required")
	}
	var timeValue, elevationValue string
	for _, subset := range query.DomainSubsets {
		kind := d.axisKind(subset.Axis)
		if kind != datasource.CoverageAxisTime && kind != datasource.CoverageAxisElevation {
			continue
		}
		if !subset.Slice {
			return nil, fmt.Errorf("mosaic axis %s must be sliced to one value", subset.Axis)
		}
		value := mosaicAxisValue(subset.Low)
		if kind == datasource.CoverageAxisTime {
			timeValue = value
		} else {
			elevationValue = value
		}
	}
	selected, err := d.selectGranules(timeValue, elevationValue, query.MaxSourceGranules)
	if err != nil {
		return nil, err
	}
	grid, err := d.renderGranules(selected, datasource.CoverageRenderRequest{
		TargetCRS: query.TargetGrid.CRS, BBox: query.TargetGrid.BBox, Width: query.TargetGrid.Width, Height: query.TargetGrid.Height,
		Bands: query.TargetGrid.Bands, Resampling: query.TargetGrid.Resampling, Time: timeValue, Elevation: elevationValue,
	})
	if err != nil {
		return nil, err
	}
	return &datasource.CoverageResult{Descriptor: d.coverageDescriptor(), Grid: grid}, nil
}

func (d *DataSource) coverageDescriptor() *datasource.CoverageDescriptor {
	descriptor := datasource.DescriptorFromInfo("mosaic", d.info)
	descriptor.Title = d.name
	times := map[string]time.Time{}
	elevations := map[float64]bool{}
	for _, item := range d.granules {
		if instant, err := time.Parse(time.RFC3339Nano, item.Time); err == nil {
			times[item.Time] = instant
		}
		if item.Elevation != nil {
			elevations[*item.Elevation] = true
		}
	}
	if len(times) > 0 {
		keys := make([]string, 0, len(times))
		for value := range times {
			keys = append(keys, value)
		}
		sort.Strings(keys)
		axis := datasource.CoverageAxis{Label: "time", Kind: datasource.CoverageAxisTime, Unit: "ISO8601", GridHigh: int64(len(keys) - 1)}
		for _, value := range keys {
			instant := times[value]
			axis.Coordinates = append(axis.Coordinates, datasource.CoverageAxisValue{Time: &instant})
		}
		descriptor.Axes = append(descriptor.Axes, axis)
	}
	if len(elevations) > 0 {
		values := make([]float64, 0, len(elevations))
		for value := range elevations {
			values = append(values, value)
		}
		sort.Float64s(values)
		axis := datasource.CoverageAxis{Label: "elevation", Kind: datasource.CoverageAxisElevation, Unit: "1", GridHigh: int64(len(values) - 1)}
		for _, value := range values {
			coordinate := value
			axis.Coordinates = append(axis.Coordinates, datasource.CoverageAxisValue{Number: &coordinate})
		}
		descriptor.Axes = append(descriptor.Axes, axis)
	}
	return descriptor
}

func (d *DataSource) axisKind(label string) datasource.CoverageAxisKind {
	for _, axis := range d.coverageDescriptor().Axes {
		if strings.EqualFold(axis.Label, label) {
			return axis.Kind
		}
	}
	return datasource.CoverageAxisOther
}

func mosaicAxisValue(value datasource.CoverageAxisValue) string {
	if value.Time != nil {
		return value.Time.UTC().Format(time.RFC3339Nano)
	}
	if value.Number != nil {
		return strconv.FormatFloat(*value.Number, 'g', -1, 64)
	}
	return value.Text
}

func (d *DataSource) withDataset(request datasource.CoverageRenderRequest, fn func(*godal.Dataset) error) error {
	selected, err := d.selectGranules(request.Time, request.Elevation, int(defaultMaxRenderGranules.Load()))
	if err != nil {
		return err
	}
	return d.withGranules(selected, fn)
}

func (d *DataSource) withGranules(selected []granule, fn func(*godal.Dataset) error) error {
	if len(selected) == len(d.granules) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return fn(d.all)
	}
	name := "/vsimem/neoserver-mosaic-selection-" + uuid.NewString() + ".vrt"
	dataset, err := godal.BuildVRT(name, d.paths(selected), nil)
	if err != nil {
		return err
	}
	defer dataset.Close()
	defer godal.VSIUnlink(name)
	return fn(dataset)
}

func (d *DataSource) RenderCoverage(_ context.Context, source string, request datasource.CoverageRenderRequest) (result *datasource.CoverageRenderGrid, err error) {
	if source != "mosaic" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	err = d.withDataset(request, func(dataset *godal.Dataset) error {
		result, err = rastergrid.WarpDataset(dataset, request, d.info.Bands)
		return err
	})
	return result, err
}

func (d *DataSource) renderGranules(selected []granule, request datasource.CoverageRenderRequest) (result *datasource.CoverageRenderGrid, err error) {
	err = d.withGranules(selected, func(dataset *godal.Dataset) error {
		result, err = rastergrid.WarpDataset(dataset, request, d.info.Bands)
		return err
	})
	return result, err
}

func (d *DataSource) ExtractCoverage(_ context.Context, source string, window datasource.CoverageWindow) ([]byte, error) {
	if source != "mosaic" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	if err := validateWindow(d.info, window); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	name := "/vsimem/neoserver-mosaic-extract-" + uuid.NewString() + ".tif"
	defer godal.VSIUnlink(name)
	out, err := d.all.Translate(name, []string{"-srcwin", strconv.Itoa(window.XOff), strconv.Itoa(window.YOff), strconv.Itoa(window.Width), strconv.Itoa(window.Height)}, godal.GTiff, godal.CreationOption("COMPRESS=DEFLATE"))
	if err != nil {
		return nil, err
	}
	if err = out.Close(); err != nil {
		return nil, err
	}
	file, err := godal.VSIOpen(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func (d *DataSource) ReadCoverage(_ context.Context, source string, window datasource.CoverageWindow) (*datasource.CoverageRaster, error) {
	if source != "mosaic" {
		return nil, fmt.Errorf("coverage %q not found", source)
	}
	if err := validateWindow(d.info, window); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	result := &datasource.CoverageRaster{Width: window.Width, Height: window.Height, Bands: make([][]float64, len(d.info.Bands))}
	for index, band := range d.all.Bands() {
		values := make([]float64, window.Width*window.Height)
		if err := band.Read(window.XOff, window.YOff, values, window.Width, window.Height, godal.Window(window.Width, window.Height)); err != nil {
			return nil, err
		}
		result.Bands[index] = values
	}
	return result, nil
}

func (d *DataSource) selectGranules(timeValue, elevationValue string, maximum int) ([]granule, error) {
	selected := d.granules
	if timeValue != "" {
		matches := make(map[int]bool)
		for _, selector := range strings.Split(timeValue, ",") {
			selector = strings.TrimSpace(selector)
			start, end := selector, selector
			if strings.EqualFold(selector, "current") {
				latest := ""
				for _, item := range selected {
					if item.Time > latest {
						latest = item.Time
					}
				}
				start, end = latest, latest
			} else if parts := strings.Split(selector, "/"); len(parts) >= 2 {
				start, end = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			}
			for index, item := range selected {
				if item.Time != "" && item.Time >= start && item.Time <= end {
					matches[index] = true
				}
			}
		}
		filtered := make([]granule, 0, len(matches))
		for index, item := range selected {
			if matches[index] {
				filtered = append(filtered, item)
			}
		}
		selected = filtered
	}
	if elevationValue != "" {
		matches := make(map[int]bool)
		for _, selector := range strings.Split(elevationValue, ",") {
			parts := strings.Split(strings.TrimSpace(selector), "/")
			minimum, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			if err != nil {
				return nil, fmt.Errorf("invalid mosaic elevation")
			}
			maximum := minimum
			if len(parts) >= 2 {
				maximum, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err != nil {
					return nil, fmt.Errorf("invalid mosaic elevation")
				}
			}
			for index, item := range selected {
				if item.Elevation != nil && *item.Elevation >= minimum && *item.Elevation <= maximum {
					matches[index] = true
				}
			}
		}
		filtered := make([]granule, 0, len(matches))
		for index, item := range selected {
			if matches[index] {
				filtered = append(filtered, item)
			}
		}
		selected = filtered
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no mosaic granule matches the requested dimensions")
	}
	if maximum > 0 && len(selected) > maximum {
		return nil, fmt.Errorf("mosaic selection contains %d granules; limit is %d", len(selected), maximum)
	}
	return selected, nil
}

func (d *DataSource) paths(values []granule) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = values[index].resolved
	}
	return result
}

func datasetInfo(dataset *godal.Dataset) (*datasource.CoverageInfo, error) {
	structure := dataset.Structure()
	if structure.SizeX < 1 || structure.SizeY < 1 || structure.NBands < 1 {
		return nil, fmt.Errorf("mosaic is empty")
	}
	gt, err := dataset.GeoTransform()
	if err != nil {
		return nil, err
	}
	if gt[1] == 0 || gt[5] == 0 || gt[2] != 0 || gt[4] != 0 {
		return nil, fmt.Errorf("mosaic must be an axis-aligned regular grid")
	}
	ref := dataset.SpatialRef()
	if ref == nil {
		return nil, fmt.Errorf("mosaic CRS is required")
	}
	if ref.AuthorityCode("") == "" {
		_ = ref.AutoIdentifyEPSG()
	}
	code := ref.AuthorityCode("")
	authority := ref.AuthorityName("")
	srid, _ := strconv.Atoi(code)
	if code == "" {
		return nil, fmt.Errorf("mosaic CRS authority is required")
	}
	x2, y2 := gt[0]+float64(structure.SizeX)*gt[1], gt[3]+float64(structure.SizeY)*gt[5]
	bands := make([]datasource.CoverageBand, structure.NBands)
	for index, band := range dataset.Bands() {
		name := band.Description()
		if name == "" {
			name = fmt.Sprintf("band%d", index+1)
		}
		bands[index] = datasource.CoverageBand{Band: index + 1, Name: name, DataType: band.Structure().DataType.String(), ColorInterpretation: band.ColorInterp().Name()}
		if value, ok := band.NoData(); ok {
			bands[index].NilValues = []string{strconv.FormatFloat(value, 'g', -1, 64)}
		}
	}
	return &datasource.CoverageInfo{CRS: "http://www.opengis.net/def/crs/" + authority + "/0/" + code, SRID: srid, AxisLabels: [2]string{"x", "y"}, Width: structure.SizeX, Height: structure.SizeY, OriginX: gt[0], OriginY: gt[3], ResolutionX: gt[1], ResolutionY: gt[5], Envelope: [4]float64{math.Min(gt[0], x2), math.Min(gt[3], y2), math.Max(gt[0], x2), math.Max(gt[3], y2)}, Bands: bands}, nil
}

func validateWindow(info *datasource.CoverageInfo, window datasource.CoverageWindow) error {
	if window.XOff < 0 || window.YOff < 0 || window.Width < 1 || window.Height < 1 || window.XOff+window.Width > info.Width || window.YOff+window.Height > info.Height {
		return fmt.Errorf("coverage window is outside the grid")
	}
	return nil
}

func (d *DataSource) DiscoverLayers(context.Context) ([]*datasource.DiscoveredLayer, error) {
	return nil, nil
}
func (d *DataSource) Query(context.Context, string, datasource.QueryParams) ([]json.RawMessage, error) {
	return nil, fmt.Errorf("raster mosaic does not support features")
}
func (d *DataSource) QueryWKB(context.Context, string, datasource.QueryParams) ([]datasource.RenderFeature, error) {
	return nil, fmt.Errorf("raster mosaic does not support features")
}
func (d *DataSource) QueryByID(context.Context, string, string, int) (json.RawMessage, bool, error) {
	return nil, false, fmt.Errorf("raster mosaic does not support features")
}
func (d *DataSource) Count(context.Context, string, datasource.QueryParams) (int, error) {
	return 0, fmt.Errorf("raster mosaic does not support features")
}
func (d *DataSource) GetLayerInfo(context.Context, string) (*datasource.LayerInfo, error) {
	return nil, fmt.Errorf("raster mosaic does not support features")
}

var _ datasource.DataSource = (*DataSource)(nil)
var _ datasource.CoverageRenderDataSource = (*DataSource)(nil)
var _ datasource.CoverageQueryDataSource = (*DataSource)(nil)
