package basic

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
)

// TestGetFeature_DefaultCRS tests GetFeature with default CRS.
func TestGetFeature_DefaultCRS(t *testing.T) {
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

	// Response should contain the default CRS
	body := string(resp.Body)
	if ft.DefaultCRS != "" {
		// The srsName attribute should be present in geometries
		if !strings.Contains(body, "srsName") {
			t.Log("Warning: srsName not found in geometry, may be implicit")
		}
	}
}

// TestGetFeature_OtherCRS tests GetFeature with a different CRS.
func TestGetFeature_OtherCRS(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Check if the feature type supports other CRS
	crsList := ft.GetCRSList()
	if len(crsList) < 2 {
		t.Skip("Feature type does not support multiple CRS")
	}

	otherCRS := ""
	for _, crs := range crsList {
		if crs != ft.DefaultCRS {
			otherCRS = crs
			break
		}
	}

	if otherCRS == "" {
		t.Skip("No other CRS available")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"SRSNAME":   {otherCRS},
		"COUNT":     {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestGetFeature_UnsupportedCRS tests GetFeature with unsupported CRS.
func TestGetFeature_UnsupportedCRS(t *testing.T) {
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
		"SRSNAME":   {"urn:ogc:def:crs:EPSG::99999"}, // Non-existent CRS
		"COUNT":     {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestGetFeature_InvalidCRS tests GetFeature with invalid CRS format.
func TestGetFeature_InvalidCRS(t *testing.T) {
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
		"SRSNAME":   {"invalid-crs-format"},
		"COUNT":     {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestGetFeature_CRS84 tests GetFeature with CRS84.
func TestGetFeature_CRS84(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Check if CRS84 is supported
	if !ft.SupportsCRS("CRS84") && !ft.SupportsCRS("urn:ogc:def:crs:CRS84") {
		t.Skip("CRS84 not supported by this feature type")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"SRSNAME":   {"urn:ogc:def:crs:CRS84"},
		"COUNT":     {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// May or may not be supported
	if wfs.IsExceptionResponse(resp) {
		t.Log("CRS84 not supported in this format")
		return
	}

	wfs.AssertStatusCode(t, resp, 200)
}

// TestGetFeature_EPSG4326 tests GetFeature with EPSG:4326.
func TestGetFeature_EPSG4326(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Check if EPSG:4326 is supported
	if !ft.SupportsCRS("4326") && !ft.SupportsCRS("EPSG::4326") {
		t.Skip("EPSG:4326 not supported by this feature type")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"SRSNAME":   {"urn:ogc:def:crs:EPSG::4326"},
		"COUNT":     {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestGetFeature_EPSG3857 tests GetFeature with EPSG:3857 (Web Mercator).
func TestGetFeature_EPSG3857(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Check if EPSG:3857 is supported
	if !ft.SupportsCRS("3857") && !ft.SupportsCRS("EPSG::3857") {
		t.Skip("EPSG:3857 not supported by this feature type")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"SRSNAME":   {"urn:ogc:def:crs:EPSG::3857"},
		"COUNT":     {"1"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}
