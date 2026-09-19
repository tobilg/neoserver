package wms

import (
	"crypto/tls"
	"encoding/xml"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fogleman/gg"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestCapabilitiesEscapesWorkspaceMetadata(t *testing.T) {
	h := &workspaceHandler{cfg: conf.Config{Server: conf.Server{UrlBase: "https://example.test"}, WMS: conf.WMS{MaxWidth: 4096, MaxHeight: 4096}}}
	ws := &workspace.Workspace{
		ID:          "ws-1",
		Name:        `name</Title><Injected value="1">`,
		Description: `description & <unsafe>`,
		Services:    map[string]*workspace.Service{},
		Settings:    &store.WorkspaceSettings{WMS: store.WMSSettings{Enabled: true, Public: true, MaxWidth: 1024, MaxHeight: 2048}},
	}
	w := httptest.NewRecorder()
	h.handleGetCapabilities(w, httptest.NewRequest(http.MethodGet, "/wms", nil), ws)

	var document struct{ XMLName xml.Name }
	if err := xml.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatalf("capabilities are not well-formed XML: %v", err)
	}
	if strings.Contains(w.Body.String(), "<Injected") {
		t.Fatal("workspace metadata was emitted as XML markup")
	}
	for _, advertised := range []string{"<Format>INIMAGE</Format>", "<Format>BLANK</Format>", "<MaxWidth>1024</MaxWidth>", "<MaxHeight>2048</MaxHeight>"} {
		if !strings.Contains(w.Body.String(), advertised) {
			t.Errorf("capabilities do not advertise implemented behavior %s", advertised)
		}
	}
}

func TestNormalizeQueryExported(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected map[string]string
	}{
		{
			name: "lowercase params",
			url:  "/wms?layers=test&crs=EPSG:4326",
			expected: map[string]string{
				"LAYERS": "test",
				"CRS":    "EPSG:4326",
			},
		},
		{
			name: "mixed case params",
			url:  "/wms?Layers=test&CRS=EPSG:4326&BbOx=0,0,1,1",
			expected: map[string]string{
				"LAYERS": "test",
				"CRS":    "EPSG:4326",
				"BBOX":   "0,0,1,1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			result := NormalizeQuery(req)

			for key, expectedValue := range tt.expected {
				if result.Get(key) != expectedValue {
					t.Errorf("NormalizeQuery()[%s] = %s, want %s", key, result.Get(key), expectedValue)
				}
			}
		})
	}
}

func TestMapCacheKeyTracksManagedAssetManifest(t *testing.T) {
	ws := &workspace.Workspace{ID: "ws", StyleAssetDigest: "red-marker"}
	request := &GetMapRequest{}
	before := getMapCacheKey(ws, request)
	ws.StyleAssetDigest = "blue-marker"
	if before == getMapCacheKey(ws, request) {
		t.Fatal("in-flight old asset render can poison the new map cache")
	}
}

