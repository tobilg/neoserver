package wms

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) renderCoverageLayer(ctx context.Context, mapRenderer *renderer.MapRenderer, req *GetMapRequest, ws *workspace.Workspace, resource *workspace.PublishedResource, inline *sld.StyledLayerDescriptor, requestedStyle string, scale float64) error {
	coverage, service := resource.Coverage, resource.Service
	source, ok := service.CoverageSource.(datasource.CoverageRenderDataSource)
	if coverage == nil || !ok {
		return fmt.Errorf("coverage source does not support portrayal")
	}
	info, err := service.CoverageSource.GetCoverageInfo(ctx, coverage.SourceCoverage)
	if err != nil {
		return err
	}
	style, err := h.resolveWMSStyle(ws, resource, inline, requestedStyle)
	if err != nil {
		return err
	}
	targetRenderer := mapRenderer
	if sld.StyleUsesCompositing(style) {
		if !h.extensionEnabled(ws, "compositing") {
			return fmt.Errorf("style requires the disabled compositing extension")
		}
		targetRenderer = h.newMapRenderer(renderer.NewTransform(req.BBox, req.Width, req.Height), true, nil, ws)
	}
	if sld.StyleUsesTransformation(style) && !h.extensionEnabled(ws, "rendering-transformations") {
		return fmt.Errorf("style requires the disabled rendering-transformations extension")
	}
	processName := ""
	if style != nil && style.Transformation != nil {
		processName = style.Transformation.Name
	}
	contour := processName == "contour"
	rasterAlgebra, rasterPoints := processName == "rasteralgebra", processName == "rasteraspointcollections"
	if processName != "" && !contour && !rasterAlgebra && !rasterPoints {
		return fmt.Errorf("rendering transformation %q does not accept coverage input", style.Transformation.Name)
	}
	var rasterStyles []*sld.RasterStyle
	if style != nil {
		for _, rule := range sld.FindMatchingRules(style, nil, scale) {
			if len(rule.Symbolizers) > 0 {
				for _, symbolizer := range rule.Symbolizers {
					if symbolizer.Raster != nil {
						rasterStyles = append(rasterStyles, symbolizer.Raster)
					}
				}
			} else if rule.RasterStyle != nil {
				rasterStyles = append(rasterStyles, rule.RasterStyle)
			}
		}
		if len(rasterStyles) == 0 && !contour && !rasterAlgebra && !rasterPoints {
			return fmt.Errorf("selected style has no applicable RasterSymbolizer")
		}
		if contour || rasterAlgebra || rasterPoints {
			rasterStyles = []*sld.RasterStyle{nil}
		}
	} else {
		rasterStyles = []*sld.RasterStyle{nil}
	}
	for _, rasterStyle := range rasterStyles {
		if sld.RasterStyleUsesEnvironment(rasterStyle) {
			if !h.extensionEnabled(ws, "dynamic-raster") {
				return fmt.Errorf("style requires the disabled dynamic-raster extension")
			}
			rasterStyle, err = sld.ResolveRasterEnvironment(rasterStyle, req.Environment)
			if err != nil {
				return fmt.Errorf("resolve raster rendering environment: %w", err)
			}
		}
		bands, err := rasterBands(rasterStyle, info, coverage)
		if err != nil {
			return err
		}
		estimated := int64(req.Width) * int64(req.Height) * (int64(len(bands))*8 + 5)
		if h.cfg.WMS.MaxRasterMemoryBytes > 0 && estimated > h.cfg.WMS.MaxRasterMemoryBytes {
			return fmt.Errorf("raster portrayal exceeds working-memory limit")
		}
		resampling := coverage.Resampling
		if resampling == "" {
			resampling = "bilinear"
		}
		if rasterStyle != nil && rasterStyle.ColorMap != nil && (rasterStyle.ColorMap.Type == "values" || rasterStyle.ColorMap.Type == "intervals") {
			resampling = "nearest"
		}
		timeValue, elevationValue := coverageDimensionValues(coverage, req.Time, req.Elevation)
		grid, err := source.RenderCoverage(ctx, coverage.SourceCoverage, datasource.CoverageRenderRequest{TargetCRS: req.CRS, BBox: [4]float64{req.BBox.MinX, req.BBox.MinY, req.BBox.MaxX, req.BBox.MaxY}, Width: req.Width, Height: req.Height, Bands: bands, Resampling: resampling, Time: timeValue, Elevation: elevationValue})
		if err != nil {
			return err
		}
		applyRangeFieldNames(grid, coverage)
		estimatedProcessBytes := int64(grid.Width) * int64(grid.Height) * int64(max(1, len(grid.Bands))) * 8
		if maximum := h.cfg.WMS.MaxProcessMemoryBytes; maximum > 0 && estimatedProcessBytes > maximum {
			return fmt.Errorf("rendering process memory limit exceeded")
		}
		if maximum := h.cfg.WMS.MaxProcessCells; maximum > 0 && grid.Width*grid.Height > maximum {
			return fmt.Errorf("rendering process cell limit exceeded")
		}
		processStarted := time.Now()
		if rasterAlgebra {
			grid, err = renderer.ApplyRasterAlgebra(grid, style.Transformation.Parameters["expression"])
			if err != nil {
				return err
			}
		}
		if contour {
			levels := style.Transformation.Levels
			maximumLevels := h.cfg.WMS.MaxContourLevels
			if maximumLevels <= 0 {
				maximumLevels = 32
			}
			if len(levels) > maximumLevels {
				return fmt.Errorf("contour level limit exceeded")
			}
			var lineStyle *sld.LineStyle
			for _, rule := range sld.FindMatchingRules(style, nil, scale) {
				if rule.LineStyle != nil {
					lineStyle = rule.LineStyle
					break
				}
			}
			targetRenderer.Composite(renderer.RenderContours(grid, levels, lineStyle))
		} else if rasterPoints {
			targetRenderer.Composite(renderer.RenderRasterPoints(grid, firstPointStyle(style)))
		} else {
			image, renderErr := renderer.RenderCoverage(grid, rasterStyle)
			if renderErr != nil {
				return renderErr
			}
			targetRenderer.Composite(image)
		}
		if processName != "" {
			if timeout := h.cfg.WMS.ProcessTimeoutMS; timeout > 0 && time.Since(processStarted) > time.Duration(timeout)*time.Millisecond {
				return fmt.Errorf("rendering process timeout exceeded")
			}
		}
	}
	if targetRenderer != mapRenderer {
		mapRenderer.CompositeWithMode(targetRenderer.Image(), style.Composite, style.CompositeOpacity)
	}
	return nil
}

