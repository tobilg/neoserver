package crs

import (
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
)

// TestCRS_GlobalList validates that the global CRS list is available.
//
// Requirement: /req/crs/crs-uri
// Test Purpose: Validate that the server provides CRS information.
func TestCRS_GlobalList(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)

	resp, err := ctx.Client.Get("/collections", ogcapi.JSONMediaType)
	if err != nil {
		t.Fatalf("Failed to retrieve collections: %v", err)
	}

	ogcapi.AssertStatusCode(t, resp, 200)

	// Check for global crs property
	if crs, ok := resp.JSON["crs"]; ok {
		crsList, ok := crs.([]any)
		if !ok {
			t.Error("crs property is not an array")
			return
		}

		if len(crsList) == 0 {
			t.Error("crs array is empty")
			return
		}

		// Validate CRS URIs
		for i, c := range crsList {
			crsStr, ok := c.(string)
			if !ok {
				t.Errorf("crs[%d] is not a string", i)
				continue
			}

			// Should be a valid URI
			if len(crsStr) < 10 {
				t.Errorf("crs[%d] is too short: %q", i, crsStr)
			}
		}
	}
}

// TestCRS_CollectionCRS validates that collections have CRS information.
//
// Requirement: /req/crs/fc-md-crs-list
// Test Purpose: Validate that collections include a list of supported CRS.
func TestCRS_CollectionCRS(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, err := ctx.Client.Get("/collections/"+col.ID, ogcapi.JSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve collection: %v", err)
			}

			ogcapi.AssertStatusCode(t, resp, 200)

			// Collection should have crs property
			crs, ok := resp.JSON["crs"]
			if !ok {
				t.Error("Collection is missing 'crs' property")
				return
			}

			crsList, ok := crs.([]any)
			if !ok {
				t.Error("crs property is not an array")
				return
			}

			if len(crsList) == 0 {
				t.Error("crs array is empty")
				return
			}

			// Validate CRS84 is in the list
			hasCRS84 := false
			for _, c := range crsList {
				crsStr, ok := c.(string)
				if !ok {
					continue
				}
				if crsStr == ogcapi.CRS84 || crsStr == ogcapi.CRS84h {
					hasCRS84 = true
					break
				}
			}

			if !hasCRS84 {
				t.Error("CRS84 or CRS84h not found in collection CRS list")
			}
		})
	}
}

// TestCRS_DefaultCRS validates that CRS84 is the default.
//
// Requirement: /req/crs/fc-md-crs-list-default-crs
// Test Purpose: Validate that CRS84 is the first (default) CRS.
func TestCRS_DefaultCRS(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			if len(col.CRS) == 0 {
				t.Skip("Collection has no CRS list")
			}

			ogcapi.AssertDefaultCRS(t, col.CRS)
		})
	}
}

// TestCRS_StorageCRS validates the storageCrs property if present.
//
// Requirement: /req/crs/fc-md-storageCrs
// Test Purpose: Validate that storageCrs is a valid CRS URI if present.
func TestCRS_StorageCRS(t *testing.T) {
	ctx := getTestContext(t)
	requireCRSConformance(t)
	requireCollections(t)

	for _, col := range ctx.Collections {
		t.Run(col.ID, func(t *testing.T) {
			resp, err := ctx.Client.Get("/collections/"+col.ID, ogcapi.JSONMediaType)
			if err != nil {
				t.Fatalf("Failed to retrieve collection: %v", err)
			}

			// storageCrs is optional
			if storageCrs, ok := resp.JSON["storageCrs"]; ok {
				crsStr, ok := storageCrs.(string)
				if !ok {
					t.Error("storageCrs is not a string")
					return
				}

				// Should be a valid CRS URI
				if len(crsStr) < 10 {
					t.Errorf("storageCrs is too short to be a valid URI: %q", crsStr)
				}

				// Should be in the collection's CRS list
				found := false
				for _, c := range col.CRS {
					if c == crsStr {
						found = true
						break
					}
				}
				if !found && len(col.CRS) > 0 {
					t.Errorf("storageCrs %q not found in collection CRS list", crsStr)
				}
			}
		})
	}
}
