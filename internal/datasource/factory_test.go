package datasource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

// ============================================================================
// Mock DataSource for testing
// ============================================================================

type mockDataSource struct {
	svcType store.ServiceType
	svcID   string
}

func (m *mockDataSource) Type() store.ServiceType {
	return m.svcType
}

func (m *mockDataSource) ID() string {
	return m.svcID
}

func (m *mockDataSource) DiscoverLayers(ctx context.Context) ([]*DiscoveredLayer, error) {
	return nil, nil
}

func (m *mockDataSource) Query(ctx context.Context, layer string, params QueryParams) ([]json.RawMessage, error) {
	return nil, nil
}

func (m *mockDataSource) QueryWKB(ctx context.Context, layer string, params QueryParams) ([]RenderFeature, error) {
	return nil, nil
}

func (m *mockDataSource) QueryByID(ctx context.Context, layer, featureID string, outputSRID int) (json.RawMessage, bool, error) {
	return nil, false, nil
}

func (m *mockDataSource) Count(ctx context.Context, layer string, params QueryParams) (int, error) {
	return 0, nil
}

func (m *mockDataSource) GetLayerInfo(ctx context.Context, layer string) (*LayerInfo, error) {
	return nil, nil
}

func (m *mockDataSource) Health(ctx context.Context) error {
	return nil
}

func (m *mockDataSource) Close() error {
	return nil
}

// ============================================================================
// Register Tests
// ============================================================================

func TestRegister(t *testing.T) {
	// Save original registry and restore after test
	originalRegistry := factoryRegistry
	defer func() { factoryRegistry = originalRegistry }()
	factoryRegistry = make(map[store.ServiceType]FactoryFunc)

	testType := store.ServiceType("test_type")
	factory := func(svc *store.Service) (DataSource, error) {
		return &mockDataSource{svcType: svc.Type, svcID: svc.ID}, nil
	}

	Register(testType, factory)

	if _, ok := factoryRegistry[testType]; !ok {
		t.Error("expected factory to be registered")
	}
}

func TestRegister_Overwrite(t *testing.T) {
	// Save original registry and restore after test
	originalRegistry := factoryRegistry
	defer func() { factoryRegistry = originalRegistry }()
	factoryRegistry = make(map[store.ServiceType]FactoryFunc)

	testType := store.ServiceType("test_type")

	// Register first factory
	factory1Called := false
	factory1 := func(svc *store.Service) (DataSource, error) {
		factory1Called = true
		return &mockDataSource{}, nil
	}
	Register(testType, factory1)

	// Register second factory (overwrites first)
	factory2Called := false
	factory2 := func(svc *store.Service) (DataSource, error) {
		factory2Called = true
		return &mockDataSource{}, nil
	}
	Register(testType, factory2)

	// Create service using registered factory
	svc := &store.Service{Type: testType}
	_, _ = CreateFromService(svc)

	if factory1Called {
		t.Error("expected factory1 to NOT be called (should be overwritten)")
	}
	if !factory2Called {
		t.Error("expected factory2 to be called")
	}
}

// ============================================================================
// CreateFromService Tests
// ============================================================================

