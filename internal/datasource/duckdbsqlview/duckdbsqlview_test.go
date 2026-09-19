package duckdbsqlview

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/neoserver/internal/datasource"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("failed to open duckdb: %v", err)
	}

	// Load spatial extension
	_, err = db.Exec("INSTALL spatial; LOAD spatial;")
	if err != nil {
		db.Close()
		t.Fatalf("failed to load spatial extension: %v", err)
	}

	// Create a test table with geometry
	_, err = db.Exec(`
		CREATE TABLE test_points (
			id INTEGER PRIMARY KEY,
			name VARCHAR,
			value DOUBLE,
			geom GEOMETRY
		)
	`)
	if err != nil {
		db.Close()
		t.Fatalf("failed to create test table: %v", err)
	}

	// Insert test data
	_, err = db.Exec(`
		INSERT INTO test_points VALUES
		(1, 'Point A', 10.5, ST_Point(0, 0)),
		(2, 'Point B', 20.5, ST_Point(1, 1)),
		(3, 'Point C', 30.5, ST_Point(2, 2))
	`)
	if err != nil {
		db.Close()
		t.Fatalf("failed to insert test data: %v", err)
	}

	return db
}

func TestValidateSQLView(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	helper := NewHelper(db, DefaultTypeMapper)
	ctx := context.Background()

	tests := []struct {
		name    string
		sql     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid SELECT",
			sql:     "SELECT id, name, geom FROM test_points",
			wantErr: false,
		},
		{
			name:    "valid SELECT with WHERE",
			sql:     "SELECT id, name, geom FROM test_points WHERE value > 10",
			wantErr: false,
		},
		{
			name:    "INSERT not allowed (fails SELECT check first)",
			sql:     "INSERT INTO test_points VALUES (4, 'D', 40.0, ST_Point(3,3))",
			wantErr: true,
			errMsg:  "SELECT statement",
		},
		{
			name:    "UPDATE not allowed (fails SELECT check first)",
			sql:     "UPDATE test_points SET name = 'X' WHERE id = 1",
			wantErr: true,
			errMsg:  "SELECT statement",
		},
		{
			name:    "DELETE not allowed (fails SELECT check first)",
			sql:     "DELETE FROM test_points WHERE id = 1",
			wantErr: true,
			errMsg:  "SELECT statement",
		},
		{
			name:    "DROP not allowed (fails SELECT check first)",
			sql:     "DROP TABLE test_points",
			wantErr: true,
			errMsg:  "SELECT statement",
		},
		{
			name:    "CREATE not allowed (fails SELECT check first)",
			sql:     "CREATE TABLE new_table (id INT)",
			wantErr: true,
			errMsg:  "SELECT statement",
		},
		{
			name:    "WITH clause must start with SELECT",
			sql:     "WITH cte AS (SELECT 1) SELECT * FROM cte",
			wantErr: true,
			errMsg:  "SELECT statement",
		},
		{
			name:    "SELECT with INSERT keyword blocked",
			sql:     "SELECT * FROM test_points; INSERT INTO test_points VALUES (4, 'D', 40.0, NULL)",
			wantErr: true,
			errMsg:  "INSERT",
		},
		{
			name:    "SELECT with DELETE keyword blocked",
			sql:     "SELECT * FROM test_points; DELETE FROM test_points",
			wantErr: true,
			errMsg:  "DELETE",
		},
		{
			name:    "SELECT with DROP keyword blocked",
			sql:     "SELECT * FROM test_points; DROP TABLE test_points",
			wantErr: true,
			errMsg:  "DROP",
		},
		{
			name:    "read_csv blocked (local file read)",
			sql:     "SELECT * FROM read_csv('/etc/passwd')",
			wantErr: true,
			errMsg:  "read_csv",
		},
		{
			name:    "read_parquet over http blocked (SSRF)",
			sql:     "SELECT * FROM read_parquet('http://169.254.169.254/x')",
			wantErr: true,
			errMsg:  "read_parquet",
		},
		{
			name:    "read_text blocked",
			sql:     "SELECT content FROM read_text('/etc/hostname')",
			wantErr: true,
			errMsg:  "read_text",
		},
		{
			name:    "st_read blocked",
			sql:     "SELECT * FROM st_read('/etc/passwd')",
			wantErr: true,
			errMsg:  "st_read",
		},
		{
			name:    "remote URL literal blocked",
			sql:     "SELECT * FROM test_points WHERE name = 'https://evil.example.com/x'",
			wantErr: true,
			errMsg:  "remote URLs",
		},
		{
			name:    "created_at alias not a false positive for CREATE",
			sql:     "SELECT id, geom, value AS created_at FROM test_points",
			wantErr: false,
		},
		{
			name:    "invalid SQL syntax",
			sql:     "SELECT * FROM nonexistent_table",
			wantErr: true,
			errMsg:  "invalid SQL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := helper.ValidateSQLView(ctx, tt.sql)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				} else if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestDiscoverSQLViewColumns(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	helper := NewHelper(db, DefaultTypeMapper)
	ctx := context.Background()

	tests := []struct {
		name           string
		sql            string
		wantErr        bool
		wantGeomCol    string
		wantIDCol      string
		wantPropCount  int
	}{
		{
			name:          "discover all columns",
			sql:           "SELECT id, name, value, geom FROM test_points",
			wantErr:       false,
			wantGeomCol:   "geom",
			wantIDCol:     "id",
			wantPropCount: 3, // id, name, value
		},
		{
			name:          "discover subset of columns",
			sql:           "SELECT name, geom FROM test_points",
			wantErr:       false,
			wantGeomCol:   "geom",
			wantIDCol:     "",
			wantPropCount: 1, // name
		},
		{
			name:    "no geometry column",
			sql:     "SELECT id, name FROM test_points",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discovery, err := helper.DiscoverSQLViewColumns(ctx, tt.sql)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if discovery.GeometryColumn != tt.wantGeomCol {
				t.Errorf("geometry column = %q, want %q", discovery.GeometryColumn, tt.wantGeomCol)
			}
			if discovery.SuggestedIDColumn != tt.wantIDCol {
				t.Errorf("suggested ID column = %q, want %q", discovery.SuggestedIDColumn, tt.wantIDCol)
			}
			if len(discovery.Columns) != tt.wantPropCount {
				t.Errorf("property count = %d, want %d", len(discovery.Columns), tt.wantPropCount)
			}
		})
	}
}

