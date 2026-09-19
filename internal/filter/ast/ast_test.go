package ast

import (
	"strings"
	"testing"
)

func TestCompileToPostgreSQL_Comparison(t *testing.T) {
	node := Equal("name", "Bob")
	sql, args, next, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"name" = $1`) {
		t.Errorf("sql = %q, expected to contain name = $1", sql)
	}
	if len(args) != 1 || args[0] != "Bob" {
		t.Errorf("args = %v, expected [Bob]", args)
	}
	if next != 2 {
		t.Errorf("next = %d, expected 2", next)
	}
}

func TestCompileToPostgreSQL_And(t *testing.T) {
	node := And(
		Equal("status", "active"),
		GreaterThan("count", 10),
	)
	sql, args, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "AND") {
		t.Errorf("sql = %q, expected to contain AND", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, expected 2 args", args)
	}
}

func TestCompileToPostgreSQL_Or(t *testing.T) {
	node := Or(
		Equal("type", "A"),
		Equal("type", "B"),
	)
	sql, args, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "OR") {
		t.Errorf("sql = %q, expected to contain OR", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, expected 2 args", args)
	}
}

func TestCompileToPostgreSQL_Not(t *testing.T) {
	node := Not(Equal("deleted", true))
	sql, _, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "NOT") {
		t.Errorf("sql = %q, expected to contain NOT", sql)
	}
}

func TestCompileToPostgreSQL_Like(t *testing.T) {
	node := Like("name", "Test*", "*", "?", "", true)
	sql, args, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "LIKE") {
		t.Errorf("sql = %q, expected to contain LIKE", sql)
	}
	if args[0] != "Test%" {
		t.Errorf("args[0] = %v, expected Test%%", args[0])
	}
}

func TestCompileToPostgreSQL_Between(t *testing.T) {
	node := Between("value", 10, 100)
	sql, args, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "BETWEEN") {
		t.Errorf("sql = %q, expected to contain BETWEEN", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, expected 2 args", args)
	}
}

func TestCompileToPostgreSQL_In(t *testing.T) {
	node := In("status", []interface{}{"active", "pending"})
	sql, args, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "IN") {
		t.Errorf("sql = %q, expected to contain IN", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, expected 2 args", args)
	}
}

func TestCompileToPostgreSQL_IsNull(t *testing.T) {
	node := IsNull("description")
	sql, _, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "IS NULL") {
		t.Errorf("sql = %q, expected to contain IS NULL", sql)
	}
}

func TestCompileToPostgreSQL_SpatialIntersects(t *testing.T) {
	node := Spatial(NodeIntersects, "geom", "POINT(10 20)", 4326)
	sql, args, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex:  1,
		SourceSRID:       4326,
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "ST_Intersects") {
		t.Errorf("sql = %q, expected to contain ST_Intersects", sql)
	}
	if !strings.Contains(sql, "::geometry") {
		t.Errorf("sql = %q, expected to contain ::geometry cast", sql)
	}
	if len(args) != 1 {
		t.Errorf("args = %v, expected 1 arg", args)
	}
}

func TestCompileToPostgreSQL_WithTableAlias(t *testing.T) {
	node := Equal("name", "Test")
	sql, _, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
		TableAlias:      "t",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `t."name"`) {
		t.Errorf("sql = %q, expected to contain t.\"name\"", sql)
	}
}

func TestCompileToDuckDB_Comparison(t *testing.T) {
	node := Equal("name", "Bob")
	sql, args, _, err := CompileToDuckDB(node, CompileOptions{
		StartParamIndex: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"name" = $1`) {
		t.Errorf("sql = %q, expected to contain name = $1", sql)
	}
	if len(args) != 1 || args[0] != "Bob" {
		t.Errorf("args = %v, expected [Bob]", args)
	}
}

