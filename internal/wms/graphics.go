package wms

import (
	"image/color"

	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/stylegraphics"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) newMapRenderer(transform *renderer.Transform, transparent bool, background color.Color, ws *workspace.Workspace) *renderer.MapRenderer {
	result := renderer.NewMapRenderer(transform, transparent, background)
	resolver := stylegraphics.New(h.cfg.WMS, ws.ID, h.extensionEnabled(ws, "remote-graphics"), ws.StyleAssets)
	result.SetGraphicResolver(resolver.Resolve)
	return result
}
