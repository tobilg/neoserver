package mgmt

import (
	"context"
	"fmt"
	"sort"

	"github.com/tobilg/neoserver/internal/sld"
)

func (h *handler) validateStyleBindings(ctx context.Context, workspaceID, defaultStyle string, styles []string, raster bool) error {
	seen := map[string]bool{}
	names := append([]string(nil), styles...)
	if defaultStyle != "" {
		names = append(names, defaultStyle)
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if seen[name] {
			return fmt.Errorf("style %q is listed more than once", name)
		}
		seen[name] = true
		stored, err := h.store.GetStyleByName(ctx, workspaceID, name)
		if err != nil {
			return fmt.Errorf("style %q does not exist", name)
		}
		doc, _, err := sld.Compile(stored.Format, stored.SLDBody)
		if err != nil {
			return fmt.Errorf("style %q is invalid", name)
		}
		if err = sld.Validate(doc); err != nil {
			return fmt.Errorf("style %q is invalid: %w", name, err)
		}
		compiled, err := doc.GetDefaultStyle()
		if err != nil {
			return fmt.Errorf("style %q has no usable UserStyle", name)
		}
		compatible := false
		for _, rule := range compiled.Rules {
			for _, symbolizer := range rule.Symbolizers {
				if raster && symbolizer.Raster != nil {
					compatible = true
				}
				if !raster && (symbolizer.Point != nil || symbolizer.Line != nil || symbolizer.Polygon != nil || symbolizer.Text != nil) {
					compatible = true
				}
			}
		}
		if !compatible {
			kind := "feature"
			if raster {
				kind = "coverage"
			}
			return fmt.Errorf("style %q is not compatible with a %s resource", name, kind)
		}
	}
	return nil
}

// validateGroupStyleBindings validates catalogued styles without imposing a
// feature-only or coverage-only symbolizer requirement. A layer group may
// contain both resource kinds, and a multi-layer SLD can legitimately provide
// a different UserStyle for each member.
func (h *handler) validateGroupStyleBindings(ctx context.Context, workspaceID, defaultStyle string, styles []string) error {
	seen := map[string]bool{}
	names := append([]string(nil), styles...)
	if defaultStyle != "" {
		names = append(names, defaultStyle)
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if seen[name] {
			return fmt.Errorf("style %q is listed more than once", name)
		}
		seen[name] = true
		stored, err := h.store.GetStyleByName(ctx, workspaceID, name)
		if err != nil {
			return fmt.Errorf("style %q does not exist", name)
		}
		doc, _, err := sld.Compile(stored.Format, stored.SLDBody)
		if err != nil {
			return fmt.Errorf("style %q is invalid", name)
		}
		if err = sld.Validate(doc); err != nil {
			return fmt.Errorf("style %q is invalid: %w", name, err)
		}
		if _, err = doc.GetDefaultStyle(); err != nil {
			return fmt.Errorf("style %q has no usable UserStyle", name)
		}
	}
	return nil
}

func (h *handler) styleReferences(workspaceID, name string) []string {
	ws, ok := h.registry.GetByID(workspaceID)
	if !ok {
		return nil
	}
	var refs []string
	for _, layer := range ws.GetCatalogLayers() {
		if layer.DefaultStyle == name || containsStyle(layer.Styles, name) {
			refs = append(refs, "layer:"+layer.PublicID)
		}
	}
	for _, coverage := range ws.GetCatalogCoverages() {
		if coverage.DefaultStyle == name || containsStyle(coverage.Styles, name) {
			refs = append(refs, "coverage:"+coverage.PublicID)
		}
	}
	for _, group := range ws.GetAllLayerGroups() {
		if group.DefaultStyle == name || containsStyle(group.Styles, name) {
			refs = append(refs, "layer-group:"+group.PublicID)
		}
		for _, member := range group.Members {
			if member.Style == name {
				refs = append(refs, "layer-group-member:"+group.PublicID+"/"+member.Resource)
			}
		}
	}
	sort.Strings(refs)
	return refs
}
func containsStyle(values []string, name string) bool {
	for _, value := range values {
		if value == name {
			return true
		}
	}
	return false
}
