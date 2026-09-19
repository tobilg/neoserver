package wms

import (
	"net/http/httptest"
	"testing"
)

func TestParseCRS(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"EPSG:4326", 4326, false},
		{"EPSG:3857", 3857, false},
		{"epsg:4326", 4326, false},
		{"CRS:84", 4326, false},
		{"OGC:CRS84", 4326, false},
		{"urn:ogc:def:crs:EPSG::4326", 4326, false},
		{"urn:ogc:def:crs:EPSG:0:3857", 3857, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseCRS(tt.input)
			if tt.hasError {
				if err == nil {
					t.Errorf("parseCRS(%s) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseCRS(%s) unexpected error: %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("parseCRS(%s) = %d, want %d", tt.input, result, tt.expected)
				}
			}
		})
	}
}

func TestParseBBox(t *testing.T) {
	tests := []struct {
		bbox     string
		crs      string
		expected struct{ minX, minY, maxX, maxY float64 }
		hasError bool
	}{
		{
			"0,0,100,100",
			"EPSG:3857",
			struct{ minX, minY, maxX, maxY float64 }{0, 0, 100, 100},
			false,
		},
		{
			"-180,-90,180,90",
			"CRS:84",
			struct{ minX, minY, maxX, maxY float64 }{-180, -90, 180, 90},
			false,
		},
		{
			// EPSG:4326 with WMS 1.3.0 uses lat,lon order - should be swapped
			"-90,-180,90,180",
			"EPSG:4326",
			struct{ minX, minY, maxX, maxY float64 }{-180, -90, 180, 90},
			false,
		},
		{
			"invalid",
			"EPSG:4326",
			struct{ minX, minY, maxX, maxY float64 }{},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.bbox+"_"+tt.crs, func(t *testing.T) {
			result, err := parseBBox(tt.bbox, tt.crs)
			if tt.hasError {
				if err == nil {
					t.Errorf("parseBBox expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("parseBBox unexpected error: %v", err)
				}
				if result.MinX != tt.expected.minX || result.MinY != tt.expected.minY ||
					result.MaxX != tt.expected.maxX || result.MaxY != tt.expected.maxY {
					t.Errorf("parseBBox = %+v, want %+v", result, tt.expected)
				}
			}
		})
	}
}

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		input    string
		expected struct{ r, g, b uint8 }
	}{
		{"0xFF0000", struct{ r, g, b uint8 }{255, 0, 0}},
		{"#00FF00", struct{ r, g, b uint8 }{0, 255, 0}},
		{"0000FF", struct{ r, g, b uint8 }{0, 0, 255}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			c := parseHexColor(tt.input)
			if c.R != tt.expected.r || c.G != tt.expected.g || c.B != tt.expected.b {
				t.Errorf("parseHexColor(%s) = RGB(%d,%d,%d), want RGB(%d,%d,%d)",
					tt.input, c.R, c.G, c.B, tt.expected.r, tt.expected.g, tt.expected.b)
			}
		})
	}
}

func TestParseGetMapRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180&WIDTH=256&HEIGHT=256&FORMAT=image/png", nil)

	result, err := ParseGetMapRequest(req, 4096, 4096)
	if err != nil {
		t.Fatalf("ParseGetMapRequest failed: %v", err)
	}

	if len(result.Layers) != 1 || result.Layers[0] != "test" {
		t.Errorf("Layers = %v, want [test]", result.Layers)
	}

	if result.Width != 256 {
		t.Errorf("Width = %d, want 256", result.Width)
	}

	if result.Height != 256 {
		t.Errorf("Height = %d, want 256", result.Height)
	}

	if result.Format != "image/png" {
		t.Errorf("Format = %s, want image/png", result.Format)
	}
}

