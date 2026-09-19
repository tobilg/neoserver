package tiles

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HugoSmits86/nativewebp"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/stylegraphics"
	"github.com/tobilg/neoserver/internal/workspace"
)

// MapTileGenerator generates raster map tiles.
type MapTileGenerator struct {
	tileSize       int
	maxFeatures    int
	maxVertices    int
	maxTileBytes   int
	graphicsConfig conf.WMS
}

func (g *MapTileGenerator) SetGraphicConfig(cfg conf.WMS) { g.graphicsConfig = cfg }

// NewMapTileGenerator creates a new map tile generator.
func NewMapTileGenerator(tileSize int) *MapTileGenerator {
	if tileSize <= 0 {
		tileSize = 256
	}
	return &MapTileGenerator{
		tileSize: tileSize, maxFeatures: 50000, maxVertices: 5000000, maxTileBytes: 10 << 20,
	}
}

// SetLimits applies server-owned hard ceilings to raster tile generation.
func (g *MapTileGenerator) SetLimits(features, vertices, bytes int) {
	if features > 0 {
		g.maxFeatures = features
	}
	if vertices > 0 {
		g.maxVertices = vertices
	}
	if bytes > 0 {
		g.maxTileBytes = bytes
	}
}

func (g *MapTileGenerator) GenerateTileWithLimits(ctx context.Context, ds datasource.DataSource, layer *datasource.LayerInfo, ws *workspace.Workspace, tms string, z, x, y int, format TileFormat, styleName string, transparent bool, bgColor color.Color, features, vertices, bytes int) ([]byte, error) {
	return g.GenerateTileWithFilterLimits(ctx, ds, layer, ws, tms, z, x, y, format, styleName, layer.Name, transparent, bgColor, features, vertices, bytes, "")
}

func (g *MapTileGenerator) GenerateTileWithFilterLimits(ctx context.Context, ds datasource.DataSource, layer *datasource.LayerInfo, ws *workspace.Workspace, tms string, z, x, y int, format TileFormat, styleName, styleLayerName string, transparent bool, bgColor color.Color, features, vertices, bytes int, filter string) ([]byte, error) {
	limited := *g
	if features > 0 {
		limited.maxFeatures = min(limited.maxFeatures, features)
	}
	if vertices > 0 {
		limited.maxVertices = min(limited.maxVertices, vertices)
	}
	if bytes > 0 {
		limited.maxTileBytes = min(limited.maxTileBytes, bytes)
	}
	if filter == "" {
		return limited.generateTile(ctx, ds, layer, ws, tms, z, x, y, format, styleName, styleLayerName, transparent, bgColor, nil)
	}
	query := func(ctx context.Context, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
		params.Filter = filter
		return ds.QueryWKB(ctx, layer.Name, params)
	}
	return limited.generateTile(ctx, ds, layer, ws, tms, z, x, y, format, styleName, styleLayerName, transparent, bgColor, query)
}

// TileFormat represents the output format for map tiles.
type TileFormat string

const (
	TileFormatPNG  TileFormat = "png"
	TileFormatJPEG TileFormat = "jpeg"
	TileFormatWEBP TileFormat = "webp"
)

// GenerateTile generates a map tile for the given layer.
func (g *MapTileGenerator) GenerateTile(
	ctx context.Context,
	ds datasource.DataSource,
	layer *datasource.LayerInfo,
	ws *workspace.Workspace,
	tms string,
	z, x, y int,
	format TileFormat,
	styleName string,
	transparent bool,
	bgColor color.Color,
) ([]byte, error) {
	return g.generateTile(ctx, ds, layer, ws, tms, z, x, y, format, styleName, layer.Name, transparent, bgColor, nil)
}

