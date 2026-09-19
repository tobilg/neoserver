package basic

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/gml"
)

// TestGetPropertyValue_GmlId tests GetPropertyValue for gml:id.
func TestGetPropertyValue_GmlId(t *testing.T) {
	ctx := getTestContext(t)
	skipIfOperationNotSupported(t, ctx, "GetPropertyValue")
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetPropertyValue"},
		"VERSION":        {"2.0.0"},
		"TYPENAMES":      {ft.Name},
		"VALUEREFERENCE": {"@gml:id"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	// Response should contain ValueCollection
	body := string(resp.Body)
	if !strings.Contains(body, "ValueCollection") {
		t.Error("Response should contain ValueCollection element")
	}
}

// TestGetPropertyValue_MissingTypeNames tests that TYPENAMES is required.
func TestGetPropertyValue_MissingTypeNames(t *testing.T) {
	ctx := getTestContext(t)
	skipIfOperationNotSupported(t, ctx, "GetPropertyValue")

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetPropertyValue"},
		"VERSION":        {"2.0.0"},
		"VALUEREFERENCE": {"@gml:id"},
		// TYPENAMES is missing
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionMissingParameterValue)
}

// TestGetPropertyValue_MissingValueReference tests that VALUEREFERENCE is required.
func TestGetPropertyValue_MissingValueReference(t *testing.T) {
	ctx := getTestContext(t)
	skipIfOperationNotSupported(t, ctx, "GetPropertyValue")
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetPropertyValue"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		// VALUEREFERENCE is missing
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionMissingParameterValue)
}

// TestGetPropertyValue_UnknownTypeName tests unknown type name.
func TestGetPropertyValue_UnknownTypeName(t *testing.T) {
	ctx := getTestContext(t)
	skipIfOperationNotSupported(t, ctx, "GetPropertyValue")

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetPropertyValue"},
		"VERSION":        {"2.0.0"},
		"TYPENAMES":      {"unknown:NonExistentType"},
		"VALUEREFERENCE": {"@gml:id"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestGetPropertyValue_WithCount tests GetPropertyValue with COUNT.
func TestGetPropertyValue_WithCount(t *testing.T) {
	ctx := getTestContext(t)
	skipIfOperationNotSupported(t, ctx, "GetPropertyValue")
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetPropertyValue"},
		"VERSION":        {"2.0.0"},
		"TYPENAMES":      {ft.Name},
		"VALUEREFERENCE": {"@gml:id"},
		"COUNT":          {"5"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	vc, err := gml.ParseValueCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if vc.NumberReturned > 5 {
		t.Errorf("Expected at most 5 values, got %d", vc.NumberReturned)
	}
}

// TestGetPropertyValue_ResultTypeHits tests resultType=hits.
func TestGetPropertyValue_ResultTypeHits(t *testing.T) {
	ctx := getTestContext(t)
	skipIfOperationNotSupported(t, ctx, "GetPropertyValue")
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetPropertyValue"},
		"VERSION":        {"2.0.0"},
		"TYPENAMES":      {ft.Name},
		"VALUEREFERENCE": {"@gml:id"},
		"RESULTTYPE":     {"hits"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	vc, err := gml.ParseValueCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// For hits, numberReturned should be 0
	if vc.NumberReturned != 0 {
		t.Errorf("Expected numberReturned=0 for hits, got %d", vc.NumberReturned)
	}
}
