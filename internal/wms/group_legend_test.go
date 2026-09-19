package wms

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestGetLegendGraphicExplainsLayerGroups(t *testing.T) {
	h, ws := rasterWMSFixture()
	ws.Groups = map[string]*workspace.LayerGroup{
		"stack": {
			ID: "g1", PublicID: "stack", Enabled: true, Public: true,
			Members: []store.LayerGroupMember{{Resource: "elevation"}},
		},
		"hidden": {
			ID: "g2", PublicID: "hidden", Enabled: true, AllowedRoles: []string{"admin"},
			Members: []store.LayerGroupMember{{Resource: "elevation"}},
		},
	}
	legend := func(layer string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		h.handleGetLegendGraphic(recorder, httptest.NewRequest(http.MethodGet,
			"/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetLegendGraphic&LAYER="+layer+"&STYLE=&FORMAT=image/png&WIDTH=32&HEIGHT=8", nil), ws)
		return recorder
	}

	visible := legend("stack").Body.String()
	if !strings.Contains(visible, `code="InvalidParameterValue"`) ||
		!strings.Contains(visible, `locator="LAYER"`) ||
		!strings.Contains(visible, "stack is a layer group; request a legend for each member layer") {
		t.Fatalf("unexpected group legend response: %s", visible)
	}

	// An anonymous caller lacks the group's role, so it stays undefined.
	hidden := legend("hidden").Body.String()
	if !strings.Contains(hidden, `code="LayerNotDefined"`) || strings.Contains(hidden, "layer group") {
		t.Fatalf("private group was disclosed: %s", hidden)
	}
}
