package tiles

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/mvt"
	"github.com/paulmach/orb/geojson"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
)

func TestNewMVTGenerator_Defaults(t *testing.T) {
	g := NewMVTGenerator(0)
	if g.extent != 4096 {
		t.Errorf("extent = %d, want 4096 for non-positive input", g.extent)
	}
	if g.buffer != 256 || !g.clipGeom {
		t.Errorf("unexpected buffer/clip defaults: buffer=%d clip=%t", g.buffer, g.clipGeom)
	}
	if g.maxFeatures != 50000 || g.maxVertices != 5000000 || g.maxTileBytes != 10<<20 {
		t.Errorf("unexpected limit defaults: %d/%d/%d", g.maxFeatures, g.maxVertices, g.maxTileBytes)
	}
	if g.statementTimeout != 30*time.Second {
		t.Errorf("statementTimeout = %v, want 30s", g.statementTimeout)
	}

	if g := NewMVTGenerator(512); g.extent != 512 {
		t.Errorf("extent = %d, want 512", g.extent)
	}
}

func TestMVTGenerator_SetLimits(t *testing.T) {
	g := NewMVTGenerator(4096)
	g.SetLimits(100, 200, 300, 400)
	if g.maxFeatures != 100 || g.maxVertices != 200 || g.maxTileBytes != 300 {
		t.Errorf("limits not applied: %d/%d/%d", g.maxFeatures, g.maxVertices, g.maxTileBytes)
	}
	if g.statementTimeout != 400*time.Millisecond {
		t.Errorf("statementTimeout = %v, want 400ms", g.statementTimeout)
	}

	// Non-positive values leave existing limits untouched.
	g.SetLimits(0, -1, 0, -5)
	if g.maxFeatures != 100 || g.maxVertices != 200 || g.maxTileBytes != 300 || g.statementTimeout != 400*time.Millisecond {
		t.Errorf("non-positive values must not change limits: %d/%d/%d/%v", g.maxFeatures, g.maxVertices, g.maxTileBytes, g.statementTimeout)
	}
}

func TestBuildPostGISMVTQuery(t *testing.T) {
	g := NewMVTGenerator(4096)
	g.SetLimits(500, 0, 0, 0)
	info := testLayerInfo("public.roads")
	info.Name = "public.roads"
	bounds, _ := TileBBox(TMSWebMercatorQuad, 3, 2, 1)

	sql, args := g.buildPostGISMVTQuery(info, TMSWebMercatorQuad, 3, 2, 1, bounds)

	if len(args) != 4 {
		t.Fatalf("args = %v, want [z x y layerName]", args)
	}
	if args[0] != 3 || args[1] != 2 || args[2] != 1 || args[3] != "roads" {
		t.Errorf("args = %v, want [3 2 1 roads] (layer name without schema prefix)", args)
	}
	if !strings.Contains(sql, `FROM "public"."roads" t`) {
		t.Errorf("query must reference quoted schema and table:\n%s", sql)
	}
	if !strings.Contains(sql, `ST_Transform(t."geom", 3857)`) {
		t.Errorf("query must transform geometry to the TMS SRID 3857:\n%s", sql)
	}
	if !strings.Contains(sql, "LIMIT 501") {
		t.Errorf("query must limit to maxFeatures+1 (501):\n%s", sql)
	}
	if !strings.Contains(sql, `t."id"`) {
		t.Errorf("query must use the ID column expression:\n%s", sql)
	}
	// Property list must include non-ID, non-geometry columns.
	if !strings.Contains(sql, `t."name"`) {
		t.Errorf("query must project property columns:\n%s", sql)
	}
}

func TestBuildIDExpr(t *testing.T) {
	g := NewMVTGenerator(4096)
	if expr := g.buildIDExpr(&datasource.LayerInfo{IDColumn: "fid"}); expr != `t."fid"` {
		t.Errorf("expr = %q, want t.\"fid\"", expr)
	}
	if expr := g.buildIDExpr(&datasource.LayerInfo{}); expr != "NULL" {
		t.Errorf("expr = %q, want NULL for missing ID column", expr)
	}
}

