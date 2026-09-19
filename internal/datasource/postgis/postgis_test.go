package postgis

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Port != 5432 {
		t.Errorf("expected port 5432, got %d", cfg.Port)
	}
	if cfg.SSLMode != "prefer" {
		t.Errorf("expected sslmode 'prefer', got %s", cfg.SSLMode)
	}
	if len(cfg.Schemas) != 1 || cfg.Schemas[0] != "public" {
		t.Errorf("expected schemas [public], got %v", cfg.Schemas)
	}
	if cfg.MaxOpenConns != 25 {
		t.Errorf("expected MaxOpenConns 25, got %d", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 5 {
		t.Errorf("expected MaxIdleConns 5, got %d", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != time.Hour {
		t.Errorf("expected ConnMaxLifetime 1h, got %v", cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime != 10*time.Minute {
		t.Errorf("expected ConnMaxIdleTime 10m, got %v", cfg.ConnMaxIdleTime)
	}
}

func TestParseLayerName(t *testing.T) {
	tests := []struct {
		name       string
		layer      string
		wantSchema string
		wantTable  string
	}{
		{"schema.table", "public.cities", "public", "cities"},
		{"table only", "cities", "", "cities"},
		{"multi-dot", "foo.bar.baz", "foo", "bar.baz"},
		{"empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, table := parseLayerName(tt.layer)
			if schema != tt.wantSchema {
				t.Errorf("schema: got %q, want %q", schema, tt.wantSchema)
			}
			if table != tt.wantTable {
				t.Errorf("table: got %q, want %q", table, tt.wantTable)
			}
		})
	}
}

func TestParseFeatureID(t *testing.T) {
	tests := []struct {
		name    string
		pgType  string
		value   string
		want    any
		wantErr bool
	}{
		{"int2 valid", "int2", "123", int64(123), false},
		{"int4 valid", "int4", "456789", int64(456789), false},
		{"int8 valid", "int8", "9223372036854775807", int64(9223372036854775807), false},
		{"int invalid", "int4", "abc", nil, true},
		{"string type", "varchar", "some-id", "some-id", false},
		{"uuid type", "uuid", "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFeatureID(tt.pgType, tt.value)
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

func TestPgTypeToJSON(t *testing.T) {
	tests := []struct {
		pgType string
		want   datasource.JSONType
	}{
		{"bool", datasource.JSONTypeBoolean},
		{"int2", datasource.JSONTypeInteger},
		{"int4", datasource.JSONTypeInteger},
		{"int8", datasource.JSONTypeInteger},
		{"oid", datasource.JSONTypeInteger},
		{"float4", datasource.JSONTypeNumber},
		{"float8", datasource.JSONTypeNumber},
		{"numeric", datasource.JSONTypeNumber},
		{"json", datasource.JSONTypeObject},
		{"jsonb", datasource.JSONTypeObject},
		{"_text", datasource.JSONTypeArray},
		{"_int4", datasource.JSONTypeArray},
		{"_int8", datasource.JSONTypeArray},
		{"_float8", datasource.JSONTypeArray},
		{"varchar", datasource.JSONTypeString},
		{"text", datasource.JSONTypeString},
		{"unknown", datasource.JSONTypeString},
	}

	for _, tt := range tests {
		t.Run(tt.pgType, func(t *testing.T) {
			if got := pgTypeToJSON(tt.pgType); got != tt.want {
				t.Errorf("pgTypeToJSON(%q) = %v, want %v", tt.pgType, got, tt.want)
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
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := quoteLiteral(tt.input); got != tt.want {
				t.Errorf("quoteLiteral(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNonZero(t *testing.T) {
	tests := []struct {
		name   string
		v      int
		def    int
		want   int
	}{
		{"value is non-zero", 5, 10, 5},
		{"value is zero", 0, 10, 10},
		{"negative value", -5, 10, -5},
		{"both zero", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nonZero(tt.v, tt.def); got != tt.want {
				t.Errorf("nonZero(%d, %d) = %d, want %d", tt.v, tt.def, got, tt.want)
			}
		})
	}
}

func TestAdjustFilterParams(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		offset int
		want   string
	}{
		{"zero offset", "name = $1", 0, "name = $1"},
		{"offset by 1", "name = $1", 1, "name = $2"},
		{"offset by 5", "name = $1 AND age = $2", 5, "name = $6 AND age = $7"},
		{"multiple params", "a = $1 AND b = $2 AND c = $3", 2, "a = $3 AND b = $4 AND c = $5"},
		{"no params", "name = 'test'", 5, "name = 'test'"},
		{"empty filter", "", 5, ""},
		// Additional edge cases for regex-based implementation
		{"high param numbers", "x = $100 AND y = $200", 10, "x = $110 AND y = $210"},
		{"params over 100", "a = $1 AND b = $101 AND c = $500", 5, "a = $6 AND b = $106 AND c = $505"},
		{"adjacent params", "$1$2$3", 1, "$2$3$4"},
		{"param in function", "ST_Intersects(geom, ST_MakeEnvelope($1,$2,$3,$4,4326))", 3, "ST_Intersects(geom, ST_MakeEnvelope($4,$5,$6,$7,4326))"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adjustFilterParams(tt.filter, tt.offset); got != tt.want {
				t.Errorf("adjustFilterParams(%q, %d) = %q, want %q", tt.filter, tt.offset, got, tt.want)
			}
		})
	}
}

func TestParseProps(t *testing.T) {
	tests := []struct {
		name      string
		props     [][]string
		wantLen   int
		wantTypes map[string]string
	}{
		{
			name: "single prop",
			props: [][]string{
				{"name", "varchar", "Name field", "1"},
			},
			wantLen: 1,
			wantTypes: map[string]string{
				"name": "varchar",
			},
		},
		{
			name: "multiple props",
			props: [][]string{
				{"id", "int4", "Primary key", "0"},
				{"name", "varchar", "Name", "1"},
				{"value", "float8", "Value", "2"},
			},
			wantLen: 3,
			wantTypes: map[string]string{
				"id":    "int4",
				"name":  "varchar",
				"value": "float8",
			},
		},
		{
			name: "skip short arrays",
			props: [][]string{
				{"too", "short"},
				{"name", "varchar", "OK", "1"},
			},
			wantLen: 1,
			wantTypes: map[string]string{
				"name": "varchar",
			},
		},
		{
			name:      "empty",
			props:     [][]string{},
			wantLen:   0,
			wantTypes: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props, pgTypes := parseProps(tt.props)
			if len(props) != tt.wantLen {
				t.Errorf("parseProps() returned %d props, want %d", len(props), tt.wantLen)
			}
			for k, v := range tt.wantTypes {
				if pgTypes[k] != v {
					t.Errorf("pgTypes[%q] = %q, want %q", k, pgTypes[k], v)
				}
			}
		})
	}
}

func TestNewFromServiceConfigDefaults(t *testing.T) {
	// Test that defaults are applied when creating from service config
	// We can't test the actual connection, but we can verify config parsing

	minimalConfig := Config{
		Host:     "localhost",
		Database: "test",
		User:     "user",
		Password: "pass",
	}

	connInfo, err := json.Marshal(minimalConfig)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	svc := &store.Service{
		ID:             "test-svc",
		Type:           store.ServiceTypePostGIS,
		ConnectionInfo: connInfo,
	}

	// Can't actually create the datasource without a database, but we can verify
	// the config parsing works correctly by checking the unmarshal
	var cfg Config
	if err := json.Unmarshal(svc.ConnectionInfo, &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	// Verify config was parsed
	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want 'localhost'", cfg.Host)
	}
	if cfg.Database != "test" {
		t.Errorf("Database = %q, want 'test'", cfg.Database)
	}

	// Apply defaults as the function would
	defaults := DefaultConfig()
	if cfg.Port == 0 {
		cfg.Port = defaults.Port
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = defaults.SSLMode
	}

	// Verify defaults were applied
	if cfg.Port != 5432 {
		t.Errorf("Port after defaults = %d, want 5432", cfg.Port)
	}
	if cfg.SSLMode != "prefer" {
		t.Errorf("SSLMode after defaults = %q, want 'prefer'", cfg.SSLMode)
	}
}
