package core

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
)

// TestFeaturesLimit_Retrieval implements Abstract Test 16: /ats/core/fc-limit-definition
//
// Test Purpose: Validate that the limit query parameter is implemented correctly.
// Requirement: /req/core/fc-limit-definition
//
// Test Method:
//  1. Issue requests with different limit values
//  2. Validate that responses are successful
func TestFeaturesLimit_Retrieval(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	// Test with first collection
	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	limits := []int{1, 5, 10, 100}

	for _, limit := range limits {
		t.Run(fmt.Sprintf("Limit_%d", limit), func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			params := url.Values{}
			params.Set("limit", fmt.Sprintf("%d", limit))

			resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve features with limit=%d: %v", limit, err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)
			ogcapi.AssertContentType(t, resp, ogcapi.GeoJSONMediaType)

			// Cache for subsequent tests
			cacheKey := fmt.Sprintf("features-limit:%s:%d", col.ID, limit)
			ctx.Client.CacheResponse(cacheKey, resp)
		})
	}
}

// TestFeaturesLimit_Response implements Abstract Test 17: /ats/core/fc-limit-response
//
// Test Purpose: Validate that the limit query parameter is processed correctly.
// Requirement: /req/core/fc-limit-response
//
// Test Method:
// Verify that the number of features returned does not exceed the limit.
func TestFeaturesLimit_Response(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	limits := []int{1, 5, 10}

	for _, limit := range limits {
		t.Run(fmt.Sprintf("Limit_%d", limit), func(t *testing.T) {
			cacheKey := fmt.Sprintf("features-limit:%s:%d", col.ID, limit)
			resp, ok := ctx.Client.GetFromCache(cacheKey)
			if !ok {
				t.Skip("No cached response available")
			}

			features, ok := resp.JSON["features"].([]any)
			if !ok {
				t.Fatal("features property is not an array")
			}

			if len(features) > limit {
				t.Errorf("Expected at most %d features, got %d", limit, len(features))
			}
		})
	}
}

// TestFeaturesLimit_NumberReturned validates numberReturned matches features count.
func TestFeaturesLimit_NumberReturned(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	limits := []int{1, 5, 10}

	for _, limit := range limits {
		t.Run(fmt.Sprintf("Limit_%d", limit), func(t *testing.T) {
			cacheKey := fmt.Sprintf("features-limit:%s:%d", col.ID, limit)
			resp, ok := ctx.Client.GetFromCache(cacheKey)
			if !ok {
				t.Skip("No cached response available")
			}

			// numberReturned is optional
			if resp.JSON["numberReturned"] != nil {
				ogcapi.AssertNumberReturned(t, resp.JSON)
			}
		})
	}
}

// TestFeaturesLimit_Zero tests limit=0 behavior.
func TestFeaturesLimit_Zero(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	path := fmt.Sprintf("/collections/%s/items", col.ID)
	params := url.Values{}
	params.Set("limit", "0")

	resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve features: %v", err)
	}

	// Should either return 200 with empty features or 400 error
	if resp.StatusCode == 200 {
		features, ok := resp.JSON["features"].([]any)
		if !ok {
			t.Fatal("features property is not an array")
		}
		if len(features) != 0 {
			t.Errorf("Expected 0 features with limit=0, got %d", len(features))
		}
	} else if resp.StatusCode != 400 {
		t.Errorf("Expected status 200 or 400 for limit=0, got %d", resp.StatusCode)
	}
}

// TestFeaturesLimit_Negative tests negative limit behavior.
func TestFeaturesLimit_Negative(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	path := fmt.Sprintf("/collections/%s/items", col.ID)
	params := url.Values{}
	params.Set("limit", "-1")

	resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve features: %v", err)
	}

	// Should return 400 Bad Request for negative limit
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400 for negative limit, got %d", resp.StatusCode)
	}
}

// TestFeaturesLimit_ExceedsMax tests limit exceeding server maximum.
func TestFeaturesLimit_ExceedsMax(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	// Request a very large limit
	path := fmt.Sprintf("/collections/%s/items", col.ID)
	params := url.Values{}
	params.Set("limit", "1000000")

	resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve features: %v", err)
	}

	// Server should either:
	// - Return 200 with capped results
	// - Return 400 error
	if resp.StatusCode != 200 && resp.StatusCode != 400 {
		t.Errorf("Expected status 200 or 400 for excessive limit, got %d", resp.StatusCode)
	}

	if resp.StatusCode == 200 {
		ogcapi.AssertTypeIsFeatureCollection(t, resp.JSON)
	}
}

// TestFeaturesLimit_AllCollections tests limit across all collections.
func TestFeaturesLimit_AllCollections(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	limit := 5

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			params := url.Values{}
			params.Set("limit", fmt.Sprintf("%d", limit))

			resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve features: %v", err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)

			features, ok := resp.JSON["features"].([]any)
			if !ok {
				t.Fatal("features property is not an array")
			}

			if len(features) > limit {
				t.Errorf("Expected at most %d features, got %d", limit, len(features))
			}
		})
	}
}
