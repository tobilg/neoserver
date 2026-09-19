// Package datasource provides the DataSource interface and implementations for various data backends.
package datasource

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

// Backend is the common lifecycle shared by feature and coverage data sources.
// A service may implement either capability, or both (for example PostGIS).
type Backend interface {
	Type() store.ServiceType
	ID() string
	Health(ctx context.Context) error
	Close() error
}

// DataSource is the interface that all data source implementations must satisfy.
type DataSource interface {
	Backend

	// DiscoverLayers discovers available layers from the data source.
	DiscoverLayers(ctx context.Context) ([]*DiscoveredLayer, error)

	// Query executes a feature query and returns GeoJSON features.
	Query(ctx context.Context, layer string, params QueryParams) ([]json.RawMessage, error)

	// QueryWKB executes a feature query and returns WKB geometry with properties for rendering.
	// This is used by WMS GetMap to get geometry in a format suitable for rendering.
	QueryWKB(ctx context.Context, layer string, params QueryParams) ([]RenderFeature, error)

	// QueryByID retrieves a single feature by ID.
	QueryByID(ctx context.Context, layer, featureID string, outputSRID int) (json.RawMessage, bool, error)

	// Count returns the number of features matching the query.
	Count(ctx context.Context, layer string, params QueryParams) (int, error)

	// GetLayerInfo returns metadata about a specific layer.
	GetLayerInfo(ctx context.Context, layer string) (*LayerInfo, error)
}

// CoverageDataSource is implemented by raster-capable backends used by WCS.
// ExtractCoverage returns a GeoTIFF containing the requested pixel window.
type CoverageDataSource interface {
	Backend
	DiscoverCoverages(ctx context.Context) ([]*DiscoveredCoverage, error)
	GetCoverageInfo(ctx context.Context, sourceCoverage string) (*CoverageInfo, error)
	ExtractCoverage(ctx context.Context, sourceCoverage string, window CoverageWindow) ([]byte, error)
	ReadCoverage(ctx context.Context, sourceCoverage string, window CoverageWindow) (*CoverageRaster, error)
}

// CoverageRenderDataSource adds target-grid reading for WMS and map tiles.
type CoverageRenderDataSource interface {
	CoverageDataSource
	RenderCoverage(ctx context.Context, sourceCoverage string, request CoverageRenderRequest) (*CoverageRenderGrid, error)
}

// DiscoveredCoverage describes a source coverage that can be published.
type DiscoveredCoverage struct {
	SourceCoverage string              `json:"source_coverage"`
	Title          string              `json:"title,omitempty"`
	Description    string              `json:"description,omitempty"`
	Info           CoverageInfo        `json:"info"`
	Descriptor     *CoverageDescriptor `json:"descriptor,omitempty"`
}

// CoverageInfo is the normalized metadata for a two-dimensional regular grid.
// Origin is the outer upper-left grid corner and ResolutionY is normally negative.
type CoverageInfo struct {
	CRS         string         `json:"crs"`
	SRID        int            `json:"srid,omitempty"`
	AxisLabels  [2]string      `json:"axis_labels"`
	Width       int            `json:"width"`
	Height      int            `json:"height"`
	OriginX     float64        `json:"origin_x"`
	OriginY     float64        `json:"origin_y"`
	ResolutionX float64        `json:"resolution_x"`
	ResolutionY float64        `json:"resolution_y"`
	Envelope    [4]float64     `json:"envelope"` // minx, miny, maxx, maxy
	Bands       []CoverageBand `json:"bands"`
}

// CoverageBand describes one numeric range component.
type CoverageBand struct {
	Band                int      `json:"band"`
	Name                string   `json:"name"`
	Description         string   `json:"description,omitempty"`
	DataType            string   `json:"data_type"`
	Definition          string   `json:"definition,omitempty"`
	UOM                 string   `json:"uom,omitempty"`
	NilValues           []string `json:"nil_values,omitempty"`
	ColorInterpretation string   `json:"color_interpretation,omitempty"`
}

// CoverageWindow identifies an inclusive source-grid subset using pixel offsets.
type CoverageWindow struct {
	XOff     int
	YOff     int
	Width    int
	Height   int
	GridLowX int
	GridLowY int
}

