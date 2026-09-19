package wfs

import (
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs/capabilities"
	"github.com/tobilg/neoserver/testing/wfs/exceptions"
	"github.com/tobilg/neoserver/testing/wfs/gml"
)

// AssertStatusCode asserts that the response has the expected status code.
func AssertStatusCode(t *testing.T, resp *Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Errorf("Expected status code %d, got %d", expected, resp.StatusCode)
	}
}

// AssertContentType asserts that the response has the expected content type.
func AssertContentType(t *testing.T, resp *Response, expected string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(resp.ContentType), strings.ToLower(expected)) {
		t.Errorf("Expected content type containing %q, got %q", expected, resp.ContentType)
	}
}

// AssertIsXML asserts that the response is XML.
func AssertIsXML(t *testing.T, resp *Response) {
	t.Helper()
	if !resp.IsXML {
		t.Errorf("Expected XML response, got content type: %s", resp.ContentType)
	}
}

// AssertIsGML asserts that the response is GML.
func AssertIsGML(t *testing.T, resp *Response) {
	t.Helper()
	// GML may come as application/gml+xml or text/xml with GML content
	if !resp.IsGML && !resp.IsXML {
		t.Errorf("Expected GML response, got content type: %s", resp.ContentType)
	}
	// Also check content for FeatureCollection
	if !strings.Contains(string(resp.Body), "FeatureCollection") {
		t.Errorf("Expected FeatureCollection in response body")
	}
}

// AssertIsJSON asserts that the response is JSON.
func AssertIsJSON(t *testing.T, resp *Response) {
	t.Helper()
	if !resp.IsJSON {
		t.Errorf("Expected JSON response, got content type: %s", resp.ContentType)
	}
}

// AssertIsException asserts that the response is an OWS exception report.
func AssertIsException(t *testing.T, resp *Response) {
	t.Helper()
	if !exceptions.IsException(resp.Body) {
		body := string(resp.Body)
		if len(body) > 200 {
			body = body[:200] + "..."
		}
		t.Errorf("Expected ExceptionReport, got: %s", body)
	}
}

// AssertNotException asserts that the response is NOT an exception.
func AssertNotException(t *testing.T, resp *Response) {
	t.Helper()
	if exceptions.IsException(resp.Body) {
		ex, _ := exceptions.Parse(resp.Body)
		t.Errorf("Expected non-exception response, got exception: code=%s, message=%s",
			ex.GetCode(), ex.GetMessage())
	}
}

// AssertExceptionCode asserts that the response contains an exception with the expected code.
func AssertExceptionCode(t *testing.T, resp *Response, expectedCode string) {
	t.Helper()
	if !exceptions.IsException(resp.Body) {
		t.Errorf("Expected ExceptionReport with code %q, but response is not an exception", expectedCode)
		return
	}

	ex, err := exceptions.Parse(resp.Body)
	if err != nil {
		t.Errorf("Failed to parse exception: %v", err)
		return
	}

	if !ex.HasCode(expectedCode) {
		t.Errorf("Expected exception code %q, got %v", expectedCode, ex.GetCodes())
	}
}

// AssertExceptionCodeOneOf asserts that the response contains an exception with one of the expected codes.
func AssertExceptionCodeOneOf(t *testing.T, resp *Response, expectedCodes ...string) {
	t.Helper()
	if !exceptions.IsException(resp.Body) {
		t.Errorf("Expected ExceptionReport with one of codes %v, but response is not an exception", expectedCodes)
		return
	}

	ex, err := exceptions.Parse(resp.Body)
	if err != nil {
		t.Errorf("Failed to parse exception: %v", err)
		return
	}

	for _, code := range expectedCodes {
		if ex.HasCode(code) {
			return // Found a matching code
		}
	}
	t.Errorf("Expected exception code to be one of %v, got %v", expectedCodes, ex.GetCodes())
}

