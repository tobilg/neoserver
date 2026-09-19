package wms

import (
	"fmt"

	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) resolveWMSStyle(ws *workspace.Workspace, resource *workspace.PublishedResource, inline *sld.StyledLayerDescriptor, requested string) (*sld.Style, error) {
	publicID := resource.PublicID()
	if inline != nil {
		return styleFromDocument(inline, publicID, requested)
	}
	name := requested
	if name == "default" {
		name = ""
	}
	if name == "" {
		if resource.Layer != nil {
			name = resource.Layer.DefaultStyle
		}
		if resource.Coverage != nil {
			name = resource.Coverage.DefaultStyle
		}
	}
	if name == "" {
		return nil, nil
	}
	if stored := ws.GetStyle(name); stored != nil {
		if !sld.IsSLDFormat(stored.Format) && !h.extensionEnabled(ws, "dynamic-style") {
			return nil, fmt.Errorf("style %q requires the disabled dynamic-style extension", name)
		}
		if !stored.Valid && len(stored.ValidationErrors) > 0 {
			return nil, fmt.Errorf("style %q is unavailable: %s", name, stored.ValidationErrors[0])
		}
		doc, err := stored.CompiledDocument()
		if err != nil {
			return nil, err
		}
		return styleFromDocument(doc, publicID, "")
	}
	path, err := safeStylePath(h.cfg.WMS.SLDPath, name)
	if err == nil {
		if style, loadErr := h.loadStyleFile(path); loadErr == nil {
			return style, nil
		}
	}
	return nil, fmt.Errorf("style %q is not defined", name)
}

func styleFromDocument(doc *sld.StyledLayerDescriptor, publicID, styleName string) (*sld.Style, error) {
	if doc == nil {
		return nil, fmt.Errorf("style document is missing")
	}
	if style, err := doc.GetStyle(publicID, styleName); err == nil {
		return style, nil
	}
	if len(doc.NamedLayers) == 1 {
		return doc.GetStyle(doc.NamedLayers[0].Name, styleName)
	}
	return nil, fmt.Errorf("style has no NamedLayer for %q", publicID)
}
