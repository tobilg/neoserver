package wfs

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
)

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestNowISO8601(t *testing.T) {
	result := nowISO8601()
	// Should be in xsd:dateTime format with +00:00 timezone (for Java compatibility)
	if !strings.Contains(result, "T") || !strings.Contains(result, "+00:00") {
		t.Errorf("nowISO8601() = %q, expected xsd:dateTime format with +00:00", result)
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"Nil value", nil, ""},
		{"String value", "test", "test"},
		{"Int value", 42, "42"},
		{"Float value", 3.14, "3.14"},
		{"Bool value", true, "true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatValue(tt.input)
			if got != tt.expected {
				t.Errorf("formatValue(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"plain text", "plain text"},
		{"<script>", "&lt;script&gt;"},
		{"a & b", "a &amp; b"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{"it's", "it&apos;s"},
		{"<a href=\"x\">text</a>", "&lt;a href=&quot;x&quot;&gt;text&lt;/a&gt;"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeXML(tt.input)
			if got != tt.expected {
				t.Errorf("escapeXML(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeXMLName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"places", "places"},
		{"public:places", "public_places"},
		{"my table", "my_table"},
		{"0starts_with_num", "_0starts_with_num"},
		{"-starts_with_dash", "_-starts_with_dash"},
		{".starts_with_dot", "_.starts_with_dot"},
		{"valid_name", "valid_name"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeXMLName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeXMLName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCoordsToPosList(t *testing.T) {
	tests := []struct {
		name     string
		coords   []interface{}
		expected string
	}{
		{
			"Two points",
			[]interface{}{[]interface{}{0.0, 0.0}, []interface{}{10.0, 10.0}},
			"0 0 10 10",
		},
		{
			"Single point",
			[]interface{}{[]interface{}{5.0, 5.0}},
			"5 5",
		},
		{
			"Empty coords",
			[]interface{}{},
			"",
		},
		{
			"Three points (polygon ring)",
			[]interface{}{[]interface{}{0.0, 0.0}, []interface{}{10.0, 0.0}, []interface{}{10.0, 10.0}},
			"0 0 10 0 10 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coordsToPosList(tt.coords)
			if got != tt.expected {
				t.Errorf("coordsToPosList(%v) = %q, want %q", tt.coords, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// WriteGeoJSONFeatureCollection Tests
// ============================================================================

func TestWriteGeoJSONFeatureCollection(t *testing.T) {
	t.Run("WithFeatures", func(t *testing.T) {
		w := httptest.NewRecorder()
		features := [][]byte{
			[]byte(`{"type":"Feature","id":1,"properties":{"name":"A"},"geometry":null}`),
			[]byte(`{"type":"Feature","id":2,"properties":{"name":"B"},"geometry":null}`),
		}
		WriteGeoJSONFeatureCollection(w, features, 10)

		if w.Code != 200 {
			t.Errorf("Status = %d, want 200", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("Content-Type = %q, expected application/json", ct)
		}

		body := w.Body.String()
		if !strings.Contains(body, `"type":"FeatureCollection"`) {
			t.Error("Response should be a FeatureCollection")
		}
		if !strings.Contains(body, `"numberMatched":10`) {
			t.Error("Response should contain numberMatched:10")
		}
		if !strings.Contains(body, `"numberReturned":2`) {
			t.Error("Response should contain numberReturned:2")
		}

		// Validate it's proper JSON
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Errorf("Response is not valid JSON: %v", err)
		}
	})

	t.Run("EmptyFeatures", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteGeoJSONFeatureCollection(w, [][]byte{}, 0)

		body := w.Body.String()
		if !strings.Contains(body, `"features":[]`) {
			t.Error("Response should have empty features array")
		}
		if !strings.Contains(body, `"numberReturned":0`) {
			t.Error("Response should contain numberReturned:0")
		}
	})
}

// ============================================================================
// WriteHitsResponse Tests
// ============================================================================

func TestWriteHitsResponse(t *testing.T) {
	t.Run("NoNextPage", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteHitsResponse(w, 50, 0, 100, "http://example.com/wfs", "ns:places")

		if w.Code != 200 {
			t.Errorf("Status = %d, want 200", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "wfs:FeatureCollection") {
			t.Error("Response should be a FeatureCollection")
		}
		if !strings.Contains(body, `numberMatched="50"`) {
			t.Error("Response should contain numberMatched=50")
		}
		if !strings.Contains(body, `numberReturned="0"`) {
			t.Error("Response should contain numberReturned=0")
		}
		if !strings.Contains(body, "next=") || !strings.Contains(body, "resultType=results") {
			t.Error("Hits must link to the first results page even when all matches fit")
		}
	})

	t.Run("WithNextPage", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteHitsResponse(w, 100, 0, 10, "http://example.com/wfs", "ns:places")

		body := w.Body.String()
		if !strings.Contains(body, "next=") {
			t.Error("Response should contain next attribute when more results available")
		}
		if !strings.Contains(body, "startIndex=0") || !strings.Contains(body, "resultType=results") {
			t.Error("Hits continuation must request results without consuming a page")
		}
	})
}

// ============================================================================
// WriteGMLFeatureCollection Tests
// ============================================================================

func TestWriteGMLFeatureCollection(t *testing.T) {
	layerInfo := &datasource.LayerInfo{
		Name:           "places",
		GeometryColumn: "geom",
		Properties: []datasource.PropertyInfo{
			{Name: "name", Type: "text"},
			{Name: "value", Type: "integer"},
		},
	}

	t.Run("BasicOutput", func(t *testing.T) {
		w := httptest.NewRecorder()
		features := [][]byte{
			[]byte(`{"id":1,"properties":{"name":"Place A","value":100},"geometry":{"type":"Point","coordinates":[10,20]}}`),
		}
		WriteGMLFeatureCollection(w, layerInfo, features, 1, 0, 10, "http://example.com/ns", "ex", 4326, "http://example.com/wfs", "ex:places")

		if w.Code != 200 {
			t.Errorf("Status = %d, want 200", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/gml+xml") {
			t.Errorf("Content-Type = %q, expected application/gml+xml", ct)
		}

		body := w.Body.String()
		if !strings.Contains(body, "wfs:FeatureCollection") {
			t.Error("Response should be a FeatureCollection")
		}
		if !strings.Contains(body, `numberMatched="1"`) {
			t.Error("Response should contain numberMatched")
		}
		if !strings.Contains(body, "gml:Point") {
			t.Error("Response should contain GML Point geometry")
		}
	})

	t.Run("WithPagination", func(t *testing.T) {
		w := httptest.NewRecorder()
		features := [][]byte{
			[]byte(`{"id":1,"properties":{"name":"Place A","value":100},"geometry":null}`),
		}
		WriteGMLFeatureCollection(w, layerInfo, features, 100, 10, 10, "http://example.com/ns", "ex", 4326, "http://example.com/wfs", "ex:places")

		body := w.Body.String()
		if !strings.Contains(body, "next=") {
			t.Error("Response should contain next link")
		}
		if !strings.Contains(body, "previous=") {
			t.Error("Response should contain previous link")
		}
	})

	t.Run("NoNext", func(t *testing.T) {
		w := httptest.NewRecorder()
		features := [][]byte{
			[]byte(`{"id":1,"properties":{"name":"Place A","value":100},"geometry":null}`),
		}
		WriteGMLFeatureCollection(w, layerInfo, features, 1, 0, 10, "http://example.com/ns", "ex", 4326, "http://example.com/wfs", "ex:places")

		body := w.Body.String()
		if strings.Contains(body, "next=") {
			t.Error("Response should NOT contain next link when all results returned")
		}
	})

	t.Run("NilPropertyValue", func(t *testing.T) {
		w := httptest.NewRecorder()
		features := [][]byte{
			[]byte(`{"id":1,"properties":{"name":"Place A","value":null},"geometry":null}`),
		}
		WriteGMLFeatureCollection(w, layerInfo, features, 1, 0, 10, "http://example.com/ns", "ex", 4326, "http://example.com/wfs", "ex:places")

		body := w.Body.String()
		if !strings.Contains(body, `xsi:nil="true"`) {
			t.Error("Response should contain xsi:nil for null values")
		}
	})

	t.Run("DifferentGeometryTypes", func(t *testing.T) {
		geometries := []struct {
			name     string
			json     string
			expected string
		}{
			{"Point", `{"type":"Point","coordinates":[10,20]}`, "gml:Point"},
			{"LineString", `{"type":"LineString","coordinates":[[0,0],[10,10]]}`, "gml:LineString"},
			{"Polygon", `{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,10],[0,0]]]}`, "gml:Polygon"},
			{"MultiPoint", `{"type":"MultiPoint","coordinates":[[0,0],[10,10]]}`, "gml:MultiPoint"},
			{"MultiLineString", `{"type":"MultiLineString","coordinates":[[[0,0],[10,10]],[[20,20],[30,30]]]}`, "gml:MultiCurve"},
			{"MultiPolygon", `{"type":"MultiPolygon","coordinates":[[[[0,0],[10,0],[10,10],[0,10],[0,0]]]]}`, "gml:MultiSurface"},
		}

		for _, geom := range geometries {
			t.Run(geom.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				features := [][]byte{
					[]byte(`{"id":1,"properties":{"name":"Test","value":1},"geometry":` + geom.json + `}`),
				}
				WriteGMLFeatureCollection(w, layerInfo, features, 1, 0, 10, "http://example.com/ns", "ex", 4326, "http://example.com/wfs", "ex:places")

				body := w.Body.String()
				if !strings.Contains(body, geom.expected) {
					t.Errorf("Response should contain %s for %s geometry", geom.expected, geom.name)
				}
			})
		}
	})

	t.Run("PolygonWithInterior", func(t *testing.T) {
		w := httptest.NewRecorder()
		// Polygon with exterior and interior ring (hole)
		features := [][]byte{
			[]byte(`{"id":1,"properties":{"name":"Test","value":1},"geometry":{"type":"Polygon","coordinates":[[[0,0],[100,0],[100,100],[0,100],[0,0]],[[25,25],[75,25],[75,75],[25,75],[25,25]]]}}`),
		}
		WriteGMLFeatureCollection(w, layerInfo, features, 1, 0, 10, "http://example.com/ns", "ex", 4326, "http://example.com/wfs", "ex:places")

		body := w.Body.String()
		if !strings.Contains(body, "gml:exterior") {
			t.Error("Response should contain gml:exterior")
		}
		if !strings.Contains(body, "gml:interior") {
			t.Error("Response should contain gml:interior for polygon with hole")
		}
	})

	t.Run("QNameInTypeName", func(t *testing.T) {
		w := httptest.NewRecorder()
		features := [][]byte{
			[]byte(`{"id":1,"properties":{"name":"Place A","value":100},"geometry":null}`),
		}
		// When typeName already has a prefix (like ns:places), it should use that
		WriteGMLFeatureCollection(w, layerInfo, features, 1, 0, 10, "http://example.com/ns", "ex", 4326, "http://example.com/wfs", "ns:places")

		body := w.Body.String()
		// The body should contain the namespace declaration
		if !strings.Contains(body, "xmlns:") {
			t.Error("Response should contain namespace declaration")
		}
	})
}

func TestWriteGMLFeatureCollectionEscapesAttributesAndURLs(t *testing.T) {
	layerInfo := &datasource.LayerInfo{Name: "places", Properties: []datasource.PropertyInfo{{Name: "label", Type: "text"}}}
	feature := []byte(`{"id":"a\"&b","properties":{"label":"ok"},"geometry":null}`)
	recorder := httptest.NewRecorder()
	WriteGMLFeatureCollection(recorder, layerInfo, [][]byte{feature}, 2, 0, 1, "https://example.test/ns?x=\"", "app", 4326, "https://example.test/wfs?tenant=a&unsafe=\"", "app:places & things")
	if recorder.Code != 200 {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	decoder := xml.NewDecoder(strings.NewReader(recorder.Body.String()))
	for {
		if _, err := decoder.Token(); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("invalid XML: %v\n%s", err, recorder.Body.String())
		}
	}
	if !strings.Contains(recorder.Body.String(), "typeNames=app%3Aplaces+%26+things") {
		t.Fatalf("pagination URL was not encoded: %s", recorder.Body.String())
	}
}

// ============================================================================
// Geometry Writing Tests (via writeGMLGeometry)
// ============================================================================

func TestWriteGMLGeometryInternal(t *testing.T) {
	// These tests verify the internal geometry writing functions indirectly
	// through the feature collection writer

	layerInfo := &datasource.LayerInfo{
		Name:           "test",
		GeometryColumn: "geom",
		Properties:     []datasource.PropertyInfo{},
	}

	t.Run("PointWithSRS", func(t *testing.T) {
		w := httptest.NewRecorder()
		features := [][]byte{
			[]byte(`{"id":1,"properties":{},"geometry":{"type":"Point","coordinates":[10,20]}}`),
		}
		WriteGMLFeatureCollection(w, layerInfo, features, 1, 0, 10, "http://example.com/ns", "ex", 4326, "http://example.com/wfs", "ex:test")

		body := w.Body.String()
		if !strings.Contains(body, `srsName="urn:ogc:def:crs:EPSG::4326"`) {
			t.Error("Point should have srsName attribute")
		}
		if !strings.Contains(body, "<gml:pos>20 10</gml:pos>") {
			t.Error("EPSG:4326 URN requires latitude/longitude coordinates")
		}
	})

	t.Run("InvalidFeatureJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		features := [][]byte{
			[]byte(`{invalid json`),
		}
		// Should not panic, just skip invalid feature
		WriteGMLFeatureCollection(w, layerInfo, features, 1, 0, 10, "http://example.com/ns", "ex", 4326, "http://example.com/wfs", "ex:test")
		// Just verify it completes without error
		if w.Code != 200 {
			t.Errorf("Status = %d, want 200", w.Code)
		}
	})
}
