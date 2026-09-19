package sld

import (
	"image/color"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
)

func TestDefaultStyle(t *testing.T) {
	cfg := conf.WMSStyle{
		FillColor:   "#3388ff",
		FillOpacity: 0.5,
		StrokeColor: "#3388ff",
		StrokeWidth: 2.0,
		PointRadius: 5.0,
	}

	style := DefaultStyle(cfg)

	if style.Name != "default" {
		t.Errorf("expected name 'default', got %q", style.Name)
	}
	if StyleUsesCompositing(style) {
		t.Error("default style must not require the optional compositing extension")
	}
	if len(style.Rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(style.Rules))
	}

	rule := style.Rules[0]
	if rule.PointStyle == nil {
		t.Fatal("expected PointStyle")
	}
	if rule.LineStyle == nil {
		t.Fatal("expected LineStyle")
	}
	if rule.PolygonStyle == nil {
		t.Fatal("expected PolygonStyle")
	}

	// Check point style
	if rule.PointStyle.Shape != "circle" {
		t.Errorf("expected shape 'circle', got %q", rule.PointStyle.Shape)
	}
	if rule.PointStyle.Size != 10.0 { // diameter = 2 * radius
		t.Errorf("expected size 10, got %v", rule.PointStyle.Size)
	}

	// Check line style
	if rule.LineStyle.Width != 2.0 {
		t.Errorf("expected width 2, got %v", rule.LineStyle.Width)
	}

	// Check polygon style
	if rule.PolygonStyle.FillOpacity != 0.5 {
		t.Errorf("expected fill opacity 0.5, got %v", rule.PolygonStyle.FillOpacity)
	}
}

func TestDefaultStyle_ZeroValues(t *testing.T) {
	cfg := conf.WMSStyle{
		FillColor:   "#3388ff",
		FillOpacity: 0.5,
		StrokeColor: "#3388ff",
		StrokeWidth: 0, // Should default
		PointRadius: 0, // Should default
	}

	style := DefaultStyle(cfg)
	rule := style.Rules[0]

	// Point radius should default to 5, so size should be 10
	if rule.PointStyle.Size != 10.0 {
		t.Errorf("expected default point size 10, got %v", rule.PointStyle.Size)
	}

	// Stroke width should default to 1
	if rule.PointStyle.StrokeWidth != 1.0 {
		t.Errorf("expected default stroke width 1, got %v", rule.PointStyle.StrokeWidth)
	}
}

func TestDefaultPointStyle(t *testing.T) {
	style := DefaultPointStyle()

	if style.Shape != "circle" {
		t.Errorf("expected shape 'circle', got %q", style.Shape)
	}
	if style.Size != 10.0 {
		t.Errorf("expected size 10, got %v", style.Size)
	}
	if style.Rotation != 0 {
		t.Errorf("expected rotation 0, got %v", style.Rotation)
	}
	if style.Opacity != 1.0 {
		t.Errorf("expected opacity 1.0, got %v", style.Opacity)
	}
	if style.StrokeWidth != 2.0 {
		t.Errorf("expected stroke width 2, got %v", style.StrokeWidth)
	}
}

func TestDefaultLineStyle(t *testing.T) {
	style := DefaultLineStyle()

	if style.Width != 2.0 {
		t.Errorf("expected width 2, got %v", style.Width)
	}
	if style.Opacity != 1.0 {
		t.Errorf("expected opacity 1.0, got %v", style.Opacity)
	}
	if style.LineCap != "round" {
		t.Errorf("expected line cap 'round', got %q", style.LineCap)
	}
	if style.LineJoin != "round" {
		t.Errorf("expected line join 'round', got %q", style.LineJoin)
	}
}

func TestDefaultPolygonStyle(t *testing.T) {
	style := DefaultPolygonStyle()

	if style.FillOpacity != 0.5 {
		t.Errorf("expected fill opacity 0.5, got %v", style.FillOpacity)
	}
	if style.StrokeWidth != 2.0 {
		t.Errorf("expected stroke width 2, got %v", style.StrokeWidth)
	}
	if style.StrokeOpacity != 1.0 {
		t.Errorf("expected stroke opacity 1.0, got %v", style.StrokeOpacity)
	}
}

func TestMergePointStyle_NilStyle(t *testing.T) {
	defaults := DefaultPointStyle()
	result := MergePointStyle(nil, defaults)

	if result != defaults {
		t.Error("expected defaults to be returned for nil style")
	}
}