func TestParseGetMapRenderingEnvironmentAndDimensions(t *testing.T) {
	req := httptest.NewRequest("GET", "/wms?VERSION=1.3.0&LAYERS=test&STYLES=&CRS=CRS:84&BBOX=0,0,1,1&WIDTH=1&HEIGHT=1&FORMAT=image/png&TIME=2026-01-01T00:00:00Z&ELEVATION=10&ENV=b:2%3Ba:1", nil)
	result, err := ParseGetMapRequest(req, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Time == "" || result.Elevation != "10" || result.Environment["a"] != "1" || result.EnvironmentKey != "a:1;b:2" {
		t.Fatalf("unexpected request: %+v", result)
	}
	bad := httptest.NewRequest("GET", "/wms?VERSION=1.3.0&LAYERS=test&STYLES=&CRS=CRS:84&BBOX=0,0,1,1&WIDTH=1&HEIGHT=1&FORMAT=image/png&ENV=a:1%3Ba:2", nil)
	if _, err := ParseGetMapRequest(bad, 10, 10); err == nil {
		t.Fatal("expected duplicate ENV error")
	}
}

func TestParseGetMapRequestValidation(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		errMsg string
	}{
		{
			"missing LAYERS",
			"/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=256&HEIGHT=256&FORMAT=image/png",
			"LAYERS parameter is required",
		},
		{
			"missing CRS",
			"/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&BBOX=0,0,1,1&WIDTH=256&HEIGHT=256&FORMAT=image/png",
			"CRS parameter is required",
		},
		{
			"missing BBOX",
			"/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&WIDTH=256&HEIGHT=256&FORMAT=image/png",
			"BBOX parameter is required",
		},
		{
			"invalid WIDTH",
			"/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=abc&HEIGHT=256&FORMAT=image/png",
			"WIDTH must be a positive integer",
		},
		{
			"exceeds max width",
			"/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=9999&HEIGHT=256&FORMAT=image/png",
			"WIDTH exceeds maximum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			_, err := ParseGetMapRequest(req, 4096, 4096)
			if err == nil {
				t.Errorf("Expected error containing '%s', got nil", tt.errMsg)
				return
			}
			if !containsString(err.Error(), tt.errMsg) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.errMsg, err.Error())
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected map[string]string
	}{
		{
			name: "lowercase params",
			url:  "/wms?layers=test&crs=EPSG:4326&bbox=0,0,1,1",
			expected: map[string]string{
				"LAYERS": "test",
				"CRS":    "EPSG:4326",
				"BBOX":   "0,0,1,1",
			},
		},
		{
			name: "uppercase params",
			url:  "/wms?LAYERS=test&CRS=EPSG:4326",
			expected: map[string]string{
				"LAYERS": "test",
				"CRS":    "EPSG:4326",
			},
		},
		{
			name: "mixed case params",
			url:  "/wms?Layers=test&cRS=EPSG:4326&BbOx=0,0,1,1",
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
			result := normalizeQuery(req)

			for key, expectedValue := range tt.expected {
				if result.Get(key) != expectedValue {
					t.Errorf("normalizeQuery()[%s] = %s, want %s", key, result.Get(key), expectedValue)
				}
			}
		})
	}
}

func TestRequestError(t *testing.T) {
	err := &RequestError{
		Code:    ExceptionMissingParameterValue,
		Message: "LAYERS parameter is required",
	}

	expectedMsg := "MissingParameterValue: LAYERS parameter is required"
	if err.Error() != expectedMsg {
		t.Errorf("RequestError.Error() = %s, want %s", err.Error(), expectedMsg)
	}
}

