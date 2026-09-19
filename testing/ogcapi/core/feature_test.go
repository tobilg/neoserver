package core

import (
	"fmt"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/geojson"
	"github.com/tobilg/neoserver/testing/ogcapi/links"
)

// TestFeature_Retrieval implements Abstract Test 27: /ats/core/f-op
//
// Test Purpose: Validate that an individual feature can be retrieved from a Collection.
// Requirement: /req/core/f-op
//
// Test Method:
//  1. For each collection with a known feature ID, issue GET to /collections/{collectionId}/items/{featureId}
//  2. Validate that a document was returned with status code 200
//  3. Validate the contents using test /ats/core/f-success
func TestFeature_Retrieval(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	// First ensure we have feature IDs by running the features test
	hasFeatures := false
	for _, col := range ctx.Collections {
		if _, ok := ctx.GetFeatureID(col.ID); ok {
			hasFeatures = true
			break
		}
	}

	if !hasFeatures {
		// Try to discover feature IDs
		for _, col := range ctx.Collections {
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			resp, err := ctx.Client.Get(path, ogcapi.GeoJSONMediaType)
			if err != nil {
				continue
			}

			if features, ok := resp.JSON["features"].([]any); ok && len(features) > 0 {
				if feature, ok := features[0].(map[string]any); ok {
					if id := feature["id"]; id != nil {
						ctx.SetFeatureID(col.ID, fmt.Sprintf("%v", id))
					}
				}
			}
		}
	}

	// Now test feature retrieval
	testedAny := false
	for _, col := range ctx.Collections {
		featureID, ok := ctx.GetFeatureID(col.ID)
		if !ok {
			continue
		}

		testedAny = true
		t.Run(col.ID, func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items/%s", col.ID, featureID)
			resp, err := ctx.Client.Get(path, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve feature: %v", err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)
			ogcapi.AssertContentType(t, resp, ogcapi.GeoJSONMediaType)

			if resp.JSON == nil {
				t.Fatal("Feature response is not valid JSON")
			}

			// Cache for subsequent tests
			ctx.Client.CacheResponse("feature:"+col.ID, resp)
		})
	}

	if !testedAny {
		t.Skip("No feature IDs available for testing")
	}
}

// TestFeature_TypeProperty implements Abstract Test 28: /ats/core/f-success (Test Method 1)
//
// Test Purpose: Validate that the type property is present and has value "Feature".
// Requirement: /req/core/f-success
func TestFeature_TypeProperty(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	testedAny := false
	for _, col := range ctx.Collections {
		if _, ok := ctx.GetFeatureID(col.ID); !ok {
			continue
		}

		resp, ok := ctx.Client.GetFromCache("feature:" + col.ID)
		if !ok {
			continue
		}

		testedAny = true
		t.Run(col.ID, func(t *testing.T) {
			typeVal := ogcapi.AssertJSONPropertyIsString(t, resp.JSON, "type")
			if typeVal != "Feature" {
				t.Errorf("Expected type 'Feature', got %q", typeVal)
			}
		})
	}

	if !testedAny {
		t.Skip("No cached feature responses available")
	}
}

// TestFeature_IDProperty validates the id property matches the requested ID.
func TestFeature_IDProperty(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	testedAny := false
	for _, col := range ctx.Collections {
		featureID, ok := ctx.GetFeatureID(col.ID)
		if !ok {
			continue
		}

		resp, ok := ctx.Client.GetFromCache("feature:" + col.ID)
		if !ok {
			continue
		}

		testedAny = true
		t.Run(col.ID, func(t *testing.T) {
			if id := resp.JSON["id"]; id != nil {
				idStr := fmt.Sprintf("%v", id)
				if idStr != featureID {
					t.Errorf("Expected feature id %q, got %q", featureID, idStr)
				}
			}
		})
	}

	if !testedAny {
		t.Skip("No cached feature responses available")
	}
}

