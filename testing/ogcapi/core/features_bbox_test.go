package core

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/geojson"
)

// TestFeaturesBBox_Retrieval implements Abstract Test 13 with bbox parameter
//
// Test Purpose: Validate that features can be filtered by bounding box.
// Requirement: /req/core/fc-op (with bbox parameter)
//
// Test Method:
//  1. For collections with spatial extent, request features with bbox parameter
//  2. Validate that a document was returned with status code 200
func TestFeaturesBBox_Retrieval(t *testing.T) {
	ctx := getTestContext(t)
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

			resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve features with bbox: %v", err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)
			ogcapi.AssertContentType(t, resp, ogcapi.GeoJSONMediaType)

			// Cache for subsequent tests
			cacheKey := fmt.Sprintf("features-bbox:%s:%s", col.ID, bbox.String())
			ctx.Client.CacheResponse(cacheKey, resp)
		})
	}
}

// TestFeaturesBBox_Response implements Abstract Test 15: /ats/core/fc-bbox-response
//
// Test Purpose: Validate that the bounding box query parameters are processed correctly.
// Requirement: /req/core/fc-bbox-response
//
// Test Method:
//  1. Verify that only features intersecting the bbox are returned
//  2. Features without geometry should also be returned
//  3. Verify coordinates are in CRS84
func TestFeaturesBBox_Response(t *testing.T) {
	ctx := getTestContext(t)
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
			cacheKey := fmt.Sprintf("features-bbox:%s:%s", col.ID, bbox.String())
			resp, ok := ctx.Client.GetFromCache(cacheKey)
			if !ok {
				t.Skip("No cached response available")
			}

			fc, err := geojson.ParseFeatureCollectionFromMap(resp.JSON)
			if err != nil {
				t.Fatalf("Failed to parse FeatureCollection: %v", err)
			}

			// Skip if no features returned
			if len(fc.Features) == 0 {
				t.Skip("No features returned for bbox query")
			}

			geomBBox := geojson.BBox{
				MinX: bbox.MinX,
				MinY: bbox.MinY,
				MaxX: bbox.MaxX,
				MaxY: bbox.MaxY,
			}

			t.Run("GeometriesInBBox", func(t *testing.T) {
				geojson.ValidateFeaturesInBBox(t, fc, geomBBox, ogcapi.DefaultFeaturesLimit)
			})

			t.Run("GeometriesInCRS84", func(t *testing.T) {
				geojson.ValidateFeaturesInCRS84(t, fc, ogcapi.DefaultFeaturesLimit)
			})
		})
	}
}

// TestFeaturesBBox_EdgeCases tests bbox queries at edge cases.
func TestFeaturesBBox_EdgeCases(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	// Define edge case bboxes
	edgeCases := []struct {
		name string
		bbox string
	}{
		{"CrossesMeridian", "-1.5,50.0,1.5,53.0"},
		{"CrossesEquator", "-80.0,-5.0,-70.0,5.0"},
		{"NorthPole", "-180.0,85.0,180.0,90.0"},
		{"SouthPole", "-180.0,-90.0,180.0,-85.0"},
	}

	// Test with first collection that has a spatial extent
	var testCol *ogcapi.Collection
	for i := range ctx.Collections {
		if ctx.Collections[i].Extent != nil && ctx.Collections[i].Extent.Spatial != nil {
			testCol = &ctx.Collections[i]
			break
		}
	}

	if testCol == nil {
		t.Skip("No collection with spatial extent available")
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items", testCol.ID)
			params := url.Values{}
			params.Set("bbox", tc.bbox)

			resp, err := ctx.Client.GetWithParams(path, params, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve features with bbox: %v", err)
			}

			// Should return 200 (even if empty result)
			ogcapi.AssertStatusCode(t, resp, 200)

			if resp.JSON == nil {
				t.Fatal("Response is not valid JSON")
			}

			// Validate it's a valid FeatureCollection
			ogcapi.AssertTypeIsFeatureCollection(t, resp.JSON)
		})
	}
}

// TestFeaturesBBox_TypeProperty validates type property with bbox.
func TestFeaturesBBox_TypeProperty(t *testing.T) {
	ctx := getTestContext(t)
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
			cacheKey := fmt.Sprintf("features-bbox:%s:%s", col.ID, bbox.String())
			resp, ok := ctx.Client.GetFromCache(cacheKey)
			if !ok {
				t.Skip("No cached response available")
			}

			ogcapi.AssertTypeIsFeatureCollection(t, resp.JSON)
		})
	}
}

// TestFeaturesBBox_FeaturesProperty validates features property with bbox.
func TestFeaturesBBox_FeaturesProperty(t *testing.T) {
	ctx := getTestContext(t)
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
			cacheKey := fmt.Sprintf("features-bbox:%s:%s", col.ID, bbox.String())
			resp, ok := ctx.Client.GetFromCache(cacheKey)
			if !ok {
				t.Skip("No cached response available")
			}

			ogcapi.AssertFeaturesProperty(t, resp.JSON)
		})
	}
}

// TestFeaturesBBox_NumberReturned validates numberReturned with bbox.
func TestFeaturesBBox_NumberReturned(t *testing.T) {
	ctx := getTestContext(t)
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
			cacheKey := fmt.Sprintf("features-bbox:%s:%s", col.ID, bbox.String())
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