func TestParseGetFeatureInfoRequest(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo"+
			"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png"+
			"&QUERY_LAYERS=test&I=128&J=128&INFO_FORMAT=application/json&FEATURE_COUNT=5", nil)

		result, err := ParseGetFeatureInfoRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetFeatureInfoRequest() error = %v", err)
		}

		if len(result.QueryLayers) != 1 || result.QueryLayers[0] != "test" {
			t.Errorf("QueryLayers = %v, want [test]", result.QueryLayers)
		}
		if result.I != 128 {
			t.Errorf("I = %d, want 128", result.I)
		}
		if result.J != 128 {
			t.Errorf("J = %d, want 128", result.J)
		}
		if result.InfoFormat != "application/json" {
			t.Errorf("InfoFormat = %s, want application/json", result.InfoFormat)
		}
		if result.FeatureCount != 5 {
			t.Errorf("FeatureCount = %d, want 5", result.FeatureCount)
		}
	})

	t.Run("default values", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo"+
			"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png"+
			"&QUERY_LAYERS=test&I=0&J=0", nil)

		result, err := ParseGetFeatureInfoRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetFeatureInfoRequest() error = %v", err)
		}

		if result.InfoFormat != InfoFormatXML {
			t.Errorf("InfoFormat = %s, want %s", result.InfoFormat, InfoFormatXML)
		}
		if result.FeatureCount != 10 {
			t.Errorf("FeatureCount = %d, want 10 (default)", result.FeatureCount)
		}
	})

	t.Run("XY fallback (WMS 1.1.1)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo"+
			"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png"+
			"&QUERY_LAYERS=test&X=64&Y=32", nil)

		result, err := ParseGetFeatureInfoRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetFeatureInfoRequest() error = %v", err)
		}

		if result.I != 64 {
			t.Errorf("I = %d, want 64", result.I)
		}
		if result.J != 32 {
			t.Errorf("J = %d, want 32", result.J)
		}
	})

	t.Run("empty query layers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo"+
			"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png"+
			"&QUERY_LAYERS=&I=0&J=0", nil)

		result, err := ParseGetFeatureInfoRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetFeatureInfoRequest() error = %v", err)
		}

		if len(result.QueryLayers) != 0 {
			t.Errorf("QueryLayers = %v, want empty", result.QueryLayers)
		}
	})
}

func TestParseGetFeatureInfoRequestErrors(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		errMsg string
	}{
		{
			name: "missing QUERY_LAYERS",
			url: "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo" +
				"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180" +
				"&WIDTH=256&HEIGHT=256&FORMAT=image/png&I=0&J=0",
			errMsg: "QUERY_LAYERS parameter is required",
		},
		{
			name: "missing I",
			url: "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo" +
				"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180" +
				"&WIDTH=256&HEIGHT=256&FORMAT=image/png&QUERY_LAYERS=test&J=0",
			errMsg: "I parameter is required",
		},
		{
			name: "missing J",
			url: "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo" +
				"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180" +
				"&WIDTH=256&HEIGHT=256&FORMAT=image/png&QUERY_LAYERS=test&I=0",
			errMsg: "J parameter is required",
		},
		{
			name: "I out of bounds",
			url: "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo" +
				"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180" +
				"&WIDTH=256&HEIGHT=256&FORMAT=image/png&QUERY_LAYERS=test&I=999&J=0",
			errMsg: "I must be a valid pixel coordinate",
		},
		{
			name: "J out of bounds",
			url: "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo" +
				"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180" +
				"&WIDTH=256&HEIGHT=256&FORMAT=image/png&QUERY_LAYERS=test&I=0&J=999",
			errMsg: "J must be a valid pixel coordinate",
		},
		{
			name: "invalid I",
			url: "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo" +
				"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180" +
				"&WIDTH=256&HEIGHT=256&FORMAT=image/png&QUERY_LAYERS=test&I=abc&J=0",
			errMsg: "I must be a valid pixel coordinate",
		},
		{
			name: "negative I",
			url: "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo" +
				"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180" +
				"&WIDTH=256&HEIGHT=256&FORMAT=image/png&QUERY_LAYERS=test&I=-1&J=0",
			errMsg: "I must be a valid pixel coordinate",
		},
		{
			name: "invalid INFO_FORMAT",
			url: "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo" +
				"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180" +
				"&WIDTH=256&HEIGHT=256&FORMAT=image/png&QUERY_LAYERS=test&I=0&J=0&INFO_FORMAT=invalid/format",
			errMsg: "Invalid INFO_FORMAT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			_, err := ParseGetFeatureInfoRequest(req, 4096, 4096)
			if err == nil {
				t.Errorf("Expected error containing '%s', got nil", tt.errMsg)
				return
			}
			if !containsString(err.Error(), tt.errMsg) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.errMsg, err.Error())
			}
		})
	}
}

