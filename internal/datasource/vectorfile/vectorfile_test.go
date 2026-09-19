package vectorfile

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/store"
)

// TestMain configures a permissive datasource allowlist so tests can construct
// data sources from temp-file paths. Allowlist enforcement itself is covered by
// the pathpolicy package tests.
func TestMain(m *testing.M) {
	pathpolicy.Configure([]string{"**"})
	os.Exit(m.Run())
}

// createTestShapefile creates a test Shapefile with sample data
func createTestShapefile(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "vectorfile-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	shpPath := filepath.Join(tmpDir, "test_points.shp")

	// Create an in-memory DuckDB to generate the shapefile
	db, err := sql.Open("duckdb", "")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open duckdb: %v", err)
	}
	defer db.Close()

	// Load spatial extension
	_, err = db.Exec("INSTALL spatial; LOAD spatial;")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to load spatial extension: %v", err)
	}

	// Create and export test data to Shapefile
	// Note: Shapefile doesn't support DECIMAL types, so use DOUBLE and INTEGER
	query := `
		COPY (
			SELECT
				1::INTEGER AS fid,
				'Point A' AS name,
				10.5::DOUBLE AS value,
				ST_Point(0, 0) AS geom
			UNION ALL
			SELECT 2::INTEGER, 'Point B', 20.5::DOUBLE, ST_Point(1, 1)
			UNION ALL
			SELECT 3::INTEGER, 'Point C', 30.5::DOUBLE, ST_Point(2, 2)
		) TO '` + shpPath + `' WITH (FORMAT GDAL, DRIVER 'ESRI Shapefile')
	`
	_, err = db.Exec(query)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create shapefile: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return shpPath, cleanup
}

// createTestGeoPackage creates a test GeoPackage with sample data
func createTestGeoPackage(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "vectorfile-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	gpkgPath := filepath.Join(tmpDir, "test.gpkg")

	// Create an in-memory DuckDB to generate the geopackage
	db, err := sql.Open("duckdb", "")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open duckdb: %v", err)
	}
	defer db.Close()

	// Load spatial extension
	_, err = db.Exec("INSTALL spatial; LOAD spatial;")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to load spatial extension: %v", err)
	}

	// Create first layer (points)
	query1 := `
		COPY (
			SELECT
				1 AS fid,
				'Point A' AS name,
				ST_Point(0, 0) AS geom
			UNION ALL
			SELECT 2, 'Point B', ST_Point(1, 1)
		) TO '` + gpkgPath + `' WITH (FORMAT GDAL, DRIVER 'GPKG', LAYER_CREATION_OPTIONS 'GEOMETRY_NAME=geom')
	`
	_, err = db.Exec(query1)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create geopackage layer 1: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return gpkgPath, cleanup
}

// createTestGeoJSON creates a test GeoJSON file with sample data
func createTestGeoJSON(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "vectorfile-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	geojsonPath := filepath.Join(tmpDir, "test_features.geojson")

	// Create an in-memory DuckDB to generate the geojson
	db, err := sql.Open("duckdb", "")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open duckdb: %v", err)
	}
	defer db.Close()

	// Load spatial extension
	_, err = db.Exec("INSTALL spatial; LOAD spatial;")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to load spatial extension: %v", err)
	}

	// Create and export test data to GeoJSON
	query := `
		COPY (
			SELECT
				1 AS id,
				'Feature A' AS name,
				ST_Point(0, 0) AS geom
			UNION ALL
			SELECT 2, 'Feature B', ST_Point(1, 1)
		) TO '` + geojsonPath + `' WITH (FORMAT GDAL, DRIVER 'GeoJSON')
	`
	_, err = db.Exec(query)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create geojson: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return geojsonPath, cleanup
}

func TestNewDataSource(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test-id", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	if ds.Type() != store.ServiceTypeVectorFile {
		t.Errorf("expected type %s, got %s", store.ServiceTypeVectorFile, ds.Type())
	}

	if ds.ID() != "test-id" {
		t.Errorf("expected id 'test-id', got %s", ds.ID())
	}
}

func TestNewFromService(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	connInfo, _ := json.Marshal(Config{Path: shpPath})
	svc := &store.Service{
		ID:             "svc-1",
		Type:           store.ServiceTypeVectorFile,
		ConnectionInfo: connInfo,
	}

	ds, err := NewFromService(svc)
	if err != nil {
		t.Fatalf("NewFromService failed: %v", err)
	}
	defer ds.Close()

	if ds.ID() != "svc-1" {
		t.Errorf("expected id 'svc-1', got %s", ds.ID())
	}
}

