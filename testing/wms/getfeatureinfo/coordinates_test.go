package getfeatureinfo

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestCoordinates_Valid tests that valid I and J coordinates work.
func TestCoordinates_Valid(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("I", "128")
	params.Set("J", "128")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
}

// TestCoordinates_InvalidI tests that invalid I coordinate returns InvalidPoint exception.
// Reference: WMS 1.3.0 section 7.4.3.6
func TestCoordinates_InvalidI(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("WIDTH", "256")
	params.Set("HEIGHT", "256")
	params.Set("I", "300") // Greater than WIDTH
	params.Set("J", "128")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "InvalidPoint")
}

// TestCoordinates_InvalidJ tests that invalid J coordinate returns InvalidPoint exception.
// Reference: WMS 1.3.0 section 7.4.3.6
func TestCoordinates_InvalidJ(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("WIDTH", "256")
	params.Set("HEIGHT", "256")
	params.Set("I", "128")
	params.Set("J", "300") // Greater than HEIGHT

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "InvalidPoint")
}

// TestCoordinates_NegativeI tests that negative I returns exception.
func TestCoordinates_NegativeI(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("I", "-10")
	params.Set("J", "128")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
}

// TestCoordinates_NegativeJ tests that negative J returns exception.
func TestCoordinates_NegativeJ(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("I", "128")
	params.Set("J", "-10")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
}

// TestCoordinates_MissingI tests that missing I returns exception.
func TestCoordinates_MissingI(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	crs := layer.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	params := url.Values{
		"SERVICE":      {"WMS"},
		"VERSION":      {"1.3.0"},
		"REQUEST":      {"GetFeatureInfo"},
		"LAYERS":       {layer.Name},
		"QUERY_LAYERS": {layer.Name},
		"CRS":          {crs},
		"BBOX":         {"-180,-90,180,90"},
		"WIDTH":        {"256"},
		"HEIGHT":       {"256"},
		"FORMAT":       {wms.FormatPNG},
		"STYLES":       {""},
		"INFO_FORMAT":  {"text/xml"},
		// No I
		"J": {"128"},
	}

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "MissingParameterValue")
}

// TestCoordinates_MissingJ tests that missing J returns exception.
func TestCoordinates_MissingJ(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	crs := layer.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	params := url.Values{
		"SERVICE":      {"WMS"},
		"VERSION":      {"1.3.0"},
		"REQUEST":      {"GetFeatureInfo"},
		"LAYERS":       {layer.Name},
		"QUERY_LAYERS": {layer.Name},
		"CRS":          {crs},
		"BBOX":         {"-180,-90,180,90"},
		"WIDTH":        {"256"},
		"HEIGHT":       {"256"},
		"FORMAT":       {wms.FormatPNG},
		"STYLES":       {""},
		"INFO_FORMAT":  {"text/xml"},
		"I":            {"128"},
		// No J
	}

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "MissingParameterValue")
}

// TestCoordinates_NonNumericI tests that non-numeric I returns exception.
func TestCoordinates_NonNumericI(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("I", "abc")
	params.Set("J", "128")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
}

// TestCoordinates_NonNumericJ tests that non-numeric J returns exception.
func TestCoordinates_NonNumericJ(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("I", "128")
	params.Set("J", "xyz")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
}

// TestCoordinates_BoundaryValues tests boundary values for I and J.
func TestCoordinates_BoundaryValues(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	testCases := []struct {
		name string
		i    string
		j    string
		ok   bool
	}{
		{"origin", "0", "0", true},
		{"max-valid", "255", "255", true}, // WIDTH-1, HEIGHT-1
		{"at-boundary-i", "256", "128", false},
		{"at-boundary-j", "128", "256", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := ctx.BuildGetFeatureInfoParams(layer)
			params.Set("WIDTH", "256")
			params.Set("HEIGHT", "256")
			params.Set("I", tc.i)
			params.Set("J", tc.j)

			resp, err := ctx.Client.GetFeatureInfo(params)
			if err != nil {
				t.Fatalf("GetFeatureInfo request failed: %v", err)
			}

			if tc.ok {
				wms.AssertStatusCode(t, resp, 200)
				wms.AssertNotException(t, resp)
			} else {
				wms.AssertIsException(t, resp)
			}
		})
	}
}
