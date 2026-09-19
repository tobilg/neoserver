package wfs

import (
	"fmt"
	"strings"

	"github.com/tobilg/neoserver/internal/workspace"
)

func applicationNamespaceDeclarations(namespace, prefix string) string {
	declarations := fmt.Sprintf(`xmlns:cite="%s"`, NSCite)
	if prefix != "cite" {
		declarations += fmt.Sprintf(` xmlns:%s="%s"`, prefix, escapeXML(namespace))
	}
	return declarations
}

// publishedFeatureTypeName returns a valid QName for a published layer. Known
// qualified identifiers (notably the CITE fixture names) retain their namespace;
// other identifiers are represented as XML-safe names in the configured
// application namespace.
func publishedFeatureTypeName(publicID, appPrefix string) string {
	qn := ParseQName(publicID)
	if qn.Prefix != "" && qn.Namespace != NSDefault && validASCIIXMLName(qn.Prefix) && validASCIIXMLName(qn.LocalPart) {
		return publicID
	}
	return appPrefix + ":" + sanitizeXMLName(publicID)
}

// resolveFeatureLayer maps an advertised QName or a gml:id type component back
// to its workspace publication. GML feature identifiers use the QName's local
// part, so a bare value such as Autos must resolve the advertised cite:Autos.
// Ambiguous local/sanitized names deliberately do not resolve.
func resolveFeatureLayer(ws *workspace.Workspace, typeName, appPrefix string) (*workspace.Layer, *workspace.Service) {
	if ws == nil || typeName == "" {
		return nil, nil
	}
	if layer, service := ws.GetLayer(typeName); layer != nil && service != nil {
		return layer, service
	}

	localName := typeName
	if idx := strings.IndexByte(typeName, ':'); idx >= 0 {
		prefix, local := typeName[:idx], typeName[idx+1:]
		localName = local
		if prefix == appPrefix {
			if layer, service := ws.GetLayer(local); layer != nil && service != nil {
				return layer, service
			}
		}
	}

	var matched *workspace.Layer
	for _, candidate := range ws.VisibleLayers("super_admin") {
		published := publishedFeatureTypeName(candidate.PublicID, appPrefix)
		qn := ParseQName(published)
		if published != typeName && qn.LocalPart != localName && sanitizeXMLName(candidate.PublicID) != localName {
			continue
		}
		if matched != nil && matched.PublicID != candidate.PublicID {
			return nil, nil
		}
		matched = candidate
	}
	if matched == nil {
		return nil, nil
	}
	return ws.GetLayer(matched.PublicID)
}

// resolveFeatureIdentifier uses the longest actual publication prefix. Dots in
// publication and local IDs are preserved; visibility is checked by the caller
// after resolution so a restricted match cannot fall back to a shorter name.
func resolveFeatureIdentifier(ws *workspace.Workspace, id, appPrefix string) (string, string, bool) {
	for dot := strings.LastIndexByte(id, '.'); dot > 0; dot = strings.LastIndexByte(id[:dot], '.') {
		if dot == len(id)-1 {
			continue
		}
		if layer, service := resolveFeatureLayer(ws, id[:dot], appPrefix); layer != nil && service != nil {
			local, _ := featureIDForType(id, id[:dot])
			return id[:dot], local, true
		}
	}
	return "", "", false
}

// featureIDForType strips only a known publication prefix, never a dot inside
// the remaining local ID. Explicit ResourceId queries already supply the type.
func featureIDForType(id, typeName string) (string, bool) {
	for _, prefix := range []string{typeName, stripNSPrefix(typeName)} {
		if prefix != "" && strings.HasPrefix(id, prefix+".") {
			return id[len(prefix)+1:], true
		}
	}
	if colon := strings.IndexByte(id, ':'); colon >= 0 {
		prefix := stripNSPrefix(typeName) + "."
		if typeName != "" && strings.HasPrefix(id[colon+1:], prefix) {
			return id[colon+1+len(prefix):], true
		}
	}
	return "", false
}