// CoverageRaster contains band-major, row-major numeric samples for GML output.
type CoverageRaster struct {
	Width  int
	Height int
	Bands  [][]float64
}

// CoverageRenderRequest describes the exact output grid required by portrayal.
// BBox is always x/y ordered in TargetCRS, regardless of WMS wire axis order.
type CoverageRenderRequest struct {
	TargetCRS  string
	BBox       [4]float64
	Width      int
	Height     int
	Bands      []int
	Resampling string
	Time       string
	Elevation  string
}

// CoverageRenderGrid contains selected, target-grid-aligned numeric samples.
// Bands are band-major and Valid is shared across all selected bands.
type CoverageRenderGrid struct {
	Width       int
	Height      int
	BandNumbers []int
	Bands       [][]float64
	Valid       []bool
	BandInfo    []CoverageBand
}

// CoverageAxisKind identifies the semantic role of a coverage axis.  The
// values are deliberately protocol-neutral so the same descriptor can back
// WCS, WMS dimensions, and raster tile selection.
type CoverageAxisKind string

const (
	CoverageAxisSpatialX  CoverageAxisKind = "spatial-x"
	CoverageAxisSpatialY  CoverageAxisKind = "spatial-y"
	CoverageAxisTime      CoverageAxisKind = "time"
	CoverageAxisElevation CoverageAxisKind = "elevation"
	CoverageAxisOther     CoverageAxisKind = "other"
)

// CoverageAxisValue is a typed coordinate. Exactly one of Number, Time, or
// Text is populated.
type CoverageAxisValue struct {
	Number *float64   `json:"number,omitempty"`
	Time   *time.Time `json:"time,omitempty"`
	Text   string     `json:"text,omitempty"`
}

// CoverageAxis describes either a regular axis (Origin and Resolution) or an
// irregular axis (Coordinates). GridLow/GridHigh are inclusive.
type CoverageAxis struct {
	Label       string              `json:"label"`
	Kind        CoverageAxisKind    `json:"kind"`
	CRS         string              `json:"crs,omitempty"`
	Unit        string              `json:"unit,omitempty"`
	GridLow     int64               `json:"grid_low"`
	GridHigh    int64               `json:"grid_high"`
	Regular     bool                `json:"regular"`
	Origin      CoverageAxisValue   `json:"origin,omitempty"`
	Resolution  CoverageAxisValue   `json:"resolution,omitempty"`
	Coordinates []CoverageAxisValue `json:"coordinates,omitempty"`
}

// CoverageRangeField is one ordered component of a coverage range.
type CoverageRangeField struct {
	Name        string   `json:"name"`
	SourceIndex int      `json:"source_index"`
	DataType    string   `json:"data_type"`
	Definition  string   `json:"definition,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	NilValues   []string `json:"nil_values,omitempty"`
	Scale       float64  `json:"scale,omitempty"`
	Offset      float64  `json:"offset,omitempty"`
}

// CoverageDescriptor is the generalized, potentially multidimensional
// description used by the WCS execution pipeline.
type CoverageDescriptor struct {
	ID          string               `json:"id"`
	Title       string               `json:"title,omitempty"`
	NativeCRS   string               `json:"native_crs,omitempty"`
	Axes        []CoverageAxis       `json:"axes"`
	RangeFields []CoverageRangeField `json:"range_fields"`
	Formats     []string             `json:"formats,omitempty"`
}

type CoverageDomainSubset struct {
	Axis  string            `json:"axis"`
	Low   CoverageAxisValue `json:"low"`
	High  CoverageAxisValue `json:"high"`
	Slice bool              `json:"slice"`
}

type CoverageRangeSelection struct {
	Start string `json:"start"`
	End   string `json:"end,omitempty"`
}

type CoverageScaleMode string

const (
	CoverageScaleNone   CoverageScaleMode = ""
	CoverageScaleFactor CoverageScaleMode = "factor"
	CoverageScaleAxes   CoverageScaleMode = "axes"
	CoverageScaleSize   CoverageScaleMode = "size"
	CoverageScaleExtent CoverageScaleMode = "extent"
)

type CoverageScaleAxis struct {
	Axis   string  `json:"axis"`
	Factor float64 `json:"factor,omitempty"`
	Size   int     `json:"size,omitempty"`
	Low    int     `json:"low,omitempty"`
	High   int     `json:"high,omitempty"`
}

type CoverageScaling struct {
	Mode   CoverageScaleMode   `json:"mode"`
	Factor float64             `json:"factor,omitempty"`
	Axes   []CoverageScaleAxis `json:"axes,omitempty"`
}

type CoverageTargetGrid struct {
	CRS        string     `json:"crs"`
	BBox       [4]float64 `json:"bbox"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	GridLowX   int        `json:"grid_low_x,omitempty"`
	GridLowY   int        `json:"grid_low_y,omitempty"`
	Bands      []int      `json:"bands,omitempty"`
	Resampling string     `json:"resampling,omitempty"`
}

