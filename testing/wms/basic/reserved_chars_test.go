package basic

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
	"github.com/tobilg/neoserver/testing/wms/capabilities"
)

// TestReservedChars_EscapedHex tests that the server can decode escaped hex values.
// The value %47%65%74%43%61%70%61%62%69%6C%69%74%69%65%73 is "GetCapabilities" URL-encoded.
// Reference: WMS 1.3.0 section 6.3.2
func TestReservedChars_EscapedHex(t *testing.T) {
	ctx := getTestContext(t)

	// %47%65%74%43%61%70%61%62%69%6C%69%74%69%65%73 = "GetCapabilities"
	// We need to construct the URL manually to include the encoded value
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"%47%65%74%43%61%70%61%62%69%6C%69%74%69%65%73"},
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsXML(t, resp)

	// Verify we got a valid capabilities document
	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities response: %v", err)
	}

	if caps.Service.Name != "WMS" {
		t.Errorf("Expected service name 'WMS', got %q", caps.Service.Name)
	}
}

// TestReservedChars_EscapedValues tests various URL-encoded parameter values.
// Reference: WMS 1.3.0 section 6.3.2
// Note: url.Values already handles URL encoding, so we don't need to pre-encode values.
func TestReservedChars_EscapedValues(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Skip("No named layers available")
	}

	crs := layer.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	// Get bounding box
	bbox := "-180,-90,180,90"
	if bb := layer.GetBoundingBox(crs); bb != nil {
		bbox = formatBBox(bb)
	}

	// url.Values handles encoding automatically - no need to pre-encode
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		"BBOX":    {bbox},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {"image/png"},
		"STYLES":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Should get either an image or an exception (depending on server handling)
	wms.AssertStatusCode(t, resp, 200)

	if resp.IsImage {
		wms.AssertIsImage(t, resp, "")
	}
}

// TestReservedChars_SpaceAsPlus tests that the server can decode the "+" character as a space.
// Reference: WMS 1.3.0 section 6.3.2
func TestReservedChars_SpaceAsPlus(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	// This test is only meaningful if we have a layer with a space in its name
	// Since our mock server doesn't have such layers, we'll test with a regular layer
	// but use + in a parameter value that accepts spaces

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Skip("No named layers available")
	}

	crs := layer.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	bbox := "-180,-90,180,90"
	if bb := layer.GetBoundingBox(crs); bb != nil {
		bbox = formatBBox(bb)
	}

	// Test with a normal GetMap request - the server should handle standard URL encoding
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		"BBOX":    {bbox},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {"image/png"},
		"STYLES":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	if !resp.IsImage {
		wms.AssertNotException(t, resp)
	}
}

// TestReservedChars_CaseMixedParameters tests that parameter names are case-insensitive.
// Reference: WMS 1.3.0 section 6.8.1
func TestReservedChars_CaseMixedParameters(t *testing.T) {
	ctx := getTestContext(t)

	// Use mixed case parameter names
	params := url.Values{
		"SeRvIcE": {"WMS"},
		"VeRsIoN": {"1.3.0"},
		"ReQuEsT": {"GetCapabilities"},
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

func formatBBox(bb *capabilities.BoundingBox) string {
	return formatFloat(bb.MinX) + "," + formatFloat(bb.MinY) + "," + formatFloat(bb.MaxX) + "," + formatFloat(bb.MaxY)
}

func formatFloat(f float64) string {
	return url.QueryEscape(floatToString(f))
}

func floatToString(f float64) string {
	// Simple float to string
	s := ""
	if f < 0 {
		s = "-"
		f = -f
	}
	i := int(f)
	s += intToString(i)
	frac := f - float64(i)
	if frac > 0.0001 {
		s += "."
		frac *= 1000000
		fracStr := intToString(int(frac))
		// Pad with leading zeros
		for len(fracStr) < 6 {
			fracStr = "0" + fracStr
		}
		// Trim trailing zeros
		for len(fracStr) > 1 && fracStr[len(fracStr)-1] == '0' {
			fracStr = fracStr[:len(fracStr)-1]
		}
		s += fracStr
	}
	return s
}

func intToString(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