func TestMergePointStyle_WithValues(t *testing.T) {
	defaults := DefaultPointStyle()
	style := &PointStyle{
		Shape: "square",
		Size:  20,
	}

	result := MergePointStyle(style, defaults)

	if result.Shape != "square" {
		t.Errorf("expected shape 'square', got %q", result.Shape)
	}
	if result.Size != 20 {
		t.Errorf("expected size 20, got %v", result.Size)
	}
}

func TestMergePointStyle_MissingValues(t *testing.T) {
	defaults := DefaultPointStyle()
	style := &PointStyle{
		// Empty shape and zero size should use defaults
		Shape: "",
		Size:  0,
	}

	result := MergePointStyle(style, defaults)

	if result.Shape != "circle" {
		t.Errorf("expected default shape 'circle', got %q", result.Shape)
	}
	if result.Size != 10.0 {
		t.Errorf("expected default size 10, got %v", result.Size)
	}
}

func TestMergeLineStyle_NilStyle(t *testing.T) {
	defaults := DefaultLineStyle()
	result := MergeLineStyle(nil, defaults)

	if result != defaults {
		t.Error("expected defaults to be returned for nil style")
	}
}

func TestMergeLineStyle_WithValues(t *testing.T) {
	defaults := DefaultLineStyle()
	style := &LineStyle{
		Width:    5.0,
		LineCap:  "butt",
		LineJoin: "miter",
	}

	result := MergeLineStyle(style, defaults)

	if result.Width != 5.0 {
		t.Errorf("expected width 5, got %v", result.Width)
	}
	if result.LineCap != "butt" {
		t.Errorf("expected line cap 'butt', got %q", result.LineCap)
	}
	if result.LineJoin != "miter" {
		t.Errorf("expected line join 'miter', got %q", result.LineJoin)
	}
}

func TestMergeLineStyle_MissingValues(t *testing.T) {
	defaults := DefaultLineStyle()
	style := &LineStyle{
		Width:    0,
		LineCap:  "",
		LineJoin: "",
	}

	result := MergeLineStyle(style, defaults)

	if result.Width != 2.0 {
		t.Errorf("expected default width 2, got %v", result.Width)
	}
	if result.LineCap != "round" {
		t.Errorf("expected default line cap 'round', got %q", result.LineCap)
	}
	if result.LineJoin != "round" {
		t.Errorf("expected default line join 'round', got %q", result.LineJoin)
	}
}

func TestMergePolygonStyle_NilStyle(t *testing.T) {
	defaults := DefaultPolygonStyle()
	result := MergePolygonStyle(nil, defaults)

	if result != defaults {
		t.Error("expected defaults to be returned for nil style")
	}
}

func TestMergePolygonStyle_WithValues(t *testing.T) {
	defaults := DefaultPolygonStyle()
	style := &PolygonStyle{
		StrokeWidth: 5.0,
		FillOpacity: 0.8,
	}

	result := MergePolygonStyle(style, defaults)

	if result.StrokeWidth != 5.0 {
		t.Errorf("expected stroke width 5, got %v", result.StrokeWidth)
	}
	if result.FillOpacity != 0.8 {
		t.Errorf("expected fill opacity 0.8, got %v", result.FillOpacity)
	}
}

func TestMergePolygonStyle_MissingValues(t *testing.T) {
	defaults := DefaultPolygonStyle()
	style := &PolygonStyle{
		StrokeWidth: 0, // Should use default
	}

	result := MergePolygonStyle(style, defaults)

	if result.StrokeWidth != 2.0 {
		t.Errorf("expected default stroke width 2, got %v", result.StrokeWidth)
	}
}

func TestColorWithOpacity(t *testing.T) {
	tests := []struct {
		name    string
		color   color.RGBA
		opacity float64
		wantA   uint8
	}{
		{"full opacity", color.RGBA{R: 255, G: 0, B: 0, A: 255}, 1.0, 255},
		{"half opacity", color.RGBA{R: 255, G: 0, B: 0, A: 255}, 0.5, 127},
		{"quarter opacity", color.RGBA{R: 255, G: 0, B: 0, A: 200}, 0.25, 50},
		{"zero opacity", color.RGBA{R: 255, G: 0, B: 0, A: 255}, 0.0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ColorWithOpacity(tt.color, tt.opacity)

			if result.R != tt.color.R || result.G != tt.color.G || result.B != tt.color.B {
				t.Errorf("RGB values changed unexpectedly")
			}
			if result.A != tt.wantA {
				t.Errorf("ColorWithOpacity alpha = %d, want %d", result.A, tt.wantA)
			}
		})
	}
}

