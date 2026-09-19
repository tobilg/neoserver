package wcs

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestWCS21CoreAndKVP(t *testing.T) {
	document := capabilities(t, "2.1.0")
	requireProfiles(t, document.Profiles, wcs21Core, wcs21KVP)
	id := coverageID(t, document, "GeneralGridCoverage")
	description := describe(t, "2.1.0", id)
	if !strings.Contains(string(description), "GeneralGridCoverage") {
		t.Fatalf("DescribeCoverage does not contain GeneralGridCoverage")
	}
	status, headers, body := get(t, baseGetCoverage("2.1.0", id))
	assertTIFF(t, status, headers, body)

	t.Run("CaseInsensitiveKVP", func(t *testing.T) {
		status, _, body := get(t, url.Values{"service": {"wcs"}, "request": {"GetCapabilities"}, "acceptversions": {"2.1.0"}})
		if status != http.StatusOK {
			t.Fatalf("lowercase KVP status=%d body=%s", status, body)
		}
	})
}

func TestWCS21ExtensionDeclarationsAndExecution(t *testing.T) {
	document := capabilities(t, "2.1.0")
	requireProfiles(t, document.Profiles,
		"http://www.opengis.net/spec/WCS_service-extension_interpolation/1.1/conf/interpolation",
		"http://www.opengis.net/spec/WCS_service-extension_interpolation/1.1/conf/interpolation-nearest-neighbor",
		"http://www.opengis.net/spec/WCS_service-extension_interpolation/1.1/conf/interpolation-linear",
	)
	id := coverageID(t, document, "GeneralGridCoverage")
	for _, method := range []string{"nearest-neighbor", "linear"} {
		t.Run(method, func(t *testing.T) {
			values := baseGetCoverage("2.1.0", id)
			values.Set("INTERPOLATION", method)
			status, headers, body := get(t, values)
			assertTIFF(t, status, headers, body)
		})
	}
}