func TestParseGetLegendGraphicRequest(t *testing.T) {
	t.Run("valid request with all params", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&REQUEST=GetLegendGraphic"+
			"&LAYER=test&STYLE=default&FORMAT=image/png&WIDTH=30&HEIGHT=25"+
			"&SLD=http://example.com/style.sld&SLD_BODY=inline", nil)

		result, err := ParseGetLegendGraphicRequest(req)
		if err != nil {
			t.Fatalf("ParseGetLegendGraphicRequest() error = %v", err)
		}

		if result.Layer != "test" {
			t.Errorf("Layer = %s, want test", result.Layer)
		}
		if result.Style != "default" {
			t.Errorf("Style = %s, want default", result.Style)
		}
		if result.Format != "image/png" {
			t.Errorf("Format = %s, want image/png", result.Format)
		}
		if result.Width != 30 {
			t.Errorf("Width = %d, want 30", result.Width)
		}
		if result.Height != 25 {
			t.Errorf("Height = %d, want 25", result.Height)
		}
		if result.SLD != "http://example.com/style.sld" {
			t.Errorf("SLD = %s, want http://example.com/style.sld", result.SLD)
		}
		if result.SLDBody != "inline" {
			t.Errorf("SLDBody = %s, want inline", result.SLDBody)
		}
	})

	t.Run("minimal request with defaults", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&REQUEST=GetLegendGraphic&LAYER=test", nil)

		result, err := ParseGetLegendGraphicRequest(req)
		if err != nil {
			t.Fatalf("ParseGetLegendGraphicRequest() error = %v", err)
		}

		if result.Layer != "test" {
			t.Errorf("Layer = %s, want test", result.Layer)
		}
		if result.Format != FormatPNG {
			t.Errorf("Format = %s, want %s (default)", result.Format, FormatPNG)
		}
		if result.Width != 20 {
			t.Errorf("Width = %d, want 20 (default)", result.Width)
		}
		if result.Height != 20 {
			t.Errorf("Height = %d, want 20 (default)", result.Height)
		}
	})

	t.Run("missing LAYER", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&REQUEST=GetLegendGraphic", nil)

		_, err := ParseGetLegendGraphicRequest(req)
		if err == nil {
			t.Error("Expected error for missing LAYER, got nil")
			return
		}
		if !containsString(err.Error(), "LAYER parameter is required") {
			t.Errorf("Expected error about LAYER, got '%s'", err.Error())
		}
	})

	t.Run("invalid WIDTH rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&REQUEST=GetLegendGraphic&LAYER=test&WIDTH=abc", nil)

		if _, err := ParseGetLegendGraphicRequest(req); err == nil {
			t.Fatal("expected invalid width to be rejected")
		}
	})

	t.Run("zero WIDTH rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&REQUEST=GetLegendGraphic&LAYER=test&WIDTH=0", nil)

		if _, err := ParseGetLegendGraphicRequest(req); err == nil {
			t.Fatal("expected zero width to be rejected")
		}
	})

	t.Run("negative WIDTH rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&REQUEST=GetLegendGraphic&LAYER=test&WIDTH=-10", nil)

		if _, err := ParseGetLegendGraphicRequest(req); err == nil {
			t.Fatal("expected negative width to be rejected")
		}
	})
}

