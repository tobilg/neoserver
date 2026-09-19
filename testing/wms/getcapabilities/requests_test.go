package getcapabilities

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
	"github.com/tobilg/neoserver/testing/wms/capabilities"
)

// TestFormat_NoFormat tests that when no FORMAT parameter is supplied,
// the response is capabilities XML with MIME type text/xml.
// Reference: WMS 1.3.0 section 7.2.3.1
func TestFormat_NoFormat(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetCapabilities"},
		// No FORMAT parameter
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertContentType(t, resp, "text/xml")
	wms.AssertIsXML(t, resp)

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities response: %v", err)
	}

	wms.AssertCapabilitiesVersion(t, caps, "1.3.0")
}

// TestFormat_TextXML tests that FORMAT=text/xml returns text/xml.
// Reference: WMS 1.3.0 section 7.2.3.1
func TestFormat_TextXML(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetCapabilities"},
		"FORMAT":  {"text/xml"},
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertContentType(t, resp, "text/xml")
	wms.AssertIsXML(t, resp)
}

// TestFormat_Invalid tests that when an invalid FORMAT parameter is supplied,
// the response is still capabilities XML with MIME type text/xml.
// Reference: WMS 1.3.0 section 7.2.3.1
func TestFormat_Invalid(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetCapabilities"},
		"FORMAT":  {"invalid/format"},
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	// Server should return text/xml even for invalid format
	wms.AssertStatusCode(t, resp, 200)
	wms.AssertContentType(t, resp, "text/xml")
	wms.AssertIsXML(t, resp)

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities response: %v", err)
	}

	wms.AssertCapabilitiesVersion(t, caps, "1.3.0")
}

// TestFormat_EachSupportedFormat tests that each supported format returns the correct MIME type.
// Reference: WMS 1.3.0 section 7.2.3.1
func TestFormat_EachSupportedFormat(t *testing.T) {
	ctx := getTestContext(t)

	formats := ctx.Capabilities.GetCapabilitiesFormats()
	if len(formats) == 0 {
		t.Skip("No GetCapabilities formats advertised")
	}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			params := url.Values{
				"SERVICE": {"WMS"},
				"VERSION": {"1.3.0"},
				"REQUEST": {"GetCapabilities"},
				"FORMAT":  {format},
			}

			resp, err := ctx.Client.GetCapabilities(params)
			if err != nil {
				t.Fatalf("GetCapabilities request failed: %v", err)
			}

			wms.AssertStatusCode(t, resp, 200)
			// Content-Type should match or contain the requested format
			if !strings.Contains(strings.ToLower(resp.ContentType), strings.ToLower(format)) &&
				!strings.Contains(strings.ToLower(resp.ContentType), "text/xml") {
				t.Errorf("Expected content type containing %q, got %q", format, resp.ContentType)
			}
		})
	}
}

// TestUpdateSequence_Ignored tests that when no updateSequence number is advertised,
// the UPDATESEQUENCE parameter is ignored.
// Reference: WMS 1.3.0 section 7.2.3.5
func TestUpdateSequence_Ignored(t *testing.T) {
	ctx := getTestContext(t)

	if ctx.Capabilities.UpdateSequence != "" {
		t.Skip("UpdateSequence is advertised, testing with value instead")
	}

	params := url.Values{
		"SERVICE":        {"WMS"},
		"VERSION":        {"1.3.0"},
		"REQUEST":        {"GetCapabilities"},
		"UPDATESEQUENCE": {"ignored"},
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsXML(t, resp)

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities response: %v", err)
	}

	wms.AssertCapabilitiesVersion(t, caps, "1.3.0")
}

// TestUpdateSequence_Current tests that when UPDATESEQUENCE equals the current value,
// the server returns CurrentUpdateSequence exception.
// Reference: WMS 1.3.0 section 7.2.3.5
func TestUpdateSequence_Current(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoUpdateSequence(t)

	params := url.Values{
		"SERVICE":        {"WMS"},
		"VERSION":        {"1.3.0"},
		"REQUEST":        {"GetCapabilities"},
		"UPDATESEQUENCE": {ctx.Capabilities.UpdateSequence},
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "CurrentUpdateSequence")
}

// TestUpdateSequence_Lower tests that when UPDATESEQUENCE is lower than current,
// the server returns capabilities XML.
// Reference: WMS 1.3.0 section 7.2.3.5
func TestUpdateSequence_Lower(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoUpdateSequence(t)

	// Try a lower value (0 should always be lower)
	params := url.Values{
		"SERVICE":        {"WMS"},
		"VERSION":        {"1.3.0"},
		"REQUEST":        {"GetCapabilities"},
		"UPDATESEQUENCE": {"0"},
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsXML(t, resp)
	wms.AssertNotException(t, resp)

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities response: %v", err)
	}

	wms.AssertCapabilitiesVersion(t, caps, "1.3.0")
}

// TestUpdateSequence_Higher tests that when UPDATESEQUENCE is higher than current,
// the server returns InvalidUpdateSequence exception.
// Reference: WMS 1.3.0 section 7.2.3.5
func TestUpdateSequence_Higher(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoUpdateSequence(t)

	// Use a very high value
	params := url.Values{
		"SERVICE":        {"WMS"},
		"VERSION":        {"1.3.0"},
		"REQUEST":        {"GetCapabilities"},
		"UPDATESEQUENCE": {"999999999"},
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "InvalidUpdateSequence")
}

// TestServiceParameter tests that SERVICE=WMS is handled correctly.
func TestServiceParameter(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetCapabilities"},
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsXML(t, resp)

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities response: %v", err)
	}

	if caps.Service.Name != "WMS" {
		t.Errorf("Expected service name 'WMS', got %q", caps.Service.Name)
	}
}

// TestServiceParameter_Invalid tests behavior with invalid SERVICE parameter.
// Note: Some servers return an exception, others ignore and return capabilities.
func TestServiceParameter_Invalid(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"INVALID"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetCapabilities"},
	}

	// Use raw Request to avoid the client overriding SERVICE
	resp, err := ctx.Client.Request(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	// Server may return exception or ignore invalid SERVICE and return capabilities
	if !wms.IsExceptionResponse(resp) {
		// Server returned capabilities - verify it's valid
		_, err := capabilities.Parse(resp.Body)
		if err != nil {
			t.Fatalf("Failed to parse capabilities: %v", err)
		}
		t.Log("Server ignores invalid SERVICE parameter and returns capabilities (lenient behavior)")
	}
}