// CoverageQuery is the transport-neutral WCS request passed to generalized
// coverage sources.
type CoverageQuery struct {
	Version            string                   `json:"version"`
	CoverageID         string                   `json:"coverage_id"`
	DomainSubsets      []CoverageDomainSubset   `json:"domain_subsets,omitempty"`
	RangeSubset        []CoverageRangeSelection `json:"range_subset,omitempty"`
	Scaling            CoverageScaling          `json:"scaling,omitempty"`
	SubsettingCRS      string                   `json:"subsetting_crs,omitempty"`
	OutputCRS          string                   `json:"output_crs,omitempty"`
	Interpolation      string                   `json:"interpolation,omitempty"`
	Format             string                   `json:"format,omitempty"`
	Multipart          bool                     `json:"multipart,omitempty"`
	TargetGrid         *CoverageTargetGrid      `json:"target_grid,omitempty"`
	MaxSourceGranules  int                      `json:"-"`
	MaxTemporaryBytes  int64                    `json:"-"`
	TemporaryDirectory string                   `json:"-"`
}

// CoverageResult supports either a streaming encoded representation or a
// numeric grid to be encoded by the WCS layer. Close must be called when set.
type CoverageResult struct {
	Descriptor    *CoverageDescriptor
	Grid          *CoverageRenderGrid
	Raster        *CoverageRaster
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
}

// CoverageQueryDataSource is the optional generalized coverage interface.
// Existing 2D data sources continue to work through the WCS compatibility
// adapter while implementations migrate incrementally.
type CoverageQueryDataSource interface {
	CoverageDataSource
	DescribeCoverage(ctx context.Context, sourceCoverage string) (*CoverageDescriptor, error)
	ExecuteCoverage(ctx context.Context, sourceCoverage string, query CoverageQuery) (*CoverageResult, error)
}

// DescriptorFromInfo adapts the legacy two-dimensional regular-grid metadata
// into the generalized model.
func DescriptorFromInfo(id string, info *CoverageInfo) *CoverageDescriptor {
	if info == nil {
		return nil
	}
	number := func(value float64) CoverageAxisValue { return CoverageAxisValue{Number: &value} }
	result := &CoverageDescriptor{ID: id, NativeCRS: info.CRS, Formats: []string{"image/tiff", "application/gml+xml", "multipart/related"}}
	result.Axes = []CoverageAxis{
		{Label: info.AxisLabels[0], Kind: CoverageAxisSpatialX, CRS: info.CRS, GridHigh: int64(info.Width - 1), Regular: true, Origin: number(info.OriginX + info.ResolutionX/2), Resolution: number(info.ResolutionX)},
		{Label: info.AxisLabels[1], Kind: CoverageAxisSpatialY, CRS: info.CRS, GridHigh: int64(info.Height - 1), Regular: true, Origin: number(info.OriginY + info.ResolutionY/2), Resolution: number(info.ResolutionY)},
	}
	result.RangeFields = make([]CoverageRangeField, len(info.Bands))
	for i, band := range info.Bands {
		result.RangeFields[i] = CoverageRangeField{Name: band.Name, SourceIndex: band.Band, DataType: band.DataType, Definition: band.Definition, Unit: band.UOM, NilValues: append([]string(nil), band.NilValues...)}
	}
	return result
}

