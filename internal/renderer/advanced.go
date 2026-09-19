package renderer

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"strings"

	"github.com/fogleman/gg"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/sld"
)

type HeatPoint struct {
	X, Y   float64
	Weight float64
}

// RenderPointStacker aggregates point observations into a bounded pixel grid.
// Symbol area and the optional count label communicate stack cardinality.
func RenderPointStacker(transform *Transform, points []HeatPoint, cellSize float64, style *sld.PointStyle) image.Image {
	if cellSize < 2 {
		cellSize = 32
	}
	if style == nil {
		style = sld.DefaultPointStyle()
	}
	type stack struct {
		x, y  float64
		count int
	}
	stacks := map[[2]int]*stack{}
	for _, point := range points {
		x, y := transform.ToPixel(point.X, point.Y)
		key := [2]int{int(math.Floor(x / cellSize)), int(math.Floor(y / cellSize))}
		value := stacks[key]
		if value == nil {
			value = &stack{}
			stacks[key] = value
		}
		value.x += x
		value.y += y
		value.count++
	}
	result := image.NewRGBA(image.Rect(0, 0, transform.Width(), transform.Height()))
	dc := gg.NewContextForRGBA(result)
	for _, value := range stacks {
		x, y := value.x/float64(value.count), value.y/float64(value.count)
		radius := math.Max(style.Size/2, math.Sqrt(float64(value.count))*3)
		dc.DrawCircle(x, y, radius)
		dc.SetRGBA255(int(style.FillColor.R), int(style.FillColor.G), int(style.FillColor.B), int(float64(style.FillColor.A)*style.Opacity))
		dc.FillPreserve()
		dc.SetRGBA255(int(style.StrokeColor.R), int(style.StrokeColor.G), int(style.StrokeColor.B), int(style.StrokeColor.A))
		dc.SetLineWidth(style.StrokeWidth)
		dc.Stroke()
		if value.count > 1 {
			dc.SetColor(color.Black)
			dc.DrawStringAnchored(strconv.Itoa(value.count), x, y, .5, .5)
		}
	}
	return result
}

// ApplyRasterAlgebra evaluates the intentionally small raster-algebra grammar:
// bandN, bandN op number, or bandN op bandM. It returns a detached one-band grid.
func ApplyRasterAlgebra(grid *datasource.CoverageRenderGrid, expression string) (*datasource.CoverageRenderGrid, error) {
	if grid == nil {
		return nil, fmt.Errorf("raster algebra input is nil")
	}
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(expression)))
	if len(fields) != 1 && len(fields) != 3 {
		return nil, fmt.Errorf("raster algebra expression must be 'bandN' or 'bandN op value'")
	}
	bandIndex, err := parseAlgebraBand(fields[0], len(grid.Bands))
	if err != nil {
		return nil, err
	}
	result := &datasource.CoverageRenderGrid{Width: grid.Width, Height: grid.Height, BandNumbers: []int{bandIndex + 1}, Bands: [][]float64{append([]float64(nil), grid.Bands[bandIndex]...)}, Valid: append([]bool(nil), grid.Valid...)}
	if bandIndex < len(grid.BandInfo) {
		result.BandInfo = []datasource.CoverageBand{grid.BandInfo[bandIndex]}
	}
	if len(fields) == 1 {
		return result, nil
	}
	operator := fields[1]
	var rightBand []float64
	rightConstant, constantErr := strconv.ParseFloat(fields[2], 64)
	if constantErr != nil {
		rightIndex, e := parseAlgebraBand(fields[2], len(grid.Bands))
		if e != nil {
			return nil, e
		}
		rightBand = grid.Bands[rightIndex]
	}
	for index, left := range result.Bands[0] {
		right := rightConstant
		if rightBand != nil {
			right = rightBand[index]
		}
		switch operator {
		case "+":
			result.Bands[0][index] = left + right
		case "-":
			result.Bands[0][index] = left - right
		case "*":
			result.Bands[0][index] = left * right
		case "/":
			if right == 0 {
				return nil, fmt.Errorf("raster algebra division by zero")
			}
			result.Bands[0][index] = left / right
		default:
			return nil, fmt.Errorf("unsupported raster algebra operator %q", operator)
		}
	}
	return result, nil
}
func parseAlgebraBand(value string, count int) (int, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "band")
	index, err := strconv.Atoi(value)
	if err != nil || index < 1 || index > count {
		return 0, fmt.Errorf("invalid raster algebra band %q", value)
	}
	return index - 1, nil
}

