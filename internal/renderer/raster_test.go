package renderer

import (
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/sld"
	"image/color"
	"testing"
)

func TestRenderCoverageColorMapAndNoData(t *testing.T) {
	grid := &datasource.CoverageRenderGrid{Width: 3, Height: 1, BandNumbers: []int{1}, Bands: [][]float64{{0, 50, 100}}, Valid: []bool{true, false, true}, BandInfo: []datasource.CoverageBand{{Band: 1, Name: "band1"}}}
	style := &sld.RasterStyle{Opacity: 1, Channels: sld.RasterChannels{Gray: &sld.ResolvedChannel{Name: "1"}}, ColorMap: &sld.ResolvedColorMap{Type: "ramp", Entries: []sld.ResolvedColorMapEntry{{Color: rgba(0, 0, 255), Quantity: 0, Opacity: 1}, {Color: rgba(255, 0, 0), Quantity: 100, Opacity: 1}}}}
	img, err := RenderCoverage(grid, style)
	if err != nil {
		t.Fatal(err)
	}
	if img.NRGBAAt(0, 0).B != 255 || img.NRGBAAt(2, 0).R != 255 {
		t.Fatalf("unexpected ramp colors: %v %v", img.NRGBAAt(0, 0), img.NRGBAAt(2, 0))
	}
	if img.NRGBAAt(1, 0).A != 0 {
		t.Fatalf("NoData pixel is not transparent: %v", img.NRGBAAt(1, 0))
	}
}

func TestRenderCoverageHistogramEqualization(t *testing.T) {
	grid := &datasource.CoverageRenderGrid{Width: 4, Height: 1, BandNumbers: []int{1}, Bands: [][]float64{{0, 0, 0, 100}}, Valid: []bool{true, true, true, true}, BandInfo: []datasource.CoverageBand{{Band: 1, Name: "band1"}}}
	style := &sld.RasterStyle{Opacity: 1, Channels: sld.RasterChannels{Gray: &sld.ResolvedChannel{Name: "1"}}, ContrastEnhancement: &sld.ResolvedContrastEnhancement{Histogram: true, Gamma: 1}}
	img, err := RenderCoverage(grid, style)
	if err != nil {
		t.Fatal(err)
	}
	if low, high := img.NRGBAAt(0, 0).R, img.NRGBAAt(3, 0).R; low < 180 || high != 255 || low >= high {
		t.Fatalf("unexpected equalized values: low=%d high=%d", low, high)
	}
}

func TestRenderCoverageNormalizeAlgorithms(t *testing.T) {
	minimum, maximum := 10.0, 20.0
	grid := &datasource.CoverageRenderGrid{Width: 3, Height: 1, BandNumbers: []int{1}, Bands: [][]float64{{0, 15, 30}}, Valid: []bool{true, true, true}, BandInfo: []datasource.CoverageBand{{Band: 1, Name: "band1"}}}
	style := &sld.RasterStyle{Opacity: 1, Channels: sld.RasterChannels{Gray: &sld.ResolvedChannel{Name: "1"}}, ContrastEnhancement: &sld.ResolvedContrastEnhancement{Normalize: true, Algorithm: "StretchToMinimumMaximum", MinValue: &minimum, MaxValue: &maximum, Gamma: 1}}
	img, err := RenderCoverage(grid, style)
	if err != nil {
		t.Fatal(err)
	}
	if img.NRGBAAt(0, 0).R != 0 || img.NRGBAAt(1, 0).R < 127 || img.NRGBAAt(1, 0).R > 128 || img.NRGBAAt(2, 0).R != 255 {
		t.Fatalf("unexpected stretch: %v %v %v", img.NRGBAAt(0, 0), img.NRGBAAt(1, 0), img.NRGBAAt(2, 0))
	}
}
func rgba(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 255} }
