package renderer

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/sld"
)

// RenderCoverage applies a compiled RasterSymbolizer to an aligned numeric
// grid. A nil style selects the deterministic smart built-in portrayal.
func RenderCoverage(grid *datasource.CoverageRenderGrid, style *sld.RasterStyle) (*image.NRGBA, error) {
	if grid == nil || grid.Width < 1 || grid.Height < 1 || len(grid.Bands) == 0 {
		return nil, fmt.Errorf("empty raster render grid")
	}
	if style == nil {
		style = smartRasterStyle(grid)
	}
	imageOut := image.NewNRGBA(image.Rect(0, 0, grid.Width, grid.Height))
	gray, red, green, blue, err := resolveRasterChannels(grid, style)
	if err != nil {
		return nil, err
	}
	if style.ColorMap != nil && gray < 0 {
		return nil, fmt.Errorf("ColorMap requires a gray channel")
	}
	stats := make(map[int]channelStatistics)
	for _, channel := range []struct {
		index       int
		enhancement *sld.ResolvedContrastEnhancement
	}{{gray, enhancement(style, style.Channels.Gray)}, {red, enhancement(style, style.Channels.Red)}, {green, enhancement(style, style.Channels.Green)}, {blue, enhancement(style, style.Channels.Blue)}} {
		if channel.index >= 0 && channel.enhancement != nil && (channel.enhancement.Normalize || channel.enhancement.Histogram) {
			statistics := channelStatistics{valueRange: validRange(grid, channel.index)}
			if channel.enhancement.Histogram {
				statistics.cdf = histogramCDF(grid, channel.index, statistics.valueRange)
			}
			stats[channel.index] = statistics
		}
	}
	alphaBand := alphaBandIndex(grid)
	for pixel := 0; pixel < grid.Width*grid.Height; pixel++ {
		if len(grid.Valid) > pixel && !grid.Valid[pixel] {
			continue
		}
		var c color.NRGBA
		if style.ColorMap != nil {
			c = mapRasterColor(grid.Bands[gray][pixel], style.ColorMap)
		} else if gray >= 0 {
			value := channelByte(grid.Bands[gray][pixel], enhancement(style, style.Channels.Gray), stats[gray])
			c = color.NRGBA{R: value, G: value, B: value, A: 255}
		} else {
			c = color.NRGBA{
				R: channelByte(grid.Bands[red][pixel], enhancement(style, style.Channels.Red), stats[red]),
				G: channelByte(grid.Bands[green][pixel], enhancement(style, style.Channels.Green), stats[green]),
				B: channelByte(grid.Bands[blue][pixel], enhancement(style, style.Channels.Blue), stats[blue]), A: 255,
			}
		}
		if alphaBand >= 0 {
			c.A = channelByte(grid.Bands[alphaBand][pixel], nil, channelStatistics{})
		}
		c.A = uint8(math.Round(float64(c.A) * clamp01(style.Opacity)))
		x, y := pixel%grid.Width, pixel/grid.Width
		imageOut.SetNRGBA(x, y, c)
	}
	return imageOut, nil
}