// WritableDataSource extends DataSource with write operations for WFS-T (Transactional WFS).
type WritableDataSource interface {
	DataSource
	FeatureWriter
}

// FeatureWriter contains the write operations available inside an atomic write.
type FeatureWriter interface {

	// Insert inserts new features into the layer.
	// Returns the IDs of the inserted features.
	Insert(ctx context.Context, layer string, features []FeatureData) ([]string, error)

	// Update updates features matching the filter.
	// Returns the number of features updated.
	Update(ctx context.Context, layer string, properties map[string]interface{}, filter string, args []interface{}) (int, error)

	// Delete deletes features matching the filter.
	// Returns the number of features deleted.
	Delete(ctx context.Context, layer string, filter string, args []interface{}) (int, error)

	// Replace replaces features matching the filter with new feature data.
	// Returns the IDs of the replaced features.
	Replace(ctx context.Context, layer string, feature FeatureData, filter string, args []interface{}) ([]string, error)
}

// AtomicWritableDataSource can execute a group of feature writes atomically.
type AtomicWritableDataSource interface {
	WritableDataSource
	AtomicWrite(ctx context.Context, fn func(FeatureWriter) error) error
}

// ReturningFeatureWriter reports every affected stable identifier from the
// same transaction as the mutation. Callers must roll back on a rejected lock
// check; a best-effort query outside that transaction is not a safe substitute.
type ReturningFeatureWriter interface {
	FeatureWriter
	GetLayerInfo(context.Context, string) (*LayerInfo, error)
	UpdateReturning(context.Context, string, map[string]interface{}, string, []interface{}) ([]string, error)
	DeleteReturning(context.Context, string, string, []interface{}) ([]string, error)
}

// GeometryValue is validated GML with an explicit input CRS. SQL adapters must
// transform it into the native column CRS, not merely assign that column's SRID.
type GeometryValue struct {
	GML  string
	SRID int
}

// FeatureData contains data for a feature to be inserted or used in replace operations.
type FeatureData struct {
	// ID is the optional feature ID (gml:id). If empty, a new ID will be generated.
	ID string
	// Properties contains the feature attributes as a map of property name to value.
	Properties map[string]interface{}
	// Geometry is the GML geometry string (optional for updates).
	Geometry     string
	GeometrySRID int
}

// RenderFeature contains geometry and properties for map rendering.
type RenderFeature struct {
	// ID is the stable source identifier used by interactive map encodings.
	ID string
	// Geometry is the WKB (Well-Known Binary) representation of the geometry.
	Geometry []byte
	// Properties contains the feature attributes.
	Properties map[string]interface{}
}

// FeatureStream iterates GeoJSON features without materializing a complete page.
type FeatureStream interface {
	Next() bool
	Feature() json.RawMessage
	Err() error
	Close() error
}

// RenderFeatureStream iterates render features without materializing a complete map query.
type RenderFeatureStream interface {
	Next() bool
	Feature() RenderFeature
	Err() error
	Close() error
}

// StreamingDataSource is implemented by datasources that support row streaming.
type StreamingDataSource interface {
	QueryStream(ctx context.Context, layer string, params QueryParams) (FeatureStream, error)
	QueryWKBStream(ctx context.Context, layer string, params QueryParams) (RenderFeatureStream, error)
}

type RenderStreamingDataSource interface {
	QueryWKBStream(context.Context, string, QueryParams) (RenderFeatureStream, error)
}

type sqlRenderStream struct {
	rows    *sql.Rows
	current RenderFeature
	err     error
}

func NewSQLRenderStream(rows *sql.Rows) RenderFeatureStream { return &sqlRenderStream{rows: rows} }
func (s *sqlRenderStream) Next() bool {
	if !s.rows.Next() {
		return false
	}
	var geom []byte
	var raw any
	var id any
	if err := s.rows.Scan(&geom, &raw, &id); err != nil {
		s.err = err
		return false
	}
	props := make(map[string]any)
	switch value := raw.(type) {
	case map[string]any:
		props = value
	case []byte:
		_ = json.Unmarshal(value, &props)
	case string:
		_ = json.Unmarshal([]byte(value), &props)
	}
	s.current = RenderFeature{ID: StableRenderFeatureID(id, geom, props), Geometry: geom, Properties: props}
	return true
}

