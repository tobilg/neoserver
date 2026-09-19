package testserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// MockServer provides a mock OGC API Features server that returns static responses.
// This is useful for testing the conformance test framework itself without a database.
type MockServer struct {
	Server *httptest.Server
}

// NewMock creates a new mock server with static responses.
func NewMock() *MockServer {
	r := chi.NewRouter()

	// Landing page
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"title":       "Mock OGC API Features Server",
			"description": "A mock server for testing",
			"links": []map[string]string{
				{"href": "/", "rel": "self", "type": "application/json", "title": "Landing page"},
				{"href": "/conformance", "rel": "conformance", "type": "application/json"},
				{"href": "/collections", "rel": "data", "type": "application/json"},
				{"href": "/api", "rel": "service-desc", "type": "application/vnd.oai.openapi+json;version=3.0"},
			},
		}
		writeJSON(w, resp)
	})

	// Conformance
	r.Get("/conformance", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"conformsTo": []string{
				"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core",
				"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/oas30",
				"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson",
				"http://www.opengis.net/spec/ogcapi-features-2/1.0/conf/crs",
				"http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/filter",
				"http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/queryables",
				"http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/features-filter",
				"http://www.opengis.net/spec/cql2/1.0/conf/basic-cql2",
				"http://www.opengis.net/spec/cql2/1.0/conf/basic-spatial-functions",
				"http://www.opengis.net/spec/cql2/1.0/conf/cql2-text",
			},
		}
		writeJSON(w, resp)
	})

	r.Get("/collections/{collectionId}/queryables", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "collectionId")
		if len(mockFeatures(id)) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/schema+json")
		json.NewEncoder(w).Encode(map[string]any{
			"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "/collections/" + id + "/queryables",
			"type": "object", "title": id, "additionalProperties": false,
			"properties": map[string]any{
				"id": map[string]any{"type": "integer"}, "name": map[string]any{"type": "string"},
				"geom": map[string]any{"$ref": "https://geojson.org/schema/Geometry.json", "format": "geometry-Geometry"},
			},
		})
	})

	// Collections list
	r.Get("/collections", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"links": []map[string]string{
				{"href": "/collections", "rel": "self", "type": "application/json"},
			},
			"collections": []map[string]any{
				mockCollection("points", "Test Points", "POINT"),
				mockCollection("lines", "Test Lines", "LINESTRING"),
				mockCollection("polygons", "Test Polygons", "POLYGON"),
			},
		}
		writeJSON(w, resp)
	})

	// Single collection
	r.Get("/collections/{collectionId}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "collectionId")
		var col map[string]any
		switch id {
		case "points":
			col = mockCollection("points", "Test Points", "POINT")
		case "lines":
			col = mockCollection("lines", "Test Lines", "LINESTRING")
		case "polygons":
			col = mockCollection("polygons", "Test Polygons", "POLYGON")
		default:
			http.NotFound(w, r)
			return
		}
		writeJSON(w, col)
	})

	// Features (items)
	r.Get("/collections/{collectionId}/items", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "collectionId")
		allowed := map[string]bool{"limit": true, "offset": true, "bbox": true, "bbox-crs": true, "crs": true, "datetime": true, "filter": true, "filter-crs": true, "filter-lang": true, "properties": true, "sortby": true}
		for name := range r.URL.Query() {
			if !allowed[name] {
				writeError(w, 400, "InvalidParameterValue", "unsupported query parameter "+name)
				return
			}
		}

		// Handle limit parameter
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			var l int
			if _, err := fmt.Sscanf(v, "%d", &l); err != nil || l <= 0 {
				writeError(w, 400, "InvalidParameterValue", "limit must be a positive integer")
				return
			}
			limit = l
		}
		offset := 0
		if v := r.URL.Query().Get("offset"); v != "" {
			parsed, err := strconv.Atoi(v)
			if err != nil || parsed < 0 {
				writeError(w, 400, "InvalidParameterValue", "offset must be a non-negative integer")
				return
			}
			offset = parsed
		}
		if value := r.URL.Query().Get("datetime"); value != "" && !validDateTime(value) {
			writeError(w, 400, "InvalidParameterValue", "datetime must be RFC 3339")
			return
		}
		if lang := r.URL.Query().Get("filter-lang"); lang != "" && !strings.EqualFold(lang, "cql2-text") {
			writeError(w, 400, "InvalidParameterValue", "filter-lang must be cql2-text")
			return
		}

		// Handle CRS parameter - validate it
		if crs := r.URL.Query().Get("crs"); crs != "" {
			if !isValidCRS(crs) {
				writeError(w, 400, "InvalidParameterValue", "unsupported CRS: "+crs)
				return
			}
		}

		// Handle bbox-crs parameter - validate it
		if bboxCRS := r.URL.Query().Get("bbox-crs"); bboxCRS != "" {
			if !isValidCRS(bboxCRS) {
				writeError(w, 400, "InvalidParameterValue", "unsupported bbox-crs: "+bboxCRS)
				return
			}
		}

		features := mockFeatures(id)
		if offset >= len(features) {
			features = []any{}
		} else {
			features = features[offset:]
		}
		hasNext := len(features) > limit
		if hasNext {
			features = features[:limit]
		}
		links := []map[string]string{{"href": r.URL.RequestURI(), "rel": "self", "type": "application/geo+json"}, {"href": "/collections/" + id + "/queryables", "rel": "http://www.opengis.net/def/rel/ogc/1.0/queryables", "type": "application/schema+json"}}
		if hasNext {
			values := cloneQuery(r.URL.Query())
			values.Set("limit", strconv.Itoa(limit))
			values.Set("offset", strconv.Itoa(offset+limit))
			links = append(links, map[string]string{"href": "/collections/" + id + "/items?" + values.Encode(), "rel": "next", "type": "application/geo+json"})
		}

		resp := map[string]any{
			"type":           "FeatureCollection",
			"features":       features,
			"numberReturned": len(features),
			"links":          links,
		}
		w.Header().Set("Content-Type", "application/geo+json")
		w.Header().Set("Content-Crs", "<http://www.opengis.net/def/crs/OGC/1.3/CRS84>")
		json.NewEncoder(w).Encode(resp)
	})

	// Single feature
	r.Get("/collections/{collectionId}/items/{featureId}", func(w http.ResponseWriter, r *http.Request) {
		colID := chi.URLParam(r, "collectionId")
		featureID := chi.URLParam(r, "featureId")

		features := mockFeatures(colID)
		for _, f := range features {
			if fm, ok := f.(map[string]any); ok {
				if id := fm["id"]; id != nil && strings.EqualFold(featureID, idToString(id)) {
					w.Header().Set("Content-Type", "application/geo+json")
					w.Header().Set("Content-Crs", "<http://www.opengis.net/def/crs/OGC/1.3/CRS84>")
					json.NewEncoder(w).Encode(fm)
					return
				}
			}
		}
		http.NotFound(w, r)
	})

	// API spec
	r.Get("/api", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"openapi": "3.0.3",
			"info": map[string]string{
				"title":   "Mock OGC API",
				"version": "1.0.0",
			},
			"paths": map[string]any{
				"/":                                      map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "Landing"}}}},
				"/conformance":                           map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "Conformance"}}}},
				"/collections":                           map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "Collections"}}}},
				"/collections/{collectionId}":            map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "Collection"}}}},
				"/collections/{collectionId}/queryables": map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "Queryables"}}}},
				"/collections/{collectionId}/items":      map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "Features"}}}},
				"/collections/{collectionId}/items/{featureId}": map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "Feature"}}}},
			},
		}
		w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.0")
		json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(r)
	return &MockServer{Server: server}
}

