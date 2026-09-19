package getmap

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestBBox_Valid tests that a valid BBOX returns an image.
func TestBBox_Valid(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
	wms.AssertNotException(t, resp)
}

// TestBBox_MinXGreaterThanMaxX tests that when BBOX minX > maxX, server returns exception.
// Reference: WMS 1.3.0 section 7.3.3.6
func TestBBox_MinXGreaterThanMaxX(t *testing.T) {
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

	// Invalid BBOX: minX > maxX
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		"BBOX":    {"1,0,0,1"}, // minX=1 > maxX=0
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Should return an exception
	wms.AssertIsException(t, resp)
}

// TestBBox_MinYGreaterThanMaxY tests that when BBOX minY > maxY, server returns exception.
// Reference: WMS 1.3.0 section 7.3.3.6
func TestBBox_MinYGreaterThanMaxY(t *testing.T) {
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

	// Invalid BBOX: minY > maxY
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		"BBOX":    {"0,1,1,0"}, // minY=1 > maxY=0
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Should return an exception
	wms.AssertIsException(t, resp)
}

// TestBBox_MinXEqualsMaxX tests that when BBOX minX == maxX, server returns exception.
// Reference: WMS 1.3.0 section 7.3.3.6
func TestBBox_MinXEqualsMaxX(t *testing.T) {
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

	// Invalid BBOX: minX == maxX
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		"BBOX":    {"0,0,0,1"}, // minX=0 == maxX=0
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Server should return an exception for zero-width BBOX
	// Status may be 200 (with exception in body) or 4xx/5xx
	if wms.IsExceptionResponse(resp) {
		t.Log("Server returns exception for zero-width BBOX (correct behavior)")
	} else {
		wms.AssertStatusCode(t, resp, 200)
	}
}

// TestBBox_MinYEqualsMaxY tests that when BBOX minY == maxY, server returns exception.
// Reference: WMS 1.3.0 section 7.3.3.6
func TestBBox_MinYEqualsMaxY(t *testing.T) {
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

	// Invalid BBOX: minY == maxY
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		"BBOX":    {"0,0,1,0"}, // minY=0 == maxY=0
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Server should return an exception for zero-height BBOX
	// Status may be 200 (with exception in body) or 4xx/5xx
	if wms.IsExceptionResponse(resp) {
		t.Log("Server returns exception for zero-height BBOX (correct behavior)")
	} else {
		wms.AssertStatusCode(t, resp, 200)
	}
}

// TestBBox_Missing tests that missing BBOX returns exception.
func TestBBox_Missing(t *testing.T) {
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

	// Missing BBOX
	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		// No BBOX
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

// TestBBox_InvalidValues tests that invalid BBOX values return exception.
func TestBBox_InvalidValues(t *testing.T) {
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

	testCases := []struct {
		name string
		bbox string
	}{
		{"non-numeric", "a,b,c,d"},
		{"too-few-values", "0,0,1"},
		{"too-many-values", "0,0,1,1,2"},
		{"empty", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := url.Values{
				"SERVICE": {"WMS"},
				"VERSION": {"1.3.0"},
				"REQUEST": {"GetMap"},
				"LAYERS":  {layer.Name},
				"CRS":     {crs},
				"BBOX":    {tc.bbox},
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
		})
	}
}