// TestFeature_GeometryProperty validates the geometry property.
func TestFeature_GeometryProperty(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	testedAny := false
	for _, col := range ctx.Collections {
		if _, ok := ctx.GetFeatureID(col.ID); !ok {
			continue
		}

		resp, ok := ctx.Client.GetFromCache("feature:" + col.ID)
		if !ok {
			continue
		}

		testedAny = true
		t.Run(col.ID, func(t *testing.T) {
			// geometry can be null for features without geometry
			if geom, exists := resp.JSON["geometry"]; exists && geom != nil {
				geomMap, ok := geom.(map[string]any)
				if !ok {
					t.Errorf("geometry is not an object: %T", geom)
					return
				}

				// geometry must have type
				if _, ok := geomMap["type"]; !ok {
					t.Error("geometry is missing 'type' property")
				}

				// Parse and validate coordinates are in CRS84
				f, err := geojson.ParseFeatureFromMap(resp.JSON)
				if err != nil {
					t.Errorf("Failed to parse feature: %v", err)
					return
				}

				if f.Geometry != nil {
					geojson.ValidateGeometryInCRS84(t, f.Geometry, fmt.Sprintf("%v", resp.JSON["id"]))
				}
			}
		})
	}

	if !testedAny {
		t.Skip("No cached feature responses available")
	}
}

// TestFeature_PropertiesProperty validates the properties property.
func TestFeature_PropertiesProperty(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	testedAny := false
	for _, col := range ctx.Collections {
		if _, ok := ctx.GetFeatureID(col.ID); !ok {
			continue
		}

		resp, ok := ctx.Client.GetFromCache("feature:" + col.ID)
		if !ok {
			continue
		}

		testedAny = true
		t.Run(col.ID, func(t *testing.T) {
			// properties can be null but should exist
			if _, exists := resp.JSON["properties"]; !exists {
				t.Error("Feature is missing 'properties' property")
			}
		})
	}

	if !testedAny {
		t.Skip("No cached feature responses available")
	}
}

// TestFeature_Links validates feature links.
func TestFeature_Links(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	testedAny := false
	for _, col := range ctx.Collections {
		if _, ok := ctx.GetFeatureID(col.ID); !ok {
			continue
		}

		resp, ok := ctx.Client.GetFromCache("feature:" + col.ID)
		if !ok {
			continue
		}

		testedAny = true
		t.Run(col.ID, func(t *testing.T) {
			featureLinks := links.ParseLinks(resp.JSON)

			// Self link is required
			t.Run("SelfLink", func(t *testing.T) {
				links.ValidateSelfLink(t, featureLinks)
			})

			// Links must have rel and type
			t.Run("LinksHaveRelAndType", func(t *testing.T) {
				links.ValidateLinksHaveRelAndType(t, featureLinks, "self")
			})
		})
	}

	if !testedAny {
		t.Skip("No cached feature responses available")
	}
}

// TestFeature_GeometryInCRS84 validates feature geometry is in CRS84.
func TestFeature_GeometryInCRS84(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	testedAny := false
	for _, col := range ctx.Collections {
		featureID, ok := ctx.GetFeatureID(col.ID)
		if !ok {
			continue
		}

		resp, ok := ctx.Client.GetFromCache("feature:" + col.ID)
		if !ok {
			continue
		}

		testedAny = true
		t.Run(col.ID, func(t *testing.T) {
			f, err := geojson.ParseFeatureFromMap(resp.JSON)
			if err != nil {
				t.Fatalf("Failed to parse feature: %v", err)
			}

			if f.Geometry != nil {
				geojson.ValidateGeometryInCRS84(t, f.Geometry, featureID)
			}
		})
	}

	if !testedAny {
		t.Skip("No cached feature responses available")
	}
}

// TestFeature_NotFound validates 404 response for non-existent feature.
func TestFeature_NotFound(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	if len(ctx.Collections) == 0 {
		t.Skip("No collections available")
	}
	col := ctx.Collections[0]

	// Use a feature ID that should not exist
	path := fmt.Sprintf("/collections/%s/items/non-existent-feature-id-12345", col.ID)
	resp, err := ctx.Client.Get(path, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	// Should return 404 Not Found
	if resp.StatusCode != 404 {
		t.Errorf("Expected status 404 for non-existent feature, got %d", resp.StatusCode)
	}
}

// TestFeature_ContentCrsHeader validates Content-Crs header.
func TestFeature_ContentCrsHeader(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	testedAny := false
	for _, col := range ctx.Collections {
		featureID, ok := ctx.GetFeatureID(col.ID)
		if !ok {
			continue
		}

		testedAny = true
		t.Run(col.ID, func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items/%s", col.ID, featureID)
			resp, err := ctx.Client.Get(path, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve feature: %v", err)
			}

			// Content-Crs header is optional but if present should indicate CRS84
			ogcapi.AssertCRS84Header(t, resp)
		})
	}

	if !testedAny {
		t.Skip("No feature IDs available for testing")
	}
}