func TestCompileToDuckDB_SpatialIntersects(t *testing.T) {
	node := Spatial(NodeIntersects, "geom", "POINT(10 20)", 4326)
	sql, args, _, err := CompileToDuckDB(node, CompileOptions{
		StartParamIndex:  1,
		SourceSRID:       4326,
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "ST_Intersects") {
		t.Errorf("sql = %q, expected to contain ST_Intersects", sql)
	}
	if !strings.Contains(sql, "ST_GeomFromText") {
		t.Errorf("sql = %q, expected to contain ST_GeomFromText for DuckDB", sql)
	}
	if len(args) != 1 {
		t.Errorf("args = %v, expected 1 arg", args)
	}
}

func TestCompileToDuckDB_SpatialTransform(t *testing.T) {
	node := Spatial(NodeIntersects, "geom", "POINT(10 20)", 4326)
	sql, _, _, err := CompileToDuckDB(node, CompileOptions{
		StartParamIndex:  1,
		SourceSRID:       3857, // Different SRID to trigger transform
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "ST_Transform") {
		t.Errorf("sql = %q, expected to contain ST_Transform", sql)
	}
	if !strings.Contains(sql, "EPSG:4326") || !strings.Contains(sql, "EPSG:3857") {
		t.Errorf("sql = %q, expected to contain EPSG strings", sql)
	}
}

func TestBBox(t *testing.T) {
	node := BBox("geom", -10, -10, 10, 10, 4326)
	sql, _, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex:  1,
		SourceSRID:       4326,
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "ST_Intersects") {
		t.Errorf("sql = %q, expected to contain ST_Intersects", sql)
	}
	if !strings.Contains(sql, "ST_MakeEnvelope") {
		t.Errorf("sql = %q, expected to contain ST_MakeEnvelope", sql)
	}
}

func TestResourceIds(t *testing.T) {
	node := ResourceIds([]string{"1", "2", "3"})
	sql, args, _, err := CompileToPostgreSQL(node, CompileOptions{
		StartParamIndex: 1,
		IDColumn:        "id",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "id::text IN") {
		t.Errorf("sql = %q, expected to contain id::text IN", sql)
	}
	if len(args) != 3 {
		t.Errorf("args = %v, expected 3 args", args)
	}
}

func TestParseSRIDFromCRS(t *testing.T) {
	tests := []struct {
		crs      string
		expected int
	}{
		{"EPSG:4326", 4326},
		{"EPSG:3857", 3857},
		{"urn:ogc:def:crs:EPSG::4326", 4326},
		{"", 0},
		{"unknown", 0},
	}
	for _, tt := range tests {
		t.Run(tt.crs, func(t *testing.T) {
			got := ParseSRIDFromCRS(tt.crs)
			if got != tt.expected {
				t.Errorf("ParseSRIDFromCRS(%q) = %d, want %d", tt.crs, got, tt.expected)
			}
		})
	}
}

func TestFESConverter_ConvertComparison(t *testing.T) {
	conv := NewFESConverter(FESConvertOptions{
		GeometryProperty:  "geom",
		AllowedProperties: map[string]struct{}{"name": {}, "count": {}},
	})

	node, err := conv.ConvertComparison("name", "Test", "=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Type != NodeEqual {
		t.Errorf("node.Type = %v, expected NodeEqual", node.Type)
	}
	if node.Property != "name" {
		t.Errorf("node.Property = %q, expected name", node.Property)
	}
}

func TestFESConverter_StripNamespacePrefix(t *testing.T) {
	conv := NewFESConverter(FESConvertOptions{
		GeometryProperty:  "geom",
		AllowedProperties: map[string]struct{}{"name": {}},
	})

	node, err := conv.ConvertComparison("tns:name", "Test", "=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Property != "name" {
		t.Errorf("node.Property = %q, expected name (without prefix)", node.Property)
	}
}

func TestFESConverter_ResourceIdMismatch(t *testing.T) {
	conv := NewFESConverter(FESConvertOptions{
		CollectionID: "Buildings",
	})

	_, err := conv.ConvertResourceIds([]string{"NamedPlaces.1"})
	if err == nil {
		t.Fatal("expected error for resource ID mismatch")
	}
	if _, ok := err.(*ResourceIdMismatchError); !ok {
		t.Errorf("expected *ResourceIdMismatchError, got %T", err)
	}
}

func TestParseEnvelope(t *testing.T) {
	minX, minY, maxX, maxY, err := ParseEnvelope("-10 -10", "10 10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if minX != -10 || minY != -10 || maxX != 10 || maxY != 10 {
		t.Errorf("ParseEnvelope returned (%f, %f, %f, %f), expected (-10, -10, 10, 10)", minX, minY, maxX, maxY)
	}
}

func TestPointToWKT(t *testing.T) {
	wkt, _, err := PointToWKT("10 20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(wkt, "POINT") {
		t.Errorf("wkt = %q, expected to contain POINT", wkt)
	}
}

func TestPolygonToWKT(t *testing.T) {
	wkt, err := PolygonToWKT("0 0 10 0 10 10 0 10 0 0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(wkt, "POLYGON") {
		t.Errorf("wkt = %q, expected to contain POLYGON", wkt)
	}
}

func TestConvertFESLikePattern(t *testing.T) {
	tests := []struct {
		pattern    string
		wildCard   string
		singleChar string
		escapeChar string
		expected   string
	}{
		{"Test*", "*", "?", "", "Test%"},
		{"Te?t", "*", "?", "", "Te_t"},
		{"Test*Value", "*", "?", "", "Test%Value"},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := ConvertFESLikePattern(tt.pattern, tt.wildCard, tt.singleChar, tt.escapeChar)
			if got != tt.expected {
				t.Errorf("ConvertFESLikePattern(%q) = %q, want %q", tt.pattern, got, tt.expected)
			}
		})
	}
}
