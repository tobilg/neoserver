package filter

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/exceptions"
	"github.com/tobilg/neoserver/testing/wfs/gml"
)

// TestFilter_ResourceId tests ResourceId filter via KVP.
func TestFilter_ResourceId(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// First get a feature to obtain its ID
	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"COUNT":     {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse features: %v", err)
	}

	ids := fc.GetFeatureIDs()
	if len(ids) == 0 {
		t.Skip("No features available to test ResourceId")
	}

	featureID := ids[0]

	// Request with RESOURCEID parameter
	params = url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"RESOURCEID": {featureID},
	}

	resp, err = ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	resultFC, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if resultFC.NumberReturned != 1 {
		t.Errorf("Expected 1 feature, got %d", resultFC.NumberReturned)
	}
}

// TestFilter_ResourceId_Multiple tests multiple ResourceIds.
func TestFilter_ResourceId_Multiple(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Get multiple features
	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"COUNT":     {"3"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse features: %v", err)
	}

	ids := fc.GetFeatureIDs()
	if len(ids) < 2 {
		t.Skip("Need at least 2 features to test multiple ResourceIds")
	}

	// Request with multiple ResourceIds
	params = url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"RESOURCEID": {ids[0] + "," + ids[1]},
	}

	resp, err = ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	resultFC, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if resultFC.NumberReturned != 2 {
		t.Errorf("Expected 2 features, got %d", resultFC.NumberReturned)
	}
}

// TestFilter_ResourceId_Unknown tests unknown ResourceId.
func TestFilter_ResourceId_Unknown(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"RESOURCEID": {"NonExistentFeature.12345"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Server behavior varies:
	// 1. Return empty collection (ResourceId not found)
	// 2. Return exception (type prefix doesn't match TYPENAMES)
	if exceptions.IsException(resp.Body) {
		// Server validates that ResourceId type prefix matches TYPENAMES
		t.Log("Server returns exception for ResourceId with mismatched type prefix (valid validation)")
		return
	}

	wfs.AssertStatusCode(t, resp, 200)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if fc.NumberReturned != 0 {
		t.Errorf("Expected 0 features for unknown ResourceId, got %d", fc.NumberReturned)
	}
}

// TestFilter_ResourceId_XML tests ResourceId filter via POST.
func TestFilter_ResourceId_XML(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// First get a feature
	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"COUNT":     {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse features: %v", err)
	}

	ids := fc.GetFeatureIDs()
	if len(ids) == 0 {
		t.Skip("No features available")
	}

	// Build XML filter
	filter := BuildResourceIdFilter(ids[0])
	requestBody := buildGetFeaturePost(ft.Name, filter)

	resp, err = ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	resultFC, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if resultFC.NumberReturned != 1 {
		t.Errorf("Expected 1 feature, got %d", resultFC.NumberReturned)
	}
}