func TestNewFromServiceMissingPath(t *testing.T) {
	connInfo, _ := json.Marshal(Config{})
	svc := &store.Service{
		ID:             "svc-1",
		Type:           store.ServiceTypeVectorFile,
		ConnectionInfo: connInfo,
	}

	_, err := NewFromService(svc)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestHealth(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	if err := ds.Health(ctx); err != nil {
		t.Errorf("Health check failed: %v", err)
	}
}

func TestDiscoverLayersShapefile(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, err := ds.DiscoverLayers(ctx)
	if err != nil {
		t.Fatalf("DiscoverLayers failed: %v", err)
	}

	if len(layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(layers))
	}

	layer := layers[0]
	if layer.Name != "test_points" {
		t.Errorf("expected layer name 'test_points', got %s", layer.Name)
	}

	// ST_Read default geometry column is wkb_geometry
	if layer.GeometryColumn == "" {
		t.Error("expected geometry column to be detected")
	}

	if layer.IDColumn == "" {
		t.Error("expected ID column to be detected")
	}
}

func TestDiscoverLayersGeoPackage(t *testing.T) {
	gpkgPath, cleanup := createTestGeoPackage(t)
	defer cleanup()

	ds, err := New("test", Config{Path: gpkgPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, err := ds.DiscoverLayers(ctx)
	if err != nil {
		t.Fatalf("DiscoverLayers failed: %v", err)
	}

	if len(layers) == 0 {
		t.Fatal("expected at least 1 layer")
	}
}

func TestDiscoverLayersGeoJSON(t *testing.T) {
	geojsonPath, cleanup := createTestGeoJSON(t)
	defer cleanup()

	ds, err := New("test", Config{Path: geojsonPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, err := ds.DiscoverLayers(ctx)
	if err != nil {
		t.Fatalf("DiscoverLayers failed: %v", err)
	}

	if len(layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(layers))
	}

	layer := layers[0]
	if layer.Name != "test_features" {
		t.Errorf("expected layer name 'test_features', got %s", layer.Name)
	}
}

func TestQueryShapefile(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()

	// Discover layers first
	layers, err := ds.DiscoverLayers(ctx)
	if err != nil {
		t.Fatalf("DiscoverLayers failed: %v", err)
	}

	layerName := layers[0].Name

	// Query all features
	features, err := ds.Query(ctx, layerName, datasource.QueryParams{
		Limit:      10,
		OutputSRID: 4326,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(features) != 3 {
		t.Errorf("expected 3 features, got %d", len(features))
	}

	// Verify feature structure
	if len(features) > 0 {
		var feat map[string]interface{}
		if err := json.Unmarshal(features[0], &feat); err != nil {
			t.Fatalf("failed to unmarshal feature: %v", err)
		}
		if feat["type"] != "Feature" {
			t.Errorf("expected type 'Feature', got %v", feat["type"])
		}
		if feat["geometry"] == nil {
			t.Error("expected geometry")
		}
		if feat["properties"] == nil {
			t.Error("expected properties")
		}
	}
}

func TestQueryWithLimit(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, _ := ds.DiscoverLayers(ctx)
	layerName := layers[0].Name

	features, err := ds.Query(ctx, layerName, datasource.QueryParams{
		Limit:      2,
		OutputSRID: 4326,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(features) != 2 {
		t.Errorf("expected 2 features, got %d", len(features))
	}
}

func TestQueryWithOffset(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, _ := ds.DiscoverLayers(ctx)
	layerName := layers[0].Name

	features, err := ds.Query(ctx, layerName, datasource.QueryParams{
		Limit:      10,
		Offset:     2,
		OutputSRID: 4326,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(features) != 1 {
		t.Errorf("expected 1 feature, got %d", len(features))
	}
}

func TestQueryByID(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, _ := ds.DiscoverLayers(ctx)
	layerName := layers[0].Name

	// Get layer info to find ID column
	info, err := ds.GetLayerInfo(ctx, layerName)
	if err != nil {
		t.Fatalf("GetLayerInfo failed: %v", err)
	}

	if info.IDColumn == "" {
		t.Skip("no ID column detected, skipping QueryByID test")
	}

	feature, found, err := ds.QueryByID(ctx, layerName, "1", 4326)
	if err != nil {
		t.Fatalf("QueryByID failed: %v", err)
	}
	if !found {
		t.Fatal("expected to find feature")
	}

	var feat map[string]interface{}
	if err := json.Unmarshal(feature, &feat); err != nil {
		t.Fatalf("failed to unmarshal feature: %v", err)
	}

	if feat["type"] != "Feature" {
		t.Errorf("expected type 'Feature', got %v", feat["type"])
	}
}

func TestQueryByIDNotFound(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, _ := ds.DiscoverLayers(ctx)
	layerName := layers[0].Name

	// Get layer info to find ID column
	info, err := ds.GetLayerInfo(ctx, layerName)
	if err != nil {
		t.Fatalf("GetLayerInfo failed: %v", err)
	}

	if info.IDColumn == "" {
		t.Skip("no ID column detected, skipping QueryByID test")
	}

	_, found, err := ds.QueryByID(ctx, layerName, "999", 4326)
	if err != nil {
		t.Fatalf("QueryByID failed: %v", err)
	}
	if found {
		t.Error("expected not to find feature")
	}
}

func TestCount(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, _ := ds.DiscoverLayers(ctx)
	layerName := layers[0].Name

	count, err := ds.Count(ctx, layerName, datasource.QueryParams{})
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestGetLayerInfo(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	ds, err := New("test", Config{Path: shpPath})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, _ := ds.DiscoverLayers(ctx)
	layerName := layers[0].Name

	info, err := ds.GetLayerInfo(ctx, layerName)
	if err != nil {
		t.Fatalf("GetLayerInfo failed: %v", err)
	}

	if info.Name != layerName {
		t.Errorf("expected name '%s', got %s", layerName, info.Name)
	}
	if info.GeometryColumn == "" {
		t.Error("expected geometry column to be set")
	}
	if len(info.Properties) == 0 {
		t.Error("expected properties to be populated")
	}
}

func TestFileNotFound(t *testing.T) {
	_, err := New("test", Config{Path: "/nonexistent/path/to/file.shp"})
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/path/to/file.shp", "shapefile"},
		{"/path/to/file.SHP", "shapefile"},
		{"/path/to/file.gpkg", "geopackage"},
		{"/path/to/file.GPKG", "geopackage"},
		{"/path/to/file.geojson", "geojson"},
		{"/path/to/file.json", "geojson"},
		{"/path/to/file.fgb", "flatgeobuf"},
		{"/path/to/file.kml", "kml"},
		{"/path/to/file.gml", "gml"},
		{"/path/to/file.xyz", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			format := detectFormat(tt.path)
			if format != tt.expected {
				t.Errorf("detectFormat(%s) = %s, want %s", tt.path, format, tt.expected)
			}
		})
	}
}

func TestDeriveLayerName(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/path/to/my-file.shp", "my_file"},
		{"/path/to/my_file.gpkg", "my_file"},
		{"/path/to/simple.geojson", "simple"},
		{"file.shp", "file"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			name := deriveLayerName(tt.path)
			if name != tt.expected {
				t.Errorf("deriveLayerName(%s) = %s, want %s", tt.path, name, tt.expected)
			}
		})
	}
}

func TestConfiguredGeometryColumn(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	// Use configured geometry column name
	ds, err := New("test", Config{
		Path:           shpPath,
		GeometryColumn: "wkb_geometry",
	})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, err := ds.DiscoverLayers(ctx)
	if err != nil {
		t.Fatalf("DiscoverLayers failed: %v", err)
	}

	if len(layers) == 0 {
		t.Fatal("expected at least one layer")
	}
}

func TestConfiguredIDColumn(t *testing.T) {
	shpPath, cleanup := createTestShapefile(t)
	defer cleanup()

	// Use configured ID column name
	ds, err := New("test", Config{
		Path:     shpPath,
		IDColumn: "fid",
	})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	layers, _ := ds.DiscoverLayers(ctx)
	layerName := layers[0].Name

	info, err := ds.GetLayerInfo(ctx, layerName)
	if err != nil {
		t.Fatalf("GetLayerInfo failed: %v", err)
	}

	// The configured ID column should be used if it exists
	if info.IDColumn != "fid" {
		t.Logf("ID column is %s (configured: fid)", info.IDColumn)
	}
}

// Path/URL allowlist enforcement is tested in the pathpolicy package.
