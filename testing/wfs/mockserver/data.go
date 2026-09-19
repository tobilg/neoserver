// Package mockserver provides a mock WFS 2.0 server for testing.
package mockserver

// Property represents a feature property definition.
type Property struct {
	Name string
	Type string // string, integer, double, date, dateTime, boolean
}

// MockFeature represents a mock feature instance.
type MockFeature struct {
	ID         string
	TypeName   string
	Properties map[string]interface{}
	Geometry   string // GML geometry string
}

// MockFeatureType represents a mock feature type definition.
type MockFeatureType struct {
	Name       string
	Title      string
	Abstract   string
	CRS        []string
	DefaultCRS string
	GeomColumn string
	GeomType   string // Point, LineString, Polygon, MultiPoint, MultiLineString, MultiPolygon
	Properties []Property
	BBox       [4]float64 // minx, miny, maxx, maxy in WGS84
}

// DefaultFeatureTypes returns the default mock feature types.
func DefaultFeatureTypes() []MockFeatureType {
	return []MockFeatureType{
		{
			Name:       "mock:Streams",
			Title:      "Streams",
			Abstract:   "Linear water features",
			CRS:        []string{"urn:ogc:def:crs:EPSG::4326", "urn:ogc:def:crs:CRS84", "urn:ogc:def:crs:EPSG::3857"},
			DefaultCRS: "urn:ogc:def:crs:EPSG::4326",
			GeomColumn: "geom",
			GeomType:   "LineString",
			Properties: []Property{
				{Name: "FID", Type: "string"},
				{Name: "NAME", Type: "string"},
			},
			BBox: [4]float64{-180, -90, 180, 90},
		},
		{
			Name:       "mock:Lakes",
			Title:      "Lakes",
			Abstract:   "Polygonal water bodies",
			CRS:        []string{"urn:ogc:def:crs:EPSG::4326", "urn:ogc:def:crs:CRS84"},
			DefaultCRS: "urn:ogc:def:crs:EPSG::4326",
			GeomColumn: "geom",
			GeomType:   "Polygon",
			Properties: []Property{
				{Name: "FID", Type: "string"},
				{Name: "NAME", Type: "string"},
				{Name: "DEPTH", Type: "double"},
			},
			BBox: [4]float64{-100, 20, -70, 50},
		},
		{
			Name:       "mock:NamedPlaces",
			Title:      "Named Places",
			Abstract:   "Point locations with names",
			CRS:        []string{"urn:ogc:def:crs:EPSG::4326"},
			DefaultCRS: "urn:ogc:def:crs:EPSG::4326",
			GeomColumn: "geom",
			GeomType:   "Point",
			Properties: []Property{
				{Name: "FID", Type: "string"},
				{Name: "NAME", Type: "string"},
			},
			BBox: [4]float64{-120, 30, -80, 45},
		},
		{
			Name:       "mock:Buildings",
			Title:      "Buildings",
			Abstract:   "Building footprints",
			CRS:        []string{"urn:ogc:def:crs:EPSG::4326", "urn:ogc:def:crs:EPSG::3857"},
			DefaultCRS: "urn:ogc:def:crs:EPSG::4326",
			GeomColumn: "geom",
			GeomType:   "Polygon",
			Properties: []Property{
				{Name: "FID", Type: "string"},
				{Name: "NAME", Type: "string"},
				{Name: "HEIGHT", Type: "double"},
				{Name: "FLOORS", Type: "integer"},
				{Name: "BUILT", Type: "date"},
			},
			BBox: [4]float64{-100, 35, -90, 45},
		},
	}
}

