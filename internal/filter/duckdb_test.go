package filter

import (
	"strings"
	"testing"
)

func TestCompileForDuckDB_ParameterizedStrings(t *testing.T) {
	sql, args, next, err := CompileForDuckDB("name = 'bob' AND age >= 21", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"name": {},
			"age":  {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if next != 3 {
		t.Fatalf("next=%d, want 3", next)
	}
	if len(args) != 2 {
		t.Fatalf("args=%v, want 2 args", args)
	}
	if sql == "" {
		t.Fatalf("expected SQL")
	}
	// Check placeholders exist
	if !strings.Contains(sql, "$1") || !strings.Contains(sql, "$2") {
		t.Fatalf("unexpected sql: %s", sql)
	}
}

func TestCompileForDuckDB_SpatialPoint(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("INTERSECTS(geom, POINT(1 2))", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"geom": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("args=%v, want 1 arg", args)
	}
	wkt, ok := args[0].(string)
	if !ok {
		t.Fatalf("expected wkt string, got %T", args[0])
	}
	if wkt != "POINT(1 2)" {
		t.Fatalf("unexpected wkt: %q", wkt)
	}
	// DuckDB uses ST_GeomFromText instead of ::geometry cast
	if !strings.Contains(sql, "ST_Intersects") {
		t.Fatalf("expected ST_Intersects in sql: %s", sql)
	}
	if !strings.Contains(sql, "ST_GeomFromText") {
		t.Fatalf("expected ST_GeomFromText in sql: %s", sql)
	}
}

func TestCompileForDuckDB_SpatialTransform(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("INTERSECTS(geom, POINT(1 2))", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      3857, // Different SRID to trigger transform
		AllowedProperties: map[string]struct{}{
			"geom": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("args=%v, want 1 arg", args)
	}
	// DuckDB uses ST_Transform with EPSG strings
	if !strings.Contains(sql, "ST_Transform") {
		t.Fatalf("expected ST_Transform in sql: %s", sql)
	}
	if !strings.Contains(sql, "EPSG:4326") || !strings.Contains(sql, "EPSG:3857") {
		t.Fatalf("expected EPSG strings in sql: %s", sql)
	}
}

func TestCompileForDuckDB_LikeOperator(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("name LIKE '%test%'", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"name": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("args=%v, want 1 arg", args)
	}
	if !strings.Contains(sql, "LIKE") {
		t.Fatalf("expected LIKE in sql: %s", sql)
	}
}

func TestCompileForDuckDB_ILikeOperator(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("name ILIKE '%test%'", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"name": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("args=%v, want 1 arg", args)
	}
	if !strings.Contains(sql, "ILIKE") {
		t.Fatalf("expected ILIKE in sql: %s", sql)
	}
}

func TestCompileForDuckDB_InOperator(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("status IN ('active', 'pending')", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"status": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("args=%v, want 2 args", args)
	}
	if !strings.Contains(sql, "IN") {
		t.Fatalf("expected IN in sql: %s", sql)
	}
}

func TestCompileForDuckDB_BetweenOperator(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("age BETWEEN 18 AND 65", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"age": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("args=%v, want 2 args", args)
	}
	if !strings.Contains(sql, "BETWEEN") {
		t.Fatalf("expected BETWEEN in sql: %s", sql)
	}
}

func TestCompileForDuckDB_IsNull(t *testing.T) {
	sql, _, _, err := CompileForDuckDB("description IS NULL", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"description": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if !strings.Contains(sql, "IS NULL") {
		t.Fatalf("expected IS NULL in sql: %s", sql)
	}
}

func TestCompileForDuckDB_IsNotNull(t *testing.T) {
	sql, _, _, err := CompileForDuckDB("description IS NOT NULL", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"description": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if !strings.Contains(sql, "IS NOT NULL") {
		t.Fatalf("expected IS NOT NULL in sql: %s", sql)
	}
}

func TestCompileForDuckDB_NotOperator(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("NOT (status = 'inactive')", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"status": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("args=%v, want 1 arg", args)
	}
	if !strings.Contains(sql, "NOT") {
		t.Fatalf("expected NOT in sql: %s", sql)
	}
}

func TestCompileForDuckDB_OrOperator(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("status = 'active' OR status = 'pending'", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"status": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("args=%v, want 2 args", args)
	}
	if !strings.Contains(sql, "OR") {
		t.Fatalf("expected OR in sql: %s", sql)
	}
}

func TestCompileForDuckDB_UnknownProperty(t *testing.T) {
	_, _, _, err := CompileForDuckDB("unknown_col = 'test'", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"name": {},
		},
		GeometryProperty: "geom",
	})
	if err == nil {
		t.Fatal("expected error for unknown property")
	}
	if !strings.Contains(err.Error(), "unknown property") {
		t.Fatalf("expected 'unknown property' error, got: %v", err)
	}
}

func TestCompileForDuckDB_EmptyFilter(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"name": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if sql != "" {
		t.Fatalf("expected empty sql for empty filter, got: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args for empty filter, got: %v", args)
	}
}

func TestCompileForDuckDB_DWithin(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("DWITHIN(geom, POINT(0 0), 1000)", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"geom": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("args=%v, want 2 args (wkt and distance)", args)
	}
	if !strings.Contains(sql, "ST_DWithin") {
		t.Fatalf("expected ST_DWithin in sql: %s", sql)
	}
}

func TestCompileForDuckDB_Envelope(t *testing.T) {
	sql, args, _, err := CompileForDuckDB("INTERSECTS(geom, ENVELOPE(0, 0, 10, 10))", DuckDBOptions{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"geom": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("CompileForDuckDB error: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("args=%v, want 1 arg (polygon wkt)", args)
	}
	wkt, ok := args[0].(string)
	if !ok {
		t.Fatalf("expected string, got %T", args[0])
	}
	if !strings.Contains(wkt, "POLYGON") {
		t.Fatalf("expected POLYGON in wkt: %s", wkt)
	}
	if !strings.Contains(sql, "ST_Intersects") {
		t.Fatalf("expected ST_Intersects in sql: %s", sql)
	}
}
