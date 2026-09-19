package wms

import (
	"fmt"
	"image/color"
	"image/png"
	"net/http"
	"strings"

	"github.com/fogleman/gg"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/workspace"
)

const remoteSLDUnsupportedMessage = "SLD URL parameter is not supported; use SLD_BODY or a stored style"

func (h *workspaceHandler) handleGetLegendGraphic(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()

	// Parse request
	maxEnvVariables, maxEnvValueBytes := h.environmentLimits()
	req, err := parseGetLegendGraphicRequestWithLimits(r, h.cfg.WMS.MaxWidth, h.cfg.WMS.MaxHeight, h.cfg.WMS.MaxPixels, maxEnvVariables, maxEnvValueBytes)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if strings.TrimSpace(req.SLD) != "" {
		WriteExceptionWithLocator(w, ExceptionInvalidParameterValue, "SLD", remoteSLDUnsupportedMessage)
		return
	}

	// Find layer and service. A layer the caller may not read is treated as
	// undefined so its existence is not disclosed.
	resource := ws.GetResource(req.Layer)
	if resource != nil && resource.Kind == workspace.ResourceGroup &&
		ws.GroupVisibleToRole(resource.Group, workspaceRole(r, ws.ID)) {
		// A group mixes member styles and geometry types, so it has no
		// single legend symbol. Point clients at the members instead of
		// claiming the layer is undefined.
		WriteExceptionWithLocator(w, ExceptionInvalidParameterValue, "LAYER",
			fmt.Sprintf("%s is a layer group; request a legend for each member layer", req.Layer))
		return
	}
	if resource == nil || resource.Service == nil ||
		(resource.Layer != nil && !resource.Layer.VisibleToRole(workspaceRole(r, ws.ID))) ||
		(resource.Coverage != nil && !resource.Coverage.VisibleToRole(workspaceRole(r, ws.ID))) {
		WriteException(w, ExceptionLayerNotDefined, fmt.Sprintf("Layer not found: %s", req.Layer))
		return
	}
	if resource.Kind == workspace.ResourceCoverage {
		var doc *sld.StyledLayerDescriptor
		if req.SLDBody != "" {
			doc, err = sld.ParseString(req.SLDBody)
			if err != nil {
				WriteException(w, ExceptionStyleNotDefined, "Invalid SLD")
				return
			}
		}
		style, err := h.resolveWMSStyle(ws, resource, doc, req.Style)
		if err != nil {
			WriteException(w, ExceptionStyleNotDefined, err.Error())
			return
		}
		var rasterStyle *sld.RasterStyle
		if style != nil {
			for _, rule := range style.Rules {
				for _, symbolizer := range rule.Symbolizers {
					if symbolizer.Raster != nil {
						rasterStyle = symbolizer.Raster
						break
					}
				}
				if rasterStyle != nil {
					break
				}
			}
		}
		if style != nil && rasterStyle == nil {
			WriteException(w, ExceptionStyleNotDefined, "Style has no RasterSymbolizer")
			return
		}
		if sld.RasterStyleUsesEnvironment(rasterStyle) {
			if !h.extensionEnabled(ws, "dynamic-raster") {
				WriteException(w, ExceptionStyleNotDefined, "Style requires the disabled dynamic-raster extension")
				return
			}
			rasterStyle, err = sld.ResolveRasterEnvironment(rasterStyle, req.Environment)
			if err != nil {
				WriteException(w, ExceptionStyleNotDefined, err.Error())
				return
			}
		}
		w.Header().Set("Content-Type", FormatPNG)
		w.WriteHeader(http.StatusOK)
		_ = png.Encode(w, renderer.RasterLegend(rasterStyle, req.Width, req.Height))
		return
	}
	layer, service := resource.Layer, resource.Service

	if service.DataSource == nil {
		WriteException(w, ExceptionLayerNotDefined, fmt.Sprintf("Service not available for layer: %s", req.Layer))
		return
	}

	// Get layer info for geometry type
	layerInfo, err := layer.FeatureInfo(ctx, service.DataSource)
	if err != nil {
		WriteException(w, ExceptionInvalidParameterValue, fmt.Sprintf("Failed to get layer info: %v", err))
		return
	}

	// Get style
	var style *sld.Style
	if req.SLDBody != "" {
		sldDoc, err := sld.ParseString(req.SLDBody)
		if err != nil {
			WriteException(w, ExceptionStyleNotDefined, fmt.Sprintf("Invalid SLD: %v", err))
			return
		}
		style, _ = sldDoc.GetStyle(req.Layer, req.Style)
	}

	if style == nil && req.Style != "" && req.Style != "default" {
		// Try to load style from file
		stylePath, pathErr := safeStylePath(h.cfg.WMS.SLDPath, req.Style)
		if sldFile, err := sld.ParseFile(stylePath); pathErr == nil && err == nil {
			style, _ = sldFile.GetDefaultStyle()
		}
	}

	if style == nil {
		// Use default style
		defaultStyleCfg := h.cfg.WMS.Styles[h.cfg.WMS.DefaultStyle]
		if defaultStyleCfg.FillColor == "" {
			defaultStyleCfg = conf.WMSStyle{
				FillColor:   "#3388ff",
				FillOpacity: 0.5,
				StrokeColor: "#3388ff",
				StrokeWidth: 2.0,
				PointRadius: 5.0,
			}
		}
		style = sld.DefaultStyle(defaultStyleCfg)
	}

	// Generate legend image
	dc := h.generateLegend(style, layerInfo, req.Width, req.Height)

	// Write response
	w.Header().Set("Content-Type", FormatPNG)
	w.WriteHeader(http.StatusOK)
	png.Encode(w, dc.Image())
}