func coverageDimensionValues(coverage *workspace.Coverage, timeValue, elevationValue string) (string, string) {
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
	return timeValue, elevationValue
}

func rasterBands(style *sld.RasterStyle, info *datasource.CoverageInfo, coverage *workspace.Coverage) ([]int, error) {
	if style == nil {
		count := len(info.Bands)
		if count > 4 {
			count = 4
		}
		bands := make([]int, count)
		for i := range bands {
			bands[i] = i + 1
		}
		return bands, nil
	}
	seen := map[int]bool{}
	var result []int
	for _, channel := range []*sld.ResolvedChannel{style.Channels.Gray, style.Channels.Red, style.Channels.Green, style.Channels.Blue} {
		if channel == nil {
			continue
		}
		number, err := rasterBandNumber(channel.Name, info, coverage)
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
	if len(result) > 4 {
		return nil, fmt.Errorf("RasterSymbolizer selects more than four bands")
	}
	return result, nil
}
func rasterBandNumber(name string, info *datasource.CoverageInfo, coverage *workspace.Coverage) (int, error) {
	if number, err := strconv.Atoi(strings.TrimSpace(name)); err == nil && number >= 1 && number <= len(info.Bands) {
		return number, nil
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
func applyRangeFieldNames(grid *datasource.CoverageRenderGrid, coverage *workspace.Coverage) {
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