// URL returns the base URL of the mock server.
func (m *MockServer) URL() string {
	return m.Server.URL
}

// Close shuts down the mock server.
func (m *MockServer) Close() {
	m.Server.Close()
}

func mockCollection(id, title, geomType string) map[string]any {
	return map[string]any{
		"id":          id,
		"title":       title,
		"description": "A test collection of " + strings.ToLower(geomType) + "s",
		"extent": map[string]any{
			"spatial": map[string]any{
				"bbox": [][]float64{{-180, -90, 180, 90}},
				"crs":  "http://www.opengis.net/def/crs/OGC/1.3/CRS84",
			},
		},
		"crs": []string{
			"http://www.opengis.net/def/crs/OGC/1.3/CRS84",
			"http://www.opengis.net/def/crs/EPSG/0/4326",
		},
		"links": []map[string]string{
			{"href": "/collections/" + id, "rel": "self", "type": "application/json"},
			{"href": "/collections/" + id + "/items", "rel": "items", "type": "application/geo+json"},
			{"href": "/collections/" + id + "/queryables", "rel": "http://www.opengis.net/def/rel/ogc/1.0/queryables", "type": "application/schema+json"},
		},
	}
}

func validDateTime(value string) bool {
	parse := func(value string) bool {
		if _, err := time.Parse(time.RFC3339, value); err == nil {
			return true
		}
		_, err := time.Parse("2006-01-02", value)
		return err == nil
	}
	duration := func(value string) bool {
		return strings.HasPrefix(strings.ToUpper(value), "P") && len(value) > 1
	}
	if !strings.Contains(value, "/") {
		return parse(value)
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 || (parts[0] == ".." && parts[1] == "..") {
		return false
	}
	return (parts[0] == ".." || parse(parts[0]) || duration(parts[0])) &&
		(parts[1] == ".." || parse(parts[1]) || duration(parts[1])) &&
		!(duration(parts[0]) && duration(parts[1]))
}

func cloneQuery(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, entries := range values {
		clone[key] = append([]string(nil), entries...)
	}
	return clone
}

func mockFeatures(collectionID string) []any {
	switch collectionID {
	case "points":
		return []any{
			mockFeature(1, "Point A", []float64{-73.99, 40.73}),
			mockFeature(2, "Point B", []float64{-122.42, 37.77}),
			mockFeature(3, "Point C", []float64{2.35, 48.85}),
		}
	case "lines":
		return []any{
			mockLineFeature(1, "Line A", [][]float64{{0, 0}, {1, 1}, {2, 0}}),
			mockLineFeature(2, "Line B", [][]float64{{-5, -5}, {5, 5}}),
		}
	case "polygons":
		return []any{
			mockPolygonFeature(1, "Polygon A", [][][]float64{{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {0, 0}}}),
			mockPolygonFeature(2, "Polygon B", [][][]float64{{{-2, -2}, {2, -2}, {2, 2}, {-2, 2}, {-2, -2}}}),
		}
	default:
		return []any{}
	}
}

func mockFeature(id int, name string, coords []float64) map[string]any {
	return map[string]any{
		"type": "Feature",
		"id":   id,
		"geometry": map[string]any{
			"type":        "Point",
			"coordinates": coords,
		},
		"properties": map[string]any{
			"name": name,
		},
		"links": []map[string]string{
			{"href": "/collections/points/items/" + idToString(id), "rel": "self", "type": "application/geo+json"},
		},
	}
}

func mockLineFeature(id int, name string, coords [][]float64) map[string]any {
	return map[string]any{
		"type": "Feature",
		"id":   id,
		"geometry": map[string]any{
			"type":        "LineString",
			"coordinates": coords,
		},
		"properties": map[string]any{
			"name": name,
		},
		"links": []map[string]string{
			{"href": "/collections/lines/items/" + idToString(id), "rel": "self", "type": "application/geo+json"},
		},
	}
}

func mockPolygonFeature(id int, name string, coords [][][]float64) map[string]any {
	return map[string]any{
		"type": "Feature",
		"id":   id,
		"geometry": map[string]any{
			"type":        "Polygon",
			"coordinates": coords,
		},
		"properties": map[string]any{
			"name": name,
		},
		"links": []map[string]string{
			{"href": "/collections/polygons/items/" + idToString(id), "rel": "self", "type": "application/geo+json"},
		},
	}
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func idToString(id any) string {
	switch v := id.(type) {
	case int:
		return fmt.Sprintf("%d", v)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", id)
	}
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"code":   code,
		"detail": detail,
	})
}

func isValidCRS(crs string) bool {
	validCRS := []string{
		"http://www.opengis.net/def/crs/OGC/1.3/CRS84",
		"http://www.opengis.net/def/crs/OGC/0/CRS84h",
		"http://www.opengis.net/def/crs/EPSG/0/4326",
	}
	for _, valid := range validCRS {
		if crs == valid {
			return true
		}
	}
	return false
}
