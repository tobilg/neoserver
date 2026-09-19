package core

import (
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/links"
)

// TestCollections_Retrieval implements Abstract Test 9: /ats/core/fc-md-op
//
// Test Purpose: Validate that information about the Collections can be retrieved from the expected location.
// Requirement: /req/core/fc-md-op
//
// Test Method:
//  1. Issue an HTTP GET request to the URL {root}/collections
//  2. Validate that a document was returned with a status code 200
//  3. Validate the contents of the returned document using test /ats/core/fc-md-success
func TestCollections_Retrieval(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.Get("/collections", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve collections: %v", err)
	}

	ogcapi.AssertStatusCode(t, resp, 200)
	ogcapi.AssertContentType(t, resp, ogcapi.JSONMediaType)

	if resp.JSON == nil {
		t.Fatal("Collections response is not valid JSON")
	}
}

// TestCollections_Structure implements Abstract Test 10: /ats/core/fc-md-success
//
// Test Purpose: Validate that the Collections response complies with the required structure.
// Requirement: /req/core/fc-md-success
//
// Test Method:
// Validate that the returned document includes:
//  1. a "links" property
//  2. a "collections" property
func TestCollections_Structure(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.GetCached("collections", "/collections", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve collections: %v", err)
	}

	if resp.JSON == nil {
		t.Fatal("Collections response is not valid JSON")
	}

	t.Run("LinksProperty", func(t *testing.T) {
		ogcapi.AssertJSONPropertyExists(t, resp.JSON, "links")
	})

	t.Run("CollectionsProperty", func(t *testing.T) {
		collections := ogcapi.AssertJSONPropertyIsArray(t, resp.JSON, "collections")
		if collections == nil {
			t.Fatal("collections property is missing or not an array")
		}
	})
}

// TestCollections_Links implements Abstract Test 11: /ats/core/fc-md-links
//
// Test Purpose: Validate that the required links are included in the Collections document.
// Requirement: /req/core/fc-md-links
//
// Test Method:
// Verify that the response document includes:
//  1. a link to this response document (relation: self)
//  2. a link to the response document in every other media type supported
func TestCollections_Links(t *testing.T) {
	ctx := getTestContext(t)

	resp, err := ctx.Client.GetCached("collections", "/collections", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve collections: %v", err)
	}

	pageLinks := links.ParseLinks(resp.JSON)

	t.Run("SelfLink", func(t *testing.T) {
		links.ValidateSelfLink(t, pageLinks)
	})

	t.Run("LinksHaveRelAndType", func(t *testing.T) {
		links.ValidateLinksHaveRelAndType(t, pageLinks, "self", "alternate")
	})
}

// TestCollections_Items validates each collection in the response.
func TestCollections_Items(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			// Each collection must have an id
			if col.ID == "" {
				t.Error("Collection is missing 'id' property")
			}
		})
	}
}

// TestCollection_Retrieval implements Abstract Test 12: /ats/core/sfc-md-op
//
// Test Purpose: Validate that a specific Collection can be retrieved.
// Requirement: /req/core/sfc-md-op
//
// Test Method:
//  1. For each collection identified, issue an HTTP GET request to {root}/collections/{collectionId}
//  2. Validate that a document was returned with a status code 200
func TestCollection_Retrieval(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, err := ctx.Client.Get("/collections/"+col.ID, ogcapi.JSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve collection %s: %v", col.ID, err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)

			if resp.JSON == nil {
				t.Fatal("Collection response is not valid JSON")
			}

			// Validate id matches
			id := ogcapi.AssertJSONPropertyIsString(t, resp.JSON, "id")
			if id != col.ID {
				t.Errorf("Expected collection id %q, got %q", col.ID, id)
			}
		})
	}
}

// TestCollection_Links validates collection links.
func TestCollection_Links(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, err := ctx.Client.Get("/collections/"+col.ID, ogcapi.JSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve collection: %v", err)
			}

			colLinks := links.ParseLinks(resp.JSON)

			t.Run("ItemsLink", func(t *testing.T) {
				links.ValidateCollectionLinks(t, colLinks)
			})

			t.Run("LinksHaveRelAndType", func(t *testing.T) {
				links.ValidateLinksHaveRelAndType(t, colLinks, "self", "items")
			})
		})
	}
}

// TestCollection_Extent validates collection extent if present.
func TestCollection_Extent(t *testing.T) {
	ctx := getTestContext(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		if col.Extent == nil {
			continue
		}

		t.Run(col.ID, func(t *testing.T) {
			if col.Extent.Spatial != nil && len(col.Extent.Spatial.BBox) > 0 {
				bbox := col.Extent.Spatial.BBox[0]
				if len(bbox) < 4 {
					t.Errorf("Spatial extent bbox has less than 4 values: %v", bbox)
					return
				}

				minLon, minLat := bbox[0], bbox[1]
				maxLon, maxLat := bbox[2], bbox[3]

				// Validate bbox is valid
				if minLon > maxLon {
					t.Errorf("Invalid bbox: minLon (%f) > maxLon (%f)", minLon, maxLon)
				}
				if minLat > maxLat {
					t.Errorf("Invalid bbox: minLat (%f) > maxLat (%f)", minLat, maxLat)
				}

				// Validate coordinates are within CRS84 bounds
				if minLat < -90 || maxLat > 90 {
					t.Errorf("Latitude out of range [-90, 90]: [%f, %f]", minLat, maxLat)
				}
				if minLon < -180 || maxLon > 180 {
					t.Errorf("Longitude out of range [-180, 180]: [%f, %f]", minLon, maxLon)
				}
			}
		})
	}
}
