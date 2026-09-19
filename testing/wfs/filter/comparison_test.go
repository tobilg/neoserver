package filter

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/gml"
)

// TestFilter_PropertyIsEqualTo tests PropertyIsEqualTo filter.
func TestFilter_PropertyIsEqualTo(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfComparisonNotSupported(t, ctx, "PropertyIsEqualTo")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// First get a feature to get a property value
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

	if fc.NumberReturned == 0 {
		t.Skip("No features available to test filter")
	}

	// Use gml:id for the test
	ids := fc.GetFeatureIDs()
	if len(ids) == 0 {
		t.Skip("Cannot extract feature IDs for filter test")
	}

	// Build filter XML
	filter := BuildPropertyIsEqualToFilter("@gml:id", ids[0])

	// Send POST request with filter
	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err = ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_PropertyIsLessThan tests PropertyIsLessThan filter.
func TestFilter_PropertyIsLessThan(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfComparisonNotSupported(t, ctx, "PropertyIsLessThan")

	// This test would need a numeric property to test properly
	// For now, just verify the filter capabilities claim
	t.Log("PropertyIsLessThan is declared as supported")
}

// TestFilter_PropertyIsGreaterThan tests PropertyIsGreaterThan filter.
func TestFilter_PropertyIsGreaterThan(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfComparisonNotSupported(t, ctx, "PropertyIsGreaterThan")

	// This test would need a numeric property to test properly
	t.Log("PropertyIsGreaterThan is declared as supported")
}

// TestFilter_PropertyIsLike tests PropertyIsLike filter.
func TestFilter_PropertyIsLike(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfComparisonNotSupported(t, ctx, "PropertyIsLike")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build a LIKE filter with wildcard
	filter := NewFilterBuilder().
		Start().
		PropertyIsLike("name", "*", "*", ".", "\\").
		End()

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_PropertyIsNull tests PropertyIsNull filter.
func TestFilter_PropertyIsNull(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfComparisonNotSupported(t, ctx, "PropertyIsNull")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build a NULL filter
	filter := NewFilterBuilder().
		Start().
		PropertyIsNull("name").
		End()

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_PropertyIsBetween tests PropertyIsBetween filter.
func TestFilter_PropertyIsBetween(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfComparisonNotSupported(t, ctx, "PropertyIsBetween")

	// This test would need a numeric property to test properly
	t.Log("PropertyIsBetween is declared as supported")
}

// buildGetFeaturePost builds a GetFeature POST request body.
func buildGetFeaturePost(typeName, filter string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<wfs:GetFeature
  xmlns:wfs="http://www.opengis.net/wfs/2.0"
  xmlns:fes="http://www.opengis.net/fes/2.0"
  xmlns:gml="http://www.opengis.net/gml/3.2"
  service="WFS"
  version="2.0.0"
  count="10">
  <wfs:Query typeNames="` + typeName + `">
` + filter + `
  </wfs:Query>
</wfs:GetFeature>
`
}