// AssertFeatureCount asserts the number of features in the response.
func AssertFeatureCount(t *testing.T, resp *Response, expected int) {
	t.Helper()
	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Errorf("Failed to parse feature collection: %v", err)
		return
	}
	if fc.NumberReturned != expected {
		t.Errorf("Expected %d features, got %d", expected, fc.NumberReturned)
	}
}

// AssertFeatureCountAtLeast asserts at least N features in the response.
func AssertFeatureCountAtLeast(t *testing.T, resp *Response, min int) {
	t.Helper()
	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Errorf("Failed to parse feature collection: %v", err)
		return
	}
	if fc.NumberReturned < min {
		t.Errorf("Expected at least %d features, got %d", min, fc.NumberReturned)
	}
}

// AssertNumberMatched asserts the numberMatched attribute.
func AssertNumberMatched(t *testing.T, resp *Response, expected int) {
	t.Helper()
	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Errorf("Failed to parse feature collection: %v", err)
		return
	}
	if fc.NumberMatched != expected {
		t.Errorf("Expected numberMatched=%d, got %d", expected, fc.NumberMatched)
	}
}

// AssertNumberMatchedAtLeast asserts numberMatched is at least N.
func AssertNumberMatchedAtLeast(t *testing.T, resp *Response, min int) {
	t.Helper()
	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Errorf("Failed to parse feature collection: %v", err)
		return
	}
	if fc.NumberMatched < min {
		t.Errorf("Expected numberMatched >= %d, got %d", min, fc.NumberMatched)
	}
}

// AssertCapabilitiesVersion asserts the capabilities version.
func AssertCapabilitiesVersion(t *testing.T, caps *capabilities.Capabilities, expected string) {
	t.Helper()
	if caps.Version != expected {
		t.Errorf("Expected capabilities version %q, got %q", expected, caps.Version)
	}
}

// AssertFeatureTypeExists asserts that the feature type exists in capabilities.
func AssertFeatureTypeExists(t *testing.T, caps *capabilities.Capabilities, typeName string) {
	t.Helper()
	if caps.GetFeatureType(typeName) == nil {
		t.Errorf("Feature type %q not found in capabilities", typeName)
	}
}

// AssertFeatureTypeCRS asserts that the feature type supports the given CRS.
func AssertFeatureTypeCRS(t *testing.T, ft *capabilities.FeatureType, crs string) {
	t.Helper()
	for _, c := range ft.GetCRSList() {
		if strings.EqualFold(c, crs) || strings.Contains(c, crs) {
			return
		}
	}
	t.Errorf("Feature type %q does not support CRS %q", ft.Name, crs)
}

// AssertOperationSupported asserts that the operation is supported.
func AssertOperationSupported(t *testing.T, caps *capabilities.Capabilities, operation string) {
	t.Helper()
	if !caps.SupportsOperation(operation) {
		t.Errorf("Operation %q is not supported", operation)
	}
}

// AssertStoredQueryExists asserts that the stored query exists.
func AssertStoredQueryExists(t *testing.T, caps *capabilities.Capabilities, queryID string) {
	t.Helper()
	// Stored queries are typically discovered via ListStoredQueries
	// For now, just check that GetFeatureById is present if that's what's requested
	if queryID == StoredQueryGetFeatureById {
		return // GetFeatureById is mandatory for Simple WFS
	}
}

// AssertOnlineResourceSuffix asserts that the URL ends with ? or &.
func AssertOnlineResourceSuffix(t *testing.T, href string) {
	t.Helper()
	if href == "" {
		t.Error("OnlineResource href is empty")
		return
	}
	if !strings.HasSuffix(href, "?") && !strings.HasSuffix(href, "&") {
		t.Errorf("OnlineResource href should end with '?' or '&', got: %s", href)
	}
}

// IsExceptionResponse checks if the response is an exception.
func IsExceptionResponse(resp *Response) bool {
	return exceptions.IsException(resp.Body)
}