func TestBuildPropertyColumns(t *testing.T) {
	g := NewMVTGenerator(4096)

	if cols := g.buildPropertyColumns(&datasource.LayerInfo{}); cols != "" {
		t.Errorf("no properties should give empty string, got %q", cols)
	}

	info := &datasource.LayerInfo{
		GeometryColumn: "geom",
		IDColumn:       "id",
		Properties: []datasource.PropertyInfo{
			{Name: "geom"}, {Name: "id"}, {Name: "name"},
		},
	}
	cols := g.buildPropertyColumns(info)
	if strings.Contains(cols, `"geom"`) || !strings.Contains(cols, `t."name"`) {
		t.Errorf("geometry/ID columns must be skipped, name kept: %q", cols)
	}
	if !strings.HasPrefix(cols, ", ") {
		t.Errorf("non-empty column list must start with ', ': %q", cols)
	}

	// Only geometry and ID columns → empty result.
	onlySkipped := &datasource.LayerInfo{
		GeometryColumn: "geom",
		IDColumn:       "id",
		Properties:     []datasource.PropertyInfo{{Name: "geom"}, {Name: "id"}},
	}
	if cols := g.buildPropertyColumns(onlySkipped); cols != "" {
		t.Errorf("all-skipped properties should give empty string, got %q", cols)
	}
}

