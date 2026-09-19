package basic

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/gml"
)

// TestGetFeature_WithResourceId tests GetFeature with ResourceId filter.
func TestGetFeature_WithResourceId(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
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

	// Now request with ResourceId
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

// TestGetFeature_WithMultipleResourceIds tests multiple ResourceIds.
func TestGetFeature_WithMultipleResourceIds(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// First get features to obtain IDs
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

// TestGetFeature_WithStartIndex tests startIndex parameter.
func TestGetFeature_WithStartIndex(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// First request to get total
	params := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"RESULTTYPE": {"hits"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if fc.NumberMatched < 2 {
		t.Skip("Need at least 2 features to test startIndex")
	}

	// Request with startIndex
	params = url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"STARTINDEX": {"1"},
		"COUNT":      {"5"},
	}

	resp, err = ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestGetFeature_InvalidStartIndex tests invalid startIndex.
func TestGetFeature_InvalidStartIndex(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"STARTINDEX": {"-1"}, // Invalid
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestGetFeature_PropertyNames tests selecting specific properties.
func TestGetFeature_PropertyNames(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Request with PROPERTYNAME - geometry property
	params := url.Values{
		"SERVICE":      {"WFS"},
		"REQUEST":      {"GetFeature"},
		"VERSION":      {"2.0.0"},
		"TYPENAMES":    {ft.Name},
		"PROPERTYNAME": {"*"}, // All properties
		"COUNT":        {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// PROPERTYNAME may or may not be supported
	if wfs.IsExceptionResponse(resp) {
		t.Skip("PROPERTYNAME not supported")
	}

	wfs.AssertStatusCode(t, resp, 200)
}