func (g *MapTileGenerator) GenerateSQLViewTileWithLimits(ctx context.Context, ds datasource.SQLViewDataSource, config *datasource.SQLViewConfig, layer *datasource.LayerInfo, ws *workspace.Workspace, tms string, z, x, y int, format TileFormat, styleName string, transparent bool, bgColor color.Color, features, vertices, bytes int) ([]byte, error) {
	return g.GenerateSQLViewTileWithFilterLimits(ctx, ds, config, layer, ws, tms, z, x, y, format, styleName, layer.Name, transparent, bgColor, features, vertices, bytes, "")
}

func (g *MapTileGenerator) GenerateSQLViewTileWithFilterLimits(ctx context.Context, ds datasource.SQLViewDataSource, config *datasource.SQLViewConfig, layer *datasource.LayerInfo, ws *workspace.Workspace, tms string, z, x, y int, format TileFormat, styleName, styleLayerName string, transparent bool, bgColor color.Color, features, vertices, bytes int, filter string) ([]byte, error) {
	limited := *g
	if features > 0 {
		limited.maxFeatures = min(limited.maxFeatures, features)
	}
	if vertices > 0 {
		limited.maxVertices = min(limited.maxVertices, vertices)
	}
	if bytes > 0 {
		limited.maxTileBytes = min(limited.maxTileBytes, bytes)
	}
	query := func(ctx context.Context, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
		params.Filter = filter
		return ds.QuerySQLViewWKB(ctx, config, params)
	}
	return limited.generateTile(ctx, ds, layer, ws, tms, z, x, y, format, styleName, styleLayerName, transparent, bgColor, query)
}

type renderQuery func(context.Context, datasource.QueryParams) ([]datasource.RenderFeature, error)

func (g *MapTileGenerator) generateTile(
	ctx context.Context, ds datasource.DataSource, layer *datasource.LayerInfo, ws *workspace.Workspace,
	tms string, z, x, y int, format TileFormat, styleName, styleLayerName string, transparent bool, bgColor color.Color,
	override renderQuery,
) ([]byte, error) {
	// Get tile bounds in the native CRS
	bounds, err := TileBBox(tms, z, x, y)
	if err != nil {
		return nil, err
	}

	// Get WGS84 bounds for querying
	wgsBounds, err := TileBBoxWGS84(tms, z, x, y)
	if err != nil {
		return nil, err
	}
	return g.generateRegion(ctx, ds, layer, ws, bounds, wgsBounds, GetTMSSRID(tms), g.tileSize, g.tileSize, format, styleName, styleLayerName, transparent, bgColor, override)
}

