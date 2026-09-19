package getcapabilities

import (
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestCapabilityMetadata_OnlineResourceSuffix tests that OnlineResource href ends with ? or &.
// Reference: WMS 1.3.0 section 7.2.4.3
func TestCapabilityMetadata_OnlineResourceSuffix(t *testing.T) {
	ctx := getTestContext(t)

	caps := ctx.Capabilities
	if caps == nil {
		t.Fatal("Capabilities not available")
	}

	// Check GetCapabilities OnlineResource
	if caps.Capability.Request != nil {
		if caps.Capability.Request.GetCapabilities != nil {
			href := caps.Capability.Request.GetCapabilities.GetOnlineResource()
			if href != "" {
				wms.AssertOnlineResourceSuffix(t, href)
			}
		}

		if caps.Capability.Request.GetMap != nil {
			href := caps.Capability.Request.GetMap.GetOnlineResource()
			if href != "" {
				wms.AssertOnlineResourceSuffix(t, href)
			}
		}

		if caps.Capability.Request.GetFeatureInfo != nil {
			href := caps.Capability.Request.GetFeatureInfo.GetOnlineResource()
			if href != "" {
				wms.AssertOnlineResourceSuffix(t, href)
			}
		}
	}
}

// TestCapabilityMetadata_XMLGetCapabilitiesFormat tests that text/xml is a supported
// GetCapabilities format.
func TestCapabilityMetadata_XMLGetCapabilitiesFormat(t *testing.T) {
	ctx := getTestContext(t)

	formats := ctx.Capabilities.GetCapabilitiesFormats()
	wms.AssertFormatSupported(t, formats, "text/xml")
}

// TestCapabilityMetadata_XMLExceptionFormat tests that XML is a supported exception format.
func TestCapabilityMetadata_XMLExceptionFormat(t *testing.T) {
	ctx := getTestContext(t)

	formats := ctx.GetExceptionFormats()
	wms.AssertFormatSupported(t, formats, "XML")
}

// TestCapabilityMetadata_GetMapFormats tests that at least one image format is supported.
func TestCapabilityMetadata_GetMapFormats(t *testing.T) {
	ctx := getTestContext(t)

	formats := ctx.GetMapFormats()
	if len(formats) == 0 {
		t.Error("No GetMap formats advertised")
		return
	}

	// Check for at least one common image format
	hasImageFormat := false
	imageFormats := []string{"image/png", "image/jpeg", "image/gif"}
	for _, format := range formats {
		for _, imgFmt := range imageFormats {
			if strings.EqualFold(format, imgFmt) {
				hasImageFormat = true
				break
			}
		}
		if hasImageFormat {
			break
		}
	}

	if !hasImageFormat {
		t.Logf("Warning: No common image format (PNG, JPEG, GIF) found. Available formats: %v", formats)
	}
}

// TestCapabilityMetadata_ServiceName tests that the service name is WMS.
func TestCapabilityMetadata_ServiceName(t *testing.T) {
	ctx := getTestContext(t)

	caps := ctx.Capabilities
	if caps == nil {
		t.Fatal("Capabilities not available")
	}

	if caps.Service.Name != "WMS" {
		t.Errorf("Expected service name 'WMS', got %q", caps.Service.Name)
	}
}

// TestCapabilityMetadata_ServiceTitle tests that the service has a title.
func TestCapabilityMetadata_ServiceTitle(t *testing.T) {
	ctx := getTestContext(t)

	caps := ctx.Capabilities
	if caps == nil {
		t.Fatal("Capabilities not available")
	}

	if caps.Service.Title == "" {
		t.Error("Service title is empty")
	}
}

// TestCapabilityMetadata_Version tests that the capabilities version is 1.3.0.
func TestCapabilityMetadata_Version(t *testing.T) {
	ctx := getTestContext(t)

	caps := ctx.Capabilities
	if caps == nil {
		t.Fatal("Capabilities not available")
	}

	wms.AssertCapabilitiesVersion(t, caps, "1.3.0")
}

// TestCapabilityMetadata_RequestOperations tests that required operations are advertised.
func TestCapabilityMetadata_RequestOperations(t *testing.T) {
	ctx := getTestContext(t)

	caps := ctx.Capabilities
	if caps == nil || caps.Capability.Request == nil {
		t.Fatal("Capabilities Request section not available")
	}

	req := caps.Capability.Request

	// GetCapabilities is required
	if req.GetCapabilities == nil {
		t.Error("GetCapabilities operation not advertised")
	}

	// GetMap is required
	if req.GetMap == nil {
		t.Error("GetMap operation not advertised")
	}

	// GetFeatureInfo is optional but commonly supported
	if req.GetFeatureInfo == nil {
		t.Log("GetFeatureInfo operation not advertised (optional)")
	}
}

// TestCapabilityMetadata_ExceptionFormats tests that exception formats are advertised.
func TestCapabilityMetadata_ExceptionFormats(t *testing.T) {
	ctx := getTestContext(t)

	formats := ctx.GetExceptionFormats()
	if len(formats) == 0 {
		t.Error("No exception formats advertised")
		return
	}

	// XML exception format should be supported
	hasXML := false
	for _, format := range formats {
		if strings.EqualFold(format, "XML") {
			hasXML = true
			break
		}
	}

	if !hasXML {
		t.Error("XML exception format not advertised")
	}
}

// TestCapabilityMetadata_RootLayer tests that a root layer exists in capabilities.
func TestCapabilityMetadata_RootLayer(t *testing.T) {
	ctx := getTestContext(t)

	caps := ctx.Capabilities
	if caps == nil || caps.Capability.Layer == nil {
		t.Error("No root layer in capabilities")
		return
	}

	// Root layer should have a title
	if caps.Capability.Layer.Title == "" {
		t.Error("Root layer has no title")
	}
}