func TestQuerySQLView(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	helper := NewHelper(db, DefaultTypeMapper)
	ctx := context.Background()

	config := &datasource.SQLViewConfig{
		SQL:            "SELECT id, name, value, geom FROM test_points",
		GeometryColumn: "geom",
		SRID:           4326,
		IDColumn:       "id",
		Properties: []*datasource.SQLViewProperty{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "VARCHAR"},
			{Name: "value", Type: "DOUBLE"},
		},
	}

	params := datasource.QueryParams{
		Limit:      10,
		Offset:     0,
		OutputSRID: 4326,
	}

	features, err := helper.QuerySQLView(ctx, config, params)
	if err != nil {
		t.Fatalf("QuerySQLView failed: %v", err)
	}

	if len(features) != 3 {
		t.Errorf("expected 3 features, got %d", len(features))
	}
}

func TestQuerySQLViewWithLimit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	helper := NewHelper(db, DefaultTypeMapper)
	ctx := context.Background()

	config := &datasource.SQLViewConfig{
		SQL:            "SELECT id, name, value, geom FROM test_points",
		GeometryColumn: "geom",
		SRID:           4326,
		IDColumn:       "id",
		Properties: []*datasource.SQLViewProperty{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "VARCHAR"},
			{Name: "value", Type: "DOUBLE"},
		},
	}

	params := datasource.QueryParams{
		Limit:      2,
		Offset:     0,
		OutputSRID: 4326,
	}

	features, err := helper.QuerySQLView(ctx, config, params)
	if err != nil {
		t.Fatalf("QuerySQLView failed: %v", err)
	}

	if len(features) != 2 {
		t.Errorf("expected 2 features, got %d", len(features))
	}
}

