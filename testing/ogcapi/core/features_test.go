package core

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/geojson"
	"github.com/tobilg/neoserver/testing/ogcapi/links"
)

// TestFeatures_Retrieval implements Abstract Test 13: /ats/core/fc-op
//
// Test Purpose: Validate that features can be identified and extracted from a Collection.
// Requirement: /req/core/fc-op
//
// Test Method:
//  1. For every feature collection, issue an HTTP GET request to /collections/{collectionId}/items
//  2. Validate that a document was returned with a status code 200
//  3. Validate the contents using test /ats/core/fc-response
func TestFeatures_Retrieval(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			resp, err := ctx.Client.Get(path, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve features: %v", err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)
			ogcapi.AssertContentType(t, resp, ogcapi.GeoJSONMediaType)

			if resp.JSON == nil {
				t.Fatal("Features response is not valid JSON")
			}

			// Cache the response for subsequent tests
			ctx.Client.CacheResponse("features:"+col.ID, resp)

			// Extract and store a feature ID for later tests
			if features, ok := resp.JSON["features"].([]any); ok && len(features) > 0 {
				if feature, ok := features[0].(map[string]any); ok {
					if id := feature["id"]; id != nil {
						ctx.SetFeatureID(col.ID, fmt.Sprintf("%v", id))
					}
				}
			}
		})
	}
}

// TestFeatures_TypeProperty implements Abstract Test 22: /ats/core/fc-response (Test Method 1)
//
// Test Purpose: Validate that the type property is present and has a value of FeatureCollection.
// Requirement: /req/core/fc-response
func TestFeatures_TypeProperty(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, ok := ctx.Client.GetFromCache("features:" + col.ID)
			if !ok {
				t.Skip("No cached response available")
			}

			ogcapi.AssertTypeIsFeatureCollection(t, resp.JSON)
		})
	}
}

// TestFeatures_FeaturesProperty implements Abstract Test 22: /ats/core/fc-response (Test Method 2)
//
// Test Purpose: Validate the features property is present and is an array.
// Requirement: /req/core/fc-response
func TestFeatures_FeaturesProperty(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, ok := ctx.Client.GetFromCache("features:" + col.ID)
			if !ok {
				t.Skip("No cached response available")
			}

			features := ogcapi.AssertFeaturesProperty(t, resp.JSON)
			if features == nil {
				t.Fatal("features property is missing or not an array")
			}
		})
	}
}

// TestFeatures_Links implements Abstract Test 23: /ats/core/fc-links
//
// Test Purpose: Validate that the required links are included.
// Requirement: /req/core/fc-links, /req/core/fc-rel-type
//
// Test Method:
// Verify that the response includes:
//  1. a link to this response document (relation: self)
//  2. a link to alternate encodings (relation: alternate)
//     All links must include rel and type parameters.
func TestFeatures_Links(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, ok := ctx.Client.GetFromCache("features:" + col.ID)
			if !ok {
				t.Skip("No cached response available")
			}

			featureLinks := links.ParseLinks(resp.JSON)

			t.Run("SelfLink", func(t *testing.T) {
				links.ValidateSelfLink(t, featureLinks)
			})

			t.Run("LinksHaveRelAndType", func(t *testing.T) {
				links.ValidateLinksHaveRelAndType(t, featureLinks, "self", "alternate")
			})
		})
	}
}

// TestFeatures_TimeStamp implements Abstract Test 24: /ats/core/fc-timeStamp
//
// Test Purpose: Validate the timeStamp parameter returned with a Features response.
// Requirement: /req/core/fc-timeStamp
//
// Test Method: Validate that the timeStamp value is set to the time when the response was generated.
func TestFeatures_TimeStamp(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			// Force origin generation: an ordinary GET can legitimately reuse a
			// response generated earlier in this suite, including its timestamp.
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			before := time.Now().Add(-1 * time.Second)
			resp, err := ctx.Client.GetWithHeaders(path, nil, ogcapi.GeoJSONMediaType, http.Header{"Cache-Control": {"no-cache"}})
			after := time.Now().Add(1 * time.Second)

			if err != nil {
				t.Fatalf("Failed to retrieve features: %v", err)
			}

			ogcapi.AssertStatusCode(t, resp, http.StatusOK)
			// timeStamp is optional, so only validate if present
			if resp.JSON["timeStamp"] != nil {
				ogcapi.AssertTimestamp(t, resp.JSON, before, after)
			}
			// A subsequent cached response retains its generation timestamp.
			// A cache-disabled source or a concurrent eviction may generate a new
			// response instead; both must remain inside the observed interval.
			warm, err := ctx.Client.Get(path, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatal(err)
			}
			ogcapi.AssertStatusCode(t, warm, http.StatusOK)
			if warm.JSON["timeStamp"] != nil {
				ogcapi.AssertTimestamp(t, warm.JSON, before, time.Now().Add(time.Second))
			}
			if warm.Headers.Get("X-Cache") == "HIT" && warm.JSON["timeStamp"] != resp.JSON["timeStamp"] {
				t.Fatal("cache hit changed the origin generation timestamp")
			}
		})
	}
}

// TestFeatures_NumberReturned implements Abstract Test 26: /ats/core/fc-numberReturned
//
// Test Purpose: Validate the numberReturned parameter.
// Requirement: /req/core/fc-numberReturned
//
// Test Method: Validate that numberReturned equals the number of features in the response.
func TestFeatures_NumberReturned(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, ok := ctx.Client.GetFromCache("features:" + col.ID)
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

// TestFeatures_GeometryInCRS84 implements Abstract Test 2: /ats/core/crs84
//
// Test Purpose: Validate that all spatial geometries are in CRS84 unless otherwise requested.
// Requirement: /req/core/crs84
//
// Test Method:
//  1. Do not specify a coordinate reference system in any request
//  2. Validate retrieved spatial data using the CRS84 reference system
func TestFeatures_GeometryInCRS84(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, ok := ctx.Client.GetFromCache("features:" + col.ID)
			if !ok {
				t.Skip("No cached response available")
			}

			fc, err := geojson.ParseFeatureCollectionFromMap(resp.JSON)
			if err != nil {
				t.Fatalf("Failed to parse FeatureCollection: %v", err)
			}

			geojson.ValidateFeaturesInCRS84(t, fc, ogcapi.DefaultFeaturesLimit)
		})
	}
}

// TestFeatures_ContentCrsHeader validates the Content-Crs response header.
func TestFeatures_ContentCrsHeader(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			path := fmt.Sprintf("/collections/%s/items", col.ID)
			resp, err := ctx.Client.Get(path, ogcapi.GeoJSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve features: %v", err)
			}

			// Content-Crs header is optional but if present should indicate CRS84
			ogcapi.AssertCRS84Header(t, resp)
		})
	}
}

// TestFeatures_FeatureStructure validates individual feature structure.
func TestFeatures_FeatureStructure(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, ok := ctx.Client.GetFromCache("features:" + col.ID)
			if !ok {
				t.Skip("No cached response available")
			}

			fc, err := geojson.ParseFeatureCollectionFromMap(resp.JSON)
			if err != nil {
				t.Fatalf("Failed to parse FeatureCollection: %v", err)
			}

			// Validate first few features
			limit := 10
			if len(fc.Features) < limit {
				limit = len(fc.Features)
			}

			for i := 0; i < limit; i++ {
				f := &fc.Features[i]
				t.Run(fmt.Sprintf("Feature_%d", i), func(t *testing.T) {
					geojson.ValidateFeature(t, f)
				})
			}
		})
	}
}
