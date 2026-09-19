package core

import (
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/links"
)

// TestLandingPage_Retrieval implements Abstract Test 3: /ats/core/root-op
//
// Test Purpose: Validate that a landing page can be retrieved from the expected location.
// Requirement: /req/core/root-op
//
// Test Method:
//  1. Issue an HTTP GET request to the URL {root}/
//  2. Validate that a document was returned with a status code 200
//  3. Validate the contents of the returned document using test /ats/core/root-success
func TestLandingPage_Retrieval(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.Get("/", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve landing page: %v", err)
	}

	ogcapi.AssertStatusCode(t, resp, 200)
	ogcapi.AssertContentType(t, resp, ogcapi.JSONMediaType)

	if resp.JSON == nil {
		t.Fatal("Landing page response is not valid JSON")
	}
}

// TestLandingPage_RequiredLinks implements Abstract Test 4: /ats/core/root-success
//
// Test Purpose: Validate that the landing page complies with the required structure and contents.
// Requirement: /req/core/root-success
//
// Test Method:
// Validate that the landing page includes:
//  1. A link to this document (relation: self)
//  2. A link to the API definition (relation: service-desc or service-doc)
//  3. A link to the Conformance declaration (relation: conformance)
//  4. A link to the Feature Collections (relation: data)
func TestLandingPage_RequiredLinks(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.GetCached("landing-page", "/", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve landing page: %v", err)
	}

	if resp.JSON == nil {
		t.Fatal("Landing page response is not valid JSON")
	}

	pageLinks := links.ParseLinks(resp.JSON)
	if pageLinks == nil {
		t.Fatal("Landing page does not contain links")
	}

	// Run link validation subtests
	t.Run("SelfLink", func(t *testing.T) {
		links.ValidateSelfLink(t, pageLinks)
	})

	t.Run("RequiredLinks", func(t *testing.T) {
		links.ValidateLandingPageLinks(t, pageLinks)
	})

	t.Run("LinkRelAndType", func(t *testing.T) {
		// Validate that self and alternate links have rel and type
		links.ValidateLinksHaveRelAndType(t, pageLinks, "self", "alternate")
	})
}

// TestLandingPage_ServiceDescLink validates the API definition link.
func TestLandingPage_ServiceDescLink(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.GetCached("landing-page", "/", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve landing page: %v", err)
	}

	pageLinks := links.ParseLinks(resp.JSON)

	// Find service-desc or service-doc link
	serviceDesc := links.FindByRel(pageLinks, ogcapi.RelServiceDesc)
	serviceDoc := links.FindByRel(pageLinks, ogcapi.RelServiceDoc)

	if serviceDesc == nil && serviceDoc == nil {
		t.Error("Landing page must include 'service-desc' or 'service-doc' link")
		return
	}

	// If service-desc exists, verify it's accessible
	if serviceDesc != nil {
		t.Run("ServiceDescAccessible", func(t *testing.T) {
			apiResp, err := ctx.Client.Get("/api", "")
			if err != nil {
				t.Errorf("Failed to access API definition: %v", err)
				return
			}
			if apiResp.StatusCode != 200 {
				t.Errorf("API definition returned status %d", apiResp.StatusCode)
			}
		})
	}
}

// TestLandingPage_ConformanceLink validates the conformance link.
func TestLandingPage_ConformanceLink(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.GetCached("landing-page", "/", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve landing page: %v", err)
	}

	pageLinks := links.ParseLinks(resp.JSON)
	conformanceLink := links.FindByRel(pageLinks, ogcapi.RelConformance)

	if conformanceLink == nil {
		t.Fatal("Landing page must include 'conformance' link")
	}

	// Verify conformance endpoint is accessible
	confResp, err := ctx.Client.Get("/conformance", ogcapi.JSONMediaType)
	if err != nil {
		t.Errorf("Failed to access conformance endpoint: %v", err)
		return
	}

	ogcapi.AssertStatusCode(t, confResp, 200)
}

// TestLandingPage_DataLink validates the data (collections) link.
func TestLandingPage_DataLink(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.GetCached("landing-page", "/", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve landing page: %v", err)
	}

	pageLinks := links.ParseLinks(resp.JSON)
	dataLink := links.FindByRel(pageLinks, ogcapi.RelData)

	if dataLink == nil {
		t.Fatal("Landing page must include 'data' link")
	}

	// Verify collections endpoint is accessible
	colResp, err := ctx.Client.Get("/collections", ogcapi.JSONMediaType)
	if err != nil {
		t.Errorf("Failed to access collections endpoint: %v", err)
		return
	}

	ogcapi.AssertStatusCode(t, colResp, 200)
}
