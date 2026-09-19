package tiles

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
)

func TestNewMapTileGenerator_Defaults(t *testing.T) {
	g := NewMapTileGenerator(0)
	if g.tileSize != 256 {
		t.Errorf("tileSize = %d, want 256 for non-positive input", g.tileSize)
	}
	if g.maxFeatures != 50000 || g.maxVertices != 5000000 || g.maxTileBytes != 10<<20 {
		t.Errorf("unexpected limit defaults: %d/%d/%d", g.maxFeatures, g.maxVertices, g.maxTileBytes)
	}
	if g := NewMapTileGenerator(512); g.tileSize != 512 {
		t.Errorf("tileSize = %d, want 512", g.tileSize)
	}
}

func TestMapTileGenerator_SetLimits(t *testing.T) {
	g := NewMapTileGenerator(256)
	g.SetLimits(10, 20, 30)
	if g.maxFeatures != 10 || g.maxVertices != 20 || g.maxTileBytes != 30 {
		t.Errorf("limits not applied: %d/%d/%d", g.maxFeatures, g.maxVertices, g.maxTileBytes)
	}
	g.SetLimits(0, -1, 0)
	if g.maxFeatures != 10 || g.maxVertices != 20 || g.maxTileBytes != 30 {
		t.Errorf("non-positive values must not change limits: %d/%d/%d", g.maxFeatures, g.maxVertices, g.maxTileBytes)
	}
}