func RenderRasterPoints(grid *datasource.CoverageRenderGrid, style *sld.PointStyle) image.Image {
	if grid == nil {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	if style == nil {
		style = sld.DefaultPointStyle()
	}
	result := image.NewRGBA(image.Rect(0, 0, grid.Width, grid.Height))
	dc := gg.NewContextForRGBA(result)
	radius := math.Max(.5, math.Min(style.Size/2, 2))
	for y := 0; y < grid.Height; y++ {
		for x := 0; x < grid.Width; x++ {
			index := y*grid.Width + x
			if len(grid.Valid) > 0 && !grid.Valid[index] {
				continue
			}
			dc.DrawCircle(float64(x)+.5, float64(y)+.5, radius)
			dc.SetRGBA255(int(style.FillColor.R), int(style.FillColor.G), int(style.FillColor.B), int(float64(style.FillColor.A)*style.Opacity))
			dc.Fill()
		}
	}
	return result
}

// CompositeWithMode composites a complete layer image using the supported
// SLD compositing subset. The source is retained as a hybrid SVG image layer.
func (r *MapRenderer) CompositeWithMode(source image.Image, mode string, opacity float64) {
	if source == nil {
		return
	}
	if mode == "" {
		mode = "source-over"
	}
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 1 {
		opacity = 1
	}
	r.scene = append(r.scene, sceneCommand{raster: source, blendMode: mode, opacity: opacity})
	destination := image.NewNRGBA(image.Rect(0, 0, r.width, r.height))
	draw.Draw(destination, destination.Bounds(), r.dc.Image(), r.dc.Image().Bounds().Min, draw.Src)
	result := image.NewRGBA(destination.Bounds())
	for y := 0; y < r.height; y++ {
		for x := 0; x < r.width; x++ {
			d := color.NRGBAModel.Convert(destination.At(x, y)).(color.NRGBA)
			s := color.NRGBAModel.Convert(source.At(source.Bounds().Min.X+x, source.Bounds().Min.Y+y)).(color.NRGBA)
			result.Set(x, y, blendPixel(d, s, mode, opacity))
		}
	}
	r.dc = gg.NewContextForRGBA(result)
}

func blendPixel(destination, source color.NRGBA, mode string, opacity float64) color.NRGBA {
	da, sa := float64(destination.A)/255, float64(source.A)/255*opacity
	porterDuff := map[string][2]float64{"source-over": {1, 1 - sa}, "source-in": {da, 0}, "source-out": {1 - da, 0}, "source-atop": {da, 1 - sa}, "destination-over": {1 - da, 1}, "destination-in": {0, sa}, "destination-out": {0, 1 - sa}, "destination-atop": {1 - da, sa}, "xor": {1 - da, 1 - sa}, "copy": {1, 0}}
	if factors, ok := porterDuff[mode]; ok {
		alpha := sa*factors[0] + da*factors[1]
		channel := func(d, s uint8) uint8 {
			if alpha == 0 {
				return 0
			}
			premultiplied := float64(s)/255*sa*factors[0] + float64(d)/255*da*factors[1]
			return byte(math.Round(clamp01(premultiplied/alpha) * 255))
		}
		return color.NRGBA{R: channel(destination.R, source.R), G: channel(destination.G, source.G), B: channel(destination.B, source.B), A: byte(math.Round(clamp01(alpha) * 255))}
	}
	alpha := sa + da - sa*da
	channel := func(d, s uint8) uint8 {
		if alpha == 0 {
			return 0
		}
		dv, sv := float64(d)/255, float64(s)/255
		blended := blendChannel(dv, sv, mode)
		premultiplied := (1-sa)*dv*da + (1-da)*sv*sa + sa*da*blended
		return byte(math.Round(clamp01(premultiplied/alpha) * 255))
	}
	return color.NRGBA{R: channel(destination.R, source.R), G: channel(destination.G, source.G), B: channel(destination.B, source.B), A: byte(math.Round(clamp01(alpha) * 255))}
}

func blendChannel(destination, source float64, mode string) float64 {
	switch mode {
	case "multiply":
		return destination * source
	case "screen":
		return 1 - (1-destination)*(1-source)
	case "overlay":
		if destination <= .5 {
			return 2 * destination * source
		}
		return 1 - 2*(1-destination)*(1-source)
	case "darken":
		return math.Min(destination, source)
	case "lighten":
		return math.Max(destination, source)
	case "color-dodge":
		if source >= 1 {
			return 1
		}
		return math.Min(1, destination/(1-source))
	case "color-burn":
		if source <= 0 {
			return 0
		}
		return 1 - math.Min(1, (1-destination)/source)
	case "hard-light":
		if source <= .5 {
			return 2 * destination * source
		}
		return 1 - 2*(1-destination)*(1-source)
	case "soft-light":
		if source <= .5 {
			return destination - (1-2*source)*destination*(1-destination)
		}
		d := math.Sqrt(destination)
		if destination <= .25 {
			d = ((16*destination-12)*destination + 4) * destination
		}
		return destination + (2*source-1)*(d-destination)
	case "difference":
		return math.Abs(destination - source)
	case "exclusion":
		return destination + source - 2*destination*source
	default:
		return source
	}
}

// RenderHeatmap rasterizes weighted map-coordinate points with a Gaussian
// kernel and a compact blue/cyan/yellow/red ramp.
func RenderHeatmap(transform *Transform, points []HeatPoint, radius float64) image.Image {
	if radius <= 0 {
		radius = 20
	}
	width, height := transform.Width(), transform.Height()
	values := make([]float64, width*height)
	maximum := 0.0
	for _, point := range points {
		x, y := transform.ToPixel(point.X, point.Y)
		weight := point.Weight
		if weight <= 0 {
			weight = 1
		}
		limit := int(math.Ceil(radius * 2))
		for dy := -limit; dy <= limit; dy++ {
			py := int(math.Round(y)) + dy
			if py < 0 || py >= height {
				continue
			}
			for dx := -limit; dx <= limit; dx++ {
				px := int(math.Round(x)) + dx
				if px < 0 || px >= width {
					continue
				}
				distance2 := float64(dx*dx + dy*dy)
				value := weight * math.Exp(-distance2/(2*radius*radius))
				index := py*width + px
				values[index] += value
				maximum = math.Max(maximum, values[index])
			}
		}
	}
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	if maximum == 0 {
		return result
	}
	for index, value := range values {
		if value <= 0 {
			continue
		}
		ratio := math.Min(1, value/maximum)
		result.SetNRGBA(index%width, index/width, heatColor(ratio))
	}
	return result
}

func heatColor(value float64) color.NRGBA {
	alpha := uint8(math.Round(math.Min(1, value*1.5) * 230))
	if value < .33 {
		t := value / .33
		return color.NRGBA{G: uint8(255 * t), B: 255, A: alpha}
	}
	if value < .66 {
		t := (value - .33) / .33
		return color.NRGBA{R: uint8(255 * t), G: 255, B: uint8(255 * (1 - t)), A: alpha}
	}
	t := (value - .66) / .34
	return color.NRGBA{R: 255, G: uint8(255 * (1 - t)), A: alpha}
}

// RenderContours uses marching squares over the first selected band.
func RenderContours(grid *datasource.CoverageRenderGrid, levels []float64, style *sld.LineStyle) image.Image {
	if grid == nil {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	result := image.NewRGBA(image.Rect(0, 0, grid.Width, grid.Height))
	if grid.Width < 2 || grid.Height < 2 || len(grid.Bands) == 0 {
		return result
	}
	if style == nil {
		style = sld.DefaultLineStyle()
	}
	if len(levels) == 0 {
		minimum, maximum := math.Inf(1), math.Inf(-1)
		for index, value := range grid.Bands[0] {
			if len(grid.Valid) == 0 || index < len(grid.Valid) && grid.Valid[index] {
				minimum, maximum = math.Min(minimum, value), math.Max(maximum, value)
			}
		}
		if minimum < maximum {
			for index := 1; index < 10; index++ {
				levels = append(levels, minimum+(maximum-minimum)*float64(index)/10)
			}
		}
	}
	dc := gg.NewContextForRGBA(result)
	dc.SetRGBA255(int(style.Color.R), int(style.Color.G), int(style.Color.B), int(float64(style.Color.A)*style.Opacity))
	dc.SetLineWidth(style.Width)
	values := grid.Bands[0]
	for _, level := range levels {
		for y := 0; y < grid.Height-1; y++ {
			for x := 0; x < grid.Width-1; x++ {
				i0, i1, i2, i3 := y*grid.Width+x, y*grid.Width+x+1, (y+1)*grid.Width+x+1, (y+1)*grid.Width+x
				if len(grid.Valid) > 0 && (i3 >= len(grid.Valid) || !grid.Valid[i0] || !grid.Valid[i1] || !grid.Valid[i2] || !grid.Valid[i3]) {
					continue
				}
				var crossings [][2]float64
				edges := [][4]float64{{float64(x), float64(y), float64(x + 1), float64(y)}, {float64(x + 1), float64(y), float64(x + 1), float64(y + 1)}, {float64(x + 1), float64(y + 1), float64(x), float64(y + 1)}, {float64(x), float64(y + 1), float64(x), float64(y)}}
				pairs := [][2]int{{i0, i1}, {i1, i2}, {i2, i3}, {i3, i0}}
				for edgeIndex, pair := range pairs {
					a, b := values[pair[0]], values[pair[1]]
					if (a < level) == (b < level) || a == b {
						continue
					}
					ratio := (level - a) / (b - a)
					edge := edges[edgeIndex]
					crossings = append(crossings, [2]float64{edge[0] + (edge[2]-edge[0])*ratio, edge[1] + (edge[3]-edge[1])*ratio})
				}
				for index := 1; index < len(crossings); index += 2 {
					dc.DrawLine(crossings[index-1][0], crossings[index-1][1], crossings[index][0], crossings[index][1])
				}
			}
		}
	}
	dc.Stroke()
	return result
}