func (g *MapTileGenerator) generateRegion(
	ctx context.Context, ds datasource.DataSource, layer *datasource.LayerInfo, ws *workspace.Workspace,
	bounds, wgsBounds *TileBounds, outputSRID, width, height int, format TileFormat, styleName, styleLayerName string,
	transparent bool, bgColor color.Color, override renderQuery,
) ([]byte, error) {

	// Query features within the tile bounds
	params := datasource.QueryParams{
		BBox: &datasource.BBox{
			MinX: wgsBounds.MinX,
			MinY: wgsBounds.MinY,
			MaxX: wgsBounds.MaxX,
			MaxY: wgsBounds.MaxY,
		},
		BBoxSRID:   4326,
		OutputSRID: outputSRID,
		Limit:      g.maxFeatures + 1,
	}

	var stream datasource.RenderFeatureStream
	var err error
	if override != nil {
		var features []datasource.RenderFeature
		features, err = override(ctx, params)
		if err == nil {
			stream = datasource.NewSliceRenderStream(features)
		}
	} else if streaming, ok := ds.(datasource.RenderStreamingDataSource); ok {
		stream, err = streaming.QueryWKBStream(ctx, layer.Name, params)
	} else {
		var features []datasource.RenderFeature
		features, err = ds.QueryWKB(ctx, layer.Name, params)
		if err == nil {
			stream = datasource.NewSliceRenderStream(features)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("query features for tile: %w", err)
	}
	defer stream.Close()

	// Get or create style
	var style *sld.Style
	if styleName != "" && ws != nil {
		if wsStyle, ok := ws.Styles[styleName]; ok {
			parsedSLD, err := wsStyle.CompiledDocument()
			if err == nil {
				style, _ = parsedSLD.GetStyle(styleLayerName, "")
				if style == nil {
					style, _ = parsedSLD.GetDefaultStyle()
				}
			}
		}
	}

	// Create default style if none provided
	if style == nil {
		style = createDefaultStyle()
	}

	// Create transform for tile coordinates using query.BBox
	bbox := query.BBox{
		MinX: bounds.MinX,
		MinY: bounds.MinY,
		MaxX: bounds.MaxX,
		MaxY: bounds.MaxY,
	}
	transform := renderer.NewTransform(bbox, width, height)

	// Create renderer
	mapRenderer := renderer.NewMapRenderer(transform, transparent, bgColor)
	if ws != nil {
		allowRemote := ws.Settings != nil && slices.Contains(g.graphicsConfig.Extensions, "remote-graphics") && slices.Contains(ws.Settings.WMS.Extensions, "remote-graphics")
		graphicResolver := stylegraphics.New(g.graphicsConfig, ws.ID, allowRemote, ws.StyleAssets)
		mapRenderer.SetGraphicResolver(graphicResolver.Resolve)
	}
	processName := ""
	if style.Transformation != nil {
		processName = style.Transformation.Name
	}
	processFeatureLimit := g.maxFeatures
	if configured := g.graphicsConfig.MaxProcessFeatures; configured > 0 && configured < processFeatureLimit {
		processFeatureLimit = configured
	}
	if style.SortBy != "" || processName == "heatmap" || processName == "barnes" || processName == "pointstacker" || processName == "groupcandidateselection" {
		var buffered []datasource.RenderFeature
		for stream.Next() {
			buffered = append(buffered, stream.Feature())
			if len(buffered) > g.maxFeatures {
				return nil, fmt.Errorf("tile feature limit exceeded")
			}
		}
		if err := stream.Err(); err != nil {
			return nil, err
		}
		if style.SortBy != "" {
			sortTileFeatures(buffered, style.SortBy, style.SortDescending)
		}
		if processName == "groupcandidateselection" {
			buffered = selectTileGroupCandidates(buffered, style.Transformation.WeightProperty)
		}
		if processName != "" && len(buffered) > processFeatureLimit {
			return nil, fmt.Errorf("rendering process feature limit exceeded")
		}
		stream = datasource.NewSliceRenderStream(buffered)
	}
	if processName == "heatmap" || processName == "barnes" || processName == "pointstacker" {
		processStarted := time.Now()
		var points []renderer.HeatPoint
		for stream.Next() {
			feature := stream.Feature()
			geometry, parseErr := renderer.ParseWKB(feature.Geometry)
			if parseErr != nil {
				continue
			}
			weight := 1.0
			if property := style.Transformation.WeightProperty; property != "" {
				weight, _ = strconv.ParseFloat(fmt.Sprint(feature.Properties[property]), 64)
			}
			appendHeatGeometry(&points, geometry, weight)
		}
		if processName == "pointstacker" {
			mapRenderer.Composite(renderer.RenderPointStacker(transform, points, style.Transformation.Radius, firstTilePointStyle(style)))
		} else {
			mapRenderer.Composite(renderer.RenderHeatmap(transform, points, style.Transformation.Radius))
		}
		if timeout := g.graphicsConfig.ProcessTimeoutMS; timeout > 0 && time.Since(processStarted) > time.Duration(timeout)*time.Millisecond {
			return nil, fmt.Errorf("rendering process timeout exceeded")
		}
		data, encodeErr := g.encodeImage(mapRenderer.Image(), format)
		if encodeErr == nil && len(data) > g.maxTileBytes {
			return nil, fmt.Errorf("tile output limit exceeded: %d > %d bytes", len(data), g.maxTileBytes)
		}
		return data, encodeErr
	}

	// Render features
	featureCount, vertexCount := 0, 0
	for stream.Next() {
		feat := stream.Feature()
		featureCount++
		if featureCount > g.maxFeatures {
			return nil, fmt.Errorf("tile feature limit exceeded: %d > %d", featureCount, g.maxFeatures)
		}
		if len(feat.Geometry) == 0 {
			continue
		}

		geom, err := renderer.ParseWKB(feat.Geometry)
		if err != nil {
			continue
		}
		vertexCount += geom.VertexCount()
		if vertexCount > g.maxVertices {
			return nil, fmt.Errorf("tile vertex limit exceeded: %d > %d", vertexCount, g.maxVertices)
		}

		for _, rule := range sld.FindMatchingRules(style, feat.Properties, transform.ScaleDenominator()) {
			if len(rule.Symbolizers) == 0 {
				if geomStyle := getStyleForGeometry(&sld.Style{Rules: []sld.ResolvedRule{rule}}, geom.Type, feat.Properties); geomStyle != nil {
					mapRenderer.DrawGeometry(geom, geomStyle)
				}
				continue
			}
			for _, symbolizer := range rule.Symbolizers {
				resolved, expressionErr := sld.ResolveSymbolizerExpressions(symbolizer, feat.Properties, nil)
				if expressionErr != nil {
					return nil, fmt.Errorf("style expression failed: %w", expressionErr)
				}
				switch {
				case resolved.Point != nil:
					mapRenderer.DrawGeometry(geom, resolved.Point)
				case resolved.Line != nil:
					mapRenderer.DrawGeometry(geom, resolved.Line)
				case resolved.Polygon != nil:
					mapRenderer.DrawGeometry(geom, resolved.Polygon)
				case resolved.Text != nil:
					label := resolved.Text.Literal
					if resolved.Text.PropertyName != "" {
						if value, ok := feat.Properties[resolved.Text.PropertyName]; ok {
							label = fmt.Sprint(value)
						}
					}
					mapRenderer.DrawText(geom, resolved.Text, label)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("stream tile features: %w", err)
	}

	// Encode image
	data, err := g.encodeImage(mapRenderer.Image(), format)
	if err == nil && len(data) > g.maxTileBytes {
		return nil, fmt.Errorf("tile output limit exceeded: %d > %d bytes", len(data), g.maxTileBytes)
	}
	return data, err
}

func firstTilePointStyle(style *sld.Style) *sld.PointStyle {
	if style == nil {
		return nil
	}
	for _, rule := range style.Rules {
		for _, symbolizer := range rule.Symbolizers {
			if symbolizer.Point != nil {
				return symbolizer.Point
			}
		}
		if rule.PointStyle != nil {
			return rule.PointStyle
		}
	}
	return nil
}
func selectTileGroupCandidates(features []datasource.RenderFeature, property string) []datasource.RenderFeature {
	if property == "" {
		return features
	}
	seen := map[string]bool{}
	result := make([]datasource.RenderFeature, 0, len(features))
	for _, feature := range features {
		key := fmt.Sprint(feature.Properties[property])
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, feature)
	}
	return result
}

func sortTileFeatures(features []datasource.RenderFeature, property string, descending bool) {
	sort.SliceStable(features, func(i, j int) bool {
		left, right := fmt.Sprint(features[i].Properties[property]), fmt.Sprint(features[j].Properties[property])
		leftNumber, leftErr := strconv.ParseFloat(left, 64)
		rightNumber, rightErr := strconv.ParseFloat(right, 64)
		comparison := strings.Compare(left, right)
		if leftErr == nil && rightErr == nil {
			switch {
			case leftNumber < rightNumber:
				comparison = -1
			case leftNumber > rightNumber:
				comparison = 1
			default:
				comparison = 0
			}
		}
		if descending {
			return comparison > 0
		}
		return comparison < 0
	})
}

func appendHeatGeometry(points *[]renderer.HeatPoint, geometry *renderer.Geometry, weight float64) {
	if geometry == nil {
		return
	}
	if geometry.Type == renderer.WKBPoint && len(geometry.Coordinates) > 0 {
		*points = append(*points, renderer.HeatPoint{X: geometry.Coordinates[0][0], Y: geometry.Coordinates[0][1], Weight: weight})
	}
	for index := range geometry.Geometries {
		appendHeatGeometry(points, &geometry.Geometries[index], weight)
	}
}

// encodeImage encodes the image to the specified format.
func (g *MapTileGenerator) encodeImage(img image.Image, format TileFormat) ([]byte, error) {
	var buf bytes.Buffer

	switch format {
	case TileFormatPNG:
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("PNG encoding failed: %w", err)
		}
	case TileFormatJPEG:
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return nil, fmt.Errorf("JPEG encoding failed: %w", err)
		}
	case TileFormatWEBP:
		if err := nativewebp.Encode(&buf, img, nil); err != nil {
			return nil, fmt.Errorf("WebP encoding failed: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported tile format %q", format)
	}

	return buf.Bytes(), nil
}

// createDefaultStyle creates a geometry-agnostic default style.
func createDefaultStyle() *sld.Style {
	return &sld.Style{
		Name: "default",
		Rules: []sld.ResolvedRule{
			{
				Name: "default",
				PointStyle: &sld.PointStyle{
					Shape:       "circle",
					Size:        8,
					FillColor:   color.RGBA{R: 51, G: 136, B: 255, A: 255},
					StrokeColor: color.RGBA{R: 51, G: 136, B: 255, A: 255},
					StrokeWidth: 1,
					Opacity:     1.0,
				},
				LineStyle: &sld.LineStyle{
					Color:   color.RGBA{R: 51, G: 136, B: 255, A: 255},
					Width:   2,
					Opacity: 1.0,
					LineCap: "round",
				},
				PolygonStyle: &sld.PolygonStyle{
					FillColor:     color.RGBA{R: 51, G: 136, B: 255, A: 255},
					FillOpacity:   0.5,
					StrokeColor:   color.RGBA{R: 51, G: 136, B: 255, A: 255},
					StrokeWidth:   1,
					StrokeOpacity: 1.0,
				},
			},
		},
	}
}

// getStyleForGeometry returns the appropriate style for a geometry type.
func getStyleForGeometry(style *sld.Style, geomType renderer.GeometryType, props map[string]interface{}) interface{} {
	if style == nil || len(style.Rules) == 0 {
		return nil
	}

	// Find matching rule (simplified - just use first rule for now)
	// A full implementation would evaluate filters
	rule := style.Rules[0]

	switch geomType {
	case renderer.WKBPoint, renderer.WKBMultiPoint:
		if rule.PointStyle != nil {
			return rule.PointStyle
		}
	case renderer.WKBLineString, renderer.WKBMultiLineString:
		if rule.LineStyle != nil {
			return rule.LineStyle
		}
	case renderer.WKBPolygon, renderer.WKBMultiPolygon:
		if rule.PolygonStyle != nil {
			return rule.PolygonStyle
		}
	}

	return nil
}

// GetContentType returns the content type for a tile format.
func GetContentType(format TileFormat) string {
	switch format {
	case TileFormatPNG:
		return MediaTypePNG
	case TileFormatJPEG:
		return MediaTypeJPEG
	case TileFormatWEBP:
		return MediaTypeWEBP
	default:
		return MediaTypePNG
	}
}

// ParseTileFormat parses a format string to TileFormat.
func ParseTileFormat(s string) (TileFormat, error) {
	switch s {
	case "", "png", "image/png":
		return TileFormatPNG, nil
	case "jpeg", "jpg", "image/jpeg":
		return TileFormatJPEG, nil
	case "webp", "image/webp":
		return TileFormatWEBP, nil
	default:
		return "", fmt.Errorf("unsupported tile format %q", s)
	}
}