func TestParseGetMapRequestAdvanced(t *testing.T) {
	t.Run("with BGCOLOR", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap"+
			"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png&BGCOLOR=0xFF0000", nil)

		result, err := ParseGetMapRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetMapRequest() error = %v", err)
		}

		if result.BgColor.R != 255 || result.BgColor.G != 0 || result.BgColor.B != 0 {
			t.Errorf("BgColor = RGB(%d,%d,%d), want RGB(255,0,0)",
				result.BgColor.R, result.BgColor.G, result.BgColor.B)
		}
	})

	t.Run("with TRANSPARENT", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap"+
			"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png&TRANSPARENT=true", nil)

		result, err := ParseGetMapRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetMapRequest() error = %v", err)
		}

		if !result.Transparent {
			t.Error("Transparent should be true")
		}
	})

	t.Run("with SLD and SLD_BODY", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap"+
			"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png&SLD=http://example.com/style.sld&SLD_BODY=inline", nil)

		result, err := ParseGetMapRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetMapRequest() error = %v", err)
		}

		if result.SLD != "http://example.com/style.sld" {
			t.Errorf("SLD = %s, want http://example.com/style.sld", result.SLD)
		}
		if result.SLDBody != "inline" {
			t.Errorf("SLDBody = %s, want inline", result.SLDBody)
		}
	})

	t.Run("with TIME", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap"+
			"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png&TIME=2023-01-01T00:00:00Z", nil)

		result, err := ParseGetMapRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetMapRequest() error = %v", err)
		}

		if result.Time != "2023-01-01T00:00:00Z" {
			t.Errorf("Time = %s, want 2023-01-01T00:00:00Z", result.Time)
		}
	})

	t.Run("EXCEPTIONS parameter", func(t *testing.T) {
		tests := []struct {
			exceptions string
			expected   string
		}{
			{"", ExceptionsXML},
			{"XML", ExceptionsXML},
			{"INIMAGE", ExceptionsINIMAGE},
			{"BLANK", ExceptionsBLANK},
			{"invalid", ExceptionsXML},
		}

		for _, tt := range tests {
			req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap"+
				"&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
				"&WIDTH=256&HEIGHT=256&FORMAT=image/png&EXCEPTIONS="+tt.exceptions, nil)

			result, err := ParseGetMapRequest(req, 4096, 4096)
			if err != nil {
				t.Fatalf("ParseGetMapRequest() error = %v", err)
			}

			if result.Exceptions != tt.expected {
				t.Errorf("Exceptions for '%s' = %s, want %s", tt.exceptions, result.Exceptions, tt.expected)
			}
		}
	})

	t.Run("SRS fallback (WMS 1.1.1)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap"+
			"&LAYERS=test&STYLES=&SRS=EPSG:3857&BBOX=0,0,1000,1000"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png", nil)

		result, err := ParseGetMapRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetMapRequest() error = %v", err)
		}

		if result.CRS != "EPSG:3857" {
			t.Errorf("CRS = %s, want EPSG:3857", result.CRS)
		}
		if result.SRID != 3857 {
			t.Errorf("SRID = %d, want 3857", result.SRID)
		}
	})

	t.Run("multiple layers and styles", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap"+
			"&LAYERS=layer1,layer2,layer3&STYLES=style1,style2,style3&CRS=EPSG:4326&BBOX=-90,-180,90,180"+
			"&WIDTH=256&HEIGHT=256&FORMAT=image/png", nil)

		result, err := ParseGetMapRequest(req, 4096, 4096)
		if err != nil {
			t.Fatalf("ParseGetMapRequest() error = %v", err)
		}

		expectedLayers := []string{"layer1", "layer2", "layer3"}
		if len(result.Layers) != len(expectedLayers) {
			t.Errorf("Layers count = %d, want %d", len(result.Layers), len(expectedLayers))
		}
		for i, l := range expectedLayers {
			if result.Layers[i] != l {
				t.Errorf("Layers[%d] = %s, want %s", i, result.Layers[i], l)
			}
		}

		expectedStyles := []string{"style1", "style2", "style3"}
		if len(result.Styles) != len(expectedStyles) {
			t.Errorf("Styles count = %d, want %d", len(result.Styles), len(expectedStyles))
		}
	})
}

func TestParseGetMapRequestMoreErrors(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		errMsg string
	}{
		{
			name:   "missing VERSION",
			url:    "/wms?SERVICE=WMS&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=256&HEIGHT=256&FORMAT=image/png",
			errMsg: "VERSION parameter is required",
		},
		{
			name:   "missing STYLES",
			url:    "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=256&HEIGHT=256&FORMAT=image/png",
			errMsg: "STYLES parameter is required",
		},
		{
			name:   "missing FORMAT",
			url:    "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=256&HEIGHT=256",
			errMsg: "FORMAT parameter is required",
		},
		{
			name:   "missing WIDTH",
			url:    "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=0,0,1,1&HEIGHT=256&FORMAT=image/png",
			errMsg: "WIDTH parameter is required",
		},
		{
			name:   "missing HEIGHT",
			url:    "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=256&FORMAT=image/png",
			errMsg: "HEIGHT parameter is required",
		},
		{
			name:   "invalid HEIGHT",
			url:    "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=256&HEIGHT=abc&FORMAT=image/png",
			errMsg: "HEIGHT must be a positive integer",
		},
		{
			name:   "zero HEIGHT",
			url:    "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=256&HEIGHT=0&FORMAT=image/png",
			errMsg: "HEIGHT must be a positive integer",
		},
		{
			name:   "HEIGHT exceeds max",
			url:    "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=EPSG:4326&BBOX=0,0,1,1&WIDTH=256&HEIGHT=9999&FORMAT=image/png",
			errMsg: "HEIGHT exceeds maximum",
		},
		{
			name:   "invalid CRS",
			url:    "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=test&STYLES=&CRS=INVALID:99&BBOX=0,0,1,1&WIDTH=256&HEIGHT=256&FORMAT=image/png",
			errMsg: "unsupported CRS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			_, err := ParseGetMapRequest(req, 4096, 4096)
			if err == nil {
				t.Errorf("Expected error containing '%s', got nil", tt.errMsg)
				return
			}
			if !containsString(err.Error(), tt.errMsg) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.errMsg, err.Error())
			}
		})
	}
}