func TestQuerySQLViewWKB(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	helper := NewHelper(db, DefaultTypeMapper)
	ctx := context.Background()

	config := &datasource.SQLViewConfig{
		SQL:            "SELECT id, name, value, geom FROM test_points",
		GeometryColumn: "geom",
		SRID:           4326,
		IDColumn:       "id",
		Properties: []*datasource.SQLViewProperty{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "VARCHAR"},
			{Name: "value", Type: "DOUBLE"},
		},
	}

	params := datasource.QueryParams{
		OutputSRID: 4326,
	}

	features, err := helper.QuerySQLViewWKB(ctx, config, params)
	if err != nil {
		t.Fatalf("QuerySQLViewWKB failed: %v", err)
	}

	if len(features) != 3 {
		t.Errorf("expected 3 features, got %d", len(features))
	}

	// Check that each feature has geometry bytes
	for i, f := range features {
		if len(f.Geometry) == 0 {
			t.Errorf("feature %d has empty geometry", i)
		}
	}
}

func TestCountSQLView(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	helper := NewHelper(db, DefaultTypeMapper)
	ctx := context.Background()

	config := &datasource.SQLViewConfig{
		SQL:            "SELECT id, name, value, geom FROM test_points",
		GeometryColumn: "geom",
		SRID:           4326,
		IDColumn:       "id",
	}

	params := datasource.QueryParams{}

	count, err := helper.CountSQLView(ctx, config, params)
	if err != nil {
		t.Fatalf("CountSQLView failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestCountSQLViewWithBBox(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	helper := NewHelper(db, DefaultTypeMapper)
	ctx := context.Background()

	config := &datasource.SQLViewConfig{
		SQL:            "SELECT id, name, value, geom FROM test_points",
		GeometryColumn: "geom",
		SRID:           4326,
		IDColumn:       "id",
	}

	// BBox that only includes the first point (0,0)
	params := datasource.QueryParams{
		BBox: &datasource.BBox{
			MinX: -0.5,
			MinY: -0.5,
			MaxX: 0.5,
			MaxY: 0.5,
		},
		BBoxSRID: 4326,
	}

	count, err := helper.CountSQLView(ctx, config, params)
	if err != nil {
		t.Fatalf("CountSQLView failed: %v", err)
	}

	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
}

func TestDefaultTypeMapper(t *testing.T) {
	tests := []struct {
		colType  string
		expected datasource.JSONType
	}{
		{"BOOLEAN", datasource.JSONTypeBoolean},
		{"bool", datasource.JSONTypeBoolean},
		{"INTEGER", datasource.JSONTypeInteger},
		{"BIGINT", datasource.JSONTypeInteger},
		{"int", datasource.JSONTypeInteger},
		{"FLOAT", datasource.JSONTypeNumber},
		{"DOUBLE", datasource.JSONTypeNumber},
		{"DECIMAL", datasource.JSONTypeNumber},
		{"numeric", datasource.JSONTypeNumber},
		{"JSON", datasource.JSONTypeObject},
		{"STRUCT", datasource.JSONTypeObject},
		{"MAP", datasource.JSONTypeObject},
		{"INTEGER[]", datasource.JSONTypeArray},
		{"LIST", datasource.JSONTypeArray},
		{"VARCHAR", datasource.JSONTypeString},
		{"TEXT", datasource.JSONTypeString},
		{"unknown", datasource.JSONTypeString},
	}

	for _, tt := range tests {
		t.Run(tt.colType, func(t *testing.T) {
			result := DefaultTypeMapper(tt.colType)
			if result != tt.expected {
				t.Errorf("DefaultTypeMapper(%q) = %v, want %v", tt.colType, result, tt.expected)
			}
		})
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", `"simple"`},
		{"with space", `"with space"`},
		{`with"quote`, `"with""quote"`},
		{"CamelCase", `"CamelCase"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := QuoteIdent(tt.input)
			if result != tt.expected {
				t.Errorf("QuoteIdent(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
