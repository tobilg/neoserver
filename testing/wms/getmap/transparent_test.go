package getmap

import (
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestTransparent_Default tests default transparency (FALSE).
func TestTransparent_Default(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	// No TRANSPARENT parameter - should default to FALSE

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestTransparent_True tests TRANSPARENT=TRUE.
func TestTransparent_True(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("TRANSPARENT", "TRUE")
	params.Set("FORMAT", "image/png") // PNG supports transparency

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "png")
}

// TestTransparent_False tests TRANSPARENT=FALSE.
func TestTransparent_False(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("TRANSPARENT", "FALSE")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestTransparent_OpaqueLayer tests transparency with opaque layer.
// Reference: WMS 1.3.0 section 7.3.3.9
func TestTransparent_OpaqueLayer(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	// Find an opaque layer
	var opaqueLayer *wms.Layer
	for _, layer := range ctx.Layers {
		if layer.IsOpaque() {
			opaqueLayer = layer
			break
		}
	}

	if opaqueLayer == nil {
		t.Skip("No opaque layers available")
	}

	params := ctx.BuildGetMapParams(opaqueLayer)
	params.Set("TRANSPARENT", "TRUE")
	params.Set("FORMAT", "image/png")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Request should still work - opaque is just a hint
	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestTransparent_WithBGCOLOR tests TRANSPARENT with BGCOLOR.
func TestTransparent_WithBGCOLOR(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("TRANSPARENT", "FALSE")
	params.Set("BGCOLOR", "0xFF0000") // Red background

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "")
}

// TestTransparent_CaseInsensitive tests that TRANSPARENT value is case-insensitive.
func TestTransparent_CaseInsensitive(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	testCases := []string{"true", "True", "TRUE", "false", "False", "FALSE"}

	for _, value := range testCases {
		t.Run(value, func(t *testing.T) {
			params := ctx.BuildGetMapParams(layer)
			params.Set("TRANSPARENT", value)
			params.Set("FORMAT", "image/png")

			resp, err := ctx.Client.GetMap(params)
			if err != nil {
				t.Fatalf("GetMap request failed: %v", err)
			}

			wms.AssertStatusCode(t, resp, 200)
			wms.AssertIsImage(t, resp, "")
		})
	}
}

// TestTransparent_InvalidValue tests that invalid TRANSPARENT value is handled.
func TestTransparent_InvalidValue(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("TRANSPARENT", "INVALID")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// Server may ignore invalid value or return exception
	wms.AssertStatusCode(t, resp, 200)
}
