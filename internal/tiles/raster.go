package tiles

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (e *Engine) generateCoverageMapTile(ctx context.Context, ws *workspace.Workspace, resource *workspace.PublishedResource, tms string, z, x, y int, format TileFormat, styleName, timeValue, elevationValue string) ([]byte, error) {
	coverage, service := resource.Coverage, resource.Service
	source, ok := service.CoverageSource.(datasource.CoverageRenderDataSource)
	if coverage == nil || !ok {
		return nil, fmt.Errorf("coverage source does not support portrayal")
	}
	info, err := service.CoverageSource.GetCoverageInfo(ctx, coverage.SourceCoverage)
	if err != nil {
		return nil, err
	}
	style, err := tileCoverageStyle(ws, coverage, styleName)
	if err != nil {
		return nil, err
	}
	tmsDef, err := GetTileMatrixSetDefinition(tms)
	if err != nil {
		return nil, err
	}
	scale := 0.0
	if z >= 0 && z < len(tmsDef.TileMatrices) {
		scale = tmsDef.TileMatrices[z].ScaleDenominator
	}
	var styles []*sld.RasterStyle
	processName := ""
	if style != nil && style.Transformation != nil {
		processName = style.Transformation.Name
	}
	contour := processName == "contour"
	rasterAlgebra, rasterPoints := processName == "rasteralgebra", processName == "rasteraspointcollections"
	if processName != "" && !contour && !rasterAlgebra && !rasterPoints {
		return nil, fmt.Errorf("rendering transformation %q does not accept coverage input", style.Transformation.Name)
	}
	if style == nil {
		styles = []*sld.RasterStyle{nil}
	} else {
		for _, rule := range sld.FindMatchingRules(style, nil, scale) {
			for _, symbolizer := range rule.Symbolizers {
				if symbolizer.Raster != nil {
					styles = append(styles, symbolizer.Raster)
				}
			}
		}
		if len(styles) == 0 && !contour && !rasterAlgebra && !rasterPoints {
			return nil, fmt.Errorf("selected style has no applicable RasterSymbolizer")
		}
		if contour || rasterAlgebra || rasterPoints {
			styles = []*sld.RasterStyle{nil}
		}
	}
	bounds, err := TileBBox(tms, z, x, y)
	if err != nil {
		return nil, err
	}
	transform := renderer.NewTransform(query.BBox{MinX: bounds.MinX, MinY: bounds.MinY, MaxX: bounds.MaxX, MaxY: bounds.MaxY}, 256, 256)
	canvas := renderer.NewMapRenderer(transform, true, nil)
	for _, dimension := range coverage.Dimensions {
		if dimension == nil {
			continue
		}
		if strings.EqualFold(dimension.Name, "time") && timeValue == "" {
			timeValue = dimension.Default
		}
		if strings.EqualFold(dimension.Name, "elevation") && elevationValue == "" {
			elevationValue = dimension.Default
		}
	}
	for _, rasterStyle := range styles {
		if sld.RasterStyleUsesEnvironment(rasterStyle) {
			if ws.Settings == nil || !slices.Contains(e.cfg.WMS.Extensions, "dynamic-raster") || !slices.Contains(ws.Settings.WMS.Extensions, "dynamic-raster") {
				return nil, fmt.Errorf("style requires the disabled dynamic-raster extension")
			}
			rasterStyle, err = sld.ResolveRasterEnvironment(rasterStyle, nil)
			if err != nil {
				return nil, fmt.Errorf("resolve raster rendering environment: %w", err)
			}
		}
		bands, err := coverageBands(rasterStyle, info, coverage)
		if err != nil {
			return nil, err
		}
		resampling := coverage.Resampling
		if resampling == "" {
			resampling = "bilinear"
		}
		if rasterStyle != nil && rasterStyle.ColorMap != nil && (rasterStyle.ColorMap.Type == "values" || rasterStyle.ColorMap.Type == "intervals") {
			resampling = "nearest"
		}
		grid, err := source.RenderCoverage(ctx, coverage.SourceCoverage, datasource.CoverageRenderRequest{TargetCRS: fmt.Sprintf("EPSG:%d", GetTMSSRID(tms)), BBox: [4]float64{bounds.MinX, bounds.MinY, bounds.MaxX, bounds.MaxY}, Width: 256, Height: 256, Bands: bands, Resampling: resampling, Time: timeValue, Elevation: elevationValue})
		if err != nil {
			return nil, err
		}
		applyCoverageFields(grid, coverage)
		estimatedProcessBytes := int64(grid.Width) * int64(grid.Height) * int64(max(1, len(grid.Bands))) * 8
		if maximum := e.cfg.WMS.MaxProcessMemoryBytes; maximum > 0 && estimatedProcessBytes > maximum {
			return nil, fmt.Errorf("rendering process memory limit exceeded")
		}
		if maximum := e.cfg.WMS.MaxProcessCells; maximum > 0 && grid.Width*grid.Height > maximum {
			return nil, fmt.Errorf("rendering process cell limit exceeded")
		}
		processStarted := time.Now()
		if rasterAlgebra {
			grid, err = renderer.ApplyRasterAlgebra(grid, style.Transformation.Parameters["expression"])
			if err != nil {
				return nil, err
			}
		}
		if contour {
			levels := style.Transformation.Levels
			maximumLevels := e.cfg.WMS.MaxContourLevels
			if maximumLevels <= 0 {
				maximumLevels = 32
			}
			if len(levels) > maximumLevels {
				return nil, fmt.Errorf("contour level limit exceeded")
			}
			var lineStyle *sld.LineStyle
			for _, rule := range sld.FindMatchingRules(style, nil, scale) {
				if rule.LineStyle != nil {
					lineStyle = rule.LineStyle
					break
				}
			}
			canvas.Composite(renderer.RenderContours(grid, levels, lineStyle))
		} else if rasterPoints {
			canvas.Composite(renderer.RenderRasterPoints(grid, firstTilePointStyle(style)))
		} else {
			img, renderErr := renderer.RenderCoverage(grid, rasterStyle)
			if renderErr != nil {
				return nil, renderErr
			}
			canvas.Composite(img)
		}
		if processName != "" {
			if timeout := e.cfg.WMS.ProcessTimeoutMS; timeout > 0 && time.Since(processStarted) > time.Duration(timeout)*time.Millisecond {
				return nil, fmt.Errorf("rendering process timeout exceeded")
			}
		}
	}
	return e.mapGen.encodeImage(canvas.Image(), format)
}

