package getfeatureinfo

import (
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestFormat_TextXML tests that INFO_FORMAT=text/xml works.
func TestFormat_TextXML(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("INFO_FORMAT", "text/xml")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
	wms.AssertContentType(t, resp, "text/xml")
}

// TestFormat_GML tests that INFO_FORMAT=application/vnd.ogc.gml works.
func TestFormat_GML(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	// Check if GML is supported
	formats := ctx.GetFeatureInfoFormats()
	hasGML := false
	for _, f := range formats {
		if strings.Contains(strings.ToLower(f), "gml") {
			hasGML = true
			break
		}
	}
	if !hasGML {
		t.Skip("GML format not supported")
	}

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("INFO_FORMAT", "application/vnd.ogc.gml")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
}

// TestFormat_TextPlain tests that INFO_FORMAT=text/plain works.
func TestFormat_TextPlain(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	// Check if text/plain is supported
	formats := ctx.GetFeatureInfoFormats()
	hasPlain := false
	for _, f := range formats {
		if strings.EqualFold(f, "text/plain") {
			hasPlain = true
			break
		}
	}
	if !hasPlain {
		t.Skip("text/plain format not supported")
	}

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("INFO_FORMAT", "text/plain")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
	wms.AssertContentType(t, resp, "text/plain")
}

// TestFormat_TextHTML tests that INFO_FORMAT=text/html works.
func TestFormat_TextHTML(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	// Check if text/html is supported
	formats := ctx.GetFeatureInfoFormats()
	hasHTML := false
	for _, f := range formats {
		if strings.EqualFold(f, "text/html") {
			hasHTML = true
			break
		}
	}
	if !hasHTML {
		t.Skip("text/html format not supported")
	}

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("INFO_FORMAT", "text/html")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
	wms.AssertContentType(t, resp, "text/html")
}

// TestFormat_JSON tests that INFO_FORMAT=application/json works.
func TestFormat_JSON(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	// Check if JSON is supported
	formats := ctx.GetFeatureInfoFormats()
	hasJSON := false
	for _, f := range formats {
		if strings.Contains(strings.ToLower(f), "json") {
			hasJSON = true
			break
		}
	}
	if !hasJSON {
		t.Skip("JSON format not supported")
	}

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	params := ctx.BuildGetFeatureInfoParams(layer)
	params.Set("INFO_FORMAT", "application/json")

	resp, err := ctx.Client.GetFeatureInfo(params)
	if err != nil {
		t.Fatalf("GetFeatureInfo request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertNotException(t, resp)
	wms.AssertContentType(t, resp, "application/json")
}

// TestFormat_EachSupported tests each supported GetFeatureInfo format.
func TestFormat_EachSupported(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoGetFeatureInfo(t)
	skipIfNoQueryableLayers(t)

	layer := ctx.GetFirstQueryableLayer()
	if layer == nil {
		t.Fatal("No queryable layers available")
	}

	formats := ctx.GetFeatureInfoFormats()
	if len(formats) == 0 {
		t.Skip("No GetFeatureInfo formats advertised")
	}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			params := ctx.BuildGetFeatureInfoParams(layer)
			params.Set("INFO_FORMAT", format)

			resp, err := ctx.Client.GetFeatureInfo(params)
			if err != nil {
				t.Fatalf("GetFeatureInfo request failed: %v", err)
			}

			wms.AssertStatusCode(t, resp, 200)
			wms.AssertNotException(t, resp)
		})
	}
}

// TestFormat_Invalid tests that invalid INFO_FORMAT returns exception.
func TestFormat_Invalid(t *testing.T) {
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