// generateLegend generates a legend image for the given style.
func (h *workspaceHandler) generateLegend(style *sld.Style, layerInfo *datasource.LayerInfo, symbolWidth, symbolHeight int) *gg.Context {
	// Determine geometry type for symbol rendering
	geomType := "Polygon"
	geomTypeLower := strings.ToLower(layerInfo.GeometryType)
	switch {
	case strings.Contains(geomTypeLower, "point"):
		geomType = "Point"
	case strings.Contains(geomTypeLower, "line"):
		geomType = "LineString"
	case strings.Contains(geomTypeLower, "polygon"):
		geomType = "Polygon"
	}

	// Create image with exact requested dimensions (required for WMS 1.3.0 compliance)
	dc := gg.NewContext(symbolWidth, symbolHeight)

	// White background
	dc.SetRGB(1, 1, 1)
	dc.Clear()

	rules := style.Rules
	if len(rules) == 0 {
		return dc
	}

	// Draw the first rule's symbol to fit the requested dimensions
	h.drawLegendSymbol(dc, 0, 0, float64(symbolWidth), float64(symbolHeight), geomType, &rules[0])

	return dc
}

// drawLegendSymbol draws a legend symbol.
func (h *workspaceHandler) drawLegendSymbol(dc *gg.Context, x, y, width, height float64, geomType string, rule *sld.ResolvedRule) {
	switch geomType {
	case "Point", "MultiPoint":
		h.drawPointLegend(dc, x, y, width, height, rule.PointStyle)
	case "LineString", "MultiLineString":
		h.drawLineLegend(dc, x, y, width, height, rule.LineStyle)
	case "Polygon", "MultiPolygon":
		h.drawPolygonLegend(dc, x, y, width, height, rule.PolygonStyle)
	default:
		h.drawPolygonLegend(dc, x, y, width, height, rule.PolygonStyle)
	}
}

// drawPointLegend draws a point legend symbol.
func (h *workspaceHandler) drawPointLegend(dc *gg.Context, x, y, width, height float64, style *sld.PointStyle) {
	if style == nil {
		style = sld.DefaultPointStyle()
	}

	cx := x + width/2
	cy := y + height/2
	radius := minFloat(width, height) / 2 * 0.8

	switch style.Shape {
	case "square":
		dc.DrawRectangle(cx-radius, cy-radius, radius*2, radius*2)
	case "triangle":
		dc.MoveTo(cx, cy-radius)
		dc.LineTo(cx+radius*0.866, cy+radius*0.5)
		dc.LineTo(cx-radius*0.866, cy+radius*0.5)
		dc.ClosePath()
	default: // circle
		dc.DrawCircle(cx, cy, radius)
	}

	setLegendColor(dc, style.FillColor, style.Opacity)
	dc.FillPreserve()
	setLegendColor(dc, style.StrokeColor, 1.0)
	dc.SetLineWidth(style.StrokeWidth)
	dc.Stroke()
}

// drawLineLegend draws a line legend symbol.
func (h *workspaceHandler) drawLineLegend(dc *gg.Context, x, y, width, height float64, style *sld.LineStyle) {
	if style == nil {
		style = sld.DefaultLineStyle()
	}

	// Draw a diagonal line
	dc.MoveTo(x+2, y+height-2)
	dc.LineTo(x+width-2, y+2)

	setLegendColor(dc, style.Color, style.Opacity)
	dc.SetLineWidth(style.Width)

	if len(style.DashArray) > 0 {
		dc.SetDash(style.DashArray...)
	}

	dc.Stroke()
	dc.SetDash()
}

// drawPolygonLegend draws a polygon legend symbol.
func (h *workspaceHandler) drawPolygonLegend(dc *gg.Context, x, y, width, height float64, style *sld.PolygonStyle) {
	if style == nil {
		style = sld.DefaultPolygonStyle()
	}

	// Draw a rectangle
	dc.DrawRectangle(x+2, y+2, width-4, height-4)

	setLegendColor(dc, style.FillColor, style.FillOpacity)
	dc.FillPreserve()

	if style.StrokeWidth > 0 {
		setLegendColor(dc, style.StrokeColor, style.StrokeOpacity)
		dc.SetLineWidth(style.StrokeWidth)
		dc.Stroke()
	} else {
		dc.ClearPath()
	}
}

// setLegendColor sets the drawing color.
func setLegendColor(dc *gg.Context, c color.RGBA, opacity float64) {
	alpha := float64(c.A) / 255.0 * opacity
	dc.SetRGBA(float64(c.R)/255.0, float64(c.G)/255.0, float64(c.B)/255.0, alpha)
}

// minFloat returns the minimum of two floats.
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
