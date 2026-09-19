// Package httputil provides shared HTTP utility functions.
package httputil

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a JSON response with the given status code.
// HTML escaping is disabled for proper URL and HTML entity handling.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// WriteGeoJSON writes a GeoJSON response with the given status code.
// Uses the application/geo+json content type per OGC API spec.
func WriteGeoJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/geo+json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// ReadJSON reads JSON from the request body into v.
func ReadJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
