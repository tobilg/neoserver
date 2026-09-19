package getmap

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestLayers_Single tests that a single layer request works.
func TestLayers_Single(t *testing.T) {
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
}

// TestLayers_TwoLayers tests that requesting two layers works.
func TestLayers_TwoLayers(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	if ctx.LayerCount() < 2 {
		t.Skip("Need at least 2 layers for this test")
	}

	// Get two layers
	layers := ctx.Layers[:2]
	layerNames := layers[0].Name + "," + layers[1].Name

	// Find a common CRS
	crs := findCommonCRS(layers[0].CRS, layers[1].CRS)
	if crs == "" {
		t.Skip("No common CRS between layers")
	}

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layerNames},
		"CRS":     {crs},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {","},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestLayers_ThreeLayers tests that requesting three layers works.
func TestLayers_ThreeLayers(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	if ctx.LayerCount() < 3 {
		t.Skip("Need at least 3 layers for this test")
	}

	// Get three layers
	layers := ctx.Layers[:3]
	layerNames := layers[0].Name + "," + layers[1].Name + "," + layers[2].Name

	// Find a common CRS
	crs := "CRS:84" // Default to CRS:84

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layerNames},
		"CRS":     {crs},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {",,"},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestLayers_Invalid tests that an invalid layer returns LayerNotDefined exception.
// Reference: WMS 1.3.0 section 7.3.3.3
func TestLayers_Invalid(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {"INVALID_LAYER_NAME_12345"},
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

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "LayerNotDefined")
}

// TestLayers_FirstInvalid tests that when first layer is invalid, returns exception.
func TestLayers_FirstInvalid(t *testing.T) {
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
		"LAYERS":  {"INVALID_LAYER," + layer.Name},
		"CRS":     {"CRS:84"},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {","},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "LayerNotDefined")
}

// TestLayers_SecondInvalid tests that when second layer is invalid, returns exception.
func TestLayers_SecondInvalid(t *testing.T) {
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
		"LAYERS":  {layer.Name + ",INVALID_LAYER"},
		"CRS":     {"CRS:84"},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {","},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "LayerNotDefined")
}

// TestLayers_Missing tests that missing LAYERS returns exception.
func TestLayers_Missing(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		// No LAYERS
		"CRS":    {"CRS:84"},
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

// TestLayers_EachLayer tests each named layer individually.
func TestLayers_EachLayer(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	// Limit to first 10 layers to avoid too many tests
	maxLayers := 10
	layers := ctx.Layers
	if len(layers) > maxLayers {
		layers = layers[:maxLayers]
	}

	for _, layer := range layers {
		t.Run(layer.Name, func(t *testing.T) {
			params := ctx.BuildGetMapParams(layer)

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

// TestLayers_LayerLimit tests that exceeding LayerLimit returns exception.
func TestLayers_LayerLimit(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	caps := ctx.Capabilities
	if caps.Service.LayerLimit == 0 {
		t.Skip("No LayerLimit specified in capabilities")
	}

	// Try to request more layers than the limit
	limit := caps.Service.LayerLimit
	if ctx.LayerCount() <= limit {
		t.Skip("Not enough layers to exceed LayerLimit")
	}

	// Build a request with too many layers
	layers := ctx.Layers[:limit+1]
	var layerNames []string
	var styles []string
	for _, layer := range layers {
		layerNames = append(layerNames, layer.Name)
		styles = append(styles, "")
	}

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {strings.Join(layerNames, ",")},
		"CRS":     {"CRS:84"},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {strings.Join(styles, ",")},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Should return an exception for exceeding layer limit
	wms.AssertIsException(t, resp)
}

func findCommonCRS(crs1, crs2 []string) string {
	for _, c1 := range crs1 {
		for _, c2 := range crs2 {
			if strings.EqualFold(c1, c2) {
				return c1
			}
		}
	}
	return ""
}
