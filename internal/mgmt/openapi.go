package mgmt

import (
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/httputil"
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

// swaggerUIHTML returns the management Swagger UI page; see httputil.SwaggerUIPage.
func swaggerUIHTML(basePath string) string {
	return httputil.SwaggerUIPage("Management API Docs", basePath, basePath+"/api/v1/api.js")
}

// apiInitJS boots Swagger UI, or explains itself when its runtime is missing.
const apiInitJS = httputil.SwaggerUIInitJS

// api returns the OpenAPI JSON document.
func (h *handler) api(w http.ResponseWriter, r *http.Request, cfg conf.Config) {
	doc := buildOpenAPI(cfg)

	// The public origin comes from configuration, never from request headers.
	// UrlBase may be empty only on a loopback bind; a relative server URL then
	// resolves against wherever the document was fetched.
	serverURL := strings.TrimRight(cfg.Server.BasePath, "/") + "/api/v1"
	if base := strings.TrimRight(cfg.Server.UrlBase, "/"); base != "" {
		serverURL = base + serverURL
	}
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
