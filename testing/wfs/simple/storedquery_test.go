package simple

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/gml"
)

// TestListStoredQueries tests the ListStoredQueries operation.
func TestListStoredQueries(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"ListStoredQueries"},
		"VERSION": {"2.0.0"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertIsXML(t, resp)
	wfs.AssertNotException(t, resp)

	// Response should contain ListStoredQueriesResponse
	body := string(resp.Body)
	if !strings.Contains(body, "ListStoredQueriesResponse") {
		t.Error("Response should contain ListStoredQueriesResponse element")
	}
}

// TestListStoredQueries_ContainsGetFeatureById tests that GetFeatureById is present.
func TestListStoredQueries_ContainsGetFeatureById(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"ListStoredQueries"},
		"VERSION": {"2.0.0"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	// Parse the response
	queries, err := gml.ParseListStoredQueries(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// GetFeatureById is mandatory for Simple WFS
	if !queries.HasQuery(wfs.StoredQueryGetFeatureById) {
		t.Errorf("GetFeatureById stored query not found. Available: %v", queries.GetQueryIDs())
	}
}

// TestDescribeStoredQueries tests the DescribeStoredQueries operation.
func TestDescribeStoredQueries(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"DescribeStoredQueries"},
		"VERSION": {"2.0.0"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertIsXML(t, resp)
	wfs.AssertNotException(t, resp)

	// Response should contain DescribeStoredQueriesResponse
	body := string(resp.Body)
	if !strings.Contains(body, "DescribeStoredQueriesResponse") {
		t.Error("Response should contain DescribeStoredQueriesResponse element")
	}
}

// TestDescribeStoredQueries_GetFeatureById tests describing GetFeatureById query.
func TestDescribeStoredQueries_GetFeatureById(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"DescribeStoredQueries"},
		"VERSION":        {"2.0.0"},
		"STOREDQUERY_ID": {wfs.StoredQueryGetFeatureById},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertIsXML(t, resp)
	wfs.AssertNotException(t, resp)

	// Response should describe the query
	body := string(resp.Body)
	if !strings.Contains(body, "GetFeatureById") {
		t.Error("Response should contain GetFeatureById description")
	}

	// Should have an 'id' parameter
	if !strings.Contains(body, "id") && !strings.Contains(body, "ID") {
		t.Error("GetFeatureById should have an 'id' parameter")
	}
}

// TestDescribeStoredQueries_UnknownQuery tests describing an unknown query.
// Note: WFS spec allows returning an empty DescribeStoredQueriesResponse for unknown queries.
func TestDescribeStoredQueries_UnknownQuery(t *testing.T) {
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
	// 3. Some servers may embed exception in response
	body := string(resp.Body)
	if strings.Contains(body, "DescribeStoredQueriesResponse") {
		// Server returned DescribeStoredQueriesResponse
		if strings.Contains(body, "ExceptionReport") || strings.Contains(body, "InvalidParameterValue") {
			t.Log("Server embeds exception info in DescribeStoredQueriesResponse")
		} else {
			t.Log("Server returns empty DescribeStoredQueriesResponse for unknown queries")
		}
		return
	}
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestGetFeatureById tests the GetFeatureById stored query.
func TestGetFeatureById(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	// First, get a feature to obtain its ID
	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Get features to find an ID
	getParams := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"COUNT":     {"1"},
	}

	getResp, err := ctx.Client.Get(getParams)
	if err != nil {
		t.Fatalf("GetFeature request failed: %v", err)
	}

	wfs.AssertNotException(t, getResp)

	fc, err := gml.ParseFeatureCollection(getResp.Body)
	if err != nil {
		t.Fatalf("Failed to parse features: %v", err)
	}

	ids := fc.GetFeatureIDs()
	if len(ids) == 0 {
		t.Skip("No features available to test GetFeatureById")
	}

	featureID := ids[0]

	// Now use GetFeatureById
	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetFeature"},
		"VERSION":        {"2.0.0"},
		"STOREDQUERY_ID": {wfs.StoredQueryGetFeatureById},
		"ID":             {featureID},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	// GetFeatureById may return:
	// 1. A FeatureCollection with one feature
	// 2. A single feature directly (raw feature element)
	body := string(resp.Body)
	if strings.Contains(body, "FeatureCollection") {
		resultFC, err := gml.ParseFeatureCollection(resp.Body)
		if err != nil {
			t.Fatalf("Failed to parse result: %v", err)
		}
		if resultFC.NumberReturned != 1 {
			t.Errorf("Expected 1 feature, got %d", resultFC.NumberReturned)
		}
	} else {
		// Server returns raw feature - verify it contains the requested ID
		if !strings.Contains(body, featureID) {
			t.Errorf("Response should contain feature with ID %s", featureID)
		}
		t.Logf("Server returns raw feature element for GetFeatureById (valid behavior)")
	}
}

// TestGetFeatureById_UnknownID tests GetFeatureById with unknown ID.
func TestGetFeatureById_UnknownID(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetFeature"},
		"VERSION":        {"2.0.0"},
		"STOREDQUERY_ID": {wfs.StoredQueryGetFeatureById},
		"ID":             {"NonExistentFeatureId.12345"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Should return empty collection or exception
	if wfs.IsExceptionResponse(resp) {
		// Some implementations return an exception
		return
	}

	wfs.AssertStatusCode(t, resp, 200)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	// Should be empty
	if fc.NumberReturned != 0 {
		t.Errorf("Expected 0 features for unknown ID, got %d", fc.NumberReturned)
	}
}

// TestGetFeatureById_MissingID tests GetFeatureById without ID parameter.
func TestGetFeatureById_MissingID(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetFeature"},
		"VERSION":        {"2.0.0"},
		"STOREDQUERY_ID": {wfs.StoredQueryGetFeatureById},
		// ID is missing
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionMissingParameterValue)
}

// TestStoredQuery_UnknownQuery tests invoking an unknown stored query.
func TestStoredQuery_UnknownQuery(t *testing.T) {
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
