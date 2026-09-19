package ogc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================================
// writeJSON Tests
// ============================================================================

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"message": "hello"}

	writeJSON(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["message"] != "hello" {
		t.Errorf("expected message=hello, got %s", result["message"])
	}
}

func TestWriteJSON_EscapeHTMLDisabled(t *testing.T) {
	w := httptest.NewRecorder()
	// Test that HTML is not escaped
	data := map[string]string{"url": "http://example.com/test?a=1&b=2"}

	writeJSON(w, http.StatusOK, data)

	// Check that & is not escaped to \u0026
	body := w.Body.String()
	if body == "" {
		t.Fatal("expected non-empty body")
	}
	// The body should contain & not \u0026
	if !contains(body, "&") || contains(body, "\\u0026") {
		t.Errorf("expected HTML escaping to be disabled, got %s", body)
	}
}

func TestWriteJSON_CustomStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"OK", http.StatusOK},
		{"Created", http.StatusCreated},
		{"BadRequest", http.StatusBadRequest},
		{"NotFound", http.StatusNotFound},
		{"InternalServerError", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSON(w, tt.status, map[string]string{})

			if w.Code != tt.status {
				t.Errorf("expected status %d, got %d", tt.status, w.Code)
			}
		})
	}
}

// ============================================================================
// writeGeoJSON Tests
// ============================================================================

func TestWriteGeoJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := FeatureCollection{
		Type:     "FeatureCollection",
		Features: []jsonRaw{},
	}

	writeGeoJSON(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/geo+json" {
		t.Errorf("expected Content-Type application/geo+json, got %s", ct)
	}

	var result FeatureCollection
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Type != "FeatureCollection" {
		t.Errorf("expected type=FeatureCollection, got %s", result.Type)
	}
}

func TestWriteGeoJSON_WithFeatures(t *testing.T) {
	w := httptest.NewRecorder()
	feature := json.RawMessage(`{"type":"Feature","geometry":{"type":"Point","coordinates":[0,0]},"properties":{}}`)
	data := FeatureCollection{
		Type:     "FeatureCollection",
		Features: []jsonRaw{feature},
		Links: []Link{
			{Href: "/collections/test", Rel: "self"},
		},
	}

	writeGeoJSON(w, http.StatusOK, data)

	var result FeatureCollection
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result.Features) != 1 {
		t.Errorf("expected 1 feature, got %d", len(result.Features))
	}
	if len(result.Links) != 1 {
		t.Errorf("expected 1 link, got %d", len(result.Links))
	}
}

// ============================================================================
// writeErr Tests
// ============================================================================

func TestWriteErr(t *testing.T) {
	w := httptest.NewRecorder()

	writeErr(w, http.StatusBadRequest, "Bad Request", "invalid input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var result Error
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Code != http.StatusBadRequest {
		t.Errorf("expected code=400, got %d", result.Code)
	}
	if result.Title != "Bad Request" {
		t.Errorf("expected title='Bad Request', got %s", result.Title)
	}
	if result.Detail != "invalid input" {
		t.Errorf("expected detail='invalid input', got %s", result.Detail)
	}
}

func TestWriteErr_DifferentStatuses(t *testing.T) {
	tests := []struct {
		status int
		title  string
		detail string
	}{
		{http.StatusNotFound, "Not Found", "resource does not exist"},
		{http.StatusInternalServerError, "Internal Server Error", "unexpected error"},
		{http.StatusUnauthorized, "Unauthorized", "authentication required"},
		{http.StatusForbidden, "Forbidden", "access denied"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeErr(w, tt.status, tt.title, tt.detail)

			if w.Code != tt.status {
				t.Errorf("expected status %d, got %d", tt.status, w.Code)
			}

			var result Error
			if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if result.Code != tt.status {
				t.Errorf("expected code=%d, got %d", tt.status, result.Code)
			}
			if result.Title != tt.title {
				t.Errorf("expected title=%q, got %q", tt.title, result.Title)
			}
		})
	}
}

func TestWriteErr_EmptyDetail(t *testing.T) {
	w := httptest.NewRecorder()

	writeErr(w, http.StatusNotFound, "Not Found", "")

	var result Error
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Detail != "" {
		t.Errorf("expected empty detail, got %q", result.Detail)
	}
}

// ============================================================================
// urlPathEscape Tests
// ============================================================================

