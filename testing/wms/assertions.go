package wms

import (
	"encoding/xml"
	"mime"
	"strconv"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wms/capabilities"
	"github.com/tobilg/neoserver/testing/wms/exceptions"
)

// AssertStatusCode asserts that the response has the expected status code.
func AssertStatusCode(t *testing.T, resp *Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Errorf("Expected status code %d, got %d", expected, resp.StatusCode)
	}
}

// AssertMediaType compares media types while ignoring optional parameters such
// as the WMS image/png mode qualifier and HTTP charset parameters.
func AssertMediaType(t *testing.T, resp *Response, expected string) {
	t.Helper()
	actualType, _, actualErr := mime.ParseMediaType(resp.ContentType)
	expectedType, _, expectedErr := mime.ParseMediaType(expected)
	if actualErr != nil || expectedErr != nil || !strings.EqualFold(actualType, expectedType) {
		t.Errorf("Expected media type %q, got %q", expected, resp.ContentType)
	}
}

// AssertContentType asserts that the response has the expected content type.
func AssertContentType(t *testing.T, resp *Response, expected string) {
	t.Helper()
	ct := strings.ToLower(resp.ContentType)
	exp := strings.ToLower(expected)
	if !strings.Contains(ct, exp) {
		t.Errorf("Expected content type containing %q, got %q", expected, resp.ContentType)
	}
}

// AssertIsImage asserts that the response is an image of the expected format.
func AssertIsImage(t *testing.T, resp *Response, format string) {
	t.Helper()
	if !resp.IsImage {
		t.Errorf("Expected image response, got content type %q", resp.ContentType)
		return
	}
	if format != "" {
		expected := strings.ToLower(strings.TrimPrefix(format, "image/"))
		if resp.ImageFormat != expected {
			t.Errorf("Expected image format %q, got %q", expected, resp.ImageFormat)
		}
	}
}

// AssertIsXML asserts that the response is XML.
func AssertIsXML(t *testing.T, resp *Response) {
	t.Helper()
	if !resp.IsXML {
		t.Errorf("Expected XML response, got content type %q", resp.ContentType)
	}
}

// AssertIsException asserts that the response is a ServiceExceptionReport.
func AssertIsException(t *testing.T, resp *Response) {
	t.Helper()
	if !exceptions.IsException(resp.Body) {
		t.Errorf("Expected ServiceExceptionReport, got: %s", truncate(string(resp.Body), 200))
	}
}

// AssertNotException asserts that the response is NOT a ServiceExceptionReport.
func AssertNotException(t *testing.T, resp *Response) {
	t.Helper()
	if exceptions.IsException(resp.Body) {
		ex, _ := exceptions.Parse(resp.Body)
		if ex != nil {
			t.Errorf("Unexpected exception: code=%q message=%q", ex.GetCode(), ex.GetMessage())
		} else {
			t.Errorf("Unexpected exception response")
		}
	}
}

// AssertExceptionCode asserts that the response is an exception with the expected code.
func AssertExceptionCode(t *testing.T, resp *Response, expectedCode string) {
	t.Helper()
	AssertIsException(t, resp)
	ex, err := exceptions.Parse(resp.Body)
	if err != nil {
		t.Errorf("Failed to parse exception: %v", err)
		return
	}
	if !ex.HasCode(expectedCode) {
		t.Errorf("Expected exception code %q, got %v", expectedCode, ex.GetCodes())
	}
}

// AssertImageDimensions asserts that the image has the expected dimensions.
func AssertImageDimensions(t *testing.T, resp *Response, width, height int) {
	t.Helper()
	if !resp.IsImage || resp.Image == nil {
		t.Errorf("Response is not a valid image")
		return
	}
	if resp.ImageWidth() != width {
		t.Errorf("Expected image width %d, got %d", width, resp.ImageWidth())
	}
	if resp.ImageHeight() != height {
		t.Errorf("Expected image height %d, got %d", height, resp.ImageHeight())
	}
}