func TestParseBBoxErrors(t *testing.T) {
	tests := []struct {
		name   string
		bbox   string
		crs    string
		errMsg string
	}{
		{
			name:   "not enough values",
			bbox:   "0,0,1",
			crs:    "EPSG:4326",
			errMsg: "BBOX must have 4 comma-separated values",
		},
		{
			name:   "too many values",
			bbox:   "0,0,1,1,2",
			crs:    "EPSG:4326",
			errMsg: "BBOX must have 4 comma-separated values",
		},
		{
			name:   "invalid value",
			bbox:   "0,abc,1,1",
			crs:    "EPSG:4326",
			errMsg: "invalid BBOX value",
		},
		{
			name:   "minx greater than maxx",
			bbox:   "100,0,50,100",
			crs:    "EPSG:3857",
			errMsg: "minx must be less than maxx",
		},
		{
			name:   "miny greater than maxy",
			bbox:   "0,100,100,50",
			crs:    "EPSG:3857",
			errMsg: "miny must be less than maxy",
		},
		{
			name:   "zero width (minx equals maxx)",
			bbox:   "50,0,50,100",
			crs:    "EPSG:3857",
			errMsg: "BBOX has zero width",
		},
		{
			name:   "zero height (miny equals maxy)",
			bbox:   "0,50,100,50",
			crs:    "EPSG:3857",
			errMsg: "BBOX has zero height",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseBBox(tt.bbox, tt.crs)
			if err == nil {
				t.Errorf("Expected error containing '%s', got nil", tt.errMsg)
				return
			}
			if !containsString(err.Error(), tt.errMsg) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.errMsg, err.Error())
			}
		})
	}
}

func TestParseCRSAdvanced(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"  EPSG:4326  ", 4326, false}, // with whitespace
		{"urn:ogc:def:crs:EPSG::32632", 32632, false},
		{"urn:ogc:def:crs:EPSG:6.6:4269", 4269, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseCRS(tt.input)
			if tt.hasError {
				if err == nil {
					t.Errorf("parseCRS(%s) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseCRS(%s) unexpected error: %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("parseCRS(%s) = %d, want %d", tt.input, result, tt.expected)
				}
			}
		})
	}
}

func TestParseHexColorAdvanced(t *testing.T) {
	tests := []struct {
		input    string
		expected struct{ r, g, b uint8 }
	}{
		{"FFFFFF", struct{ r, g, b uint8 }{255, 255, 255}},
		{"000000", struct{ r, g, b uint8 }{0, 0, 0}},
		{"abc", struct{ r, g, b uint8 }{255, 255, 255}},     // Invalid length, fallback to white
		{"12345", struct{ r, g, b uint8 }{255, 255, 255}},   // Invalid length
		{"1234567", struct{ r, g, b uint8 }{255, 255, 255}}, // Invalid length
		{"0x123456", struct{ r, g, b uint8 }{18, 52, 86}},
		{"#AABBCC", struct{ r, g, b uint8 }{170, 187, 204}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			c := parseHexColor(tt.input)
			if c.R != tt.expected.r || c.G != tt.expected.g || c.B != tt.expected.b {
				t.Errorf("parseHexColor(%s) = RGB(%d,%d,%d), want RGB(%d,%d,%d)",
					tt.input, c.R, c.G, c.B, tt.expected.r, tt.expected.g, tt.expected.b)
			}
		})
	}
}
