package mgmt

import (
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/tobilg/neoserver/internal/conf"
)

// BuildOpenAPI creates the deterministic OpenAPI 3.0 specification for the
// Management API. It is exported for client generation and drift checks.
func BuildOpenAPI(cfg conf.Config) *openapi3.T {
	schemas := getSchemas()
	params := getPathParams()
	paths := getPaths(params)

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:       "neoserver Management API",
			Description: "REST API for managing workspaces, services, layers, API keys, roles, and OIDC claim mappings.",
			Version:     conf.App.Version,
		},
		Servers: openapi3.Servers{{URL: cfg.Server.UrlBase + cfg.Server.BasePath + "/api/v1"}},
		Paths:   paths,
		Components: &openapi3.Components{
			Schemas: schemas,
			SecuritySchemes: openapi3.SecuritySchemes{
				"bearerAuth": &openapi3.SecuritySchemeRef{
					Value: &openapi3.SecurityScheme{
						Type:         "http",
						Scheme:       "bearer",
						BearerFormat: "JWT",
						Description:  "JWT access token",
					},
				},
				"apiKey": &openapi3.SecuritySchemeRef{
					Value: &openapi3.SecurityScheme{
						Type:        "apiKey",
						In:          "header",
						Name:        "X-API-Key",
						Description: "API key for authentication",
					},
				},
			},
		},
		Security: openapi3.SecurityRequirements{
			{"bearerAuth": {}},
			{"apiKey": {}},
		},
	}

	return doc
}

func buildOpenAPI(cfg conf.Config) *openapi3.T { return BuildOpenAPI(cfg) }

// swaggerUIHTML returns the HTML for the Swagger UI page.
//
// Every asset is same-origin. The server sends a restrictive
// Content-Security-Policy (`default-src 'self'; script-src 'self'`), so a
// CDN-hosted bundle or an inline initialiser is blocked and the page renders
// blank. Swagger UI's runtime is copied into the embedded console build by
// web/admin/scripts/copy-swagger-assets.mjs, and the initialiser is served
// separately from apiInitJS below.
func swaggerUIHTML(basePath string) string {
	vendor := basePath + "/admin/vendor/swagger"
	return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Management API Docs</title>
    <link rel="stylesheet" href="` + vendor + `/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="` + vendor + `/swagger-ui-bundle.js"></script>
    <script src="` + basePath + `/api/v1/api.js"></script>
  </body>
</html>`
}

// apiInitJS boots Swagger UI, or explains itself when its runtime is missing.
//
// The runtime ships with the embedded console, so it is absent from a binary
// built without Node and unreachable when the console is disabled. Both cases
// would otherwise leave an empty page with only a console error.
const apiInitJS = `(function () {
  var mount = document.getElementById("swagger-ui");
  if (typeof SwaggerUIBundle === "undefined") {
    mount.innerHTML =
      '<div style="font:14px system-ui;max-width:44rem;margin:3rem auto;padding:0 1rem">' +
      "<h1>API documentation is unavailable</h1>" +
      "<p>The Swagger UI runtime ships with the administration console. It is " +
      "missing when the server was built without the console, or when the " +
      "console is disabled with <code>Server.AdminUI=false</code>.</p>" +
      '<p>Build it with <code>make ui-build</code>, or use the OpenAPI document ' +
      'directly at <a href="./api">./api</a>.</p></div>';
    return;
  }
  window.ui = SwaggerUIBundle({ url: "./api", dom_id: "#swagger-ui" });
})();
`

// api returns the OpenAPI JSON document.
func (h *handler) api(w http.ResponseWriter, r *http.Request, cfg conf.Config) {
	doc := buildOpenAPI(cfg)

	// Update server URL based on request
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	serverURL := scheme + "://" + r.Host + cfg.Server.BasePath + "/api/v1"
	doc.Servers = openapi3.Servers{{URL: serverURL}}

	writeJSON(w, http.StatusOK, doc)
}

// apiHTML returns the Swagger UI HTML page.
func (h *handler) apiHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerUIHTML(strings.TrimSuffix(h.cfg.Server.BasePath, "/"))))
}

// apiJS serves the Swagger UI initialiser. It is a separate same-origin script
// because the Content-Security-Policy forbids inline scripts.
func (h *handler) apiJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(apiInitJS))
}
