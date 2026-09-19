package exceptiontests

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
)

// TestException_MissingRequest tests that missing REQUEST returns MissingParameterValue.
func TestException_MissingRequest(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"VERSION": {"2.0.0"},
		// REQUEST is missing
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionMissingParameterValue)
}

// TestException_InvalidRequest tests that invalid REQUEST returns OperationNotSupported.
func TestException_InvalidRequest(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"VERSION": {"2.0.0"},
		"REQUEST": {"InvalidOperation"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionOperationNotSupported)
}

// TestException_InvalidService tests that wrong SERVICE returns InvalidParameterValue.
func TestException_InvalidService(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WCS"}, // Wrong service
		"REQUEST": {"GetCapabilities"},
		"VERSION": {"2.0.0"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestException_InvalidVersion tests that an unsupported VERSION is rejected.
func TestException_InvalidVersion(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"GetFeature"},
		"VERSION": {"9.9.9"}, // Unsupported version
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	// VersionNegotiationFailed is the WFS/OWS-specific response. Retain the two
	// generic parameter codes for interoperability with otherwise conforming
	// implementations that validate the operation parameters first.
	wfs.AssertExceptionCodeOneOf(t, resp, wfs.ExceptionVersionNegotiationFailed, wfs.ExceptionInvalidParameterValue, wfs.ExceptionMissingParameterValue)
}

// TestException_InvalidTypeName tests that unknown TYPENAMES returns InvalidParameterValue.
func TestException_InvalidTypeName(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
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

// TestException_MissingTypeName tests that missing TYPENAMES returns MissingParameterValue.
func TestException_MissingTypeName(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"GetFeature"},
		"VERSION": {"2.0.0"},
		// TYPENAMES is missing (and no STOREDQUERY_ID)
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionMissingParameterValue)
}

// TestException_InvalidOutputFormat tests that invalid outputFormat returns InvalidParameterValue.
func TestException_InvalidOutputFormat(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":      {"WFS"},
		"REQUEST":      {"GetFeature"},
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

// TestException_InvalidCount tests that invalid COUNT returns InvalidParameterValue.
func TestException_InvalidCount(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"COUNT":     {"-5"}, // Invalid negative count
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestException_InvalidStartIndex tests that invalid STARTINDEX returns InvalidParameterValue.
func TestException_InvalidStartIndex(t *testing.T) {
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
		"STARTINDEX": {"-1"}, // Invalid negative startIndex
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestException_InvalidResultType tests that invalid RESULTTYPE returns InvalidParameterValue.
func TestException_InvalidResultType(t *testing.T) {
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
		"RESULTTYPE": {"invalid"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestException_InvalidSrsName tests that invalid SRSNAME returns InvalidParameterValue.
func TestException_InvalidSrsName(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"SRSNAME":   {"invalid:crs:format"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestException_InvalidBBOX tests that invalid BBOX returns InvalidParameterValue.
func TestException_InvalidBBOX(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"BBOX":      {"not,a,valid,bbox"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestException_InvalidStoredQueryID tests that unknown stored query returns InvalidParameterValue.
func TestException_InvalidStoredQueryID(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetFeature"},
		"VERSION":        {"2.0.0"},
		"STOREDQUERY_ID": {"urn:ogc:def:query:OGC-WFS::NonExistentQuery"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestException_DescribeUnknownStoredQuery tests that describing unknown stored query returns exception or empty response.
// Note: WFS spec allows returning an empty DescribeStoredQueriesResponse for unknown queries.
func TestException_DescribeUnknownStoredQuery(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"DescribeStoredQueries"},
		"VERSION":        {"2.0.0"},
		"STOREDQUERY_ID": {"urn:ogc:def:query:OGC-WFS::NonExistentQuery"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Server behavior varies:
	// 1. Return an exception (InvalidParameterValue)
	// 2. Return an empty DescribeStoredQueriesResponse
	// 3. Some servers may embed exception in response (malformed but contains error info)
	body := string(resp.Body)
	if strings.Contains(body, "DescribeStoredQueriesResponse") {
		// Server returned DescribeStoredQueriesResponse - may be empty or contain embedded error
		if strings.Contains(body, "ExceptionReport") || strings.Contains(body, "InvalidParameterValue") {
			t.Log("Server embeds exception info in DescribeStoredQueriesResponse (non-standard but informative)")
		} else {
			t.Log("Server returns empty DescribeStoredQueriesResponse for unknown queries")
		}
		return
	}
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestException_ExceptionStructure tests the structure of exception reports.
func TestException_ExceptionStructure(t *testing.T) {
	ctx := getTestContext(t)

	// Generate an exception
	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"InvalidOperation"},
		"VERSION": {"2.0.0"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)

	// The exception should be XML
	wfs.AssertIsXML(t, resp)

	// Status code should typically be 400 for client errors
	if resp.StatusCode != 400 && resp.StatusCode != 200 {
		t.Logf("Status code for exception: %d (expected 400, but some servers return 200)", resp.StatusCode)
	}
}
