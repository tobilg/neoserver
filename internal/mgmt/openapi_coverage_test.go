package mgmt

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
)

func TestCoveragePublicationSchemasMatchRequestBodies(t *testing.T) {
	doc := BuildOpenAPI(conf.Config{})
	create := doc.Components.Schemas["CoverageCreate"].Value
	update := doc.Components.Schemas["CoverageUpdate"].Value
	for _, field := range []string{"id", "service_id", "native_extent", "tile_cache_generation"} {
		if _, ok := create.Properties[field]; ok {
			t.Errorf("create accepts server-owned %s", field)
		}
		if _, ok := update.Properties[field]; ok {
			t.Errorf("update accepts server-owned %s", field)
		}
	}
	if !slices.Equal(create.Required, []string{"source_coverage", "public_id"}) {
		t.Fatalf("create required fields: %v", create.Required)
	}
	if len(update.Required) != 0 {
		t.Fatalf("partial update requires fields: %v", update.Required)
	}
	if _, ok := update.Properties["source_coverage"]; ok {
		t.Error("update accepts immutable source")
	}
	op := doc.Paths.Value("/workspaces/{workspace}/services/{service}/coverages").Post
	if got := op.RequestBody.Value.Content["application/json"].Schema.Ref; got != "#/components/schemas/CoverageCreate" {
		t.Fatalf("publication body: %s", got)
	}
}

func TestRegisteredManagementRoutesAreDocumented(t *testing.T) {
	router := chi.NewRouter()
	RegisterRoutes(router, Dependencies{})
	registered := make(map[string]bool)
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		// Swagger/OpenAPI discovery endpoints document the API itself; api.js
		// is the docs page's initialiser, not a management operation.
		if route == "/api" || route == "/api.html" || route == "/api.js" {
			return nil
		}
		registered[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	documented := make(map[string]bool)
	for path, item := range BuildOpenAPI(conf.Config{}).Paths.Map() {
		for method := range item.Operations() {
			documented[method+" "+strings.TrimSuffix(path, "/")] = true
		}
	}
	for route := range registered {
		if !documented[route] {
			t.Errorf("registered route is absent from OpenAPI: %s", route)
		}
	}
	for operation := range documented {
		if !registered[operation] {
			t.Errorf("OpenAPI operation has no registered route: %s", operation)
		}
	}
	if t.Failed() {
		t.Logf("registered=%d documented=%d", len(registered), len(documented))
	}
}
