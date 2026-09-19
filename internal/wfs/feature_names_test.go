package wfs

import (
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/workspace"
)

func TestApplicationNamespaceDeclarations(t *testing.T) {
	if got := applicationNamespaceDeclarations(NSCite, "cite"); got != `xmlns:cite="http://cite.opengeospatial.org/gmlsf"` {
		t.Fatalf("CITE declarations = %q", got)
	}
	got := applicationNamespaceDeclarations("http://neoserver/app", "app")
	if strings.Count(got, "xmlns:cite=") != 1 || strings.Count(got, "xmlns:app=") != 1 {
		t.Fatalf("application declarations = %q", got)
	}
}

func TestPublishedFeatureTypeName(t *testing.T) {
	tests := map[string]string{
		"places":      "app:places",
		"cite:Autos":  "cite:Autos",
		"tenant:lots": "app:tenant_lots",
	}
	for publicID, want := range tests {
		if got := publishedFeatureTypeName(publicID, "app"); got != want {
			t.Errorf("publishedFeatureTypeName(%q) = %q, want %q", publicID, got, want)
		}
	}
}

func TestResolveFeatureLayerQNameAndGMLID(t *testing.T) {
	cite := &workspace.Layer{PublicID: "cite:Autos", Enabled: true}
	places := &workspace.Layer{PublicID: "places", Enabled: true}
	ws := &workspace.Workspace{Services: map[string]*workspace.Service{
		"source": {
			ID: "source", Enabled: true,
			Layers: map[string]*workspace.Layer{cite.PublicID: cite, places.PublicID: places},
		},
	}}

	for _, name := range []string{"cite:Autos", "Autos", "app:cite_Autos"} {
		layer, _ := resolveFeatureLayer(ws, name, "app")
		if layer != cite {
			t.Errorf("resolveFeatureLayer(%q) resolved %#v, want cite:Autos", name, layer)
			continue
		}
		if got := publishedFeatureTypeName(layer.PublicID, "app"); got != "cite:Autos" {
			t.Errorf("resolveFeatureLayer(%q) canonical name = %q, want cite:Autos", name, got)
		}
	}
	layer, _ := resolveFeatureLayer(ws, "app:places", "app")
	if layer != places {
		t.Errorf("resolveFeatureLayer(app:places) resolved %#v, want places", layer)
	}
}
