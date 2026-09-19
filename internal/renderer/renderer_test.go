package renderer

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/sld"
)

func TestNewMapRenderer_Basic(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)

	r := NewMapRenderer(transform, false, nil)
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}
	if r.width != 256 {
		t.Errorf("expected width 256, got %d", r.width)
	}
	if r.height != 256 {
		t.Errorf("expected height 256, got %d", r.height)
	}
}

func TestMapRendererSVGPreservesVectorAndRasterContent(t *testing.T) {
	transform := NewTransform(query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}, 200, 100)
	r := NewMapRenderer(transform, false, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	point := &Geometry{Type: WKBPoint, Coordinates: [][]float64{{50, 50}}}
	r.DrawGeometry(point, &sld.PointStyle{Shape: "circle", Size: 12, FillColor: color.RGBA{R: 255, A: 255}, StrokeColor: color.RGBA{A: 255}, StrokeWidth: 1, Opacity: 1})
	r.DrawText(point, &sld.TextStyle{Literal: "unsafe", FontSize: 12, Color: color.RGBA{A: 255}, AnchorX: .5, AnchorY: .5}, `A&B <label>`)
	r.Composite(imageWithColor(200, 100, color.NRGBA{G: 255, A: 128}))

	body, err := r.SVG()
	if err != nil {
		t.Fatalf("SVG failed: %v", err)
	}
	var root struct {
		XMLName xml.Name
		Width   int `xml:"width,attr"`
		Height  int `xml:"height,attr"`
	}
	if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&root); err != nil {
		t.Fatalf("invalid SVG XML: %v", err)
	}
	if root.XMLName.Local != "svg" || root.Width != 200 || root.Height != 100 {
		t.Fatalf("unexpected SVG root: %+v", root)
	}
	text := string(body)
	for _, expected := range []string{"<circle", "<text", "A&amp;B &lt;label&gt;", "data:image/png;base64,"} {
		if !strings.Contains(text, expected) {
			t.Errorf("SVG missing %q", expected)
		}
	}
	if strings.Contains(text, "http://example") || strings.Contains(text, "<script") {
		t.Fatal("SVG contains an external or executable reference")
	}
}

func imageWithColor(width, height int, value color.NRGBA) *image.NRGBA {
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			result.SetNRGBA(x, y, value)
		}
	}
	return result
}

func TestNewMapRenderer_Transparent(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)

	r := NewMapRenderer(transform, true, nil)
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}

	img := r.Image()
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestNewMapRenderer_WithBgColor(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)

	bgColor := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	r := NewMapRenderer(transform, false, bgColor)
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}

	img := r.Image()
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestMapRenderer_Image(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)

	r := NewMapRenderer(transform, false, nil)
	img := r.Image()

	if img == nil {
		t.Fatal("expected non-nil image")
	}

	bounds := img.Bounds()
	if bounds.Dx() != 100 {
		t.Errorf("expected width 100, got %d", bounds.Dx())
	}
	if bounds.Dy() != 100 {
		t.Errorf("expected height 100, got %d", bounds.Dy())
	}
}

func TestMapRenderer_Context(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)

	r := NewMapRenderer(transform, false, nil)
	ctx := r.Context()

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
}

func TestMapRenderer_DrawGeometry_Point(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type:        WKBPoint,
		Coordinates: [][]float64{{50, 50}},
	}

	style := &sld.PointStyle{
		Shape:       "circle",
		Size:        10,
		FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
		StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth: 1,
		Opacity:     1.0,
	}

	// Should not panic
	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_PointShapes(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)

	shapes := []string{"circle", "square", "triangle", "star", "cross", "x", "unknown"}

	for _, shape := range shapes {
		t.Run(shape, func(t *testing.T) {
			r := NewMapRenderer(transform, true, nil)
			geom := &Geometry{
				Type:        WKBPoint,
				Coordinates: [][]float64{{50, 50}},
			}

			style := &sld.PointStyle{
				Shape:       shape,
				Size:        10,
				FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
				StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
				StrokeWidth: 1,
				Opacity:     1.0,
			}

			r.DrawGeometry(geom, style)
		})
	}
}

