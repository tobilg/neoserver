package renderer

import (
	"math"
	"testing"

	"github.com/tobilg/neoserver/internal/query"
)

func TestTransformToPixel(t *testing.T) {
	bbox := query.BBox{
		MinX: 0,
		MinY: 0,
		MaxX: 100,
		MaxY: 100,
	}
	transform := NewTransform(bbox, 500, 500)

	tests := []struct {
		name    string
		x, y    float64
		expectX float64
		expectY float64
	}{
		{"origin", 0, 0, 0, 500},
		{"center", 50, 50, 250, 250},
		{"top-right", 100, 100, 500, 0},
		{"bottom-left", 0, 0, 0, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			px, py := transform.ToPixel(tt.x, tt.y)
			if !floatEquals(px, tt.expectX) || !floatEquals(py, tt.expectY) {
				t.Errorf("ToPixel(%v, %v) = (%v, %v), want (%v, %v)",
					tt.x, tt.y, px, py, tt.expectX, tt.expectY)
			}
		})
	}
}

func TestTransformToMap(t *testing.T) {
	bbox := query.BBox{
		MinX: 0,
		MinY: 0,
		MaxX: 100,
		MaxY: 100,
	}
	transform := NewTransform(bbox, 500, 500)

	tests := []struct {
		name    string
		px, py  float64
		expectX float64
		expectY float64
	}{
		{"origin", 0, 500, 0, 0},
		{"center", 250, 250, 50, 50},
		{"top-right", 500, 0, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := transform.ToMap(tt.px, tt.py)
			if !floatEquals(x, tt.expectX) || !floatEquals(y, tt.expectY) {
				t.Errorf("ToMap(%v, %v) = (%v, %v), want (%v, %v)",
					tt.px, tt.py, x, y, tt.expectX, tt.expectY)
			}
		})
	}
}

func TestTransformRoundTrip(t *testing.T) {
	bbox := query.BBox{
		MinX: -180,
		MinY: -90,
		MaxX: 180,
		MaxY: 90,
	}
	transform := NewTransform(bbox, 800, 400)

	testPoints := []struct{ x, y float64 }{
		{0, 0},
		{-180, -90},
		{180, 90},
		{45.5, 23.7},
	}

	for _, pt := range testPoints {
		px, py := transform.ToPixel(pt.x, pt.y)
		x, y := transform.ToMap(px, py)

		if !floatEquals(x, pt.x) || !floatEquals(y, pt.y) {
			t.Errorf("Round trip failed for (%v, %v): got (%v, %v)", pt.x, pt.y, x, y)
		}
	}
}

func TestTransformContains(t *testing.T) {
	bbox := query.BBox{
		MinX: 0,
		MinY: 0,
		MaxX: 100,
		MaxY: 100,
	}
	transform := NewTransform(bbox, 500, 500)

	tests := []struct {
		x, y   float64
		expect bool
	}{
		{50, 50, true},
		{0, 0, true},
		{100, 100, true},
		{-1, 50, false},
		{101, 50, false},
		{50, -1, false},
		{50, 101, false},
	}

	for _, tt := range tests {
		result := transform.Contains(tt.x, tt.y)
		if result != tt.expect {
			t.Errorf("Contains(%v, %v) = %v, want %v", tt.x, tt.y, result, tt.expect)
		}
	}
}

func floatEquals(a, b float64) bool {
	return math.Abs(a-b) < 0.0001
}

func TestNewTransform_ZeroBBox(t *testing.T) {
	// Test zero width bbox - should not panic
	bbox := query.BBox{MinX: 50, MinY: 0, MaxX: 50, MaxY: 100}
	tr := NewTransform(bbox, 500, 500)

	x, y := tr.ToPixel(50, 50)
	if math.IsNaN(x) || math.IsNaN(y) {
		t.Error("ToPixel returned NaN for zero width bbox")
	}

	// Test zero height bbox
	bbox2 := query.BBox{MinX: 0, MinY: 50, MaxX: 100, MaxY: 50}
	tr2 := NewTransform(bbox2, 500, 500)

	x2, y2 := tr2.ToPixel(50, 50)
	if math.IsNaN(x2) || math.IsNaN(y2) {
		t.Error("ToPixel returned NaN for zero height bbox")
	}
}

func TestTransform_WidthHeight(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	tr := NewTransform(bbox, 800, 600)

	if tr.Width() != 800 {
		t.Errorf("Width() = %d, want 800", tr.Width())
	}
	if tr.Height() != 600 {
		t.Errorf("Height() = %d, want 600", tr.Height())
	}
}

func TestTransform_BBox(t *testing.T) {
	bbox := query.BBox{MinX: -180, MinY: -90, MaxX: 180, MaxY: 90}
	tr := NewTransform(bbox, 800, 400)

	got := tr.BBox()
	if got != bbox {
		t.Errorf("BBox() = %v, want %v", got, bbox)
	}
}

func TestTransform_PixelSize(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	tr := NewTransform(bbox, 500, 500)

	pixelSize := tr.PixelSize()
	expected := 0.2 // 100 units / 500 pixels = 0.2

	if math.Abs(pixelSize-expected) > 0.001 {
		t.Errorf("PixelSize() = %v, want %v", pixelSize, expected)
	}
}

func TestTransform_ScaleDenominator_Projected(t *testing.T) {
	// Test with projected coordinates (not lat/lon)
	bbox := query.BBox{MinX: 500000, MinY: 5000000, MaxX: 510000, MaxY: 5010000}
	tr := NewTransform(bbox, 1000, 1000)

	scale := tr.ScaleDenominator()

	// With 10000m / 1000px = 10m per pixel
	// Scale = 10m / 0.00028m = ~35714
	if scale < 30000 || scale > 40000 {
		t.Errorf("ScaleDenominator() = %v, expected ~35714", scale)
	}
}

func TestTransform_ScaleDenominator_Geographic(t *testing.T) {
	// Test with geographic coordinates (lat/lon)
	bbox := query.BBox{MinX: -10, MinY: 40, MaxX: 10, MaxY: 60}
	tr := NewTransform(bbox, 1000, 1000)

	scale := tr.ScaleDenominator()

	// Should be a reasonable map scale for 20 degrees
	if scale < 1000000 {
		t.Errorf("ScaleDenominator() = %v, expected > 1000000 for geographic coords", scale)
	}
}

func TestTransform_Intersects(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	tr := NewTransform(bbox, 500, 500)

	tests := []struct {
		name                   string
		minX, minY, maxX, maxY float64
		want                   bool
	}{
		{"fully inside", 25, 25, 75, 75, true},
		{"overlapping", -25, -25, 25, 25, true},
		{"touching edge", 100, 0, 150, 50, true},
		{"fully outside left", -100, 0, -50, 50, false},
		{"fully outside right", 150, 0, 200, 50, false},
		{"fully outside top", 0, 150, 50, 200, false},
		{"fully outside bottom", 0, -100, 50, -50, false},
		{"enclosing", -50, -50, 150, 150, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tr.Intersects(tt.minX, tt.minY, tt.maxX, tt.maxY)
			if got != tt.want {
				t.Errorf("Intersects(%v, %v, %v, %v) = %v, want %v",
					tt.minX, tt.minY, tt.maxX, tt.maxY, got, tt.want)
			}
		})
	}
}
