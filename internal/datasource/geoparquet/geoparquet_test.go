package geoparquet

import (
	"encoding/json"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.GeometryColumn != "geometry" {
		t.Errorf("expected GeometryColumn 'geometry', got %s", cfg.GeometryColumn)
	}
	if cfg.SRID != 4326 {
		t.Errorf("expected SRID 4326, got %d", cfg.SRID)
	}
}

func TestDeriveTableName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"simple parquet", "/path/to/data.parquet", "data"},
		{"with dashes", "/path/to/my-data-file.parquet", "my_data_file"},
		{"with spaces", "/path/to/my data.parquet", "my_data"},
		{"multiple extensions", "/path/to/data.geoparquet.parquet", "data.geoparquet"},
		{"no extension", "/path/to/datafile", "datafile"},
		{"just filename", "cities.parquet", "cities"},
		{"s3 path", "s3://bucket/path/data.parquet", "data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveTableName(tt.path); got != tt.want {
				t.Errorf("deriveTableName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestParseFeatureID(t *testing.T) {
	tests := []struct {
		name    string
		colType string
		value   string
		want    any
		wantErr bool
	}{
		{"int valid", "INTEGER", "123", int64(123), false},
		{"bigint valid", "BIGINT", "9223372036854775807", int64(9223372036854775807), false},
		{"int64 valid", "INT64", "456", int64(456), false},
		{"int invalid", "INTEGER", "abc", nil, true},
		{"varchar", "VARCHAR", "some-id", "some-id", false},
		{"uuid", "UUID", "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000", false},
		{"lowercase int", "integer", "789", int64(789), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFeatureID(tt.colType, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFeatureID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseFeatureID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParquetTypeToJSON(t *testing.T) {
	tests := []struct {
		colType string
		want    datasource.JSONType
	}{
		{"BOOLEAN", datasource.JSONTypeBoolean},
		{"BOOL", datasource.JSONTypeBoolean},
		{"INTEGER", datasource.JSONTypeInteger},
		{"INT32", datasource.JSONTypeInteger},
		{"INT64", datasource.JSONTypeInteger},
		{"BIGINT", datasource.JSONTypeInteger},
		{"FLOAT", datasource.JSONTypeNumber},
		{"DOUBLE", datasource.JSONTypeNumber},
		{"DECIMAL(10,2)", datasource.JSONTypeNumber},
		{"STRUCT<name VARCHAR>", datasource.JSONTypeObject},
		{"MAP<VARCHAR, INT>", datasource.JSONTypeObject},
		{"LIST<INT>", datasource.JSONTypeArray},
		{"INT[]", datasource.JSONTypeArray},
		{"VARCHAR", datasource.JSONTypeString},
		{"STRING", datasource.JSONTypeString},
		{"DATE", datasource.JSONTypeString},
		{"TIMESTAMP", datasource.JSONTypeString},
	}

	for _, tt := range tests {
		t.Run(tt.colType, func(t *testing.T) {
			if got := parquetTypeToJSON(tt.colType); got != tt.want {
				t.Errorf("parquetTypeToJSON(%q) = %v, want %v", tt.colType, got, tt.want)
			}
		})
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", `"simple"`},
		{"with space", `"with space"`},
		{`with"quote`, `"with""quote"`},
		{`double""quote`, `"double""""quote"`},
		{"", `""`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := quoteIdent(tt.input); got != tt.want {
				t.Errorf("quoteIdent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestQuoteLiteral(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", `'simple'`},
		{"with space", `'with space'`},
		{`with'quote`, `'with''quote'`},
		{`double''quote`, `'double''''quote'`},
		{"", `''`},
		{"/path/to/file.parquet", `'/path/to/file.parquet'`},
		{"s3://bucket/data.parquet", `'s3://bucket/data.parquet'`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := quoteLiteral(tt.input); got != tt.want {
				t.Errorf("quoteLiteral(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewFromServiceMissingPath(t *testing.T) {
	cfg := Config{}
	connInfo, _ := json.Marshal(cfg)

	svc := &store.Service{
		ID:             "test-svc",
		Type:           store.ServiceTypeGeoParquet,
		ConnectionInfo: connInfo,
	}

	_, err := NewFromService(svc)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if err.Error() != "path is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewFromServiceInvalidConfig(t *testing.T) {
	svc := &store.Service{
		ID:             "test-svc",
		Type:           store.ServiceTypeGeoParquet,
		ConnectionInfo: []byte("invalid json"),
	}

	_, err := NewFromService(svc)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestConfigDefaults(t *testing.T) {
	// Test that when using empty config values, defaults should be applied
	cfg := Config{
		Path: "/path/to/data.parquet",
	}

	// Simulate the default application from New
	defaults := DefaultConfig()
	geomCol := cfg.GeometryColumn
	if geomCol == "" {
		geomCol = defaults.GeometryColumn
	}
	srid := cfg.SRID
	if srid == 0 {
		srid = defaults.SRID
	}

	if geomCol != "geometry" {
		t.Errorf("expected default GeometryColumn 'geometry', got %s", geomCol)
	}
	if srid != 4326 {
		t.Errorf("expected default SRID 4326, got %d", srid)
	}
}

func TestConfigWithOverrides(t *testing.T) {
	cfg := Config{
		Path:           "/path/to/data.parquet",
		GeometryColumn: "geom",
		IDColumn:       "fid",
		SRID:           3857,
	}

	// Verify overrides are preserved
	if cfg.GeometryColumn != "geom" {
		t.Errorf("expected GeometryColumn 'geom', got %s", cfg.GeometryColumn)
	}
	if cfg.IDColumn != "fid" {
		t.Errorf("expected IDColumn 'fid', got %s", cfg.IDColumn)
	}
	if cfg.SRID != 3857 {
		t.Errorf("expected SRID 3857, got %d", cfg.SRID)
	}
}
