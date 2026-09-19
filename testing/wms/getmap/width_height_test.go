package getmap

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestWidthHeight_Valid tests that valid WIDTH and HEIGHT work.
func TestWidthHeight_Valid(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("WIDTH", "256")
	params.Set("HEIGHT", "256")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
	wms.AssertImageDimensions(t, resp, 256, 256)
}

// TestWidthHeight_LargeSize tests that large dimensions work.
func TestWidthHeight_LargeSize(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	// Use dimensions that should be within MaxWidth/MaxHeight
	width := 1024
	height := 768

	params := ctx.BuildGetMapParams(layer)
	params.Set("WIDTH", intToString(width))
	params.Set("HEIGHT", intToString(height))

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	if resp.IsImage {
		wms.AssertStatusCode(t, resp, 200)
		wms.AssertImageDimensions(t, resp, width, height)
	} else {
		// May exceed server limits
		t.Logf("Large size request returned non-image response")
	}
}

// TestWidthHeight_SmallSize tests that small dimensions work.
func TestWidthHeight_SmallSize(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("WIDTH", "16")
	params.Set("HEIGHT", "16")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
	wms.AssertImageDimensions(t, resp, 16, 16)
}

// TestWidthHeight_ExceedsMaxWidth tests that exceeding MaxWidth returns exception.
func TestWidthHeight_ExceedsMaxWidth(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	caps := ctx.Capabilities
	if caps.Service.MaxWidth == 0 {
		t.Skip("No MaxWidth specified in capabilities")
	}

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("WIDTH", intToString(caps.Service.MaxWidth+1))
	params.Set("HEIGHT", "256")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Should return an exception
	wms.AssertIsException(t, resp)
}

// TestWidthHeight_ExceedsMaxHeight tests that exceeding MaxHeight returns exception.
func TestWidthHeight_ExceedsMaxHeight(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	caps := ctx.Capabilities
	if caps.Service.MaxHeight == 0 {
		t.Skip("No MaxHeight specified in capabilities")
	}

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("WIDTH", "256")
	params.Set("HEIGHT", intToString(caps.Service.MaxHeight+1))

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Should return an exception
	wms.AssertIsException(t, resp)
}

// TestWidthHeight_MissingWidth tests that missing WIDTH returns exception.
func TestWidthHeight_MissingWidth(t *testing.T) {
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
		// No WIDTH
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

// TestWidthHeight_MissingHeight tests that missing HEIGHT returns exception.
func TestWidthHeight_MissingHeight(t *testing.T) {
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
		// No HEIGHT
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

// TestWidthHeight_InvalidWidth tests that invalid WIDTH returns exception.
func TestWidthHeight_InvalidWidth(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	testCases := []struct {
		name  string
		width string
	}{
		{"non-numeric", "abc"},
		{"negative", "-100"},
		{"zero", "0"},
		{"float", "256.5"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := ctx.BuildGetMapParams(layer)
			params.Set("WIDTH", tc.width)

			resp, err := ctx.Client.GetMap(params)
			if err != nil {
				t.Fatalf("GetMap request failed: %v", err)
			}

			wms.AssertIsException(t, resp)
		})
	}
}

// TestWidthHeight_InvalidHeight tests that invalid HEIGHT returns exception.
func TestWidthHeight_InvalidHeight(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	testCases := []struct {
		name   string
		height string
	}{
		{"non-numeric", "abc"},
		{"negative", "-100"},
		{"zero", "0"},
		{"float", "256.5"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := ctx.BuildGetMapParams(layer)
			params.Set("HEIGHT", tc.height)

			resp, err := ctx.Client.GetMap(params)
			if err != nil {
				t.Fatalf("GetMap request failed: %v", err)
			}

			wms.AssertIsException(t, resp)
		})
	}
}

// TestWidthHeight_AspectRatio tests different aspect ratios.
func TestWidthHeight_AspectRatio(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	testCases := []struct {
		name   string
		width  int
		height int
	}{
		{"square", 256, 256},
		{"wide", 512, 256},
		{"tall", 256, 512},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := ctx.BuildGetMapParams(layer)
			params.Set("WIDTH", intToString(tc.width))
			params.Set("HEIGHT", intToString(tc.height))

			resp, err := ctx.Client.GetMap(params)
			if err != nil {
				t.Fatalf("GetMap request failed: %v", err)
			}

			wms.AssertStatusCode(t, resp, 200)
			wms.AssertIsImage(t, resp, "")
			wms.AssertImageDimensions(t, resp, tc.width, tc.height)
		})
	}
}
