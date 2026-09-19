package mockserver

// BBox represents a bounding box.
type BBox struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
}

// Dimension represents a layer dimension (e.g., TIME).
type Dimension struct {
	Name    string
	Units   string
	Default string
	Values  string // Comma-separated list or interval
}

// Style represents a layer style.
type Style struct {
	Name     string
	Title    string
	Abstract string
}

// MockLayer defines a layer in the mock WMS server.
type MockLayer struct {
	Name        string
	Title       string
	Abstract    string
	CRS         []string
	BBox        map[string]BBox
	Queryable   bool
	Opaque      bool
	Cascaded    int
	NoSubsets   bool
	FixedWidth  int
	FixedHeight int
	Styles      []Style
	Dimensions  []Dimension
	MinScale    float64
	MaxScale    float64
	Attribution string
}

// DefaultMockLayers returns the default set of mock layers for testing.
func DefaultMockLayers() []MockLayer {
	return []MockLayer{
		{
			Name:     "mock:BasicPolygons",
			Title:    "Basic Polygons",
			Abstract: "A test layer with basic polygon geometries",
			CRS: []string{
				"CRS:84",
				"EPSG:4326",
				"EPSG:3857",
				"EPSG:32632",
			},
			BBox: map[string]BBox{
				"CRS:84":     {-180, -90, 180, 90},
				"EPSG:4326":  {-90, -180, 90, 180}, // Note: axis order swap
				"EPSG:3857":  {-20037508.34, -20037508.34, 20037508.34, 20037508.34},
				"EPSG:32632": {166021.44, 0, 833978.56, 9329005.18},
			},
			Queryable: true,
			Opaque:    false,
			Styles: []Style{
				{Name: "default", Title: "Default Style", Abstract: "Default polygon style"},
				{Name: "highlight", Title: "Highlight Style", Abstract: "Highlighted polygon style"},
			},
		},
		{
			Name:     "mock:Roads",
			Title:    "Roads",
			Abstract: "A test layer with road line geometries",
			CRS: []string{
				"CRS:84",
				"EPSG:4326",
			},
			BBox: map[string]BBox{
				"CRS:84":    {-10, -10, 10, 10},
				"EPSG:4326": {-10, -10, 10, 10},
			},
			Queryable: false, // Used to test LayerNotQueryable exception
			Opaque:    true,
			Styles: []Style{
				{Name: "default", Title: "Default Style"},
			},
		},
		{
			Name:     "mock:Points",
			Title:    "Test Points",
			Abstract: "A test layer with point geometries",
			CRS: []string{
				"CRS:84",
				"EPSG:4326",
				"EPSG:3857",
			},
			BBox: map[string]BBox{
				"CRS:84":    {-180, -90, 180, 90},
				"EPSG:4326": {-90, -180, 90, 180},
				"EPSG:3857": {-20037508.34, -20037508.34, 20037508.34, 20037508.34},
			},
			Queryable: true,
			Opaque:    false,
			Styles: []Style{
				{Name: "default", Title: "Default Style"},
				{Name: "circle", Title: "Circle Style"},
			},
		},
		{
			Name:     "mock:TimeSeries",
			Title:    "Time Series Data",
			Abstract: "A test layer with TIME dimension support",
			CRS: []string{
				"CRS:84",
				"EPSG:4326",
			},
			BBox: map[string]BBox{
				"CRS:84":    {-180, -90, 180, 90},
				"EPSG:4326": {-90, -180, 90, 180},
			},
			Queryable: true,
			Opaque:    false,
			Dimensions: []Dimension{
				{
					Name:    "time",
					Units:   "ISO8601",
					Default: "2024-01-01T00:00:00Z",
					Values:  "2024-01-01T00:00:00Z/2024-12-31T23:59:59Z/P1D",
				},
			},
			Styles: []Style{
				{Name: "default", Title: "Default Style"},
			},
		},
		{
			Name:     "mock:Elevation",
			Title:    "Elevation Data",
			Abstract: "A test layer with ELEVATION dimension",
			CRS: []string{
				"CRS:84",
				"EPSG:4326",
			},
			BBox: map[string]BBox{
				"CRS:84":    {-180, -90, 180, 90},
				"EPSG:4326": {-90, -180, 90, 180},
			},
			Queryable: true,
			Opaque:    false,
			Dimensions: []Dimension{
				{
					Name:    "elevation",
					Units:   "EPSG:5030",
					Default: "0",
					Values:  "0,100,500,1000,2000,5000",
				},
			},
			Styles: []Style{
				{Name: "default", Title: "Default Style"},
			},
		},
		{
			Name:     "mock:ScaleLimited",
			Title:    "Scale Limited Layer",
			Abstract: "A test layer with scale denominators",
			CRS: []string{
				"CRS:84",
				"EPSG:4326",
			},
			BBox: map[string]BBox{
				"CRS:84":    {-180, -90, 180, 90},
				"EPSG:4326": {-90, -180, 90, 180},
			},
			Queryable: true,
			Opaque:    false,
			MinScale:  1000,
			MaxScale:  100000,
			Styles: []Style{
				{Name: "default", Title: "Default Style"},
			},
		},
	}
}

// GetLayerByName returns a mock layer by name.
func GetLayerByName(name string) *MockLayer {
	for _, layer := range DefaultMockLayers() {
		if layer.Name == name {
			return &layer
		}
	}
	return nil
}

// GetQueryableLayers returns all queryable mock layers.
func GetQueryableLayers() []MockLayer {
	var result []MockLayer
	for _, layer := range DefaultMockLayers() {
		if layer.Queryable {
			result = append(result, layer)
		}
	}
	return result
}

// GetNonQueryableLayers returns all non-queryable mock layers.
func GetNonQueryableLayers() []MockLayer {
	var result []MockLayer
	for _, layer := range DefaultMockLayers() {
		if !layer.Queryable {
			result = append(result, layer)
		}
	}
	return result
}

// GetLayerWithTimeDimension returns a layer with TIME dimension.
func GetLayerWithTimeDimension() *MockLayer {
	for _, layer := range DefaultMockLayers() {
		for _, dim := range layer.Dimensions {
			if dim.Name == "time" {
				return &layer
			}
		}
	}
	return nil
}

// GetLayerWithElevationDimension returns a layer with ELEVATION dimension.
func GetLayerWithElevationDimension() *MockLayer {
	for _, layer := range DefaultMockLayers() {
		for _, dim := range layer.Dimensions {
			if dim.Name == "elevation" {
				return &layer
			}
		}
	}
	return nil
}