// AssertSVGDimensions verifies that a vector GetMap response is an SVG with
// the requested pixel dimensions. SVG is an image media type, but it is not a
// raster format understood by image.Decode.
func AssertSVGDimensions(t *testing.T, resp *Response, width, height int) {
	t.Helper()
	var document struct {
		XMLName xml.Name
		Width   string `xml:"width,attr"`
		Height  string `xml:"height,attr"`
	}
	if err := xml.Unmarshal(resp.Body, &document); err != nil {
		t.Fatalf("Response is not valid SVG XML: %v", err)
	}
	if document.XMLName.Local != "svg" {
		t.Fatalf("Expected SVG root element, got %q", document.XMLName.Local)
	}
	parseDimension := func(value string) (int, error) {
		value = strings.TrimSpace(strings.TrimSuffix(value, "px"))
		return strconv.Atoi(value)
	}
	actualWidth, widthErr := parseDimension(document.Width)
	actualHeight, heightErr := parseDimension(document.Height)
	if widthErr != nil || heightErr != nil {
		t.Fatalf("SVG has invalid dimensions width=%q height=%q", document.Width, document.Height)
	}
	if actualWidth != width || actualHeight != height {
		t.Errorf("Expected SVG dimensions %dx%d, got %dx%d", width, height, actualWidth, actualHeight)
	}
}

// AssertCapabilitiesVersion asserts that the capabilities has the expected version.
func AssertCapabilitiesVersion(t *testing.T, caps *capabilities.Capabilities, version string) {
	t.Helper()
	if caps.Version != version {
		t.Errorf("Expected capabilities version %q, got %q", version, caps.Version)
	}
}

// AssertLayerExists asserts that a layer with the given name exists.
func AssertLayerExists(t *testing.T, caps *capabilities.Capabilities, layerName string) {
	t.Helper()
	layer := caps.GetLayerByName(layerName)
	if layer == nil {
		t.Errorf("Layer %q not found in capabilities", layerName)
	}
}

// AssertLayerHasCRS asserts that the layer supports the given CRS.
func AssertLayerHasCRS(t *testing.T, layer *capabilities.Layer, crs string) {
	t.Helper()
	for _, c := range layer.CRS {
		if strings.EqualFold(c, crs) {
			return
		}
	}
	t.Errorf("Layer %q does not support CRS %q, available: %v", layer.Name, crs, layer.CRS)
}

// AssertLayerQueryable asserts that the layer is queryable.
func AssertLayerQueryable(t *testing.T, layer *capabilities.Layer) {
	t.Helper()
	if !layer.IsQueryable() {
		t.Errorf("Layer %q is not queryable", layer.Name)
	}
}

// AssertLayerNotQueryable asserts that the layer is not queryable.
func AssertLayerNotQueryable(t *testing.T, layer *capabilities.Layer) {
	t.Helper()
	if layer.IsQueryable() {
		t.Errorf("Layer %q should not be queryable", layer.Name)
	}
}

// AssertWarningHeader asserts that the response has a Warning header containing the pattern.
func AssertWarningHeader(t *testing.T, resp *Response, pattern string) {
	t.Helper()
	warning := resp.Headers.Get("Warning")
	if warning == "" {
		t.Errorf("Expected Warning header, but none present")
		return
	}
	if !strings.Contains(strings.ToLower(warning), strings.ToLower(pattern)) {
		t.Errorf("Expected Warning header to contain %q, got %q", pattern, warning)
	}
}

// AssertOnlineResourceSuffix asserts that the OnlineResource href ends with ? or &.
func AssertOnlineResourceSuffix(t *testing.T, href string) {
	t.Helper()
	if href == "" {
		t.Errorf("OnlineResource href is empty")
		return
	}
	if !strings.HasSuffix(href, "?") && !strings.HasSuffix(href, "&") {
		t.Errorf("OnlineResource href should end with ? or &, got %q", href)
	}
}

// AssertFormatSupported asserts that the format is in the list of supported formats.
func AssertFormatSupported(t *testing.T, formats []string, format string) {
	t.Helper()
	for _, f := range formats {
		if strings.EqualFold(f, format) {
			return
		}
	}
	t.Errorf("Format %q not in supported formats: %v", format, formats)
}

// truncate truncates a string to the given length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// IsExceptionResponse checks if the response is a ServiceExceptionReport.
func IsExceptionResponse(resp *Response) bool {
	return exceptions.IsException(resp.Body)
}
