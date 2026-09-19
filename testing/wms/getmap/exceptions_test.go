package getmap

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestExceptions_Default tests that default exception format is XML.
func TestExceptions_Default(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	// Request with invalid layer to trigger exception
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
		// No EXCEPTIONS parameter - should default to XML
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertContentType(t, resp, "text/xml")
}

// TestExceptions_XML tests that EXCEPTIONS=XML returns XML exception.
func TestExceptions_XML(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	params := url.Values{
		"SERVICE":    {"WMS"},
		"VERSION":    {"1.3.0"},
		"REQUEST":    {"GetMap"},
		"LAYERS":     {"INVALID_LAYER_NAME_12345"},
		"CRS":        {"CRS:84"},
		"BBOX":       {"-180,-90,180,90"},
		"WIDTH":      {"256"},
		"HEIGHT":     {"256"},
		"FORMAT":     {wms.FormatPNG},
		"STYLES":     {""},
		"EXCEPTIONS": {"XML"},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertContentType(t, resp, "text/xml")
}

// TestExceptions_INIMAGE tests that EXCEPTIONS=INIMAGE returns an image.
// Reference: WMS 1.3.0 section 7.3.3.11
func TestExceptions_INIMAGE(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	// Check if INIMAGE is supported
	formats := ctx.GetExceptionFormats()
	hasINIMAGE := false
	for _, f := range formats {
		if strings.EqualFold(f, "INIMAGE") {
			hasINIMAGE = true
			break
		}
	}
	if !hasINIMAGE {
		t.Skip("INIMAGE exception format not supported")
	}

	params := url.Values{
		"SERVICE":    {"WMS"},
		"VERSION":    {"1.3.0"},
		"REQUEST":    {"GetMap"},
		"LAYERS":     {"INVALID_LAYER_NAME_12345"},
		"CRS":        {"CRS:84"},
		"BBOX":       {"-180,-90,180,90"},
		"WIDTH":      {"256"},
		"HEIGHT":     {"256"},
		"FORMAT":     {wms.FormatPNG},
		"STYLES":     {""},
		"EXCEPTIONS": {"INIMAGE"},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// INIMAGE should return an image, not XML
	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "png")
}

// TestExceptions_BLANK tests that EXCEPTIONS=BLANK returns a blank image.
// Reference: WMS 1.3.0 section 7.3.3.11
func TestExceptions_BLANK(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	// Check if BLANK is supported
	formats := ctx.GetExceptionFormats()
	hasBLANK := false
	for _, f := range formats {
		if strings.EqualFold(f, "BLANK") {
			hasBLANK = true
			break
		}
	}
	if !hasBLANK {
		t.Skip("BLANK exception format not supported")
	}

	params := url.Values{
		"SERVICE":    {"WMS"},
		"VERSION":    {"1.3.0"},
		"REQUEST":    {"GetMap"},
		"LAYERS":     {"INVALID_LAYER_NAME_12345"},
		"CRS":        {"CRS:84"},
		"BBOX":       {"-180,-90,180,90"},
		"WIDTH":      {"256"},
		"HEIGHT":     {"256"},
		"FORMAT":     {wms.FormatPNG},
		"STYLES":     {""},
		"EXCEPTIONS": {"BLANK"},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// BLANK should return an image
	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "png")
}

// TestExceptions_BLANK_WithBGCOLOR tests EXCEPTIONS=BLANK with BGCOLOR.
func TestExceptions_BLANK_WithBGCOLOR(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	// Check if BLANK is supported
	formats := ctx.GetExceptionFormats()
	hasBLANK := false
	for _, f := range formats {
		if strings.EqualFold(f, "BLANK") {
			hasBLANK = true
			break
		}
	}
	if !hasBLANK {
		t.Skip("BLANK exception format not supported")
	}

	params := url.Values{
		"SERVICE":     {"WMS"},
		"VERSION":     {"1.3.0"},
		"REQUEST":     {"GetMap"},
		"LAYERS":      {"INVALID_LAYER_NAME_12345"},
		"CRS":         {"CRS:84"},
		"BBOX":        {"-180,-90,180,90"},
		"WIDTH":       {"256"},
		"HEIGHT":      {"256"},
		"FORMAT":      {wms.FormatPNG},
		"STYLES":      {""},
		"EXCEPTIONS":  {"BLANK"},
		"BGCOLOR":     {"0xFF0000"}, // Red
		"TRANSPARENT": {"FALSE"},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// BLANK should return an image with the background color
	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "png")
}

// TestExceptions_BLANK_Transparent tests EXCEPTIONS=BLANK with TRANSPARENT=TRUE.
func TestExceptions_BLANK_Transparent(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	// Check if BLANK is supported
	formats := ctx.GetExceptionFormats()
	hasBLANK := false
	for _, f := range formats {
		if strings.EqualFold(f, "BLANK") {
			hasBLANK = true
			break
		}
	}
	if !hasBLANK {
		t.Skip("BLANK exception format not supported")
	}

	params := url.Values{
		"SERVICE":     {"WMS"},
		"VERSION":     {"1.3.0"},
		"REQUEST":     {"GetMap"},
		"LAYERS":      {"INVALID_LAYER_NAME_12345"},
		"CRS":         {"CRS:84"},
		"BBOX":        {"-180,-90,180,90"},
		"WIDTH":       {"256"},
		"HEIGHT":      {"256"},
		"FORMAT":      {wms.FormatPNG},
		"STYLES":      {""},
		"EXCEPTIONS":  {"BLANK"},
		"TRANSPARENT": {"TRUE"},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	// BLANK should return a transparent image
	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "png")
}