func TestGetEffectiveStyle_Point(t *testing.T) {
	rule := &ResolvedRule{
		PointStyle: DefaultPointStyle(),
	}

	style := GetEffectiveStyle(rule, "Point")
	if _, ok := style.(*PointStyle); !ok {
		t.Error("expected *PointStyle for Point geometry")
	}

	style = GetEffectiveStyle(rule, "MultiPoint")
	if _, ok := style.(*PointStyle); !ok {
		t.Error("expected *PointStyle for MultiPoint geometry")
	}
}

func TestGetEffectiveStyle_Line(t *testing.T) {
	rule := &ResolvedRule{
		LineStyle: DefaultLineStyle(),
	}

	style := GetEffectiveStyle(rule, "LineString")
	if _, ok := style.(*LineStyle); !ok {
		t.Error("expected *LineStyle for LineString geometry")
	}

	style = GetEffectiveStyle(rule, "MultiLineString")
	if _, ok := style.(*LineStyle); !ok {
		t.Error("expected *LineStyle for MultiLineString geometry")
	}
}

func TestGetEffectiveStyle_Polygon(t *testing.T) {
	rule := &ResolvedRule{
		PolygonStyle: DefaultPolygonStyle(),
	}

	style := GetEffectiveStyle(rule, "Polygon")
	if _, ok := style.(*PolygonStyle); !ok {
		t.Error("expected *PolygonStyle for Polygon geometry")
	}

	style = GetEffectiveStyle(rule, "MultiPolygon")
	if _, ok := style.(*PolygonStyle); !ok {
		t.Error("expected *PolygonStyle for MultiPolygon geometry")
	}
}

func TestGetEffectiveStyle_Defaults(t *testing.T) {
	// Test that default styles are returned when rule doesn't have styles
	rule := &ResolvedRule{}

	pointStyle := GetEffectiveStyle(rule, "Point")
	if _, ok := pointStyle.(*PointStyle); !ok {
		t.Error("expected default *PointStyle")
	}

	lineStyle := GetEffectiveStyle(rule, "LineString")
	if _, ok := lineStyle.(*LineStyle); !ok {
		t.Error("expected default *LineStyle")
	}

	polygonStyle := GetEffectiveStyle(rule, "Polygon")
	if _, ok := polygonStyle.(*PolygonStyle); !ok {
		t.Error("expected default *PolygonStyle")
	}
}

func TestGetEffectiveStyle_Unknown(t *testing.T) {
	// For unknown geometry types, should return polygon style first (if available)
	rule := &ResolvedRule{
		PolygonStyle: DefaultPolygonStyle(),
	}

	style := GetEffectiveStyle(rule, "GeometryCollection")
	if _, ok := style.(*PolygonStyle); !ok {
		t.Error("expected *PolygonStyle for unknown geometry")
	}

	// If no polygon style, return line style
	rule2 := &ResolvedRule{
		LineStyle: DefaultLineStyle(),
	}

	style2 := GetEffectiveStyle(rule2, "GeometryCollection")
	if _, ok := style2.(*LineStyle); !ok {
		t.Error("expected *LineStyle for unknown geometry without polygon style")
	}

	// If no polygon or line style, return point style
	rule3 := &ResolvedRule{
		PointStyle: DefaultPointStyle(),
	}

	style3 := GetEffectiveStyle(rule3, "GeometryCollection")
	if _, ok := style3.(*PointStyle); !ok {
		t.Error("expected *PointStyle for unknown geometry without polygon/line style")
	}

	// If no styles at all, return default polygon
	rule4 := &ResolvedRule{}
	style4 := GetEffectiveStyle(rule4, "Unknown")
	if _, ok := style4.(*PolygonStyle); !ok {
		t.Error("expected default *PolygonStyle")
	}
}

func TestWellKnownMarks(t *testing.T) {
	marks := []string{"circle", "square", "triangle", "star", "cross", "x"}

	for _, mark := range marks {
		if !WellKnownMarks[mark] {
			t.Errorf("expected %q to be a well-known mark", mark)
		}
	}

	if WellKnownMarks["unknown"] {
		t.Error("'unknown' should not be a well-known mark")
	}
}