func TestUrlPathEscape(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple string", "hello", "hello"},
		{"string with space", "hello world", "hello%20world"},
		{"string with slash", "hello/world", "hello%2Fworld"},
		{"string with special chars", "hello?world&foo=bar", "hello%3Fworld&foo=bar"}, // & is valid in path
		{"empty string", "", ""},
		{"unicode", "日本語", "%E6%97%A5%E6%9C%AC%E8%AA%9E"},
		{"already encoded", "hello%20world", "hello%2520world"},
		{"mixed", "layer:name/id", "layer:name%2Fid"}, // : is valid in path
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := urlPathEscape(tt.input)
			if got != tt.want {
				t.Errorf("urlPathEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ============================================================================
// swaggerUIHTML Tests
// ============================================================================

func TestSwaggerUIHTML(t *testing.T) {
	html := swaggerUIHTML()

	// Check that it returns non-empty HTML
	if html == "" {
		t.Error("expected non-empty HTML")
	}

	// Check for essential Swagger UI elements
	requiredElements := []string{
		"<!doctype html>",
		"<html>",
		"</html>",
		"swagger-ui",
		"SwaggerUIBundle",
		"./api",
	}

	for _, elem := range requiredElements {
		if !contains(html, elem) {
			t.Errorf("expected HTML to contain %q", elem)
		}
	}
}

func TestSwaggerUIHTML_IsDeterministic(t *testing.T) {
	// Calling multiple times should return the same result
	html1 := swaggerUIHTML()
	html2 := swaggerUIHTML()

	if html1 != html2 {
		t.Error("expected swaggerUIHTML to return the same result on multiple calls")
	}
}

// ============================================================================
// Model Tests
// ============================================================================

func TestLink_JSONSerialization(t *testing.T) {
	link := Link{
		Href:  "http://example.com/test",
		Rel:   "self",
		Type:  "application/json",
		Title: "Test Link",
	}

	data, err := json.Marshal(link)
	if err != nil {
		t.Fatalf("failed to marshal Link: %v", err)
	}

	var decoded Link
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Link: %v", err)
	}

	if decoded.Href != link.Href {
		t.Errorf("expected Href=%q, got %q", link.Href, decoded.Href)
	}
	if decoded.Rel != link.Rel {
		t.Errorf("expected Rel=%q, got %q", link.Rel, decoded.Rel)
	}
	if decoded.Type != link.Type {
		t.Errorf("expected Type=%q, got %q", link.Type, decoded.Type)
	}
	if decoded.Title != link.Title {
		t.Errorf("expected Title=%q, got %q", link.Title, decoded.Title)
	}
}

func TestLink_OmitEmpty(t *testing.T) {
	link := Link{
		Href: "http://example.com/test",
		Rel:  "self",
		// Type and Title are omitted
	}

	data, err := json.Marshal(link)
	if err != nil {
		t.Fatalf("failed to marshal Link: %v", err)
	}

	s := string(data)
	if contains(s, "type") {
		t.Error("expected type to be omitted when empty")
	}
	if contains(s, "title") {
		t.Error("expected title to be omitted when empty")
	}
}

func TestLandingPage_JSONSerialization(t *testing.T) {
	lp := LandingPage{
		Title:       "Test API",
		Description: "A test API",
		Links: []Link{
			{Href: "/", Rel: "self"},
		},
	}

	data, err := json.Marshal(lp)
	if err != nil {
		t.Fatalf("failed to marshal LandingPage: %v", err)
	}

	var decoded LandingPage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal LandingPage: %v", err)
	}

	if decoded.Title != lp.Title {
		t.Errorf("expected Title=%q, got %q", lp.Title, decoded.Title)
	}
	if len(decoded.Links) != 1 {
		t.Errorf("expected 1 link, got %d", len(decoded.Links))
	}
}

func TestConformance_JSONSerialization(t *testing.T) {
	conf := Conformance{
		ConformsTo: []string{
			"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core",
			"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson",
		},
	}

	data, err := json.Marshal(conf)
	if err != nil {
		t.Fatalf("failed to marshal Conformance: %v", err)
	}

	var decoded Conformance
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Conformance: %v", err)
	}

	if len(decoded.ConformsTo) != 2 {
		t.Errorf("expected 2 conformance URIs, got %d", len(decoded.ConformsTo))
	}
}

func TestCollections_JSONSerialization(t *testing.T) {
	cols := Collections{
		Collections: []Collection{
			{
				ID:          "test",
				Title:       "Test Collection",
				Description: "A test collection",
				CRS:         []string{"http://www.opengis.net/def/crs/EPSG/0/4326"},
				StorageCRS:  "http://www.opengis.net/def/crs/EPSG/0/4326",
			},
		},
		Links: []Link{
			{Href: "/collections", Rel: "self"},
		},
	}

	data, err := json.Marshal(cols)
	if err != nil {
		t.Fatalf("failed to marshal Collections: %v", err)
	}

	var decoded Collections
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Collections: %v", err)
	}

	if len(decoded.Collections) != 1 {
		t.Errorf("expected 1 collection, got %d", len(decoded.Collections))
	}
	if decoded.Collections[0].ID != "test" {
		t.Errorf("expected collection ID=test, got %q", decoded.Collections[0].ID)
	}
}

func TestError_JSONSerialization(t *testing.T) {
	e := Error{
		Code:   400,
		Title:  "Bad Request",
		Detail: "invalid input",
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("failed to marshal Error: %v", err)
	}

	var decoded Error
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Error: %v", err)
	}

	if decoded.Code != e.Code {
		t.Errorf("expected Code=%d, got %d", e.Code, decoded.Code)
	}
	if decoded.Title != e.Title {
		t.Errorf("expected Title=%q, got %q", e.Title, decoded.Title)
	}
	if decoded.Detail != e.Detail {
		t.Errorf("expected Detail=%q, got %q", e.Detail, decoded.Detail)
	}
}

func TestFeatureCollection_JSONSerialization(t *testing.T) {
	fc := FeatureCollection{
		Type: "FeatureCollection",
		Features: []jsonRaw{
			json.RawMessage(`{"type":"Feature","id":1}`),
		},
		Links: []Link{
			{Href: "/items", Rel: "self"},
		},
	}

	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("failed to marshal FeatureCollection: %v", err)
	}

	var decoded FeatureCollection
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal FeatureCollection: %v", err)
	}

	if decoded.Type != "FeatureCollection" {
		t.Errorf("expected Type=FeatureCollection, got %q", decoded.Type)
	}
	if len(decoded.Features) != 1 {
		t.Errorf("expected 1 feature, got %d", len(decoded.Features))
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
