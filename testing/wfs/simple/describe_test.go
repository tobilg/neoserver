package simple

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
)

// TestDescribe_AllTypes tests describing all feature types.
func TestDescribe_AllTypes(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"DescribeFeatureType"},
		"VERSION": {"2.0.0"},
		// No TYPENAMES = describe all types
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertIsXML(t, resp)
	wfs.AssertNotException(t, resp)

	// Response should be an XSD schema
	body := string(resp.Body)
	if !strings.Contains(body, "schema") {
		t.Error("Response should contain an XML schema")
	}
}

// TestDescribe_SingleType tests describing a single feature type.
func TestDescribe_SingleType(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"DescribeFeatureType"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertIsXML(t, resp)
	wfs.AssertNotException(t, resp)

	// Response should contain the type name
	body := string(resp.Body)
	localName := ft.Name
	if idx := strings.Index(ft.Name, ":"); idx >= 0 {
		localName = ft.Name[idx+1:]
	}
	if !strings.Contains(body, localName) {
		t.Errorf("Response should contain type name %q", localName)
	}
}

// TestDescribe_MultipleTypes tests describing multiple feature types.
func TestDescribe_MultipleTypes(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	names := ctx.GetFeatureTypeNames()
	if len(names) < 2 {
		t.Skip("Need at least 2 feature types for this test")
	}

	// Request first two types
	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"DescribeFeatureType"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {names[0] + "," + names[1]},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertIsXML(t, resp)
	wfs.AssertNotException(t, resp)
}

// TestDescribe_UnknownType tests that unknown type returns exception.
func TestDescribe_UnknownType(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"DescribeFeatureType"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {"unknown:NonExistentType"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestDescribe_MixedKnownUnknown tests request with both known and unknown types.
func TestDescribe_MixedKnownUnknown(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"DescribeFeatureType"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name + ",unknown:NonExistentType"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Should return an exception for the unknown type
	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestDescribe_OutputFormatGML tests output format parameter.
func TestDescribe_OutputFormatGML(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":      {"WFS"},
		"REQUEST":      {"DescribeFeatureType"},
		"VERSION":      {"2.0.0"},
		"TYPENAMES":    {ft.Name},
		"OUTPUTFORMAT": {"application/gml+xml; version=3.2"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestDescribe_InvalidOutputFormat tests that invalid output format returns exception.
func TestDescribe_InvalidOutputFormat(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":      {"WFS"},
		"REQUEST":      {"DescribeFeatureType"},
		"VERSION":      {"2.0.0"},
		"TYPENAMES":    {ft.Name},
		"OUTPUTFORMAT": {"invalid/format"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestDescribe_TypeNameCompat tests TYPENAME (WFS 1.x) compatibility.
func TestDescribe_TypeNameCompat(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Use TYPENAME instead of TYPENAMES
	params := url.Values{
		"SERVICE":  {"WFS"},
		"REQUEST":  {"DescribeFeatureType"},
		"VERSION":  {"2.0.0"},
		"TYPENAME": {ft.Name}, // WFS 1.x style
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Many WFS 2.0 servers support TYPENAME for backward compatibility
	// If not, they return an exception
	if !wfs.IsExceptionResponse(resp) {
		wfs.AssertStatusCode(t, resp, 200)
	}
}

// TestDescribe_SchemaStructure tests the schema structure.
func TestDescribe_SchemaStructure(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"DescribeFeatureType"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	// Should contain xsd:schema element
	body := string(resp.Body)
	if !strings.Contains(body, "schema") && !strings.Contains(body, "Schema") {
		t.Error("Response should contain schema element")
	}

	// Should reference GML
	if !strings.Contains(body, "gml") {
		t.Error("Schema should reference GML namespace")
	}
}