func TestCreateFromService_Success(t *testing.T) {
	// Save original registry and restore after test
	originalRegistry := factoryRegistry
	defer func() { factoryRegistry = originalRegistry }()
	factoryRegistry = make(map[store.ServiceType]FactoryFunc)

	testType := store.ServiceType("test_type")
	factory := func(svc *store.Service) (DataSource, error) {
		return &mockDataSource{svcType: svc.Type, svcID: svc.ID}, nil
	}
	Register(testType, factory)

	svc := &store.Service{
		ID:   "svc-123",
		Type: testType,
	}

	ds, err := CreateFromService(svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds == nil {
		t.Fatal("expected non-nil DataSource")
	}
	if ds.Type() != testType {
		t.Errorf("expected type %s, got %s", testType, ds.Type())
	}
	if ds.ID() != "svc-123" {
		t.Errorf("expected ID svc-123, got %s", ds.ID())
	}
}

func TestCreateFromService_UnsupportedType(t *testing.T) {
	// Save original registry and restore after test
	originalRegistry := factoryRegistry
	defer func() { factoryRegistry = originalRegistry }()
	factoryRegistry = make(map[store.ServiceType]FactoryFunc)

	svc := &store.Service{
		ID:   "svc-123",
		Type: store.ServiceType("unsupported"),
	}

	ds, err := CreateFromService(svc)
	if err == nil {
		t.Error("expected error for unsupported type")
	}
	if ds != nil {
		t.Error("expected nil DataSource on error")
	}
	if err.Error() != "unsupported service type: unsupported" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCreateFromService_MultipleTypes(t *testing.T) {
	// Save original registry and restore after test
	originalRegistry := factoryRegistry
	defer func() { factoryRegistry = originalRegistry }()
	factoryRegistry = make(map[store.ServiceType]FactoryFunc)

	typeA := store.ServiceType("type_a")
	typeB := store.ServiceType("type_b")

	Register(typeA, func(svc *store.Service) (DataSource, error) {
		return &mockDataSource{svcType: typeA, svcID: "from_a"}, nil
	})
	Register(typeB, func(svc *store.Service) (DataSource, error) {
		return &mockDataSource{svcType: typeB, svcID: "from_b"}, nil
	})

	// Test type A
	svcA := &store.Service{ID: "a", Type: typeA}
	dsA, err := CreateFromService(svcA)
	if err != nil {
		t.Fatalf("unexpected error for type A: %v", err)
	}
	if dsA.Type() != typeA {
		t.Errorf("expected type %s, got %s", typeA, dsA.Type())
	}

	// Test type B
	svcB := &store.Service{ID: "b", Type: typeB}
	dsB, err := CreateFromService(svcB)
	if err != nil {
		t.Fatalf("unexpected error for type B: %v", err)
	}
	if dsB.Type() != typeB {
		t.Errorf("expected type %s, got %s", typeB, dsB.Type())
	}
}

// ============================================================================
// Error Type Tests
// ============================================================================

func TestInvalidFeatureIDError_Error(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"abc", "invalid feature id: abc"},
		{"", "invalid feature id: "},
		{"123abc!@#", "invalid feature id: 123abc!@#"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := InvalidFeatureIDError{Value: tt.value}
			if err.Error() != tt.want {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestLayerNotFoundError_Error(t *testing.T) {
	tests := []struct {
		layer string
		want  string
	}{
		{"test_layer", "layer not found: test_layer"},
		{"", "layer not found: "},
		{"public.cities", "layer not found: public.cities"},
	}

	for _, tt := range tests {
		t.Run(tt.layer, func(t *testing.T) {
			err := LayerNotFoundError{Layer: tt.layer}
			if err.Error() != tt.want {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

// ============================================================================
// Type Tests
// ============================================================================

func TestJSONType_Constants(t *testing.T) {
	tests := []struct {
		jsonType JSONType
		want     string
	}{
		{JSONTypeString, "string"},
		{JSONTypeNumber, "number"},
		{JSONTypeInteger, "integer"},
		{JSONTypeBoolean, "boolean"},
		{JSONTypeObject, "object"},
		{JSONTypeArray, "array"},
	}

	for _, tt := range tests {
		t.Run(string(tt.jsonType), func(t *testing.T) {
			if string(tt.jsonType) != tt.want {
				t.Errorf("JSONType constant = %q, want %q", string(tt.jsonType), tt.want)
			}
		})
	}
}

func TestQueryParams_DefaultValues(t *testing.T) {
	params := QueryParams{}

	if params.Limit != 0 {
		t.Errorf("expected default Limit=0, got %d", params.Limit)
	}
	if params.Offset != 0 {
		t.Errorf("expected default Offset=0, got %d", params.Offset)
	}
	if params.BBox != nil {
		t.Error("expected default BBox=nil")
	}
	if params.Filter != "" {
		t.Errorf("expected default Filter='', got %q", params.Filter)
	}
}

func TestBBox_Values(t *testing.T) {
	bbox := BBox{
		MinX: -180,
		MinY: -90,
		MaxX: 180,
		MaxY: 90,
	}

	if bbox.MinX != -180 {
		t.Errorf("expected MinX=-180, got %f", bbox.MinX)
	}
	if bbox.MinY != -90 {
		t.Errorf("expected MinY=-90, got %f", bbox.MinY)
	}
	if bbox.MaxX != 180 {
		t.Errorf("expected MaxX=180, got %f", bbox.MaxX)
	}
	if bbox.MaxY != 90 {
		t.Errorf("expected MaxY=90, got %f", bbox.MaxY)
	}
}

func TestSortField_Values(t *testing.T) {
	ascending := SortField{Name: "name", Desc: false}
	descending := SortField{Name: "created_at", Desc: true}

	if ascending.Name != "name" {
		t.Errorf("expected Name=name, got %q", ascending.Name)
	}
	if ascending.Desc {
		t.Error("expected ascending sort (Desc=false)")
	}

	if descending.Name != "created_at" {
		t.Errorf("expected Name=created_at, got %q", descending.Name)
	}
	if !descending.Desc {
		t.Error("expected descending sort (Desc=true)")
	}
}

func TestExtent_Values(t *testing.T) {
	extent := Extent{
		MinX: -122.5,
		MinY: 37.5,
		MaxX: -122.0,
		MaxY: 38.0,
		SRID: 4326,
	}

	if extent.MinX != -122.5 {
		t.Errorf("expected MinX=-122.5, got %f", extent.MinX)
	}
	if extent.SRID != 4326 {
		t.Errorf("expected SRID=4326, got %d", extent.SRID)
	}
}

func TestLayerInfo_Values(t *testing.T) {
	info := LayerInfo{
		Name:           "cities",
		Schema:         "public",
		Title:          "World Cities",
		GeometryColumn: "geom",
		GeometryType:   "Point",
		SRID:           4326,
		IDColumn:       "id",
		Properties: []PropertyInfo{
			{Name: "name", Type: "text", JSONType: JSONTypeString, Ordinal: 1},
			{Name: "population", Type: "integer", JSONType: JSONTypeInteger, Ordinal: 2},
		},
	}

	if info.Name != "cities" {
		t.Errorf("expected Name=cities, got %q", info.Name)
	}
	if len(info.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(info.Properties))
	}
	if info.Properties[0].JSONType != JSONTypeString {
		t.Errorf("expected first property JSONType=string, got %q", info.Properties[0].JSONType)
	}
}

func TestDiscoveredLayer_Values(t *testing.T) {
	layer := DiscoveredLayer{
		Name:           "roads",
		Schema:         "osm",
		Title:          "Road Network",
		Description:    "OpenStreetMap roads",
		GeometryColumn: "way",
		GeometryType:   "LineString",
		SRID:           3857,
		IDColumn:       "osm_id",
	}

	if layer.Name != "roads" {
		t.Errorf("expected Name=roads, got %q", layer.Name)
	}
	if layer.Schema != "osm" {
		t.Errorf("expected Schema=osm, got %q", layer.Schema)
	}
	if layer.GeometryType != "LineString" {
		t.Errorf("expected GeometryType=LineString, got %q", layer.GeometryType)
	}
}

func TestRenderFeature_Values(t *testing.T) {
	feature := RenderFeature{
		Geometry: []byte{0x01, 0x02, 0x03},
		Properties: map[string]interface{}{
			"name": "Test",
			"id":   123,
		},
	}

	if len(feature.Geometry) != 3 {
		t.Errorf("expected Geometry length=3, got %d", len(feature.Geometry))
	}
	if feature.Properties["name"] != "Test" {
		t.Errorf("expected Properties[name]=Test, got %v", feature.Properties["name"])
	}
}
