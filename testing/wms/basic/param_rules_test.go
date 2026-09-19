package basic

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
	"github.com/tobilg/neoserver/testing/wms/capabilities"
)

// TestParamRules_ExtraGetCapabilitiesParam tests that when a GetCapabilities request
// contains a parameter not defined by the spec (BOGUS), the result is still valid.
// Reference: WMS 1.3.0 section 6.8.1
func TestParamRules_ExtraGetCapabilitiesParam(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetCapabilities"},
		"BOGUS":   {"ignored"}, // Extra parameter
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsXML(t, resp)

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities response: %v", err)
	}

	wms.AssertCapabilitiesVersion(t, caps, "1.3.0")
}

// TestParamRules_ExtraGetMapParam tests that when a GetMap request contains
// a parameter not defined by the spec (BOGUS), the result is still valid.
// Reference: WMS 1.3.0 section 6.8.1
func TestParamRules_ExtraGetMapParam(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("BOGUS", "ignored") // Extra parameter

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "png")
	wms.AssertNotException(t, resp)
}

// TestParamRules_ExtraGetFeatureInfoParam tests that when a GetFeatureInfo request
// contains a parameter not defined by the spec (BOGUS), the result is still valid.
// Reference: WMS 1.3.0 section 6.8.1
func TestParamRules_ExtraGetFeatureInfoParam(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Skip("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("BOGUS", "ignored") // Extra parameter

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	// GetFeatureInfo should return XML or another supported format, not an exception
	wms.AssertNotException(t, resp)
}

// TestParamRules_CaseInsensitiveParamNames tests that parameter names are case-insensitive.
// Reference: WMS 1.3.0 section 6.8.1
func TestParamRules_CaseInsensitiveParamNames(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	crs := layer.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	bbox := "-180,-90,180,90"
	if bb := layer.GetBoundingBox(crs); bb != nil {
		bbox = formatBBox(bb)
	}

	// Use mixed case parameter names (as in the CTL test)
	params := url.Values{
		"ReQuEsT": {"GetMap"},
		"VeRsIoN": {"1.3.0"},
		"LaYeRs":  {layer.Name},
		"CrS":     {crs},
		"BbOx":    {bbox},
		"WiDtH":   {"256"},
		"HeIgHt":  {"256"},
		"FoRmAt":  {"image/png"},
		"StYlEs":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "png")
}

// TestParamRules_CaseInsensitiveParamValues tests that parameter values for
// keyword parameters (SERVICE, REQUEST) are case-insensitive.
// Reference: WMS 1.3.0 section 6.8.1
func TestParamRules_CaseInsensitiveParamValues(t *testing.T) {
	ctx := getTestContext(t)

	// Test with mixed case values for SERVICE and REQUEST
	params := url.Values{
		"SERVICE": {"wMs"},             // lowercase/mixed
		"VERSION": {"1.3.0"},
		"REQUEST": {"gEtCaPaBiLiTiEs"}, // mixed case
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsXML(t, resp)

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities response: %v", err)
	}

	wms.AssertCapabilitiesVersion(t, caps, "1.3.0")
}

// TestParamRules_ParameterOrder tests that parameters can appear in any order.
// Reference: WMS 1.3.0 section 6.8.1
func TestParamRules_ParameterOrder(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	crs := layer.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	bbox := "-180,-90,180,90"
	if bb := layer.GetBoundingBox(crs); bb != nil {
		bbox = formatBBox(bb)
	}

	// Parameters in non-standard order
	params := url.Values{
		"STYLES":  {""},
		"HEIGHT":  {"256"},
		"FORMAT":  {"image/png"},
		"LAYERS":  {layer.Name},
		"WIDTH":   {"256"},
		"BBOX":    {bbox},
		"REQUEST": {"GetMap"},
		"CRS":     {crs},
		"VERSION": {"1.3.0"},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "png")
}

// TestParamRules_DuplicateParameters tests behavior with duplicate parameters.
// WMS spec doesn't explicitly define this, but servers should handle it gracefully.
func TestParamRules_DuplicateParameters(t *testing.T) {
	ctx := getTestContext(t)

	// Add the same parameter twice (url.Values allows this)
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetCapabilities"},
	}
	params.Add("SERVICE", "WMS") // Duplicate

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	// Server should still return a valid response
	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsXML(t, resp)
}

// TestParamRules_EmptyOptionalParameters tests that empty optional parameters are accepted.
func TestParamRules_EmptyOptionalParameters(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("STYLES", "")       // Empty STYLES is valid
	params.Set("TRANSPARENT", "")  // Empty TRANSPARENT should be ignored
	params.Set("BGCOLOR", "")      // Empty BGCOLOR should be ignored

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}