func TestMapRenderer_DrawGeometry_PointWithRotation(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type:        WKBPoint,
		Coordinates: [][]float64{{50, 50}},
	}

	style := &sld.PointStyle{
		Shape:       "square",
		Size:        10,
		FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
		StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth: 1,
		Opacity:     1.0,
		Rotation:    45.0,
	}

	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_LineString(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type: WKBLineString,
		Coordinates: [][]float64{
			{10, 10},
			{50, 50},
			{90, 10},
		},
	}

	style := &sld.LineStyle{
		Color:    color.RGBA{R: 0, G: 0, B: 255, A: 255},
		Width:    2,
		Opacity:  1.0,
		LineCap:  "round",
		LineJoin: "round",
	}

	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_LineStringStyles(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)

	testCases := []struct {
		name     string
		lineCap  string
		lineJoin string
		dash     []float64
	}{
		{"round-round", "round", "round", nil},
		{"butt-miter", "butt", "miter", nil},
		{"square-bevel", "square", "bevel", nil},
		{"dashed", "round", "round", []float64{5, 3}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMapRenderer(transform, true, nil)
			geom := &Geometry{
				Type: WKBLineString,
				Coordinates: [][]float64{
					{10, 10},
					{50, 50},
					{90, 10},
				},
			}

			style := &sld.LineStyle{
				Color:     color.RGBA{R: 0, G: 0, B: 255, A: 255},
				Width:     2,
				Opacity:   1.0,
				LineCap:   tc.lineCap,
				LineJoin:  tc.lineJoin,
				DashArray: tc.dash,
			}

			r.DrawGeometry(geom, style)
		})
	}
}

func TestMapRenderer_DrawGeometry_Polygon(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type: WKBPolygon,
		Rings: [][][]float64{
			{
				{10, 10},
				{90, 10},
				{90, 90},
				{10, 90},
				{10, 10},
			},
		},
	}

	style := &sld.PolygonStyle{
		FillColor:     color.RGBA{R: 0, G: 255, B: 0, A: 255},
		FillOpacity:   0.5,
		StrokeColor:   color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth:   2,
		StrokeOpacity: 1.0,
	}

	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_PolygonNoStroke(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type: WKBPolygon,
		Rings: [][][]float64{
			{
				{10, 10},
				{90, 10},
				{90, 90},
				{10, 90},
				{10, 10},
			},
		},
	}

	style := &sld.PolygonStyle{
		FillColor:   color.RGBA{R: 0, G: 255, B: 0, A: 255},
		FillOpacity: 0.5,
		StrokeWidth: 0, // No stroke
	}

	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_MultiPoint(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type: WKBMultiPoint,
		Geometries: []Geometry{
			{Type: WKBPoint, Coordinates: [][]float64{{20, 20}}},
			{Type: WKBPoint, Coordinates: [][]float64{{50, 50}}},
			{Type: WKBPoint, Coordinates: [][]float64{{80, 80}}},
		},
	}

	style := &sld.PointStyle{
		Shape:       "circle",
		Size:        10,
		FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
		StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth: 1,
		Opacity:     1.0,
	}

	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_MultiLineString(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type: WKBMultiLineString,
		Geometries: []Geometry{
			{Type: WKBLineString, Coordinates: [][]float64{{10, 10}, {30, 30}}},
			{Type: WKBLineString, Coordinates: [][]float64{{70, 70}, {90, 90}}},
		},
	}

	style := &sld.LineStyle{
		Color:    color.RGBA{R: 0, G: 0, B: 255, A: 255},
		Width:    2,
		Opacity:  1.0,
		LineCap:  "round",
		LineJoin: "round",
	}

	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_MultiPolygon(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type: WKBMultiPolygon,
		Geometries: []Geometry{
			{
				Type: WKBPolygon,
				Rings: [][][]float64{
					{{10, 10}, {40, 10}, {40, 40}, {10, 40}, {10, 10}},
				},
			},
			{
				Type: WKBPolygon,
				Rings: [][][]float64{
					{{60, 60}, {90, 60}, {90, 90}, {60, 90}, {60, 60}},
				},
			},
		},
	}

	style := &sld.PolygonStyle{
		FillColor:     color.RGBA{R: 0, G: 255, B: 0, A: 255},
		FillOpacity:   0.5,
		StrokeColor:   color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth:   2,
		StrokeOpacity: 1.0,
	}

	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_GeometryCollection(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type: WKBGeometryCollection,
		Geometries: []Geometry{
			{Type: WKBPoint, Coordinates: [][]float64{{50, 50}}},
		},
	}

	style := &sld.PointStyle{
		Shape:       "circle",
		Size:        10,
		FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
		StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth: 1,
		Opacity:     1.0,
	}

	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_WrongStyle(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	geom := &Geometry{
		Type:        WKBPoint,
		Coordinates: [][]float64{{50, 50}},
	}

	// Pass wrong style type
	style := &sld.LineStyle{
		Color: color.RGBA{R: 0, G: 0, B: 255, A: 255},
		Width: 2,
	}

	// Should not panic, just do nothing
	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_EmptyCoords(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	// Point with empty coordinates
	geom := &Geometry{
		Type:        WKBPoint,
		Coordinates: [][]float64{},
	}

	style := &sld.PointStyle{
		Shape:       "circle",
		Size:        10,
		FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
		StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth: 1,
		Opacity:     1.0,
	}

	// Should not panic
	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_ShortLineString(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	// LineString with only one point
	geom := &Geometry{
		Type:        WKBLineString,
		Coordinates: [][]float64{{50, 50}},
	}

	style := &sld.LineStyle{
		Color:   color.RGBA{R: 0, G: 0, B: 255, A: 255},
		Width:   2,
		Opacity: 1.0,
	}

	// Should not panic
	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawGeometry_EmptyPolygon(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 256, 256)
	r := NewMapRenderer(transform, true, nil)

	// Polygon with empty rings
	geom := &Geometry{
		Type:  WKBPolygon,
		Rings: [][][]float64{},
	}

	style := &sld.PolygonStyle{
		FillColor:   color.RGBA{R: 0, G: 255, B: 0, A: 255},
		FillOpacity: 0.5,
	}

	// Should not panic
	r.DrawGeometry(geom, style)
}

func TestMapRenderer_DrawLegendSymbol_Point(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)
	r := NewMapRenderer(transform, true, nil)

	style := &sld.PointStyle{
		Shape:       "circle",
		Size:        10,
		FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
		StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth: 1,
		Opacity:     1.0,
	}

	r.DrawLegendSymbol(5, 5, 20, 20, "Point", style)
}

func TestMapRenderer_DrawLegendSymbol_MultiPoint(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)
	r := NewMapRenderer(transform, true, nil)

	style := &sld.PointStyle{
		Shape:       "circle",
		Size:        10,
		FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
		StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth: 1,
		Opacity:     1.0,
	}

	r.DrawLegendSymbol(5, 5, 20, 20, "MultiPoint", style)
}