func TestHandleWMSNoWorkspace(t *testing.T) {
	h := &workspaceHandler{
		cfg: conf.Config{},
	}

	req := httptest.NewRequest("GET", "/wms?REQUEST=GetCapabilities", nil)
	w := httptest.NewRecorder()

	h.handleWMS(w, req)

	// InvalidParameterValue returns 400 Bad Request
	if w.Code != http.StatusBadRequest {
		t.Errorf("handleWMS() status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	body := w.Body.String()
	if !strings.Contains(body, "workspace not found") {
		t.Errorf("Expected error about workspace not found, got: %s", body)
	}
}

func TestHandleWMSDisabled(t *testing.T) {
	h := &workspaceHandler{
		cfg: conf.Config{},
	}

	// Create workspace with WMS disabled
	ws := &workspace.Workspace{
		ID:   "test",
		Name: "test",
		Settings: &store.WorkspaceSettings{
			WMS: store.WMSSettings{
				Enabled: false,
			},
		},
	}

	req := httptest.NewRequest("GET", "/wms?REQUEST=GetCapabilities", nil)
	req = req.WithContext(workspace.WithWorkspace(req.Context(), ws))
	w := httptest.NewRecorder()

	h.handleWMS(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("handleWMS() status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	body := w.Body.String()
	if !strings.Contains(body, "WMS is not enabled") {
		t.Errorf("Expected error about WMS not enabled, got: %s", body)
	}
}

func TestHandleWMSNilSettings(t *testing.T) {
	h := &workspaceHandler{
		cfg: conf.Config{},
	}

	// Create workspace with nil settings
	ws := &workspace.Workspace{
		ID:       "test",
		Name:     "test",
		Settings: nil,
	}

	req := httptest.NewRequest("GET", "/wms?REQUEST=GetCapabilities", nil)
	req = req.WithContext(workspace.WithWorkspace(req.Context(), ws))
	w := httptest.NewRecorder()

	h.handleWMS(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "WMS is not enabled") {
		t.Errorf("Expected error about WMS not enabled, got: %s", body)
	}
}

func TestHandleWMSMissingRequest(t *testing.T) {
	h := &workspaceHandler{
		cfg: conf.Config{},
	}

	ws := &workspace.Workspace{
		ID:   "test",
		Name: "test",
		Settings: &store.WorkspaceSettings{
			WMS: store.WMSSettings{
				Enabled: true,
				Public:  true,
			},
		},
	}

	req := httptest.NewRequest("GET", "/wms", nil)
	req = req.WithContext(workspace.WithWorkspace(req.Context(), ws))
	w := httptest.NewRecorder()

	h.handleWMS(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "REQUEST parameter is required") {
		t.Errorf("Expected error about REQUEST required, got: %s", body)
	}
}

func TestHandleWMSUnsupportedRequest(t *testing.T) {
	h := &workspaceHandler{
		cfg: conf.Config{},
	}

	ws := &workspace.Workspace{
		ID:   "test",
		Name: "test",
		Settings: &store.WorkspaceSettings{
			WMS: store.WMSSettings{
				Enabled: true,
				Public:  true,
			},
		},
	}

	req := httptest.NewRequest("GET", "/wms?REQUEST=INVALID", nil)
	req = req.WithContext(workspace.WithWorkspace(req.Context(), ws))
	w := httptest.NewRecorder()

	h.handleWMS(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Unsupported REQUEST") {
		t.Errorf("Expected error about unsupported request, got: %s", body)
	}
}

func TestWriteGetMapException(t *testing.T) {
	h := &workspaceHandler{}

	tests := []struct {
		name       string
		exceptions string
		checkFunc  func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:       "XML exception",
			exceptions: ExceptionsXML,
			checkFunc: func(t *testing.T, w *httptest.ResponseRecorder) {
				if w.Header().Get("Content-Type") != "text/xml" {
					t.Errorf("Expected Content-Type text/xml, got %s", w.Header().Get("Content-Type"))
				}
			},
		},
		{
			name:       "INIMAGE exception",
			exceptions: ExceptionsINIMAGE,
			checkFunc: func(t *testing.T, w *httptest.ResponseRecorder) {
				if w.Header().Get("Content-Type") != "image/png" {
					t.Errorf("Expected Content-Type image/png, got %s", w.Header().Get("Content-Type"))
				}
				if w.Code != http.StatusOK {
					t.Errorf("INIMAGE should return 200 OK, got %d", w.Code)
				}
			},
		},
		{
			name:       "BLANK exception",
			exceptions: ExceptionsBLANK,
			checkFunc: func(t *testing.T, w *httptest.ResponseRecorder) {
				if w.Header().Get("Content-Type") != "image/png" {
					t.Errorf("Expected Content-Type image/png, got %s", w.Header().Get("Content-Type"))
				}
				if w.Code != http.StatusOK {
					t.Errorf("BLANK should return 200 OK, got %d", w.Code)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := &GetMapRequest{
				Width:       256,
				Height:      256,
				Format:      "image/png",
				Exceptions:  tt.exceptions,
				Transparent: true,
				BgColor:     color.RGBA{R: 255, G: 255, B: 255, A: 255},
			}

			h.writeGetMapException(w, req, ExceptionLayerNotDefined, "Layer not found")
			tt.checkFunc(t, w)
		})
	}
}

func TestWriteFeatureInfoXML(t *testing.T) {
	h := &workspaceHandler{}
	w := httptest.NewRecorder()

	results := []featureInfoResult{
		{
			LayerName: "test_layer",
			Properties: map[string]interface{}{
				"name":  "Test",
				"value": 123,
			},
		},
	}

	h.writeFeatureInfoXML(w, results)

	if w.Code != http.StatusOK {
		t.Errorf("writeFeatureInfoXML() status = %d, want %d", w.Code, http.StatusOK)
	}

	if w.Header().Get("Content-Type") != InfoFormatXML {
		t.Errorf("writeFeatureInfoXML() Content-Type = %s, want %s", w.Header().Get("Content-Type"), InfoFormatXML)
	}

	body := w.Body.String()
	if !strings.Contains(body, "test_layer") {
		t.Error("Response should contain layer name")
	}
	if !strings.Contains(body, "name") {
		t.Error("Response should contain property name")
	}
}

func TestWriteFeatureInfoJSON(t *testing.T) {
	h := &workspaceHandler{}
	w := httptest.NewRecorder()

	results := []featureInfoResult{
		{
			LayerName: "test_layer",
			Properties: map[string]interface{}{
				"name":  "Test",
				"value": 123,
			},
		},
	}

	h.writeFeatureInfoJSON(w, results)

	if w.Code != http.StatusOK {
		t.Errorf("writeFeatureInfoJSON() status = %d, want %d", w.Code, http.StatusOK)
	}

	if w.Header().Get("Content-Type") != InfoFormatJSON {
		t.Errorf("writeFeatureInfoJSON() Content-Type = %s, want %s", w.Header().Get("Content-Type"), InfoFormatJSON)
	}

	body := w.Body.String()
	if !strings.Contains(body, "FeatureCollection") {
		t.Error("Response should contain FeatureCollection")
	}
	if !strings.Contains(body, "test_layer") {
		t.Error("Response should contain layer name")
	}
}

func TestWriteFeatureInfoHTML(t *testing.T) {
	h := &workspaceHandler{}

	t.Run("with results", func(t *testing.T) {
		w := httptest.NewRecorder()
		results := []featureInfoResult{
			{
				LayerName: "layer1",
				Properties: map[string]interface{}{
					"name": "Test",
				},
			},
			{
				LayerName: "layer1",
				Properties: map[string]interface{}{
					"name": "Test2",
				},
			},
			{
				LayerName: "layer2",
				Properties: map[string]interface{}{
					"value": 456,
				},
			},
		}

		h.writeFeatureInfoHTML(w, results)

		if w.Code != http.StatusOK {
			t.Errorf("writeFeatureInfoHTML() status = %d, want %d", w.Code, http.StatusOK)
		}

		if w.Header().Get("Content-Type") != InfoFormatHTML {
			t.Errorf("writeFeatureInfoHTML() Content-Type = %s, want %s", w.Header().Get("Content-Type"), InfoFormatHTML)
		}

		body := w.Body.String()
		if !strings.Contains(body, "<!DOCTYPE html>") {
			t.Error("Response should be valid HTML")
		}
		if !strings.Contains(body, "layer1") {
			t.Error("Response should contain layer1")
		}
		if !strings.Contains(body, "layer2") {
			t.Error("Response should contain layer2")
		}
	})

	t.Run("empty results", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.writeFeatureInfoHTML(w, []featureInfoResult{})

		body := w.Body.String()
		if !strings.Contains(body, "No features found") {
			t.Error("Response should indicate no features found")
		}
	})
}

func TestWriteFeatureInfoText(t *testing.T) {
	h := &workspaceHandler{}

	t.Run("with results", func(t *testing.T) {
		w := httptest.NewRecorder()
		results := []featureInfoResult{
			{
				LayerName: "test_layer",
				Properties: map[string]interface{}{
					"name": "Test",
				},
			},
			{
				LayerName: "another_layer",
				Properties: map[string]interface{}{
					"value": 123,
				},
			},
		}

		h.writeFeatureInfoText(w, results)

		if w.Code != http.StatusOK {
			t.Errorf("writeFeatureInfoText() status = %d, want %d", w.Code, http.StatusOK)
		}

		if w.Header().Get("Content-Type") != InfoFormatText {
			t.Errorf("writeFeatureInfoText() Content-Type = %s, want %s", w.Header().Get("Content-Type"), InfoFormatText)
		}

		body := w.Body.String()
		if !strings.Contains(body, "test_layer") {
			t.Error("Response should contain layer name")
		}
		if !strings.Contains(body, "---") {
			t.Error("Response should contain separator between features")
		}
	})

	t.Run("empty results", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.writeFeatureInfoText(w, []featureInfoResult{})

		body := w.Body.String()
		if !strings.Contains(body, "No features found") {
			t.Error("Response should indicate no features found")
		}
	})
}

func TestMinFloat(t *testing.T) {
	tests := []struct {
		a, b, expected float64
	}{
		{1.0, 2.0, 1.0},
		{2.0, 1.0, 1.0},
		{1.0, 1.0, 1.0},
		{-1.0, 1.0, -1.0},
		{0.0, 0.0, 0.0},
	}

	for _, tt := range tests {
		result := minFloat(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("minFloat(%f, %f) = %f, want %f", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestSetLegendColor(t *testing.T) {
	dc := gg.NewContext(10, 10)

	tests := []struct {
		name    string
		c       color.RGBA
		opacity float64
	}{
		{
			name:    "full opacity",
			c:       color.RGBA{R: 255, G: 0, B: 0, A: 255},
			opacity: 1.0,
		},
		{
			name:    "half opacity",
			c:       color.RGBA{R: 0, G: 255, B: 0, A: 255},
			opacity: 0.5,
		},
		{
			name:    "zero opacity",
			c:       color.RGBA{R: 0, G: 0, B: 255, A: 255},
			opacity: 0.0,
		},
		{
			name:    "with alpha channel",
			c:       color.RGBA{R: 128, G: 128, B: 128, A: 128},
			opacity: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify it doesn't panic
			setLegendColor(dc, tt.c, tt.opacity)
		})
	}
}

func TestWorkspaceBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		cfg      conf.Config
		reqURL   string
		wsName   string
		useTLS   bool
		expected string
	}{
		{
			name: "with UrlBase",
			cfg: conf.Config{
				Server: conf.Server{
					UrlBase:  "https://example.com/",
					BasePath: "/api",
				},
			},
			reqURL:   "http://localhost/wms",
			wsName:   "test",
			expected: "https://example.com/api/workspaces/test/wms",
		},
		{
			name: "with UrlBase no trailing slash",
			cfg: conf.Config{
				Server: conf.Server{
					UrlBase:  "https://example.com",
					BasePath: "/api",
				},
			},
			reqURL:   "http://localhost/wms",
			wsName:   "test",
			expected: "https://example.com/api/workspaces/test/wms",
		},
		{
			name: "without UrlBase http",
			cfg: conf.Config{
				Server: conf.Server{
					BasePath: "/api",
				},
			},
			reqURL:   "http://localhost:9000/wms",
			wsName:   "myworkspace",
			useTLS:   false,
			expected: "/api/workspaces/myworkspace/wms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &workspaceHandler{cfg: tt.cfg}
			req := httptest.NewRequest("GET", tt.reqURL, nil)
			if tt.useTLS {
				req.TLS = &tls.ConnectionState{}
			}

			result := h.workspaceBaseURL(req, tt.wsName)
			if result != tt.expected {
				t.Errorf("workspaceBaseURL() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestDrawLegendSymbols(t *testing.T) {
	h := &workspaceHandler{}
	dc := gg.NewContext(30, 30)

	t.Run("Point legend", func(t *testing.T) {
		style := sld.DefaultPointStyle()
		rule := &sld.ResolvedRule{PointStyle: style}
		h.drawLegendSymbol(dc, 0, 0, 30, 30, "Point", rule)
		h.drawLegendSymbol(dc, 0, 0, 30, 30, "MultiPoint", rule)
	})

	t.Run("Line legend", func(t *testing.T) {
		style := sld.DefaultLineStyle()
		rule := &sld.ResolvedRule{LineStyle: style}
		h.drawLegendSymbol(dc, 0, 0, 30, 30, "LineString", rule)
		h.drawLegendSymbol(dc, 0, 0, 30, 30, "MultiLineString", rule)
	})

	t.Run("Polygon legend", func(t *testing.T) {
		style := sld.DefaultPolygonStyle()
		rule := &sld.ResolvedRule{PolygonStyle: style}
		h.drawLegendSymbol(dc, 0, 0, 30, 30, "Polygon", rule)
		h.drawLegendSymbol(dc, 0, 0, 30, 30, "MultiPolygon", rule)
	})

	t.Run("Unknown geometry type uses polygon", func(t *testing.T) {
		style := sld.DefaultPolygonStyle()
		rule := &sld.ResolvedRule{PolygonStyle: style}
		h.drawLegendSymbol(dc, 0, 0, 30, 30, "Unknown", rule)
	})
}

func TestDrawPointLegend(t *testing.T) {
	h := &workspaceHandler{}
	dc := gg.NewContext(30, 30)

	t.Run("nil style uses default", func(t *testing.T) {
		h.drawPointLegend(dc, 0, 0, 30, 30, nil)
	})

	t.Run("circle shape", func(t *testing.T) {
		style := &sld.PointStyle{
			Shape:       "circle",
			Size:        10,
			FillColor:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
			StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
			StrokeWidth: 1,
			Opacity:     1.0,
		}
		h.drawPointLegend(dc, 0, 0, 30, 30, style)
	})

	t.Run("square shape", func(t *testing.T) {
		style := &sld.PointStyle{
			Shape:       "square",
			Size:        10,
			FillColor:   color.RGBA{R: 0, G: 255, B: 0, A: 255},
			StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
			StrokeWidth: 1,
			Opacity:     1.0,
		}
		h.drawPointLegend(dc, 0, 0, 30, 30, style)
	})

	t.Run("triangle shape", func(t *testing.T) {
		style := &sld.PointStyle{
			Shape:       "triangle",
			Size:        10,
			FillColor:   color.RGBA{R: 0, G: 0, B: 255, A: 255},
			StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
			StrokeWidth: 1,
			Opacity:     1.0,
		}
		h.drawPointLegend(dc, 0, 0, 30, 30, style)
	})
}

func TestDrawLineLegend(t *testing.T) {
	h := &workspaceHandler{}
	dc := gg.NewContext(30, 30)

	t.Run("nil style uses default", func(t *testing.T) {
		h.drawLineLegend(dc, 0, 0, 30, 30, nil)
	})

	t.Run("solid line", func(t *testing.T) {
		style := &sld.LineStyle{
			Color:   color.RGBA{R: 255, G: 0, B: 0, A: 255},
			Width:   2.0,
			Opacity: 1.0,
		}
		h.drawLineLegend(dc, 0, 0, 30, 30, style)
	})

	t.Run("dashed line", func(t *testing.T) {
		style := &sld.LineStyle{
			Color:     color.RGBA{R: 0, G: 255, B: 0, A: 255},
			Width:     2.0,
			Opacity:   1.0,
			DashArray: []float64{5, 3},
		}
		h.drawLineLegend(dc, 0, 0, 30, 30, style)
	})
}

func TestDrawPolygonLegend(t *testing.T) {
	h := &workspaceHandler{}
	dc := gg.NewContext(30, 30)

	t.Run("nil style uses default", func(t *testing.T) {
		h.drawPolygonLegend(dc, 0, 0, 30, 30, nil)
	})

	t.Run("with stroke", func(t *testing.T) {
		style := &sld.PolygonStyle{
			FillColor:     color.RGBA{R: 255, G: 0, B: 0, A: 255},
			FillOpacity:   0.5,
			StrokeColor:   color.RGBA{R: 0, G: 0, B: 0, A: 255},
			StrokeWidth:   2.0,
			StrokeOpacity: 1.0,
		}
		h.drawPolygonLegend(dc, 0, 0, 30, 30, style)
	})

	t.Run("without stroke", func(t *testing.T) {
		style := &sld.PolygonStyle{
			FillColor:   color.RGBA{R: 0, G: 255, B: 0, A: 255},
			FillOpacity: 0.8,
			StrokeWidth: 0,
		}
		h.drawPolygonLegend(dc, 0, 0, 30, 30, style)
	})
}

func TestWorkspaceDependencies(t *testing.T) {
	deps := WorkspaceDependencies{
		Config: conf.Config{
			WMS: conf.WMS{
				Enabled: true,
			},
		},
	}

	if !deps.Config.WMS.Enabled {
		t.Error("WorkspaceDependencies should preserve Config")
	}
}
