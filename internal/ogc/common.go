package ogc

import (
	"net/http"
	"net/url"

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

// swaggerUIHTML returns the HTML for Swagger UI.
func swaggerUIHTML() string {
	return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>API Docs</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
      window.ui = SwaggerUIBundle({
        url: './api',
        dom_id: '#swagger-ui',
      });
    </script>
  </body>
</html>`
}
