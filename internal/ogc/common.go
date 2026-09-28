package ogc

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/tobilg/neoserver/internal/httputil"
)

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	httputil.WriteJSON(w, status, v)
}

// writeGeoJSON writes a GeoJSON response.
func writeGeoJSON(w http.ResponseWriter, status int, v any) {
	httputil.WriteGeoJSON(w, status, v)
}

// writeErr writes an error response.
func writeErr(w http.ResponseWriter, status int, title, detail string) {
	httputil.WriteJSON(w, status, Error{
		Code:   status,
		Title:  title,
		Detail: detail,
	})
}

// urlPathEscape escapes a string for use in URL paths.
func urlPathEscape(s string) string {
	return url.PathEscape(s)
}

// swaggerUIHTML returns the workspace Swagger UI page. Its assets and the
// initialiser (served beside the page as api.js) are same-origin, because the
// server's Content-Security-Policy blocks CDN and inline scripts.
func swaggerUIHTML(basePath string) string {
	return httputil.SwaggerUIPage("API Docs", strings.TrimSuffix(basePath, "/"), "./api.js")
}