// StableRenderFeatureID returns a source identifier when supplied, otherwise a
// deterministic digest of the geometry and canonical JSON properties.
func StableRenderFeatureID(source any, geometry []byte, properties map[string]any) string {
	var value string
	switch typed := source.(type) {
	case nil:
	case string:
		value = typed
	case []byte:
		value = string(typed)
	default:
		value = fmt.Sprint(typed)
	}
	if value != "" {
		return value
	}
	encoded, _ := json.Marshal(properties)
	hash := sha256.New()
	_, _ = hash.Write(geometry)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(encoded)
	return fmt.Sprintf("feature-%x", hash.Sum(nil)[:12])
}
func (s *sqlRenderStream) Feature() RenderFeature { return s.current }
func (s *sqlRenderStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.rows.Err()
}
func (s *sqlRenderStream) Close() error {
	if err := s.rows.Close(); err != nil {
		return fmt.Errorf("close rows: %w", err)
	}
	return nil
}

type sliceRenderStream struct {
	values []RenderFeature
	index  int
}

func NewSliceRenderStream(values []RenderFeature) RenderFeatureStream {
	return &sliceRenderStream{values: values, index: -1}
}
func (s *sliceRenderStream) Next() bool             { s.index++; return s.index < len(s.values) }
func (s *sliceRenderStream) Feature() RenderFeature { return s.values[s.index] }
func (s *sliceRenderStream) Err() error             { return nil }
func (s *sliceRenderStream) Close() error           { s.values = nil; return nil }

// DiscoveredLayer contains information about a layer discovered from a data source.
type DiscoveredLayer struct {
	Name           string `json:"name"`
	Schema         string `json:"schema,omitempty"`
	Title          string `json:"title,omitempty"`
	Description    string `json:"description,omitempty"`
	GeometryColumn string `json:"geometry_column"`
	GeometryType   string `json:"geometry_type"`
	SRID           int    `json:"srid"`
	IDColumn       string `json:"id_column,omitempty"`
}

// LayerInfo contains metadata about a layer.
type LayerInfo struct {
	Name           string
	Schema         string
	Title          string
	Description    string
	GeometryColumn string
	GeometryType   string
	SRID           int
	IDColumn       string
	Properties     []PropertyInfo
	PGTypes        map[string]string
	Extent         *Extent
}

// LayerExtentDataSource is implemented by adapters that can efficiently
// aggregate a native-CRS layer envelope without transferring all features.
type LayerExtentDataSource interface {
	GetLayerExtent(ctx context.Context, layer string) (*Extent, error)
}

// PropertyInfo describes a property/attribute of a layer.
type PropertyInfo struct {
	Name        string
	Type        string
	JSONType    JSONType
	Description string
	Ordinal     int
}

// JSONType represents the JSON type of a property.
type JSONType string

const (
	JSONTypeString  JSONType = "string"
	JSONTypeNumber  JSONType = "number"
	JSONTypeInteger JSONType = "integer"
	JSONTypeBoolean JSONType = "boolean"
	JSONTypeObject  JSONType = "object"
	JSONTypeArray   JSONType = "array"
)

// Extent represents the geographic extent of a layer.
type Extent struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
	SRID int
}

// ParseExtentWKT extracts a bounding box from an aggregate polygon WKT.
func ParseExtentWKT(wkt string, srid int) (*Extent, error) {
	fields := strings.FieldsFunc(wkt, func(r rune) bool {
		return !((r >= '0' && r <= '9') || r == '-' || r == '+' || r == '.' || r == 'e' || r == 'E')
	})
	values := make([]float64, 0, len(fields))
	for _, field := range fields {
		if value, err := strconv.ParseFloat(field, 64); err == nil {
			values = append(values, value)
		}
	}
	if len(values) < 4 || len(values)%2 != 0 {
		return nil, fmt.Errorf("invalid extent WKT")
	}
	extent := &Extent{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1), SRID: srid}
	for index := 0; index < len(values); index += 2 {
		extent.MinX, extent.MaxX = min(extent.MinX, values[index]), max(extent.MaxX, values[index])
		extent.MinY, extent.MaxY = min(extent.MinY, values[index+1]), max(extent.MaxY, values[index+1])
	}
	return extent, nil
}