func TestEncodeImage_PNG(t *testing.T) {
	g := NewMapTileGenerator(2)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	data, err := g.encodeImage(img, TileFormatPNG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	if decoded.Bounds().Dx() != 2 || decoded.Bounds().Dy() != 2 {
		t.Errorf("unexpected decoded size: %v", decoded.Bounds())
	}
}

func TestEncodeImage_JPEG(t *testing.T) {
	g := NewMapTileGenerator(2)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	data, err := g.encodeImage(img, TileFormatJPEG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("decode JPEG: %v", err)
	}
}

func TestEncodeImage_Unsupported(t *testing.T) {
	g := NewMapTileGenerator(2)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if _, err := g.encodeImage(img, TileFormat("gif")); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestParseTileFormat(t *testing.T) {
	tests := []struct {
		in   string
		want TileFormat
	}{
		{"", TileFormatPNG},
		{"png", TileFormatPNG},
		{"image/png", TileFormatPNG},
		{"jpeg", TileFormatJPEG},
		{"jpg", TileFormatJPEG},
		{"image/jpeg", TileFormatJPEG},
		{"webp", TileFormatWEBP},
		{"image/webp", TileFormatWEBP},
	}
	for _, tt := range tests {
		got, err := ParseTileFormat(tt.in)
		if err != nil {
			t.Errorf("ParseTileFormat(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseTileFormat(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGetContentType(t *testing.T) {
	tests := []struct {
		format TileFormat
		want   string
	}{
		{TileFormatPNG, MediaTypePNG},
		{TileFormatJPEG, MediaTypeJPEG},
		{TileFormatWEBP, MediaTypeWEBP},
		{TileFormat("bogus"), MediaTypePNG}, // default
	}
	for _, tt := range tests {
		if got := GetContentType(tt.format); got != tt.want {
			t.Errorf("GetContentType(%q) = %q, want %q", tt.format, got, tt.want)
		}
	}
}

func TestCreateDefaultStyle(t *testing.T) {
	style := createDefaultStyle()
	if style == nil || len(style.Rules) != 1 {
		t.Fatalf("default style must have one rule: %+v", style)
	}
	rule := style.Rules[0]
	if rule.PointStyle == nil || rule.LineStyle == nil || rule.PolygonStyle == nil {
		t.Errorf("default rule must style all geometry families: %+v", rule)
	}
}

func TestGetStyleForGeometry(t *testing.T) {
	full := createDefaultStyle()
	rule := full.Rules[0]

	tests := []struct {
		name     string
		style    *sld.Style
		geomType renderer.GeometryType
		want     any
	}{
		{"nil style", nil, renderer.WKBPoint, nil},
		{"empty rules", &sld.Style{}, renderer.WKBPoint, nil},
		{"point", full, renderer.WKBPoint, rule.PointStyle},
		{"multipoint", full, renderer.WKBMultiPoint, rule.PointStyle},
		{"linestring", full, renderer.WKBLineString, rule.LineStyle},
		{"multilinestring", full, renderer.WKBMultiLineString, rule.LineStyle},
		{"polygon", full, renderer.WKBPolygon, rule.PolygonStyle},
		{"multipolygon", full, renderer.WKBMultiPolygon, rule.PolygonStyle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getStyleForGeometry(tt.style, tt.geomType, nil)
			if got != tt.want {
				t.Errorf("getStyleForGeometry = %#v, want %#v", got, tt.want)
			}
		})
	}

	// A rule without a matching symbolizer family yields nil.
	polygonOnly := &sld.Style{Rules: []sld.ResolvedRule{{PolygonStyle: rule.PolygonStyle}}}
	if got := getStyleForGeometry(polygonOnly, renderer.WKBPoint, nil); got != nil {
		t.Errorf("point on polygon-only rule should be nil, got %#v", got)
	}
}

func TestMapGenerateTile_HappyPathPNG(t *testing.T) {
	g := NewMapTileGenerator(256)
	ds := &fakeDataSource{
		wkbResult: []datasource.RenderFeature{
			{Geometry: pointWKB(0, 0), Properties: map[string]any{"name": "p"}},
		},
	}
	data, err := g.GenerateTile(context.Background(), ds, testLayerInfo("pts"), nil, TMSWebMercatorQuad, 0, 0, 0, TileFormatPNG, "", true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	if img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Errorf("tile size = %v, want 256x256", img.Bounds())
	}
	if ds.params().OutputSRID != 3857 {
		t.Errorf("OutputSRID = %d, want TMS SRID 3857", ds.params().OutputSRID)
	}
}

func TestMapGenerateTile_FeatureLimit(t *testing.T) {
	g := NewMapTileGenerator(256)
	g.SetLimits(1, 0, 0)
	ds := &fakeDataSource{
		wkbResult: []datasource.RenderFeature{
			{Geometry: pointWKB(0, 0)},
			{Geometry: pointWKB(1, 1)},
		},
	}
	_, err := g.GenerateTile(context.Background(), ds, testLayerInfo("pts"), nil, TMSWebMercatorQuad, 0, 0, 0, TileFormatPNG, "", true, nil)
	if err == nil || !strings.Contains(err.Error(), "feature limit exceeded") {
		t.Fatalf("expected feature limit error, got %v", err)
	}
}

func TestMapGenerateTile_VertexLimit(t *testing.T) {
	g := NewMapTileGenerator(256)
	g.SetLimits(0, 1, 0)
	ds := &fakeDataSource{
		wkbResult: []datasource.RenderFeature{
			{Geometry: pointWKB(0, 0)},
			{Geometry: pointWKB(1, 1)},
		},
	}
	_, err := g.GenerateTile(context.Background(), ds, testLayerInfo("pts"), nil, TMSWebMercatorQuad, 0, 0, 0, TileFormatPNG, "", true, nil)
	if err == nil || !strings.Contains(err.Error(), "vertex limit exceeded") {
		t.Fatalf("expected vertex limit error, got %v", err)
	}
}

func TestMapGenerateTile_ByteLimit(t *testing.T) {
	g := NewMapTileGenerator(256)
	g.SetLimits(0, 0, 1)
	ds := &fakeDataSource{
		wkbResult: []datasource.RenderFeature{{Geometry: pointWKB(0, 0)}},
	}
	_, err := g.GenerateTile(context.Background(), ds, testLayerInfo("pts"), nil, TMSWebMercatorQuad, 0, 0, 0, TileFormatPNG, "", true, nil)
	if err == nil || !strings.Contains(err.Error(), "output limit exceeded") {
		t.Fatalf("expected output limit error, got %v", err)
	}
}

func TestMapGenerateTile_QueryError(t *testing.T) {
	g := NewMapTileGenerator(256)
	ds := &fakeDataSource{wkbErr: context.DeadlineExceeded}
	_, err := g.GenerateTile(context.Background(), ds, testLayerInfo("pts"), nil, TMSWebMercatorQuad, 0, 0, 0, TileFormatPNG, "", true, nil)
	if err == nil || !strings.Contains(err.Error(), "query features for tile") {
		t.Fatalf("expected query error, got %v", err)
	}
}

func TestMapGenerateTile_SkipsUnparseableWKB(t *testing.T) {
	g := NewMapTileGenerator(256)
	ds := &fakeDataSource{
		wkbResult: []datasource.RenderFeature{
			{Geometry: []byte{0xde, 0xad}}, // invalid WKB, skipped
			{Geometry: nil},                // empty geometry, skipped
			{Geometry: pointWKB(0, 0)},
		},
	}
	if _, err := g.GenerateTile(context.Background(), ds, testLayerInfo("pts"), nil, TMSWebMercatorQuad, 0, 0, 0, TileFormatPNG, "", true, nil); err != nil {
		t.Fatalf("invalid geometries must be skipped, got error: %v", err)
	}
}

func TestMapGenerateSQLViewTileWithLimits(t *testing.T) {
	g := NewMapTileGenerator(256)
	ds := &fakeSQLViewDataSource{
		sqlViewWKB: []datasource.RenderFeature{{Geometry: pointWKB(0, 0)}},
	}
	config := &datasource.SQLViewConfig{SQL: "SELECT 1", GeometryColumn: "geom"}
	data, err := g.GenerateSQLViewTileWithLimits(context.Background(), ds, config, testLayerInfo("view"), nil, TMSWebMercatorQuad, 0, 0, 0, TileFormatPNG, "", true, nil, 10, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
}
