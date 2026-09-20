package protocolrequest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEffectiveOperation(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, body, want string
		invalid                        bool
	}{
		{"xml wins", "POST", "/maps/workspaces/demo/wfs?request=GetCapabilities", `<w:GetFeature xmlns:w="http://www.opengis.net/wfs/2.0"/>`, "GETFEATURE", false},
		{"mutation wins", "POST", "/workspaces/demo/wfs?request=GetCapabilities", `<Transaction/>`, "TRANSACTION", false},
		{"trailing root", "POST", "/workspaces/demo/wfs?request=GetCapabilities", `<GetFeature/><Transaction/>`, "", true},
		{"malformed", "POST", "/workspaces/demo/wfs", `<GetFeature>`, "", true},
		{"namespace", "POST", "/workspaces/demo/wfs", `<GetFeature xmlns="wrong"/>`, "", true},
		{"mixed duplicate", "GET", "/workspaces/demo/wfs?request=GetCapabilities&REQUEST=Transaction", "", "", true},
		{"same duplicate", "GET", "/workspaces/demo/wfs?request=GetCapabilities&request=GetCapabilities", "", "GETCAPABILITIES", false},
		{"equivalent mixed case", "GET", "/workspaces/demo/wms?request=GetMap&ReQuEsT=GETMAP&SERVICE=WMS&service=wms&VERSION=1.3.0&version=1.3.0", "", "GETMAP", false},
		{"conflicting service", "GET", "/workspaces/demo/wms?request=GetMap&service=WMS&SERVICE=WFS", "", "", true},
		{"conflicting version", "GET", "/workspaces/demo/wfs?request=GetFeature&version=2.0.0&VERSION=1.0.0", "", "", true},
		{"REST ignores query", "GET", "/workspaces/demo/ogc/collections/places/items?request=ListCollections", "", "GETFEATURES", false},
		{"REST workspace named items", "GET", "/workspaces/items/ogc/collections/places/items?request=GetItem", "", "GETFEATURES", false},
		{"REST collection named items", "GET", "/workspaces/demo/ogc/collections/items", "", "GET", false},
		{"REST collection named collections", "GET", "/workspaces/demo/ogc/collections/collections", "", "GET", false},
		{"REST item named collections", "GET", "/workspaces/demo/ogc/collections/items/items/collections", "", "GETITEM", false},
		{"tile metadata", "GET", "/workspaces/items/ogc-tiles/collections/tiles/tiles/WebMercatorQuad", "", "GET", false},
		{"dataset map tile", "GET", "/prefix/workspaces/demo/ogc-tiles/map/tiles/WebMercatorQuad/2/1/2?request=GET", "", "GETTILE", false},
		{"dataset metadata", "GET", "/workspaces/demo/ogc-tiles/map/tiles/WebMercatorQuad", "", "GET", false},
		{"vector tile", "GET", "/workspaces/items/ogc-tiles/collections/items/tiles/WebMercatorQuad/0/0/0", "", "GETTILE", false},
		{"map tile", "GET", "/workspaces/items/ogc-tiles/collections/items/map/tiles/WebMercatorQuad/0/0/0", "", "GETTILE", false},
		{"WMTS legend uses tile read permission", "GET", "/workspaces/demo/wmts/1.0.0/places/default/legend.png?request=GetCapabilities", "", "GETTILE", false},
		{"WMTS REST ignores query", "GET", "/workspaces/demo/wmts/1.0.0/places/default/grid/0/0/0.png?request=GetCapabilities", "", "GETTILE", false},
		{"WCS XML", "POST", "/workspaces/demo/wcs?request=GetCapabilities", `<GetCoverage xmlns="http://www.opengis.net/wcs/2.0"/>`, "GETCOVERAGE", false},
		{"management body untouched", "POST", "/api/v1/workspaces/demo/services", `{}`, "POST", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := Prepare(httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)), "")
			d := Get(r)
			if (d.Err != nil) != tt.invalid || (!tt.invalid && d.Name != tt.want) {
				t.Fatalf("descriptor=%+v", d)
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != tt.body {
				t.Fatal("request body changed")
			}
			if Prepare(r, "") != r {
				t.Fatal("descriptor was parsed twice")
			}
		})
	}
}

func TestConfiguredMountCannotChangeOperationClassification(t *testing.T) {
	for _, base := range []string{"", "/maps", "/workspaces/prefix/wms", "/items/workspaces/prefix/wfs"} {
		h := Middleware(base)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d := Get(r)
			if d.Service != "wfs" || d.Workspace != "demo" || d.Name != "DROPSTOREDQUERY" || !d.Mutating(r.Method) {
				t.Fatalf("mount %q: %+v", base, d)
			}
			if Prepare(r, "wfs") != r {
				t.Fatal("authorization reparsed the mounted request")
			}
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", base+"/workspaces/demo/wfs?request=DropStoredQuery", nil))
	}
}
