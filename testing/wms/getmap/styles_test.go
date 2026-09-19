package getmap

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestStyles_Empty tests that an empty STYLES parameter works.
func TestStyles_Empty(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("STYLES", "")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestStyles_Default tests that default style works.
func TestStyles_Default(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	// Get first style if available
	styleName := ""
	if len(layer.Style) > 0 {
		styleName = layer.Style[0].Name
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("STYLES", styleName)

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestStyles_Invalid tests that an invalid style returns StyleNotDefined exception.
// Reference: WMS 1.3.0 section 7.3.3.4
func TestStyles_Invalid(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("STYLES", "INVALID_STYLE_NAME_12345")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "StyleNotDefined")
}

// TestStyles_Missing tests that missing STYLES returns exception.
func TestStyles_Missing(t *testing.T) {
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

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		// No STYLES
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "MissingParameterValue")
}

// TestStyles_TwoLayers tests STYLES with two layers.
func TestStyles_TwoLayers(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	if ctx.LayerCount() < 2 {
		t.Skip("Need at least 2 layers for this test")
	}

	layers := ctx.Layers[:2]
	layerNames := layers[0].Name + "," + layers[1].Name

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layerNames},
		"CRS":     {"CRS:84"},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {","}, // Two empty styles for two layers
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestStyles_ThreeLayers tests STYLES with three layers.
func TestStyles_ThreeLayers(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	if ctx.LayerCount() < 3 {
		t.Skip("Need at least 3 layers for this test")
	}

	layers := ctx.Layers[:3]
	layerNames := layers[0].Name + "," + layers[1].Name + "," + layers[2].Name

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layerNames},
		"CRS":     {"CRS:84"},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {",,"}, // Three empty styles
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestStyles_FirstInvalid tests that when first style is invalid, returns exception.
func TestStyles_FirstInvalid(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	if ctx.LayerCount() < 2 {
		t.Skip("Need at least 2 layers for this test")
	}

	layers := ctx.Layers[:2]
	layerNames := layers[0].Name + "," + layers[1].Name

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layerNames},
		"CRS":     {"CRS:84"},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {"INVALID_STYLE,"}, // First style invalid
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "StyleNotDefined")
}

// TestStyles_SecondInvalid tests that when second style is invalid, returns exception.
func TestStyles_SecondInvalid(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	if ctx.LayerCount() < 2 {
		t.Skip("Need at least 2 layers for this test")
	}

	layers := ctx.Layers[:2]
	layerNames := layers[0].Name + "," + layers[1].Name

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layerNames},
		"CRS":     {"CRS:84"},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		"FORMAT":  {wms.FormatPNG},
		"STYLES":  {",INVALID_STYLE"}, // Second style invalid
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "StyleNotDefined")
}

// TestStyles_EachStyle tests each available style.
func TestStyles_EachStyle(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	// Find a layer with styles
	var layerWithStyles *wms.Layer
	for _, layer := range ctx.Layers {
		if len(layer.Style) > 0 {
			layerWithStyles = layer
			break
		}
	}

	if layerWithStyles == nil {
		t.Skip("No layers with styles found")
	}

	for _, style := range layerWithStyles.Style {
		t.Run(style.Name, func(t *testing.T) {
			params := ctx.BuildGetMapParams(layerWithStyles)
			params.Set("STYLES", style.Name)

			resp, err := ctx.Client.GetMap(params)
			if err != nil {
				t.Fatalf("GetMap request failed: %v", err)
			}

			wms.AssertStatusCode(t, resp, 200)
			wms.AssertIsImage(t, resp, "")
		})
	}
}