func TestCastPropertyForMVT(t *testing.T) {
	g := NewMVTGenerator(4096)
	tests := []struct {
		name   string
		pgType string
		want   string
	}{
		{"jsonb to text", "jsonb", `t."col"::text AS "col"`},
		{"uuid to text", "uuid", `t."col"::text AS "col"`},
		{"timestamptz to text", "timestamptz", `t."col"::text AS "col"`},
		{"underscore array to text", "_int4", `t."col"::text AS "col"`},
		{"bracket array to text", "text[]", `t."col"::text AS "col"`},
		{"numeric to double", "numeric", `t."col"::double precision AS "col"`},
		{"decimal to double", "decimal", `t."col"::double precision AS "col"`},
		{"plain int as-is", "int4", `t."col"`},
		{"plain text as-is", "text", `t."col"`},
		{"unknown type as-is", "", `t."col"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prop := datasource.PropertyInfo{Name: "col"}
			got := g.castPropertyForMVT(prop, map[string]string{"col": tt.pgType})
			if got != tt.want {
				t.Errorf("cast(%q) = %q, want %q", tt.pgType, got, tt.want)
			}
		})
	}

	// Nil pgTypes map means no cast.
	if got := g.castPropertyForMVT(datasource.PropertyInfo{Name: "col"}, nil); got != `t."col"` {
		t.Errorf("nil pgTypes should pass through, got %q", got)
	}
}

func TestQuoteIdent(t *testing.T) {
	if got := quoteIdent("plain"); got != `"plain"` {
		t.Errorf("quoteIdent(plain) = %q", got)
	}
	if got := quoteIdent(`evil"col`); got != `"evil""col"` {
		t.Errorf("embedded quotes must be doubled, got %q", got)
	}
}

func TestGeometryVertices(t *testing.T) {
	poly := orb.Polygon{
		{{0, 0}, {1, 0}, {1, 1}, {0, 0}},                 // outer ring, 4 points
		{{0.1, 0.1}, {0.2, 0.1}, {0.1, 0.2}, {0.1, 0.1}}, // hole, 4 points
	}
	tests := []struct {
		name string
		geom orb.Geometry
		want int
	}{
		{"point", orb.Point{0, 0}, 1},
		{"multipoint", orb.MultiPoint{{0, 0}, {1, 1}, {2, 2}}, 3},
		{"linestring", orb.LineString{{0, 0}, {1, 1}}, 2},
		{"multilinestring", orb.MultiLineString{{{0, 0}, {1, 1}}, {{2, 2}, {3, 3}, {4, 4}}}, 5},
		{"ring", orb.Ring{{0, 0}, {1, 0}, {0, 1}, {0, 0}}, 4},
		{"polygon with hole", poly, 8},
		{"multipolygon", orb.MultiPolygon{poly, poly}, 16},
		{"collection", orb.Collection{orb.Point{0, 0}, orb.LineString{{0, 0}, {1, 1}}}, 3},
		{"unknown zero", orb.Bound{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := geometryVertices(tt.geom); got != tt.want {
				t.Errorf("geometryVertices = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNormalizeMVTProperties(t *testing.T) {
	props := geojson.Properties{
		"str":    "keep",
		"num":    float64(3.5),
		"bool":   true,
		"nil":    nil,
		"obj":    map[string]any{"a": 1},
		"arr":    []any{1, 2},
		"broken": func() {}, // not JSON-marshalable
	}
	normalizeMVTProperties(props)

	if props["str"] != "keep" || props["num"] != float64(3.5) || props["bool"] != true {
		t.Errorf("scalar properties must remain untouched: %+v", props)
	}
	if _, ok := props["nil"]; !ok {
		t.Errorf("nil property must be kept")
	}
	if s, ok := props["obj"].(string); !ok || !strings.Contains(s, `"a":1`) {
		t.Errorf("object must be JSON-encoded to string, got %#v", props["obj"])
	}
	if s, ok := props["arr"].(string); !ok || s != "[1,2]" {
		t.Errorf("array must be JSON-encoded to string, got %#v", props["arr"])
	}
	if _, ok := props["broken"]; ok {
		t.Errorf("unmarshalable property must be deleted")
	}
}

func TestEncodeFeaturesToMVT_FeatureLimit(t *testing.T) {
	g := NewMVTGenerator(4096)
	g.SetLimits(2, 0, 0, 0)
	features := []json.RawMessage{pointFeature(1, 0, 0), pointFeature(2, 1, 1), pointFeature(3, 2, 2)}
	_, err := g.encodeFeaturesToMVT(features, "pts", &TileBounds{MinX: -10, MinY: -10, MaxX: 10, MaxY: 10})
	if err == nil || !strings.Contains(err.Error(), "feature limit exceeded") {
		t.Fatalf("expected feature limit error, got %v", err)
	}
}

func TestEncodeFeaturesToMVT_VertexLimit(t *testing.T) {
	g := NewMVTGenerator(4096)
	g.SetLimits(0, 1, 0, 0)
	features := []json.RawMessage{pointFeature(1, 0, 0), pointFeature(2, 1, 1)}
	_, err := g.encodeFeaturesToMVT(features, "pts", &TileBounds{MinX: -10, MinY: -10, MaxX: 10, MaxY: 10})
	if err == nil || !strings.Contains(err.Error(), "vertex limit exceeded") {
		t.Fatalf("expected vertex limit error, got %v", err)
	}
}

func TestEncodeFeaturesToMVT_ByteLimit(t *testing.T) {
	g := NewMVTGenerator(4096)
	g.SetLimits(0, 0, 1, 0)
	features := []json.RawMessage{pointFeature(1, 0, 0)}
	_, err := g.encodeFeaturesToMVT(features, "pts", &TileBounds{MinX: -10, MinY: -10, MaxX: 10, MaxY: 10})
	if err == nil || !strings.Contains(err.Error(), "output limit exceeded") {
		t.Fatalf("expected output limit error, got %v", err)
	}
}

func TestEncodeFeaturesToMVT_InvalidBounds(t *testing.T) {
	g := NewMVTGenerator(4096)
	features := []json.RawMessage{pointFeature(1, 0, 0)}
	for _, bounds := range []*TileBounds{
		{MinX: 10, MinY: -10, MaxX: 10, MaxY: 10}, // zero width
		{MinX: -10, MinY: 10, MaxX: 10, MaxY: 10}, // zero height
	} {
		if _, err := g.encodeFeaturesToMVT(features, "pts", bounds); err == nil || !strings.Contains(err.Error(), "invalid tile bounds") {
			t.Fatalf("expected invalid bounds error for %+v, got %v", bounds, err)
		}
	}
}

func TestEncodeFeaturesToMVT_InvalidFeature(t *testing.T) {
	g := NewMVTGenerator(4096)
	_, err := g.encodeFeaturesToMVT([]json.RawMessage{json.RawMessage(`{"not":`)}, "pts", &TileBounds{MinX: -1, MinY: -1, MaxX: 1, MaxY: 1})
	if err == nil || !strings.Contains(err.Error(), "decode GeoJSON feature") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestEncodeFeaturesToMVT_StripsSchemaPrefixFromLayerName(t *testing.T) {
	g := NewMVTGenerator(4096)
	data, err := g.encodeFeaturesToMVT([]json.RawMessage{pointFeature(1, 0, 0)}, "public.roads", &TileBounds{MinX: -1, MinY: -1, MaxX: 1, MaxY: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	layers, err := mvt.Unmarshal(data)
	if err != nil {
		t.Fatalf("decode MVT: %v", err)
	}
	if len(layers) != 1 || layers[0].Name != "roads" {
		t.Fatalf("layer name should strip public. prefix, got %+v", layers)
	}
}

func TestGenerateTile_UnsupportedType(t *testing.T) {
	g := NewMVTGenerator(4096)
	ds := &fakeDataSource{svcType: store.ServiceType("bogus")}
	_, err := g.GenerateTile(context.Background(), ds, testLayerInfo("l"), TMSWebMercatorQuad, 0, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported type error, got %v", err)
	}
}

func TestGenerateTile_DuckDBQueriesWGS84Bounds(t *testing.T) {
	g := NewMVTGenerator(4096)
	g.SetLimits(100, 0, 0, 0)
	ds := &fakeDataSource{
		svcType:     store.ServiceTypeDuckDB,
		queryResult: []json.RawMessage{pointFeature(1, 0, 0)},
	}
	data, err := g.GenerateTile(context.Background(), ds, testLayerInfo("pts"), TMSWebMercatorQuad, 0, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds.queryCalls != 1 {
		t.Fatalf("expected 1 query call, got %d", ds.queryCalls)
	}
	p := ds.params()
	if p.Limit != 101 {
		t.Errorf("query limit = %d, want maxFeatures+1 = 101", p.Limit)
	}
	if p.BBox == nil || p.BBoxSRID != 4326 || p.OutputSRID != 3857 {
		t.Errorf("query must use WGS84 bbox: %+v", p)
	}
	if !almostEqual(p.BBox.MinX, -180) || !almostEqual(p.BBox.MaxX, 180) {
		t.Errorf("z0 bbox longitude = [%v, %v], want [-180, 180]", p.BBox.MinX, p.BBox.MaxX)
	}
	layers, err := mvt.Unmarshal(data)
	if err != nil || len(layers) != 1 || len(layers[0].Features) != 1 {
		t.Fatalf("expected 1 layer with 1 feature, err=%v layers=%v", err, layers)
	}
}

func TestGenerateTile_PostGISNonWebMercatorFallsBack(t *testing.T) {
	// PostGIS with a non-WebMercator TMS must use the generic fallback (Query),
	// not the pgx pool path.
	g := NewMVTGenerator(4096)
	ds := &fakeDataSource{
		svcType:     store.ServiceTypePostGIS,
		queryResult: []json.RawMessage{pointFeature(1, 0, 0)},
	}
	if _, err := g.GenerateTile(context.Background(), ds, testLayerInfo("pts"), TMSWorldCRS84Quad, 0, 0, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds.queryCalls != 1 {
		t.Fatalf("fallback must call Query once, got %d", ds.queryCalls)
	}
}

func TestGenerateTile_PostGISWebMercatorNeedsPool(t *testing.T) {
	g := NewMVTGenerator(4096)
	ds := &fakeDataSource{svcType: store.ServiceTypePostGIS}
	_, err := g.GenerateTile(context.Background(), ds, testLayerInfo("pts"), TMSWebMercatorQuad, 0, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "does not expose connection pool") {
		t.Fatalf("expected pool error, got %v", err)
	}
}

func TestGenerateTileWithLimits_MinSemantics(t *testing.T) {
	g := NewMVTGenerator(4096)
	g.SetLimits(100, 0, 0, 0)
	ds := &fakeDataSource{svcType: store.ServiceTypeDuckDB}

	// Workspace limit below the generator limit wins.
	if _, err := g.GenerateTileWithLimits(context.Background(), ds, testLayerInfo("pts"), TMSWebMercatorQuad, 0, 0, 0, 10, 0, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds.params().Limit != 11 {
		t.Errorf("limit = %d, want min(100,10)+1 = 11", ds.params().Limit)
	}

	// Workspace limit above the generator limit does not raise the ceiling.
	if _, err := g.GenerateTileWithLimits(context.Background(), ds, testLayerInfo("pts"), TMSWebMercatorQuad, 0, 0, 0, 5000, 0, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds.params().Limit != 101 {
		t.Errorf("limit = %d, want min(100,5000)+1 = 101", ds.params().Limit)
	}

	// Zero workspace limits are ignored.
	if _, err := g.GenerateTileWithLimits(context.Background(), ds, testLayerInfo("pts"), TMSWebMercatorQuad, 0, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds.params().Limit != 101 {
		t.Errorf("limit = %d, want generator maxFeatures+1 = 101", ds.params().Limit)
	}

	// The receiver's own limits must not be mutated by per-request tightening.
	if g.maxFeatures != 100 {
		t.Errorf("generator maxFeatures mutated to %d", g.maxFeatures)
	}
}

func TestGenerateSQLViewTileWithLimits(t *testing.T) {
	g := NewMVTGenerator(4096)
	ds := &fakeSQLViewDataSource{
		sqlViewRows: []json.RawMessage{pointFeature(1, 0, 0)},
	}
	config := &datasource.SQLViewConfig{SQL: "SELECT 1", GeometryColumn: "geom"}
	data, err := g.GenerateSQLViewTileWithLimits(context.Background(), ds, config, testLayerInfo("view"), TMSWebMercatorQuad, 0, 0, 0, 10, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	layers, err := mvt.Unmarshal(data)
	if err != nil || len(layers) != 1 || len(layers[0].Features) != 1 {
		t.Fatalf("expected 1 layer with 1 feature, err=%v layers=%v", err, layers)
	}
}
