package getfeatureinfo

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestExceptions_InvalidInfoFormat tests that invalid INFO_FORMAT returns InvalidFormat exception.
// Reference: WMS 1.3.0 section 7.4.3.7
func TestExceptions_InvalidInfoFormat(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("INFO_FORMAT", "invalid/format")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "InvalidFormat")
}

// TestExceptions_LayerNotQueryable tests that querying non-queryable layer returns exception.
// Reference: WMS 1.3.0 section 7.4.3.5
func TestExceptions_LayerNotQueryable(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoNonQueryableLayers(t)

	nonQueryableLayer := ctx.GetNonQueryableLayer()
	if nonQueryableLayer == nil {
		t.Fatal("No non-queryable layers available")
	}

	// Build params with non-queryable layer
	crs := nonQueryableLayer.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	params := url.Values{
		"SERVICE":      {"WMS"},
		"VERSION":      {"1.3.0"},
		"REQUEST":      {"GetFeatureInfo"},
		"LAYERS":       {nonQueryableLayer.Name},
		"QUERY_LAYERS": {nonQueryableLayer.Name},
		"CRS":          {crs},
		"BBOX":         {"-180,-90,180,90"},
		"WIDTH":        {"256"},
		"HEIGHT":       {"256"},
		"FORMAT":       {wms.FormatPNG},
		"STYLES":       {""},
		"INFO_FORMAT":  {"text/xml"},
		"I":            {"128"},
		"J":            {"128"},
	}

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "LayerNotQueryable")
}

// TestExceptions_InvalidLayer tests that invalid layer returns LayerNotDefined exception.
func TestExceptions_InvalidLayer(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)

	params := url.Values{
		"SERVICE":      {"WMS"},
		"VERSION":      {"1.3.0"},
		"REQUEST":      {"GetFeatureInfo"},
		"LAYERS":       {"INVALID_LAYER"},
		"QUERY_LAYERS": {"INVALID_LAYER"},
		"CRS":          {"CRS:84"},
		"BBOX":         {"-180,-90,180,90"},
		"WIDTH":        {"256"},
		"HEIGHT":       {"256"},
		"FORMAT":       {wms.FormatPNG},
		"STYLES":       {""},
		"INFO_FORMAT":  {"text/xml"},
		"I":            {"128"},
		"J":            {"128"},
	}

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "LayerNotDefined")
}

// TestExceptions_MissingQueryLayers tests that missing QUERY_LAYERS returns exception.
func TestExceptions_MissingQueryLayers(t *testing.T) {
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
		"SERVICE":     {"WMS"},
		"VERSION":     {"1.3.0"},
		"REQUEST":     {"GetFeatureInfo"},
		"LAYERS":      {layer.Name},
		// No QUERY_LAYERS
		"CRS":         {crs},
		"BBOX":        {"-180,-90,180,90"},
		"WIDTH":       {"256"},
		"HEIGHT":      {"256"},
		"FORMAT":      {wms.FormatPNG},
		"STYLES":      {""},
		"INFO_FORMAT": {"text/xml"},
		"I":           {"128"},
		"J":           {"128"},
	}

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "MissingParameterValue")
}