func tileCoverageStyle(ws *workspace.Workspace, coverage *workspace.Coverage, name string) (*sld.Style, error) {
	if name == "" {
		name = coverage.DefaultStyle
	}
	if name == "" {
		return nil, nil
	}
	stored := ws.GetStyle(name)
	if stored == nil {
		return nil, fmt.Errorf("style not found")
	}
	if !stored.Valid && len(stored.ValidationErrors) > 0 {
		return nil, fmt.Errorf("style is unavailable: %s", stored.ValidationErrors[0])
	}
	doc, err := stored.CompiledDocument()
	if err != nil {
		return nil, err
	}
	if style, e := doc.GetStyle(coverage.PublicID, ""); e == nil {
		return style, nil
	}
	if len(doc.NamedLayers) == 1 {
		return doc.GetStyle(doc.NamedLayers[0].Name, "")
	}
	return nil, fmt.Errorf("style has no NamedLayer for %q", coverage.PublicID)
}
func coverageBands(style *sld.RasterStyle, info *datasource.CoverageInfo, coverage *workspace.Coverage) ([]int, error) {
	if style == nil {
		count := len(info.Bands)
		if count > 4 {
			count = 4
		}
		result := make([]int, count)
		for i := range result {
			result[i] = i + 1
		}
		return result, nil
	}
	seen := map[int]bool{}
	var result []int
	for _, channel := range []*sld.ResolvedChannel{style.Channels.Gray, style.Channels.Red, style.Channels.Green, style.Channels.Blue} {
		if channel == nil {
			continue
		}
		number, err := coverageBandNumber(channel.Name, info, coverage)
		if err != nil {
			return nil, err
		}
		if !seen[number] {
			seen[number] = true
			result = append(result, number)
		}
	}
	if len(result) == 0 {
		if style.ColorMap != nil {
			return []int{1}, nil
		}
		count := len(info.Bands)
		if count > 3 {
			count = 3
		}
		for i := 0; i < count; i++ {
			result = append(result, i+1)
		}
	}
	for _, band := range info.Bands {
		if strings.EqualFold(band.ColorInterpretation, "alpha") && !seen[band.Band] && len(result) < 4 {
			seen[band.Band] = true
			result = append(result, band.Band)
			break
		}
	}
	return result, nil
}
func coverageBandNumber(name string, info *datasource.CoverageInfo, coverage *workspace.Coverage) (int, error) {
	if value, err := strconv.Atoi(strings.TrimSpace(name)); err == nil && value > 0 && value <= len(info.Bands) {
		return value, nil
	}
	for _, field := range coverage.RangeFields {
		if strings.EqualFold(field.Name, name) {
			return field.Band, nil
		}
	}
	for _, band := range info.Bands {
		if strings.EqualFold(band.Name, name) {
			return band.Band, nil
		}
	}
	return 0, fmt.Errorf("raster channel %q does not exist", name)
}
func applyCoverageFields(grid *datasource.CoverageRenderGrid, coverage *workspace.Coverage) {
	for i, number := range grid.BandNumbers {
		for _, field := range coverage.RangeFields {
			if field.Band == number {
				grid.BandInfo[i].Name = field.Name
				if len(field.NilValues) > 0 {
					grid.BandInfo[i].NilValues = field.NilValues
				}
				break
			}
		}
	}
}