// DefaultFeatures returns mock features for testing.
func DefaultFeatures() []MockFeature {
	return []MockFeature{
		// Streams
		{
			ID:       "Streams.1",
			TypeName: "mock:Streams",
			Properties: map[string]interface{}{
				"FID":  "1",
				"NAME": "Mississippi River",
			},
			Geometry: `<gml:LineString srsName="urn:ogc:def:crs:EPSG::4326" gml:id="Streams.1.geom">
				<gml:posList>-90.0 30.0 -89.0 31.0 -88.0 32.0</gml:posList>
			</gml:LineString>`,
		},
		{
			ID:       "Streams.2",
			TypeName: "mock:Streams",
			Properties: map[string]interface{}{
				"FID":  "2",
				"NAME": "Missouri River",
			},
			Geometry: `<gml:LineString srsName="urn:ogc:def:crs:EPSG::4326" gml:id="Streams.2.geom">
				<gml:posList>-95.0 38.0 -94.0 39.0 -93.0 40.0</gml:posList>
			</gml:LineString>`,
		},
		{
			ID:       "Streams.3",
			TypeName: "mock:Streams",
			Properties: map[string]interface{}{
				"FID":  "3",
				"NAME": "Colorado River",
			},
			Geometry: `<gml:LineString srsName="urn:ogc:def:crs:EPSG::4326" gml:id="Streams.3.geom">
				<gml:posList>-110.0 35.0 -112.0 34.0 -114.0 33.0</gml:posList>
			</gml:LineString>`,
		},
		// Lakes
		{
			ID:       "Lakes.1",
			TypeName: "mock:Lakes",
			Properties: map[string]interface{}{
				"FID":   "1",
				"NAME":  "Lake Superior",
				"DEPTH": 406.0,
			},
			Geometry: `<gml:Polygon srsName="urn:ogc:def:crs:EPSG::4326" gml:id="Lakes.1.geom">
				<gml:exterior>
					<gml:LinearRing>
						<gml:posList>-92.0 46.0 -84.0 46.0 -84.0 48.0 -92.0 48.0 -92.0 46.0</gml:posList>
					</gml:LinearRing>
				</gml:exterior>
			</gml:Polygon>`,
		},
		{
			ID:       "Lakes.2",
			TypeName: "mock:Lakes",
			Properties: map[string]interface{}{
				"FID":   "2",
				"NAME":  "Lake Michigan",
				"DEPTH": 281.0,
			},
			Geometry: `<gml:Polygon srsName="urn:ogc:def:crs:EPSG::4326" gml:id="Lakes.2.geom">
				<gml:exterior>
					<gml:LinearRing>
						<gml:posList>-88.0 41.0 -86.0 41.0 -86.0 46.0 -88.0 46.0 -88.0 41.0</gml:posList>
					</gml:LinearRing>
				</gml:exterior>
			</gml:Polygon>`,
		},
		// NamedPlaces
		{
			ID:       "NamedPlaces.1",
			TypeName: "mock:NamedPlaces",
			Properties: map[string]interface{}{
				"FID":  "1",
				"NAME": "New York",
			},
			Geometry: `<gml:Point srsName="urn:ogc:def:crs:EPSG::4326" gml:id="NamedPlaces.1.geom">
				<gml:pos>-74.0060 40.7128</gml:pos>
			</gml:Point>`,
		},
		{
			ID:       "NamedPlaces.2",
			TypeName: "mock:NamedPlaces",
			Properties: map[string]interface{}{
				"FID":  "2",
				"NAME": "Los Angeles",
			},
			Geometry: `<gml:Point srsName="urn:ogc:def:crs:EPSG::4326" gml:id="NamedPlaces.2.geom">
				<gml:pos>-118.2437 34.0522</gml:pos>
			</gml:Point>`,
		},
		{
			ID:       "NamedPlaces.3",
			TypeName: "mock:NamedPlaces",
			Properties: map[string]interface{}{
				"FID":  "3",
				"NAME": "Chicago",
			},
			Geometry: `<gml:Point srsName="urn:ogc:def:crs:EPSG::4326" gml:id="NamedPlaces.3.geom">
				<gml:pos>-87.6298 41.8781</gml:pos>
			</gml:Point>`,
		},
		// Buildings
		{
			ID:       "Buildings.1",
			TypeName: "mock:Buildings",
			Properties: map[string]interface{}{
				"FID":    "1",
				"NAME":   "City Hall",
				"HEIGHT": 50.0,
				"FLOORS": 10,
				"BUILT":  "1950-01-15",
			},
			Geometry: `<gml:Polygon srsName="urn:ogc:def:crs:EPSG::4326" gml:id="Buildings.1.geom">
				<gml:exterior>
					<gml:LinearRing>
						<gml:posList>-95.0 40.0 -94.99 40.0 -94.99 40.01 -95.0 40.01 -95.0 40.0</gml:posList>
					</gml:LinearRing>
				</gml:exterior>
			</gml:Polygon>`,
		},
		{
			ID:       "Buildings.2",
			TypeName: "mock:Buildings",
			Properties: map[string]interface{}{
				"FID":    "2",
				"NAME":   "Library",
				"HEIGHT": 25.0,
				"FLOORS": 5,
				"BUILT":  "1985-06-20",
			},
			Geometry: `<gml:Polygon srsName="urn:ogc:def:crs:EPSG::4326" gml:id="Buildings.2.geom">
				<gml:exterior>
					<gml:LinearRing>
						<gml:posList>-95.1 40.1 -95.09 40.1 -95.09 40.11 -95.1 40.11 -95.1 40.1</gml:posList>
					</gml:LinearRing>
				</gml:exterior>
			</gml:Polygon>`,
		},
	}
}

// GetFeatureType finds a feature type by name.
func GetFeatureType(name string) *MockFeatureType {
	for _, ft := range DefaultFeatureTypes() {
		if ft.Name == name {
			return &ft
		}
	}
	return nil
}

// GetFeaturesForType returns all features of the given type.
func GetFeaturesForType(typeName string) []MockFeature {
	var result []MockFeature
	for _, f := range DefaultFeatures() {
		if f.TypeName == typeName {
			result = append(result, f)
		}
	}
	return result
}

// GetFeatureByID finds a feature by its ID.
func GetFeatureByID(id string) *MockFeature {
	for _, f := range DefaultFeatures() {
		if f.ID == id {
			return &f
		}
	}
	return nil
}
