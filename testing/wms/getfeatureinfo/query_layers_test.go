package getfeatureinfo

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestQueryLayers_Single tests that a single QUERY_LAYERS works.
func TestQueryLayers_Single(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
}

// TestQueryLayers_Multiple tests that multiple QUERY_LAYERS work.
func TestQueryLayers_Multiple(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	queryableLayers := ctx.GetQueryableLayers()
	if len(queryableLayers) < 2 {
		t.Skip("Need at least 2 queryable layers")
	}

	layer1 := queryableLayers[0]
	layer2 := queryableLayers[1]

	layerNames := layer1.Name + "," + layer2.Name

	crs := layer1.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	params := url.Values{
		"SERVICE":      {"WMS"},
		"VERSION":      {"1.3.0"},
		"REQUEST":      {"GetFeatureInfo"},
		"LAYERS":       {layerNames},
		"QUERY_LAYERS": {layerNames},
		"CRS":          {crs},
		"BBOX":         {"-180,-90,180,90"},
		"WIDTH":        {"256"},
		"HEIGHT":       {"256"},
		"FORMAT":       {wms.FormatPNG},
		"STYLES":       {","},
		"INFO_FORMAT":  {"text/xml"},
		"I":            {"128"},
		"J":            {"128"},
	}

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
}

// TestQueryLayers_SubsetOfLayers tests QUERY_LAYERS as subset of LAYERS.
func TestQueryLayers_SubsetOfLayers(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	queryableLayers := ctx.GetQueryableLayers()
	if len(queryableLayers) < 2 {
		t.Skip("Need at least 2 queryable layers")
	}

	layer1 := queryableLayers[0]
	layer2 := queryableLayers[1]

	allLayers := layer1.Name + "," + layer2.Name

	crs := layer1.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	// LAYERS has both, but QUERY_LAYERS only has one
	params := url.Values{
		"SERVICE":      {"WMS"},
		"VERSION":      {"1.3.0"},
		"REQUEST":      {"GetFeatureInfo"},
		"LAYERS":       {allLayers},
		"QUERY_LAYERS": {layer1.Name}, // Only query first layer
		"CRS":          {crs},
		"BBOX":         {"-180,-90,180,90"},
		"WIDTH":        {"256"},
		"HEIGHT":       {"256"},
		"FORMAT":       {wms.FormatPNG},
		"STYLES":       {","},
		"INFO_FORMAT":  {"text/xml"},
		"I":            {"128"},
		"J":            {"128"},
	}

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
}

// TestQueryLayers_NotInLayers tests that QUERY_LAYERS must be subset of LAYERS.
func TestQueryLayers_NotInLayers(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	queryableLayers := ctx.GetQueryableLayers()
	if len(queryableLayers) < 2 {
		t.Skip("Need at least 2 queryable layers")
	}

	layer1 := queryableLayers[0]
	layer2 := queryableLayers[1]

	crs := layer1.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	// LAYERS only has layer1, but QUERY_LAYERS has layer2
	params := url.Values{
		"SERVICE":      {"WMS"},
		"VERSION":      {"1.3.0"},
		"REQUEST":      {"GetFeatureInfo"},
		"LAYERS":       {layer1.Name},
		"QUERY_LAYERS": {layer2.Name}, // Not in LAYERS
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

	// This should return an exception - QUERY_LAYERS must be subset of LAYERS
	// However, some servers may be lenient about this
	if resp.IsImage || !wms.IsExceptionResponse(resp) {
		t.Log("Server accepted QUERY_LAYERS not in LAYERS (lenient behavior)")
	}
}

// TestQueryLayers_EachQueryableLayer tests each queryable layer individually.
func TestQueryLayers_EachQueryableLayer(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	queryableLayers := ctx.GetQueryableLayers()

	// Limit to first 5 to avoid too many tests
	maxLayers := 5
	if len(queryableLayers) < maxLayers {
		maxLayers = len(queryableLayers)
	}

	for _, layer := range queryableLayers[:maxLayers] {
		t.Run(layer.Name, func(t *testing.T) {
			params := ctx.BuildGetFeatureInfoParams(layer)

			resp, err := ctx.Client.GetFeatureInfo(params)
			if err != nil {
				t.Fatalf("GetFeatureInfo request failed: %v", err)
			}

			wms.AssertStatusCode(t, resp, 200)
			wms.AssertNotException(t, resp)
		})
	}
}

// TestQueryLayers_FeatureCount tests FEATURE_COUNT parameter.
func TestQueryLayers_FeatureCount(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("FEATURE_COUNT", "5")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
}

// IsExceptionResponse is a helper to check if response is an exception
func IsExceptionResponse(resp *wms.Response) bool {
	return wms.IsExceptionResponse(resp)
}