// RasterLegend renders a compact color strip for a RasterSymbolizer.
func RasterLegend(style *sld.RasterStyle, width, height int) *image.NRGBA {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		ratio := 0.0
		if width > 1 {
			ratio = float64(x) / float64(width-1)
		}
		c := color.NRGBA{R: uint8(ratio * 255), G: uint8(ratio * 255), B: uint8(ratio * 255), A: 255}
		if style != nil && style.ColorMap != nil {
			entries := style.ColorMap.Entries
			value := entries[0].Quantity
			if len(entries) > 1 {
				value += ratio * (entries[len(entries)-1].Quantity - entries[0].Quantity)
			}
			c = mapRasterColor(value, style.ColorMap)
		}
		for y := 0; y < height; y++ {
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

func smartRasterStyle(grid *datasource.CoverageRenderGrid) *sld.RasterStyle {
	normalize := &sld.ResolvedContrastEnhancement{Normalize: true, Gamma: 1}
	style := &sld.RasterStyle{Opacity: 1}
	red, green, blue := -1, -1, -1
	for i, info := range grid.BandInfo {
		switch strings.ToLower(info.ColorInterpretation) {
		case "red":
			red = i
		case "green":
			green = i
		case "blue":
			blue = i
		}
	}
	if red >= 0 && green >= 0 && blue >= 0 {
		style.Channels.Red = &sld.ResolvedChannel{Name: strconv.Itoa(grid.BandNumbers[red]), ContrastEnhancement: normalizeIfNeeded(grid.BandInfo[red], normalize)}
		style.Channels.Green = &sld.ResolvedChannel{Name: strconv.Itoa(grid.BandNumbers[green]), ContrastEnhancement: normalizeIfNeeded(grid.BandInfo[green], normalize)}
		style.Channels.Blue = &sld.ResolvedChannel{Name: strconv.Itoa(grid.BandNumbers[blue]), ContrastEnhancement: normalizeIfNeeded(grid.BandInfo[blue], normalize)}
	} else if len(grid.Bands) >= 3 {
		style.Channels.Red = &sld.ResolvedChannel{Name: strconv.Itoa(grid.BandNumbers[0]), ContrastEnhancement: normalizeIfNeeded(grid.BandInfo[0], normalize)}
		style.Channels.Green = &sld.ResolvedChannel{Name: strconv.Itoa(grid.BandNumbers[1]), ContrastEnhancement: normalizeIfNeeded(grid.BandInfo[1], normalize)}
		style.Channels.Blue = &sld.ResolvedChannel{Name: strconv.Itoa(grid.BandNumbers[2]), ContrastEnhancement: normalizeIfNeeded(grid.BandInfo[2], normalize)}
	} else {
		style.Channels.Gray = &sld.ResolvedChannel{Name: strconv.Itoa(grid.BandNumbers[0]), ContrastEnhancement: normalize}
	}
	return style
}

func normalizeIfNeeded(info datasource.CoverageBand, value *sld.ResolvedContrastEnhancement) *sld.ResolvedContrastEnhancement {
	if strings.EqualFold(info.DataType, "Byte") {
		return nil
	}
	return value
}

func resolveRasterChannels(grid *datasource.CoverageRenderGrid, style *sld.RasterStyle) (gray, red, green, blue int, err error) {
	gray, red, green, blue = -1, -1, -1, -1
	lookup := func(channel *sld.ResolvedChannel) (int, error) {
		if channel == nil {
			return -1, nil
		}
		for i, info := range grid.BandInfo {
			if strings.EqualFold(channel.Name, info.Name) || channel.Name == strconv.Itoa(grid.BandNumbers[i]) {
				return i, nil
			}
		}
		return -1, fmt.Errorf("raster channel %q is not available", channel.Name)
	}
	if style.Channels.Gray != nil {
		gray, err = lookup(style.Channels.Gray)
		return
	}
	if style.Channels.Red != nil || style.Channels.Green != nil || style.Channels.Blue != nil {
		red, err = lookup(style.Channels.Red)
		if err != nil {
			return
		}
		green, err = lookup(style.Channels.Green)
		if err != nil {
			return
		}
		blue, err = lookup(style.Channels.Blue)
		return
	}
	if len(grid.Bands) >= 3 {
		red, green, blue = 0, 1, 2
	} else {
		gray = 0
	}
	return
}

func enhancement(style *sld.RasterStyle, channel *sld.ResolvedChannel) *sld.ResolvedContrastEnhancement {
	if channel != nil && channel.ContrastEnhancement != nil {
		return channel.ContrastEnhancement
	}
	return style.ContrastEnhancement
}
func validRange(grid *datasource.CoverageRenderGrid, band int) [2]float64 {
	min, max := math.Inf(1), math.Inf(-1)
	for i, v := range grid.Bands[band] {
		if len(grid.Valid) > i && !grid.Valid[i] {
			continue
		}
		min = math.Min(min, v)
		max = math.Max(max, v)
	}
	if math.IsInf(min, 1) || min == max {
		return [2]float64{min, min + 1}
	}
	return [2]float64{min, max}
}

type channelStatistics struct {
	valueRange [2]float64
	cdf        [256]float64
}

func channelByte(value float64, ce *sld.ResolvedContrastEnhancement, statistics channelStatistics) uint8 {
	normalized := value / 255
	if ce != nil && ce.Histogram {
		bin := histogramBin(value, statistics.valueRange)
		normalized = statistics.cdf[bin]
	} else if ce != nil && ce.Normalize {
		if ce.Algorithm != "" && ce.MinValue != nil && ce.MaxValue != nil {
			minimum, maximum := *ce.MinValue, *ce.MaxValue
			switch ce.Algorithm {
			case "StretchToMinimumMaximum":
				normalized = (value - minimum) / (maximum - minimum)
			case "ClipToMinimumMaximum":
				normalized = math.Max(minimum, math.Min(maximum, value)) / 255
			case "ClipToZero":
				if value < minimum || value > maximum {
					normalized = 0
				} else {
					normalized = value / 255
				}
			}
		} else {
			normalized = (value - statistics.valueRange[0]) / (statistics.valueRange[1] - statistics.valueRange[0])
		}
	}
	normalized = clamp01(normalized)
	if ce != nil && ce.Gamma > 0 && ce.Gamma != 1 {
		normalized = math.Pow(normalized, 1/ce.Gamma)
	}
	return uint8(math.Round(normalized * 255))
}

func histogramCDF(grid *datasource.CoverageRenderGrid, band int, valueRange [2]float64) [256]float64 {
	var counts [256]int
	total := 0
	for i, value := range grid.Bands[band] {
		if len(grid.Valid) > i && !grid.Valid[i] {
			continue
		}
		counts[histogramBin(value, valueRange)]++
		total++
	}
	var result [256]float64
	if total == 0 {
		return result
	}
	cumulative := 0
	for i, count := range counts {
		cumulative += count
		result[i] = float64(cumulative) / float64(total)
	}
	return result
}

func histogramBin(value float64, valueRange [2]float64) int {
	span := valueRange[1] - valueRange[0]
	if span <= 0 || math.IsInf(span, 0) || math.IsNaN(span) {
		return 0
	}
	bin := int(math.Floor(clamp01((value-valueRange[0])/span) * 255))
	if bin < 0 {
		return 0
	}
	if bin > 255 {
		return 255
	}
	return bin
}
func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func mapRasterColor(value float64, cm *sld.ResolvedColorMap) color.NRGBA {
	entries := cm.Entries
	if cm.Type == "values" {
		for _, entry := range entries {
			if value == entry.Quantity {
				return entryColor(entry)
			}
		}
		return color.NRGBA{}
	}
	if value <= entries[0].Quantity {
		return entryColor(entries[0])
	}
	if value >= entries[len(entries)-1].Quantity {
		return entryColor(entries[len(entries)-1])
	}
	for i := 1; i < len(entries); i++ {
		matches := value <= entries[i].Quantity
		if cm.Type == "intervals" {
			matches = value < entries[i].Quantity
		}
		if matches {
			lower, upper := entries[i-1], entries[i]
			if cm.Type == "intervals" {
				return entryColor(lower)
			}
			ratio := (value - lower.Quantity) / (upper.Quantity - lower.Quantity)
			return color.NRGBA{R: lerp(lower.Color.R, upper.Color.R, ratio), G: lerp(lower.Color.G, upper.Color.G, ratio), B: lerp(lower.Color.B, upper.Color.B, ratio), A: lerp(uint8(lower.Opacity*255), uint8(upper.Opacity*255), ratio)}
		}
	}
	return color.NRGBA{}
}
func entryColor(entry sld.ResolvedColorMapEntry) color.NRGBA {
	return color.NRGBA{R: entry.Color.R, G: entry.Color.G, B: entry.Color.B, A: uint8(math.Round(entry.Opacity * 255))}
}
func lerp(a, b uint8, t float64) uint8 {
	return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
}
func alphaBandIndex(grid *datasource.CoverageRenderGrid) int {
	for i, info := range grid.BandInfo {
		if strings.EqualFold(info.ColorInterpretation, "alpha") {
			return i
		}
	}
	return -1
}
