package core

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/links"
)

// TestFeatures_DateTimeParameter covers the mandatory Part 1 datetime query
// parameter. Untimed collections must still accept a valid selection and match
// features that have no temporal association.
func TestFeatures_DateTimeParameter(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)
	for _, collection := range ctx.Collections {
		for _, value := range []string{"2000-01-01T00:00:00Z", "2000-01-01", "2000-01-01/P2D", "../2000-01-02T00:00:00Z"} {
			path := fmt.Sprintf("/collections/%s/items?datetime=%s", collection.ID, url.QueryEscape(value))
			resp, err := ctx.Client.Get(path, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("datetime request failed: %v", err)
			}
			ogcapi.AssertStatusCode(t, resp, 200)
		}
	}
}

func TestFeatures_InvalidAndUnknownParametersReturn400(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)
	collection := ctx.Collections[0]
	tests := []string{"datetime=invalid", "unknown-ogc-parameter=true", "limit=0"}
	for _, query := range tests {
		resp, err := ctx.Client.Get(fmt.Sprintf("/collections/%s/items?%s", collection.ID, query), ogcapi.GeoJSONMediaType)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		ogcapi.AssertStatusCode(t, resp, 400)
	}
}

func TestFeatures_NextPageAndNumberReturned(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)
	collection := ctx.Collections[0]
	resp, err := ctx.Client.Get(fmt.Sprintf("/collections/%s/items?limit=1", collection.ID), ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("page request failed: %v", err)
	}
	ogcapi.AssertStatusCode(t, resp, 200)
	if returned, ok := resp.JSON["numberReturned"].(float64); !ok || returned != 1 {
		t.Fatalf("numberReturned=%v", resp.JSON["numberReturned"])
	}
	parsed := links.ParseLinks(resp.JSON)
	var next string
	for _, link := range parsed {
		if link.Rel == "next" {
			next = link.Href
		}
	}
	if next == "" {
		t.Fatal("lookahead page must include a next link")
	}
}

func TestOpenAPI_DescribesEveryFeatureResource(t *testing.T) {
	ctx := getTestContext(t)
	resp, err := ctx.Client.GetJSON("/api")
	if err != nil {
		t.Fatalf("OpenAPI request failed: %v", err)
	}
	ogcapi.AssertStatusCode(t, resp, 200)
	paths, ok := resp.JSON["paths"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPI paths object is missing")
	}
	for _, path := range []string{"/", "/conformance", "/collections", "/collections/{collectionId}", "/collections/{collectionId}/queryables", "/collections/{collectionId}/items", "/collections/{collectionId}/items/{featureId}"} {
		if _, exists := paths[path]; !exists {
			t.Errorf("OpenAPI path %s is missing", path)
		}
	}
}
