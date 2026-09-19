package getmap

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestCRS_Valid tests that a valid CRS returns an image.
func TestCRS_Valid(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	crs := layer.GetFirstCRS()
	if crs == "" {
		t.Skip("Layer has no CRS")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("CRS", crs)

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
	wms.AssertNotException(t, resp)
}

// TestCRS_Invalid tests that an invalid CRS returns InvalidCRS exception.
// Reference: WMS 1.3.0 section 7.3.3.5
func TestCRS_Invalid(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {"INVALID:CRS"},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "InvalidCRS")
}

// TestCRS_Missing tests that missing CRS returns exception.
func TestCRS_Missing(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		// No CRS
		"BBOX":   {"-180,-90,180,90"},
		"WIDTH":  {"256"},
		"HEIGHT": {"256"},
		"FORMAT": {wms.FormatPNG},
		"STYLES": {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "MissingParameterValue")
}

// TestCRS_EachAdvertised tests each advertised CRS for a layer.
func TestCRS_EachAdvertised(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	crsList := layer.CRS
	if len(crsList) == 0 {
		t.Skip("Layer has no CRS")
	}

	// Limit to first 5 CRS to avoid too many tests
	maxCRS := 5
	if len(crsList) < maxCRS {
		maxCRS = len(crsList)
	}

	for _, crs := range crsList[:maxCRS] {
		t.Run(crs, func(t *testing.T) {
			params := ctx.BuildGetMapParams(layer)
			params.Set("CRS", crs)

			// Get appropriate bbox for CRS
			if bb := layer.GetBoundingBox(crs); bb != nil {
				params.Set("BBOX", formatBBox(bb.MinX, bb.MinY, bb.MaxX, bb.MaxY))
			}

			resp, err := ctx.Client.GetMap(params)
			if err != nil {
				t.Fatalf("GetMap request failed: %v", err)
			}

			wms.AssertStatusCode(t, resp, 200)
			if !resp.IsImage {
				wms.AssertNotException(t, resp)
			}
		})
	}
}

// TestCRS_CRS84 tests that CRS:84 works correctly.
func TestCRS_CRS84(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	// Check if CRS:84 is supported
	hasCRS84 := false
	for _, crs := range layer.CRS {
		if crs == "CRS:84" {
			hasCRS84 = true
			break
		}
	}

	if !hasCRS84 {
		t.Skip("CRS:84 not supported by layer")
	}

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {"CRS:84"},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestCRS_EPSG4326 tests that EPSG:4326 works correctly with axis order.
// Note: EPSG:4326 uses lat/lon axis order in WMS 1.3.0
func TestCRS_EPSG4326(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	// Check if EPSG:4326 is supported
	hasEPSG4326 := false
	for _, crs := range layer.CRS {
		if crs == "EPSG:4326" {
			hasEPSG4326 = true
			break
		}
	}

	if !hasEPSG4326 {
		t.Skip("EPSG:4326 not supported by layer")
	}

	// EPSG:4326 uses lat/lon order, so bbox is: minLat,minLon,maxLat,maxLon
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {"EPSG:4326"},
		"BBOX":    {"-90,-180,90,180"}, // lat/lon order
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

func formatBBox(minX, minY, maxX, maxY float64) string {
	return formatFloat(minX) + "," + formatFloat(minY) + "," + formatFloat(maxX) + "," + formatFloat(maxY)
}

func formatFloat(f float64) string {
	// Simple float formatting
	if f == float64(int(f)) {
		return intToString(int(f))
	}
	// Use a basic approach for decimals
	s := ""
	if f < 0 {
		s = "-"
		f = -f
	}
	whole := int(f)
	frac := int((f - float64(whole)) * 1000000)
	s += intToString(whole) + "." + padLeft(intToString(frac), 6, '0')
	// Trim trailing zeros
	for len(s) > 1 && s[len(s)-1] == '0' && s[len(s)-2] != '.' {
		s = s[:len(s)-1]
	}
	return s
}

func intToString(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

func padLeft(s string, length int, pad rune) string {
	for len(s) < length {
		s = string(pad) + s
	}
	return s
}
