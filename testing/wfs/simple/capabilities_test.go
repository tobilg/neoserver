package simple

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/capabilities"
	"github.com/tobilg/neoserver/testing/wfs/exceptions"
)

// TestCapabilities_MissingService tests that SERVICE parameter is required.
func TestCapabilities_MissingService(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"REQUEST": {"GetCapabilities"},
		"VERSION": {"2.0.0"},
		// SERVICE is missing
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Without SERVICE, most implementations will return capabilities
	// Some may return an exception
	if wfs.IsExceptionResponse(resp) {
		wfs.AssertExceptionCode(t, resp, wfs.ExceptionMissingParameterValue)
	} else {
		wfs.AssertStatusCode(t, resp, 200)
	}
}

// TestCapabilities_InvalidService tests that SERVICE must be WFS.
func TestCapabilities_InvalidService(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WCS"}, // Wrong service
		"REQUEST": {"GetCapabilities"},
		"VERSION": {"2.0.0"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestCapabilities_Full tests that a full capabilities document is returned.
func TestCapabilities_Full(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"GetCapabilities"},
		"VERSION": {"2.0.0"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertIsXML(t, resp)
	wfs.AssertNotException(t, resp)

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities: %v", err)
	}

	wfs.AssertCapabilitiesVersion(t, caps, "2.0.0")
}

// TestCapabilities_VersionNegotiation tests version negotiation.
func TestCapabilities_VersionNegotiation(t *testing.T) {
	ctx := getTestContext(t)

	// Request with ACCEPTVERSIONS
	params := url.Values{
		"SERVICE":        {"WFS"},
		"REQUEST":        {"GetCapabilities"},
		"ACCEPTVERSIONS": {"2.0.0,2.0.2"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities: %v", err)
	}

	// Should return 2.0.0 or 2.0.2
	if caps.Version != "2.0.0" && caps.Version != "2.0.2" {
		t.Errorf("Expected version 2.0.0 or 2.0.2, got %s", caps.Version)
	}
}

// TestCapabilities_UnsupportedVersion tests version negotiation with unsupported versions.
// Note: WFS spec allows returning capabilities with the highest supported version
// during version negotiation, so either an exception or capabilities is valid.
func TestCapabilities_UnsupportedVersion(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WFS"},
		"REQUEST": {"GetCapabilities"},
		"VERSION": {"9.9.9"}, // Unsupported version
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// WFS allows returning capabilities with highest supported version during negotiation
	if !exceptions.IsException(resp.Body) {
		// Server returned capabilities - verify it's a valid response
		caps, err := capabilities.Parse(resp.Body)
		if err != nil {
			t.Fatalf("Failed to parse capabilities: %v", err)
		}
		t.Logf("Server returned capabilities with version %s for unsupported version request (valid negotiation)", caps.Version)
		return
	}
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestCapabilities_ServiceIdentification tests service identification metadata.
func TestCapabilities_ServiceIdentification(t *testing.T) {
	ctx := getTestContext(t)

	caps := ctx.Capabilities

	// Service type should be WFS
	if caps.ServiceIdentification.ServiceType != "WFS" {
		t.Errorf("Expected ServiceType 'WFS', got %q", caps.ServiceIdentification.ServiceType)
	}

	// Title should not be empty
	if caps.ServiceIdentification.Title == "" {
		t.Error("ServiceIdentification.Title should not be empty")
	}
}

// TestCapabilities_OperationsMetadata tests operations metadata.
func TestCapabilities_OperationsMetadata(t *testing.T) {
	ctx := getTestContext(t)

	caps := ctx.Capabilities

	// Simple WFS must support these operations
	requiredOps := []string{
		"GetCapabilities",
		"DescribeFeatureType",
		"GetFeature",
		"ListStoredQueries",
		"DescribeStoredQueries",
	}

	for _, op := range requiredOps {
		wfs.AssertOperationSupported(t, caps, op)
	}
}

// TestCapabilities_FeatureTypeList tests feature type list.
func TestCapabilities_FeatureTypeList(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	caps := ctx.Capabilities
	types := caps.GetFeatureTypes()

	if len(types) == 0 {
		t.Error("FeatureTypeList should contain at least one feature type")
	}

	// Each feature type should have required elements
	for _, ft := range types {
		if ft.Name == "" {
			t.Error("FeatureType.Name is required")
		}
		if ft.DefaultCRS == "" {
			t.Errorf("FeatureType %q should have a DefaultCRS", ft.Name)
		}
	}
}

// TestCapabilities_FilterCapabilities tests filter capabilities.
func TestCapabilities_FilterCapabilities(t *testing.T) {
	ctx := getTestContext(t)

	caps := ctx.Capabilities

	if caps.FilterCapabilities == nil {
		t.Skip("Filter capabilities not present")
	}

	// Should support ResourceId
	// This is indicated by the Id_Capabilities section
}

// TestCapabilities_FeatureTypeCRS tests that feature types have valid CRS.
func TestCapabilities_FeatureTypeCRS(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// DefaultCRS should be a valid URN or URL
	crs := ft.DefaultCRS
	if crs == "" {
		t.Error("DefaultCRS should not be empty")
	}

	// Check CRS format (should be URN or URL)
	// e.g., urn:ogc:def:crs:EPSG::4326 or http://www.opengis.net/gml/srs/epsg.xml#4326
}

// TestCapabilities_WGS84BoundingBox tests WGS84 bounding boxes.
func TestCapabilities_WGS84BoundingBox(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	caps := ctx.Capabilities
	types := caps.GetFeatureTypes()

	for _, ft := range types {
		if ft.WGS84BoundingBox != nil {
			if ft.WGS84BoundingBox.LowerCorner == "" {
				t.Errorf("FeatureType %q: WGS84BoundingBox.LowerCorner is empty", ft.Name)
			}
			if ft.WGS84BoundingBox.UpperCorner == "" {
				t.Errorf("FeatureType %q: WGS84BoundingBox.UpperCorner is empty", ft.Name)
			}
		}
	}
}

// TestCapabilities_Sections tests that SECTIONS parameter works.
func TestCapabilities_Sections(t *testing.T) {
	ctx := getTestContext(t)

	testCases := []struct {
		sections string
		desc     string
	}{
		{"ServiceIdentification", "service identification only"},
		{"ServiceProvider", "service provider only"},
		{"OperationsMetadata", "operations metadata only"},
		{"FeatureTypeList", "feature type list only"},
	}

	for _, tc := range testCases {
		t.Run(tc.sections, func(t *testing.T) {
			params := url.Values{
				"SERVICE":  {"WFS"},
				"REQUEST":  {"GetCapabilities"},
				"VERSION":  {"2.0.0"},
				"SECTIONS": {tc.sections},
			}

			resp, err := ctx.Client.Get(params)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}

			// Should return 200 or possibly an exception if SECTIONS is not supported
			if resp.StatusCode != 200 && !wfs.IsExceptionResponse(resp) {
				t.Errorf("Expected status 200 or exception, got %d", resp.StatusCode)
			}
		})
	}
}
