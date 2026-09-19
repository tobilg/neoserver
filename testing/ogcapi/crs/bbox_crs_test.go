package crs

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
)

// TestBBoxCRS_Parameter validates the bbox-crs query parameter.
//
// Requirement: /req/crs/fc-bbox-crs-definition
// Test Purpose: Validate that the bbox-crs query parameter is implemented.
func TestBBoxCRS_Parameter(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		if col.Extent == nil || col.Extent.Spatial == nil {
			continue
		}

		bbox := ogcapi.ParseBBox(col.Extent.Spatial)
		if bbox == nil {
			continue
		}

		t.Run(col.ID, func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			params := url.Values{}
			params.Set("bbox", bbox.String())
			params.Set("bbox-crs", ogcapi.CRS84)
			params.Set("limit", "1")

			resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve features: %v", err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)
		})
	}
}

// TestBBoxCRS_WithCRS validates bbox-crs with crs parameter.
//
// Requirement: /req/crs/fc-bbox-crs-valid-value
// Test Purpose: Validate that bbox-crs and crs can be used together.
func TestBBoxCRS_WithCRS(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		if col.Extent == nil || col.Extent.Spatial == nil {
			continue
		}

		bbox := ogcapi.ParseBBox(col.Extent.Spatial)
		if bbox == nil {
			continue
		}

		t.Run(col.ID, func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			params := url.Values{}
			params.Set("bbox", bbox.String())
			params.Set("bbox-crs", ogcapi.CRS84)
			params.Set("crs", ogcapi.CRS84)
			params.Set("limit", "1")

			resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve features: %v", err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)
		})
	}
}

// TestBBoxCRS_UnsupportedCRS validates error for unsupported bbox-crs.
//
// Requirement: /req/crs/fc-bbox-crs-valid-value
// Test Purpose: Validate that unsupported bbox-crs returns an error.
func TestBBoxCRS_UnsupportedCRS(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	if col.Extent == nil || col.Extent.Spatial == nil {
		t.Skip("Collection has no spatial extent")
	}

	bbox := ogcapi.ParseBBox(col.Extent.Spatial)
	if bbox == nil {
		t.Skip("Could not parse spatial extent")
	}

	path := fmt.Sprintf("/collections/%s/items", col.ID)
	params := url.Values{}
	params.Set("bbox", bbox.String())
	params.Set("bbox-crs", "http://www.opengis.net/def/crs/EPSG/0/99999") // Invalid CRS
	params.Set("limit", "1")

	resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	// Should return 400 Bad Request
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400 for unsupported bbox-crs, got %d", resp.StatusCode)
	}
}

// TestBBoxCRS_DefaultCRS validates default bbox-crs is CRS84.
//
// Requirement: /req/crs/fc-bbox-crs-default-value
// Test Purpose: Validate that bbox without bbox-crs defaults to CRS84.
func TestBBoxCRS_DefaultCRS(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}

	// Find a collection with spatial extent
	var testCol *ogcapi.Collection
	for i := range ctx.Collections {
		if ctx.Collections[i].Extent != nil && ctx.Collections[i].Extent.Spatial != nil {
			testCol = &ctx.Collections[i]
			break
		}
	}

	if testCol == nil {
		t.Skip("No collection with spatial extent found")
	}

	bbox := ogcapi.ParseBBox(testCol.Extent.Spatial)
	if bbox == nil {
		t.Skip("Could not parse spatial extent")
	}

	// Request with bbox but no bbox-crs (should default to CRS84)
	path := fmt.Sprintf("/collections/%s/items", testCol.ID)
	params := url.Values{}
	params.Set("bbox", bbox.String())
	params.Set("limit", "1")

	resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve features: %v", err)
	}

	ogcapi.AssertStatusCode(t, resp, 200)

	// Response should be valid
	if resp.JSON == nil {
		t.Fatal("Response is not valid JSON")
	}

	ogcapi.AssertTypeIsFeatureCollection(t, resp.JSON)
}

// TestBBoxCRS_RequiresBbox validates that bbox-crs requires bbox.
func TestBBoxCRS_RequiresBbox(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	// Request with bbox-crs but no bbox
	path := fmt.Sprintf("/collections/%s/items", col.ID)
	params := url.Values{}
	params.Set("bbox-crs", ogcapi.CRS84)
	params.Set("limit", "1")

	resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	// Behavior varies by implementation:
	// - Some return 400 (bbox-crs requires bbox)
	// - Some ignore bbox-crs and return 200
	if resp.StatusCode != 200 && resp.StatusCode != 400 {
		t.Errorf("Expected status 200 or 400 for bbox-crs without bbox, got %d", resp.StatusCode)
	}
}
