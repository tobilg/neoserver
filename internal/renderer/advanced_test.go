package renderer

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/sld"
)

func TestLabelCollisionPriorityAndSVG(t *testing.T) {
	renderer := NewMapRenderer(NewTransform(query.BBox{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10}, 100, 100), true, nil)
	point := &Geometry{Type: WKBPoint, Coordinates: [][]float64{{5, 5}}}
	low := &sld.TextStyle{FontSize: 12, Color: color.RGBA{A: 255}, AnchorX: .5, AnchorY: .5, ConflictResolution: true, SpaceAround: 2, Priority: 1}
	high := *low
	high.Priority = 10
	renderer.DrawText(point, low, "low")
	renderer.DrawText(point, &high, "high")
	svg, err := renderer.SVG()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(svg), ">low</text>") || !strings.Contains(string(svg), ">high</text>") {
		t.Fatalf("collision result: %s", svg)
	}
}

func TestBlendHeatmapAndContours(t *testing.T) {
	renderer := NewMapRenderer(NewTransform(query.BBox{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}, 4, 4), false, color.White)
	source := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			source.Set(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	renderer.CompositeWithMode(source, "multiply", .5)
	if _, _, _, alpha := renderer.Image().At(0, 0).RGBA(); alpha == 0 {
		t.Fatal("blend produced transparent pixel")
	}
	heat := RenderHeatmap(NewTransform(query.BBox{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}, 16, 16), []HeatPoint{{X: .5, Y: .5, Weight: 1}}, 3)
	if _, _, _, alpha := heat.At(8, 8).RGBA(); alpha == 0 {
		t.Fatal("heatmap center is transparent")
	}
	grid := &datasource.CoverageRenderGrid{Width: 2, Height: 2, Bands: [][]float64{{0, 1, 0, 1}}, Valid: []bool{true, true, true, true}}
	contours := RenderContours(grid, []float64{.5}, nil)
	found := false
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			_, _, _, a := contours.At(x, y).RGBA()
			found = found || a > 0
		}
	}
	if !found {
		t.Fatal("contour was not drawn")
	}
}
