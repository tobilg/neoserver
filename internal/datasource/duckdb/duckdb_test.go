package duckdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/store"
)

// TestMain configures a permissive datasource allowlist so tests can construct
// data sources from temp-file paths. Allowlist enforcement itself is covered by
// the pathpolicy package tests.
func TestMain(m *testing.M) {
	pathpolicy.Configure([]string{"**"})
	// The data source only LOADs spatial; the server INSTALLs it at startup.
	// These tests open DuckDB directly, so without this they pass only where an
	// earlier run happened to populate the extension cache.
	if err := datasource.PreloadExtensions(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		fmt.Fprintf(os.Stderr, "preload duckdb extensions: %v\n", err)
	}
	os.Exit(m.Run())
}

// createTestDuckDB creates a test DuckDB database with spatial data.
func createTestDuckDB(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "duckdb-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create database and add test data - use spatial extension
	ds, err := New("test", Config{Path: dbPath, ReadOnly: false, Extensions: []string{"spatial"}})
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create duckdb: %v", err)
	}

	// Create a test table with geometry
	_, err = ds.db.Exec(`
		CREATE SCHEMA __neoserver;
		CREATE TABLE __neoserver.layers (name VARCHAR PRIMARY KEY, srid INTEGER);
		INSERT INTO __neoserver.layers VALUES ('test_points', 4326);
		CREATE TABLE test_points (
			id INTEGER PRIMARY KEY,
			name VARCHAR,
			value DOUBLE,
			observed TIMESTAMPTZ,
			geom GEOMETRY
		)
	`)
	if err != nil {
		ds.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert test data
	_, err = ds.db.Exec(`
		INSERT INTO test_points (id, name, value, observed, geom) VALUES
		(1, 'Point A', 10.5, TIMESTAMPTZ '2020-01-01T00:00:00Z', ST_Point(0, 0)),
		(2, 'Point B', 20.5, TIMESTAMPTZ '2020-01-02T00:00:00Z', ST_Point(1, 1)),
		(3, 'Point C', 30.5, NULL, ST_Point(2, 2))
	`)
	if err != nil {
		ds.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to insert data: %v", err)
	}

	ds.Close()

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return dbPath, cleanup
}

func TestNewDataSource(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test-id", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	if ds.Type() != store.ServiceTypeDuckDB {
		t.Errorf("expected type %s, got %s", store.ServiceTypeDuckDB, ds.Type())
	}

	if ds.ID() != "test-id" {
		t.Errorf("expected id 'test-id', got %s", ds.ID())
	}
}

func TestNewFromService(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	connInfo, _ := json.Marshal(Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	svc := &store.Service{
		ID:             "svc-1",
		Type:           store.ServiceTypeDuckDB,
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

func TestHealth(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	if err := ds.Health(ctx); err != nil {
		t.Errorf("Health check failed: %v", err)
	}
}

func TestDiscoverLayers(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
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

	found := false
	for _, l := range layers {
		if l.Name == "test_points" {
			found = true
			if l.GeometryColumn != "geom" {
				t.Errorf("expected geometry column 'geom', got %s", l.GeometryColumn)
			}
			break
		}
	}
	if !found {
		t.Error("test_points layer not found")
	}
}

func TestQuery(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	// Cache layer info
	ctx := context.Background()
	_, err = ds.DiscoverLayers(ctx)
	if err != nil {
		t.Fatalf("DiscoverLayers failed: %v", err)
	}

	// Query all features
	features, err := ds.Query(ctx, "test_points", datasource.QueryParams{
		Limit:      10,
		OutputSRID: 4326,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(features) != 3 {
		t.Errorf("expected 3 features, got %d", len(features))
	}

	// Verify first feature structure
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

func TestQuery_DateTimeFilter(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()
	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	instant := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	params := datasource.QueryParams{Limit: 10, OutputSRID: 4326, DateTime: &datasource.DateTimeFilter{SourceProperty: "observed", Start: &instant, End: &instant, Instant: true}}
	features, err := ds.Query(context.Background(), "test_points", params)
	if err != nil {
		t.Fatalf("datetime query: %v", err)
	}
	// The exact instant plus the feature without a temporal association match.
	if len(features) != 2 {
		t.Fatalf("got %d features, want 2", len(features))
	}
}

func TestQueryWithLimit(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	ds.DiscoverLayers(ctx)

	features, err := ds.Query(ctx, "test_points", datasource.QueryParams{
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
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	ds.DiscoverLayers(ctx)

	features, err := ds.Query(ctx, "test_points", datasource.QueryParams{
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
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	ds.DiscoverLayers(ctx)

	feature, found, err := ds.QueryByID(ctx, "test_points", "1", 4326)
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

	props := feat["properties"].(map[string]interface{})
	if props["name"] != "Point A" {
		t.Errorf("expected name 'Point A', got %v", props["name"])
	}
}

func TestQueryByIDNotFound(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	ds.DiscoverLayers(ctx)

	_, found, err := ds.QueryByID(ctx, "test_points", "999", 4326)
	if err != nil {
		t.Fatalf("QueryByID failed: %v", err)
	}
	if found {
		t.Error("expected not to find feature")
	}
}

func TestCount(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	ds.DiscoverLayers(ctx)

	count, err := ds.Count(ctx, "test_points", datasource.QueryParams{})
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestGetLayerInfo(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	ds.DiscoverLayers(ctx)

	info, err := ds.GetLayerInfo(ctx, "test_points")
	if err != nil {
		t.Fatalf("GetLayerInfo failed: %v", err)
	}

	if info.Name != "test_points" {
		t.Errorf("expected name 'test_points', got %s", info.Name)
	}
	if info.GeometryColumn != "geom" {
		t.Errorf("expected geometry column 'geom', got %s", info.GeometryColumn)
	}
}

func TestLayerNotFound(t *testing.T) {
	dbPath, cleanup := createTestDuckDB(t)
	defer cleanup()

	ds, err := New("test", Config{Path: dbPath, ReadOnly: true, Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatalf("failed to create datasource: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()

	_, err = ds.GetLayerInfo(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent layer")
	}
	if _, ok := err.(datasource.LayerNotFoundError); !ok {
		t.Errorf("expected LayerNotFoundError, got %T", err)
	}
}
