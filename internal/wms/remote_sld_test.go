package wms

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func getMapRequestWithSLD(sldURL, sldBody, exceptions string) *http.Request {
	values := url.Values{
		"SERVICE": {"WMS"}, "VERSION": {"1.3.0"}, "REQUEST": {"GetMap"}, "LAYERS": {"elevation"},
		"STYLES": {""}, "CRS": {"EPSG:4326"}, "BBOX": {"-90,-180,90,180"}, "WIDTH": {"32"},
		"HEIGHT": {"16"}, "FORMAT": {"image/png"}, "SLD": {sldURL},
	}
	if sldBody != "" {
		values.Set("SLD_BODY", sldBody)
	}
	if exceptions != "" {
		values.Set("EXCEPTIONS", exceptions)
	}
	return httptest.NewRequest(http.MethodGet, "/wms?"+values.Encode(), nil)
}

func TestGetMapRejectsRemoteSLDBeforeRendering(t *testing.T) {
	h, ws := rasterWMSFixture()
	tests := []struct {
		name       string
		exceptions string
		status     int
		content    string
	}{
		{name: "xml", exceptions: ExceptionsXML, status: http.StatusBadRequest, content: "text/xml"},
		{name: "in image", exceptions: ExceptionsINIMAGE, status: http.StatusOK, content: FormatPNG},
		{name: "blank", exceptions: ExceptionsBLANK, status: http.StatusOK, content: FormatPNG},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			h.handleGetMap(recorder, getMapRequestWithSLD("https://styles.example/style.sld", "", test.exceptions), ws)
			if recorder.Code != test.status || recorder.Header().Get("Content-Type") != test.content {
				t.Fatalf("response = %d %s: %s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
			}
			if test.exceptions == ExceptionsXML {
				body := recorder.Body.String()
				if !strings.Contains(body, `code="InvalidParameterValue"`) || !strings.Contains(body, `locator="SLD"`) || !strings.Contains(body, remoteSLDUnsupportedMessage) {
					t.Fatalf("unexpected exception: %s", body)
				}
			}
		})
	}
}

func TestGetMapRejectsRemoteSLDEvenWithInlineBody(t *testing.T) {
	h, ws := rasterWMSFixture()
	recorder := httptest.NewRecorder()
	h.handleGetMap(recorder, getMapRequestWithSLD("https://styles.example/style.sld", "<invalid-inline/>", ExceptionsXML), ws)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), remoteSLDUnsupportedMessage) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestWhitespaceRemoteSLDIsTreatedAsAbsent(t *testing.T) {
	h, ws := rasterWMSFixture()
	recorder := httptest.NewRecorder()
	h.handleGetMap(recorder, getMapRequestWithSLD(" \t ", "", ExceptionsXML), ws)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != FormatPNG {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestGetLegendGraphicRejectsRemoteSLD(t *testing.T) {
	h, ws := rasterWMSFixture()
	values := url.Values{
		"SERVICE": {"WMS"}, "VERSION": {"1.3.0"}, "REQUEST": {"GetLegendGraphic"}, "LAYER": {"elevation"},
		"STYLE": {""}, "FORMAT": {"image/png"}, "WIDTH": {"32"}, "HEIGHT": {"8"},
		"SLD": {"https://styles.example/style.sld"}, "SLD_BODY": {"<invalid-inline/>"},
	}
	recorder := httptest.NewRecorder()
	h.handleGetLegendGraphic(recorder, httptest.NewRequest(http.MethodGet, "/wms?"+values.Encode(), nil), ws)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `locator="SLD"`) || !strings.Contains(body, remoteSLDUnsupportedMessage) {
		t.Fatalf("unexpected exception: %s", body)
	}
}

func TestRemoteSLDDoesNotAffectAcceptedCacheIdentity(t *testing.T) {
	_, ws := rasterWMSFixture()
	first := &GetMapRequest{SLD: "https://one.example/style.sld", SLDBody: "inline"}
	second := &GetMapRequest{SLD: "https://two.example/style.sld", SLDBody: "inline"}
	if getMapCacheKey(ws, first) != getMapCacheKey(ws, second) {
		t.Fatal("unsupported remote SLD leaked into the accepted request cache key")
	}
}