func TestMapRenderer_DrawLegendSymbol_LineString(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)
	r := NewMapRenderer(transform, true, nil)

	style := &sld.LineStyle{
		Color:   color.RGBA{R: 0, G: 0, B: 255, A: 255},
		Width:   2,
		Opacity: 1.0,
	}

	r.DrawLegendSymbol(5, 5, 20, 20, "LineString", style)
}

func TestMapRenderer_DrawLegendSymbol_MultiLineString(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)
	r := NewMapRenderer(transform, true, nil)

	style := &sld.LineStyle{
		Color:   color.RGBA{R: 0, G: 0, B: 255, A: 255},
		Width:   2,
		Opacity: 1.0,
	}

	r.DrawLegendSymbol(5, 5, 20, 20, "MultiLineString", style)
}

func TestMapRenderer_DrawLegendSymbol_Polygon(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)
	r := NewMapRenderer(transform, true, nil)

	style := &sld.PolygonStyle{
		FillColor:     color.RGBA{R: 0, G: 255, B: 0, A: 255},
		FillOpacity:   0.5,
		StrokeColor:   color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth:   2,
		StrokeOpacity: 1.0,
	}

	r.DrawLegendSymbol(5, 5, 20, 20, "Polygon", style)
}

func TestMapRenderer_DrawLegendSymbol_PolygonNoStroke(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)
	r := NewMapRenderer(transform, true, nil)

	style := &sld.PolygonStyle{
		FillColor:   color.RGBA{R: 0, G: 255, B: 0, A: 255},
		FillOpacity: 0.5,
		StrokeWidth: 0,
	}

	r.DrawLegendSymbol(5, 5, 20, 20, "MultiPolygon", style)
}

func TestMapRenderer_DrawLegendSymbol_Unknown(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)
	r := NewMapRenderer(transform, true, nil)

	style := &sld.PointStyle{
		Shape:       "circle",
		Size:        10,
		FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
		StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth: 1,
		Opacity:     1.0,
	}

	// Unknown geometry type should not panic
	r.DrawLegendSymbol(5, 5, 20, 20, "Unknown", style)
}

func TestMapRenderer_DrawLegendSymbol_WrongStyle(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)
	r := NewMapRenderer(transform, true, nil)

	style := &sld.LineStyle{
		Color:   color.RGBA{R: 0, G: 0, B: 255, A: 255},
		Width:   2,
		Opacity: 1.0,
	}

	// Point with LineStyle should not panic
	r.DrawLegendSymbol(5, 5, 20, 20, "Point", style)
}

func TestMapRenderer_DrawPointDirect_Shapes(t *testing.T) {
	bbox := query.BBox{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100}
	transform := NewTransform(bbox, 100, 100)

	shapes := []string{"circle", "square", "triangle", "star", "cross", "x", "unknown"}

	for _, shape := range shapes {
		t.Run(shape, func(t *testing.T) {
			r := NewMapRenderer(transform, true, nil)

			style := &sld.PointStyle{
				Shape:       shape,
				Size:        10,
				FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
				StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
				StrokeWidth: 1,
				Opacity:     1.0,
			}

			r.DrawLegendSymbol(5, 5, 20, 20, "Point", style)
		})
	}
}
