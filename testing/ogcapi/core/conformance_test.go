package core

import (
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
)

// TestConformance_Retrieval implements Abstract Test 7: /ats/core/conformance-op
//
// Test Purpose: Validate that a Conformance Declaration can be retrieved from the expected location.
// Requirement: /req/core/conformance-op
//
// Test Method:
//  1. Issue an HTTP GET request to the URL {root}/conformance
//  2. Validate that a document was returned with a status code 200
//  3. Validate the contents of the returned document using test /ats/core/conformance-success
func TestConformance_Retrieval(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.Get("/conformance", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve conformance: %v", err)
	}

	ogcapi.AssertStatusCode(t, resp, 200)
	ogcapi.AssertContentType(t, resp, ogcapi.JSONMediaType)

	if resp.JSON == nil {
		t.Fatal("Conformance response is not valid JSON")
	}
}

// TestConformance_ConformsTo implements Abstract Test 8: /ats/core/conformance-success
//
// Test Purpose: Validate that the Conformance Declaration response complies
// with the required structure and contents.
// Requirement: /req/core/conformance-success
//
// Test Method:
// Validate that the document includes the conformsTo property.
func TestConformance_ConformsTo(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.GetCached("conformance", "/conformance", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve conformance: %v", err)
	}

	if resp.JSON == nil {
		t.Fatal("Conformance response is not valid JSON")
	}

	// Validate conformsTo property exists and is an array
	conformsTo := ogcapi.AssertJSONPropertyIsArray(t, resp.JSON, "conformsTo")
	if conformsTo == nil {
		return
	}

	if len(conformsTo) == 0 {
		t.Error("conformsTo array is empty")
	}
}

// TestConformance_CoreClass validates the Core conformance class is declared.
func TestConformance_CoreClass(t *testing.T) {
	ctx := getTestContext(t)

	if !ctx.HasConformanceClass(ogcapi.ConformanceCore) {
		t.Errorf("Server does not declare Core conformance class: %s", ogcapi.ConformanceCore)
	}
}

// TestConformance_GeoJSONClass validates the GeoJSON conformance class is declared.
func TestConformance_GeoJSONClass(t *testing.T) {
	ctx := getTestContext(t)

	if !ctx.HasConformanceClass(ogcapi.ConformanceGeoJSON) {
		t.Errorf("Server does not declare GeoJSON conformance class: %s", ogcapi.ConformanceGeoJSON)
	}
}

// TestConformance_OAS30Class validates the OpenAPI 3.0 conformance class is declared.
func TestConformance_OAS30Class(t *testing.T) {
	ctx := getTestContext(t)

	if !ctx.HasConformanceClass(ogcapi.ConformanceOAS30) {
		t.Errorf("Server does not declare OAS30 conformance class: %s", ogcapi.ConformanceOAS30)
	}
}

// TestConformance_ValidURIs validates that conformance class URIs are valid.
func TestConformance_ValidURIs(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.GetCached("conformance", "/conformance", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve conformance: %v", err)
	}

	conformsTo, ok := resp.JSON["conformsTo"].([]any)
	if !ok {
		t.Fatal("conformsTo is not an array")
	}

	for i, c := range conformsTo {
		classStr, ok := c.(string)
		if !ok {
			t.Errorf("conformsTo[%d] is not a string: %T", i, c)
			continue
		}

		// Validate it looks like a URI
		if len(classStr) < 10 {
			t.Errorf("conformsTo[%d] is too short to be a valid URI: %q", i, classStr)
		}

		// Should start with http:// or https://
		if classStr[:7] != "http://" && classStr[:8] != "https://" {
			t.Errorf("conformsTo[%d] does not look like a valid URI: %q", i, classStr)
		}
	}
}
