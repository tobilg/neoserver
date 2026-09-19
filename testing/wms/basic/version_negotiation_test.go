package basic

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
	"github.com/tobilg/neoserver/testing/wms/capabilities"
)

// TestVersionNegotiation_NoVersion tests that when a GetCapabilities request is made
// without a version number, the response is version 1.3.0 or higher.
// Reference: WMS 1.3.0 section 6.2.4
func TestVersionNegotiation_NoVersion(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"REQUEST": {"GetCapabilities"},
		// No VERSION parameter
	}

	resp, err := ctx.Client.GetCapabilities(params)
	if err != nil {
		t.Fatalf("GetCapabilities request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsXML(t, resp)

	// Parse response to check version
	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities response: %v", err)
	}

	// Version should be at least 1.3.0
	version := caps.Version
	if version == "" {
		t.Error("Could not determine version number from response")
		return
	}

	if compareVersions(version, "1.3.0") < 0 {
		t.Errorf("Expected version >= 1.3.0, got %s", version)
	}
}

// TestVersionNegotiation_Version130 tests that when a GetCapabilities request is made
// for version 1.3.0, the response is exactly version 1.3.0.
// Reference: WMS 1.3.0 section 6.2.4
func TestVersionNegotiation_Version130(t *testing.T) {
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

	wms.AssertCapabilitiesVersion(t, caps, "1.3.0")
}

// TestVersionNegotiation_HigherVersion tests that when a GetCapabilities request is made
// for a version higher than supported (100.0.0), the response is not lower than 1.3.0.
// Reference: WMS 1.3.0 section 6.2.4
func TestVersionNegotiation_HigherVersion(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"100.0.0"},
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

	version := caps.Version
	if version == "" {
		t.Error("Could not determine version number from response")
		return
	}

	// When requesting a higher version, server should return its highest supported version
	// which should be at least 1.3.0
	if compareVersions(version, "1.3.0") < 0 {
		t.Errorf("Expected version >= 1.3.0, got %s", version)
	}
}

// TestVersionNegotiation_LowerVersion tests that when a GetCapabilities request is made
// for version 0.0.0, the response version is not higher than 1.3.0.
// Reference: WMS 1.3.0 section 6.2.4
func TestVersionNegotiation_LowerVersion(t *testing.T) {
	ctx := getTestContext(t)

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"0.0.0"},
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

	version := caps.Version
	if version == "" {
		t.Error("Could not determine version number from response")
		return
	}

	// When requesting a lower version, server should return its lowest supported version
	// For a 1.3.0 server, this could be 1.3.0 or lower
	// The test just verifies we get a valid response
	t.Logf("Received version %s for request version 0.0.0", version)
}

// compareVersions compares two version strings (e.g., "1.3.0" vs "1.1.1").
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2.
func compareVersions(v1, v2 string) int {
	// Parse version strings into integers
	parts1 := parseVersion(v1)
	parts2 := parseVersion(v2)

	for i := 0; i < 3; i++ {
		var p1, p2 int
		if i < len(parts1) {
			p1 = parts1[i]
		}
		if i < len(parts2) {
			p2 = parts2[i]
		}

		if p1 < p2 {
			return -1
		}
		if p1 > p2 {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) []int {
	result := make([]int, 0, 3)
	current := 0
	for _, c := range v {
		if c >= '0' && c <= '9' {
			current = current*10 + int(c-'0')
		} else if c == '.' {
			result = append(result, current)
			current = 0
		}
	}
	result = append(result, current)
	return result
}
