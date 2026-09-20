package wms

import (
	"context"
	"fmt"
	"image"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/workspace"
)

// RenderResourceLegend shares WMS style resolution and symbol rendering with
// other portrayal protocols. The caller authorizes the resource and style.
func RenderResourceLegend(ctx context.Context, cfg conf.Config, ws *workspace.Workspace, resource *workspace.PublishedResource, name string, width, height int) (image.Image, error) {
	h := &workspaceHandler{cfg: cfg}
	style, err := h.resolveWMSStyle(ws, resource, nil, name)
	if err != nil {
		return nil, err
	}
	if resource.Coverage != nil {
		raster, err := h.resolveRasterLegendStyle(ws, style, nil)
		if err != nil {
			return nil, err
		}
		return renderer.RasterLegend(raster, width, height), nil
	}
	if resource.Layer == nil || resource.Service == nil || resource.Service.DataSource == nil {
		return nil, fmt.Errorf("feature source is unavailable")
	}
	info, err := resource.Layer.FeatureInfo(ctx, resource.Service.DataSource)
	if err != nil {
		return nil, err
	}
	if style == nil {
		defaults := cfg.WMS.Styles[cfg.WMS.DefaultStyle]
		if defaults.FillColor == "" {
			defaults = conf.WMSStyle{FillColor: "#3388ff", FillOpacity: 0.5, StrokeColor: "#3388ff", StrokeWidth: 2, PointRadius: 5}
		}
		style = sld.DefaultStyle(defaults)
	}
	return h.generateLegend(style, info, width, height).Image(), nil
}

// resolveRasterLegendStyle keeps raster style selection and extension gates
// identical for WMS and WMTS legends.
func (h *workspaceHandler) resolveRasterLegendStyle(ws *workspace.Workspace, style *sld.Style, environment map[string]string) (*sld.RasterStyle, error) {
	var raster *sld.RasterStyle
	if style != nil {
		for _, rule := range style.Rules {
			for _, symbol := range rule.Symbolizers {
				if symbol.Raster != nil {
					raster = symbol.Raster
					break
				}
			}
			if raster != nil {
				break
			}
		}
		if raster == nil {
			return nil, fmt.Errorf("style has no RasterSymbolizer")
		}
	}
	if sld.RasterStyleUsesEnvironment(raster) {
		if !h.extensionEnabled(ws, "dynamic-raster") {
			return nil, fmt.Errorf("style requires the disabled dynamic-raster extension")
		}
		return sld.ResolveRasterEnvironment(raster, environment)
	}
	return raster, nil
}
