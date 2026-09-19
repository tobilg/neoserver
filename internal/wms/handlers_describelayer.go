package wms

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) handleDescribeLayer(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	q := NormalizeQuery(r)
	rawLayers := strings.TrimSpace(q.Get("LAYERS"))
	if rawLayers == "" {
		WriteException(w, ExceptionMissingParameterValue, "LAYERS parameter is required")
		return
	}
	base := strings.TrimRight(h.cfg.Server.UrlBase, "/") + h.cfg.Server.BasePath + "/workspaces/" + url.PathEscape(ws.Name)
	role := workspaceRole(r, ws.ID)
	var descriptions strings.Builder
	for _, name := range strings.Split(rawLayers, ",") {
		name = strings.TrimSpace(name)
		resource := ws.GetResource(name)
		if resource == nil ||
			(resource.Layer != nil && !resource.Layer.VisibleToRole(role)) ||
			(resource.Coverage != nil && !resource.Coverage.VisibleToRole(role)) ||
			(resource.Group != nil && !ws.GroupVisibleToRole(resource.Group, role)) {
			WriteException(w, ExceptionLayerNotDefined, "Layer not found: "+name)
			return
		}
		switch resource.Kind {
		case workspace.ResourceFeature:
			typeName := resource.Layer.PublicID
			if prefix := strings.TrimSpace(h.cfg.WFS.AppNamespacePrefix); prefix != "" {
				typeName = prefix + ":" + typeName
			}
			fmt.Fprintf(&descriptions, `<sld:LayerDescription name="%s" owsType="WFS" owsURL="%s"><sld:TypeName><se:FeatureTypeName>%s</se:FeatureTypeName></sld:TypeName></sld:LayerDescription>`, xmlEscape(name), xmlEscape(base+"/wfs"), xmlEscape(typeName))
		case workspace.ResourceCoverage:
			fmt.Fprintf(&descriptions, `<sld:LayerDescription name="%s" owsType="WCS" owsURL="%s"><sld:TypeName><se:FeatureTypeName>%s</se:FeatureTypeName></sld:TypeName></sld:LayerDescription>`, xmlEscape(name), xmlEscape(base+"/wcs"), xmlEscape(resource.Coverage.PublicID))
		default:
			fmt.Fprintf(&descriptions, `<sld:LayerDescription name="%s" owsType="WMS" owsURL="%s"/>`, xmlEscape(name), xmlEscape(base+"/wms"))
		}
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><sld:DescribeLayerResponse version="1.1.0" xmlns:sld="http://www.opengis.net/sld" xmlns:se="http://www.opengis.net/se">%s</sld:DescribeLayerResponse>`, descriptions.String())
}
