package crs

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
)

// TestCRSParameter_Features validates the crs query parameter for features.
//
// Requirement: /req/crs/fc-crs-definition
// Test Purpose: Validate that the crs query parameter is implemented.
func TestCRSParameter_Features(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		if len(col.CRS) == 0 {
			continue
		}

		t.Run(col.ID, func(t *testing.T) {
			// Test with CRS84
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			params := url.Values{}
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

// TestCRSParameter_Feature validates the crs query parameter for single feature.
//
// Requirement: /req/crs/f-crs-definition
// Test Purpose: Validate that the crs query parameter works for single features.
func TestCRSParameter_Feature(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		featureID, ok := ctx.GetFeatureID(col.ID)
		if !ok {
			// Try to get a feature ID
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			params := url.Values{}
			params.Set("limit", "1")
			resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
			if err != nil {
				continue
			}
			if features, ok := resp.JSON["features"].([]any); ok && len(features) > 0 {
				if feature, ok := features[0].(map[string]any); ok {
					if id := feature["id"]; id != nil {
						featureID = fmt.Sprintf("%v", id)
						ctx.SetFeatureID(col.ID, featureID)
					}
				}
			}
		}

		if featureID == "" {
			continue
		}

		t.Run(col.ID, func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items/%s", col.ID, featureID)
			params := url.Values{}
			params.Set("crs", ogcapi.CRS84)

			resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve feature: %v", err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)
		})
	}
}

// TestCRSParameter_ContentCrsHeader validates Content-Crs header in response.
//
// Requirement: /req/crs/fc-crs-response
// Test Purpose: Validate that Content-Crs header matches requested CRS.
func TestCRSParameter_ContentCrsHeader(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	path := fmt.Sprintf("/collections/%s/items", col.ID)
	params := url.Values{}
	params.Set("crs", ogcapi.CRS84)
	params.Set("limit", "1")

	resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve features: %v", err)
	}

	ogcapi.AssertStatusCode(t, resp, 200)

	// Check Content-Crs header
	crsHeader := resp.Headers.Get("Content-Crs")
	if crsHeader == "" {
		t.Error("Content-Crs header is missing")
		return
	}

	// Remove angle brackets if present
	crsHeader = strings.TrimPrefix(crsHeader, "<")
	crsHeader = strings.TrimSuffix(crsHeader, ">")

	if crsHeader != ogcapi.CRS84 && crsHeader != ogcapi.CRS84h {
		t.Errorf("Expected Content-Crs to be CRS84, got %q", crsHeader)
	}
}

// TestCRSParameter_UnsupportedCRS validates error handling for unsupported CRS.
//
// Requirement: /req/crs/fc-crs-valid-value
// Test Purpose: Validate that unsupported CRS returns an error.
func TestCRSParameter_UnsupportedCRS(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	path := fmt.Sprintf("/collections/%s/items", col.ID)
	params := url.Values{}
	params.Set("crs", "http://www.opengis.net/def/crs/EPSG/0/99999") // Invalid CRS
	params.Set("limit", "1")

	resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	// Should return 400 Bad Request
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400 for unsupported CRS, got %d", resp.StatusCode)
	}
}

// TestCRSParameter_EPSG validates EPSG CRS if supported.
func TestCRSParameter_EPSG(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}

	// Find a collection with EPSG:4326
	var testCol *ogcapi.Collection
	for i := range ctx.Collections {
		for _, crs := range ctx.Collections[i].CRS {
			if strings.Contains(crs, "EPSG") && strings.Contains(crs, "4326") {
				testCol = &ctx.Collections[i]
				break
			}
		}
		if testCol != nil {
			break
		}
	}

	if testCol == nil {
		t.Skip("No collection with EPSG:4326 found")
	}

	path := fmt.Sprintf("/collections/%s/items", testCol.ID)
	params := url.Values{}
	params.Set("crs", ogcapi.EPSG4326URI)
	params.Set("limit", "1")

	resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve features: %v", err)
	}

	// If EPSG:4326 is supported, should return 200
	if resp.StatusCode != 200 && resp.StatusCode != 400 {
		t.Errorf("Expected status 200 or 400, got %d", resp.StatusCode)
	}
}
