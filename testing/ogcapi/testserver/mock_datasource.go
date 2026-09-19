package testserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
)

// MockDataSource implements datasource.DataSource for testing.
type MockDataSource struct {
	id     string
	layers map[string]*mockLayer
}

type mockLayer struct {
	name        string
	title       string
	description string
	geomType    string
	srid        int
	features    []map[string]any
}

// NewMockDataSource creates a new mock data source with test data.
func NewMockDataSource(id string) *MockDataSource {
	ds := &MockDataSource{
		id:     id,
		layers: make(map[string]*mockLayer),
	}

	// Add test layers
	ds.layers["points"] = &mockLayer{
		name:        "points",
		title:       "Test Points",
		description: "A collection of test points",
		geomType:    "Point",
		srid:        4326,
		features: []map[string]any{
			mockFeatureMap(1, "Point A", map[string]any{"type": "Point", "coordinates": []float64{-73.99, 40.73}}),
			mockFeatureMap(2, "Point B", map[string]any{"type": "Point", "coordinates": []float64{-122.42, 37.77}}),
			mockFeatureMap(3, "Point C", map[string]any{"type": "Point", "coordinates": []float64{2.35, 48.85}}),
		},
	}

	ds.layers["lines"] = &mockLayer{
		name:        "lines",
		title:       "Test Lines",
		description: "A collection of test linestrings",
		geomType:    "LineString",
		srid:        4326,
		features: []map[string]any{
			mockFeatureMap(1, "Line A", map[string]any{"type": "LineString", "coordinates": [][]float64{{0, 0}, {1, 1}, {2, 0}}}),
			mockFeatureMap(2, "Line B", map[string]any{"type": "LineString", "coordinates": [][]float64{{-5, -5}, {5, 5}}}),
		},
	}

	ds.layers["polygons"] = &mockLayer{
		name:        "polygons",
		title:       "Test Polygons",
		description: "A collection of test polygons",
		geomType:    "Polygon",
		srid:        4326,
		features: []map[string]any{
			mockFeatureMap(1, "Polygon A", map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {0, 0}}}}),
			mockFeatureMap(2, "Polygon B", map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{-2, -2}, {2, -2}, {2, 2}, {-2, 2}, {-2, -2}}}}),
		},
	}

	return ds
}

func mockFeatureMap(id int, name string, geometry map[string]any) map[string]any {
	return map[string]any{
		"type":     "Feature",
		"id":       id,
		"geometry": geometry,
		"properties": map[string]any{
			"name": name,
		},
	}
}

func (ds *MockDataSource) Type() store.ServiceType {
	return store.ServiceTypePostGIS
}

func (ds *MockDataSource) ID() string {
	return ds.id
}

func (ds *MockDataSource) DiscoverLayers(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	var layers []*datasource.DiscoveredLayer
	for _, l := range ds.layers {
		layers = append(layers, &datasource.DiscoveredLayer{
			Name:           l.name,
			Title:          l.title,
			Description:    l.description,
			GeometryColumn: "geom",
			GeometryType:   l.geomType,
			SRID:           l.srid,
			IDColumn:       "id",
		})
	}
	return layers, nil
}

func (ds *MockDataSource) Query(ctx context.Context, layer string, params datasource.QueryParams) ([]json.RawMessage, error) {
	l, ok := ds.layers[layer]
	if !ok {
		return nil, datasource.LayerNotFoundError{Layer: layer}
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	// Apply pagination
	features := l.features
	if offset >= len(features) {
		return []json.RawMessage{}, nil
	}
	features = features[offset:]
	if limit < len(features) {
		features = features[:limit]
	}

	// Convert to json.RawMessage
	result := make([]json.RawMessage, 0, len(features))
	for _, f := range features {
		data, err := json.Marshal(f)
		if err != nil {
			return nil, err
		}
		result = append(result, data)
	}

	return result, nil
}

func (ds *MockDataSource) QueryByID(ctx context.Context, layer, featureID string, outputSRID int) (json.RawMessage, bool, error) {
	l, ok := ds.layers[layer]
	if !ok {
		return nil, false, datasource.LayerNotFoundError{Layer: layer}
	}

	// Parse feature ID
	id, err := strconv.Atoi(featureID)
	if err != nil {
		return nil, false, datasource.InvalidFeatureIDError{Value: featureID}
	}

	// Find feature
	for _, f := range l.features {
		if fid, ok := f["id"].(int); ok && fid == id {
			data, err := json.Marshal(f)
			if err != nil {
				return nil, false, err
			}
			return data, true, nil
		}
	}

	return nil, false, nil
}

func (ds *MockDataSource) QueryWKB(ctx context.Context, layer string, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	l, ok := ds.layers[layer]
	if !ok {
		return nil, datasource.LayerNotFoundError{Layer: layer}
	}

	// Mock implementation - return empty features for WMS rendering tests
	// In real usage, the actual datasources (PostGIS, DuckDB, etc.) return WKB geometry
	result := make([]datasource.RenderFeature, 0, len(l.features))
	for _, f := range l.features {
		props := make(map[string]interface{})
		if p, ok := f["properties"].(map[string]any); ok {
			for k, v := range p {
				props[k] = v
			}
		}
		result = append(result, datasource.RenderFeature{
			Geometry:   nil, // Mock returns no WKB data
			Properties: props,
		})
	}
	return result, nil
}

func (ds *MockDataSource) Count(ctx context.Context, layer string, params datasource.QueryParams) (int, error) {
	l, ok := ds.layers[layer]
	if !ok {
		return 0, datasource.LayerNotFoundError{Layer: layer}
	}
	return len(l.features), nil
}

func (ds *MockDataSource) GetLayerInfo(ctx context.Context, layer string) (*datasource.LayerInfo, error) {
	l, ok := ds.layers[layer]
	if !ok {
		return nil, datasource.LayerNotFoundError{Layer: layer}
	}

	return &datasource.LayerInfo{
		Name:           l.name,
		Title:          l.title,
		Description:    l.description,
		GeometryColumn: "geom",
		GeometryType:   l.geomType,
		SRID:           l.srid,
		IDColumn:       "id",
		Properties: []datasource.PropertyInfo{
			{Name: "id", Type: "integer", JSONType: datasource.JSONTypeInteger},
			{Name: "name", Type: "text", JSONType: datasource.JSONTypeString},
		},
		Extent: &datasource.Extent{
			MinX: -180,
			MinY: -90,
			MaxX: 180,
			MaxY: 90,
			SRID: 4326,
		},
	}, nil
}

func (ds *MockDataSource) Health(ctx context.Context) error {
	return nil
}

func (ds *MockDataSource) Close() error {
	return nil
}

// GetLayers returns all layer names.
func (ds *MockDataSource) GetLayers() []string {
	var names []string
	for name := range ds.layers {
		names = append(names, name)
	}
	return names
}

// GetLayerTitle returns the title for a layer.
func (ds *MockDataSource) GetLayerTitle(name string) string {
	if l, ok := ds.layers[name]; ok {
		return l.title
	}
	return name
}

// GetLayerDescription returns the description for a layer.
func (ds *MockDataSource) GetLayerDescription(name string) string {
	if l, ok := ds.layers[name]; ok {
		return l.description
	}
	return fmt.Sprintf("Layer %s", name)
}