// QueryParams contains parameters for feature queries.
type QueryParams struct {
	FeatureIDs        []string     // Exact published scalar IDs; applied by SQL-view queries.
	Predicate         SQLPredicate // Adapter-neutral predicate; preferred over legacy CompiledFilter.
	Limit             int
	Offset            int
	BBox              *BBox
	BBoxSRID          int
	OutputSRID        int
	Filter            string // CQL2 text filter
	FilterSRID        int
	SortBy            []SortField
	Properties        []string
	SimplifyTolerance float64 // output-CRS tolerance for visual rendering only
	// DateTime carries the validated OGC API Features temporal selection. OGC
	// handlers currently compile this selection into Filter as well so every
	// datasource uses the same parameterized CQL2 path.
	DateTime *DateTimeFilter

	// CompiledFilter contains a pre-compiled SQL WHERE clause (e.g., from FES XML).
	// If set, this takes precedence over Filter.
	CompiledFilter string
	// CompiledFilterArgs contains parameters for the compiled filter.
	CompiledFilterArgs []interface{}
	// CompiledFilterParamOffset is the starting parameter index for the compiled filter.
	CompiledFilterParamOffset int
}

// DateTimeFilter describes an instant or interval selection against a published
// time dimension. Nil Start or End represents an open interval endpoint.
type DateTimeFilter struct {
	SourceProperty string
	EndProperty    string
	Start          *time.Time
	End            *time.Time
	Instant        bool
}

