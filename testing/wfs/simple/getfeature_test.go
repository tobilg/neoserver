package simple

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/exceptions"
	"github.com/tobilg/neoserver/testing/wfs/gml"
)

// TestGetFeature_ByTypeName tests basic GetFeature by type name.
func TestGetFeature_ByTypeName(t *testing.T) {
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
		"COUNT":     {"10"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
	wfs.AssertIsGML(t, resp)
}

// TestGetFeature_MissingTypeName tests that TYPENAMES is required.
func TestGetFeature_MissingTypeName(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"GetFeature"},
		"VERSION": {"2.0.0"},
		// TYPENAMES is missing and no STOREDQUERY_ID
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionMissingParameterValue)
}

// TestGetFeature_UnknownTypeName tests that unknown type returns exception.
func TestGetFeature_UnknownTypeName(t *testing.T) {
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

// TestGetFeature_MultipleTypes tests requesting multiple feature types.
// Note: Some servers don't support spatial joins (multiple type names in a single query).
func TestGetFeature_MultipleTypes(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	names := ctx.GetFeatureTypeNames()
	if len(names) < 2 {
		t.Skip("Need at least 2 feature types for this test")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {names[0] + "," + names[1]},
		"COUNT":     {"10"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Server may not support spatial joins - skip if OperationNotSupported
	if exceptions.IsException(resp.Body) {
		ex, _ := exceptions.Parse(resp.Body)
		if ex.HasCode(wfs.ExceptionOperationNotSupported) {
			t.Skip("Server does not support multiple type names (spatial joins)")
		}
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestGetFeature_WithCount tests the COUNT parameter.
func TestGetFeature_WithCount(t *testing.T) {
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
		"COUNT":     {"5"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// numberReturned should be at most COUNT
	if fc.NumberReturned > 5 {
		t.Errorf("Expected at most 5 features, got %d", fc.NumberReturned)
	}
}

// TestGetFeature_InvalidCount tests that invalid COUNT returns exception.
func TestGetFeature_InvalidCount(t *testing.T) {
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
		"COUNT":     {"-1"}, // Invalid negative count
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestGetFeature_ResultTypeHits tests resultType=hits.
func TestGetFeature_ResultTypeHits(t *testing.T) {
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
		"RESULTTYPE": {"hits"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// For resultType=hits, numberReturned should be 0
	if fc.NumberReturned != 0 {
		t.Errorf("Expected numberReturned=0 for hits, got %d", fc.NumberReturned)
	}

	// But numberMatched should be the total count
	if fc.NumberMatched < 0 {
		t.Error("numberMatched should be >= 0")
	}
}

// TestGetFeature_ResultTypeResults tests resultType=results (default).
func TestGetFeature_ResultTypeResults(t *testing.T) {
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
		"RESULTTYPE": {"results"},
		"COUNT":      {"10"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestGetFeature_InvalidResultType tests invalid resultType.
func TestGetFeature_InvalidResultType(t *testing.T) {
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

// TestGetFeature_FeatureCollectionStructure tests the response structure.
func TestGetFeature_FeatureCollectionStructure(t *testing.T) {
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
		"COUNT":     {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check required attributes
	if fc.TimeStamp == "" {
		t.Error("FeatureCollection should have timeStamp attribute")
	}

	// numberMatched should be >= numberReturned
	if fc.NumberMatched < fc.NumberReturned && fc.NumberMatched != -1 {
		t.Errorf("numberMatched (%d) should be >= numberReturned (%d)",
			fc.NumberMatched, fc.NumberReturned)
	}
}

// TestGetFeature_TypeNameCompat tests TYPENAME (WFS 1.x) compatibility.
func TestGetFeature_TypeNameCompat(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":  {"WFS"},
		"REQUEST":  {"GetFeature"},
		"VERSION":  {"2.0.0"},
		"TYPENAME": {ft.Name}, // WFS 1.x style
		"COUNT":    {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Many implementations support TYPENAME for backward compatibility
	if !wfs.IsExceptionResponse(resp) {
		wfs.AssertStatusCode(t, resp, 200)
	}
}