// CQL2 returns the temporal predicate in the common CQL2 text subset compiled
// by every feature datasource. It includes features whose start value is null,
// because those features have no temporal association.
func (f *DateTimeFilter) CQL2() string {
	if f == nil || strings.TrimSpace(f.SourceProperty) == "" {
		return ""
	}
	identifier := func(value string) string {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	timestamp := func(value *time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
	source := identifier(f.SourceProperty)
	endProperty := identifier(f.EndProperty)
	var match string
	if f.EndProperty == "" {
		switch {
		case f.Instant:
			match = fmt.Sprintf("%s = %s", source, timestamp(f.Start))
		case f.Start != nil && f.End != nil:
			match = fmt.Sprintf("%s >= %s AND %s <= %s", source, timestamp(f.Start), source, timestamp(f.End))
		case f.Start != nil:
			match = fmt.Sprintf("%s >= %s", source, timestamp(f.Start))
		default:
			match = fmt.Sprintf("%s <= %s", source, timestamp(f.End))
		}
	} else {
		startMatch, endMatch := "", ""
		if f.Start != nil {
			stamp := timestamp(f.Start)
			startMatch = fmt.Sprintf("(%s >= %s OR (%s IS NULL AND %s >= %s))", endProperty, stamp, endProperty, source, stamp)
		}
		if f.End != nil {
			endMatch = fmt.Sprintf("%s <= %s", source, timestamp(f.End))
		}
		switch {
		case startMatch != "" && endMatch != "":
			match = endMatch + " AND " + startMatch
		case startMatch != "":
			match = startMatch
		default:
			match = endMatch
		}
	}
	return fmt.Sprintf("(%s IS NULL OR (%s))", source, match)
}

// WithDateTimeFilter combines a validated temporal selection with the caller's
// CQL2 filter. Datasource query, stream, SQL-view, WKB, and count builders call
// this method so the behavior is identical across adapters.
func (p QueryParams) WithDateTimeFilter() QueryParams {
	temporal := p.DateTime.CQL2()
	if temporal == "" {
		return p
	}
	if strings.TrimSpace(p.Filter) == "" {
		p.Filter = temporal
	} else {
		p.Filter = "(" + p.Filter + ") AND (" + temporal + ")"
	}
	return p
}

// BBox represents a bounding box for spatial queries.
type BBox struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
}

// Parts returns the one or two ordinary envelopes represented by a bbox. In
// CRS84, west > east denotes an antimeridian-crossing bbox and is split into
// two envelopes so adapters can build a correct spatial predicate.
func (b BBox) Parts(srid int) []BBox {
	if srid == 4326 && b.MinX > b.MaxX {
		return []BBox{
			{MinX: b.MinX, MinY: b.MinY, MaxX: 180, MaxY: b.MaxY},
			{MinX: -180, MinY: b.MinY, MaxX: b.MaxX, MaxY: b.MaxY},
		}
	}
	return []BBox{b}
}

// SortField represents a field to sort by.
type SortField struct {
	Name string
	Desc bool
}

// StableSort appends the feature identifier as an ascending tie-breaker when a
// caller requested another sort order. This keeps offset paging deterministic.
func StableSort(fields []SortField, idProperty string) []SortField {
	if len(fields) == 0 || idProperty == "" {
		return fields
	}
	for _, field := range fields {
		if field.Name == idProperty {
			return fields
		}
	}
	result := append([]SortField(nil), fields...)
	return append(result, SortField{Name: idProperty})
}

// PropertySelected reports whether a property should be included in a projected query.
func PropertySelected(name string, requested []string) bool {
	if len(requested) == 0 {
		return true
	}
	for _, candidate := range requested {
		if candidate == name {
			return true
		}
	}
	return false
}

// Factory creates a DataSource from a store.Service.
type Factory func(svc *store.Service) (DataSource, error)

// InvalidFeatureIDError is returned when a feature ID is invalid.
type InvalidFeatureIDError struct {
	Value string
}

func (e InvalidFeatureIDError) Error() string {
	return "invalid feature id: " + e.Value
}

// LayerNotFoundError is returned when a layer doesn't exist.
type LayerNotFoundError struct {
	Layer string
}

func (e LayerNotFoundError) Error() string {
	return "layer not found: " + e.Layer
}

// SQLViewConfig contains configuration for a SQL View layer.
// This mirrors store.SQLViewConfig but is used in the datasource package.
type SQLViewConfig struct {
	SQL            string             `json:"sql"`
	GeometryColumn string             `json:"geometry_column"`
	GeometryType   string             `json:"geometry_type,omitempty"`
	SRID           int                `json:"srid,omitempty"`
	IDColumn       string             `json:"id_column,omitempty"`
	Properties     []*SQLViewProperty `json:"properties,omitempty"`
	ReadOnly       bool               `json:"read_only"`
}

// SQLViewProperty describes a property/column in a SQL view.
type SQLViewProperty struct {
	Name string `json:"name"`
	Type string `json:"type"` // string, number, integer, boolean
}

// SQLViewDiscovery contains auto-discovered metadata from a SQL query.
type SQLViewDiscovery struct {
	Columns           []PropertyInfo `json:"columns"`
	GeometryColumn    string         `json:"geometry_column"`
	GeometryType      string         `json:"geometry_type"`
	SRID              int            `json:"srid"`
	SuggestedIDColumn string         `json:"suggested_id_column"`
}

// SQLViewDataSource extends DataSource with SQL View query methods.
type SQLViewDataSource interface {
	DataSource

	// ValidateSQLView validates a SQL query for use as a SQL View.
	// Returns an error if the SQL is invalid or contains forbidden keywords.
	ValidateSQLView(ctx context.Context, sql string) error

	// DiscoverSQLViewColumns executes a SQL query with LIMIT 0 to discover column metadata.
	DiscoverSQLViewColumns(ctx context.Context, sql string) (*SQLViewDiscovery, error)

	// QuerySQLView executes a feature query against a SQL View and returns GeoJSON features.
	QuerySQLView(ctx context.Context, config *SQLViewConfig, params QueryParams) ([]json.RawMessage, error)

	// QuerySQLViewWKB executes a SQL View query and returns WKB geometry with properties for rendering.
	QuerySQLViewWKB(ctx context.Context, config *SQLViewConfig, params QueryParams) ([]RenderFeature, error)

	// CountSQLView returns the number of features matching the SQL View query.
	CountSQLView(ctx context.Context, config *SQLViewConfig, params QueryParams) (int, error)
}
