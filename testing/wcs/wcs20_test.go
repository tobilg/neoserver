package wcs

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestWCS20CoreAndKVP(t *testing.T) {
	document := capabilities(t, "2.0.1")
	requireProfiles(t, document.Profiles, wcs20Core, wcs20KVP)
	if !contains(document.Formats, "image/tiff") {
		t.Errorf("capabilities do not advertise image/tiff: %v", document.Formats)
	}

	rectified := coverageID(t, document, "RectifiedGridCoverage")
	description := describe(t, "2.0.1", rectified)
	for _, expected := range []string{"CoverageDescription", rectified, "Envelope", "rangeType", "RectifiedGridCoverage", "nativeFormat"} {
		if !strings.Contains(string(description), expected) {
			t.Errorf("DescribeCoverage is missing %q", expected)
		}
	}

	status, headers, body := get(t, baseGetCoverage("2.0.1", rectified))
	assertTIFF(t, status, headers, body)

	t.Run("CaseInsensitiveKVP", func(t *testing.T) {
		status, _, body := get(t, url.Values{"service": {"wcs"}, "request": {"GetCapabilities"}, "acceptversions": {"2.0.1"}})
		if status != http.StatusOK {
			t.Fatalf("lowercase KVP status=%d body=%s", status, body)
		}
	})
}

func TestWCS20CoverageRepresentations(t *testing.T) {
	document := capabilities(t, "2.0.1")
	for _, subtype := range []string{"RectifiedGridCoverage", "GridCoverage"} {
		t.Run(subtype, func(t *testing.T) {
			id := coverageID(t, document, subtype)
			description := describe(t, "2.0.1", id)
			if !strings.Contains(string(description), subtype) {
				t.Fatalf("DescribeCoverage does not contain subtype %s", subtype)
			}

			values := baseGetCoverage("2.0.1", id)
			values.Set("FORMAT", "application/gml+xml")
			status, headers, body := get(t, values)
			if status != http.StatusOK || !strings.Contains(strings.ToLower(headers.Get("Content-Type")), "xml") {
				t.Fatalf("GML GetCoverage status=%d content-type=%q body=%s", status, headers.Get("Content-Type"), body)
			}
			assertXML(t, body)
		})
	}
}

func TestWCS20MultipartAndErrors(t *testing.T) {
	id := coverageID(t, capabilities(t, "2.0.1"), "RectifiedGridCoverage")
	values := baseGetCoverage("2.0.1", id)
	values.Del("FORMAT")
	values.Set("MEDIATYPE", "multipart/related")
	status, headers, body := get(t, values)
	if status != http.StatusOK || !strings.HasPrefix(strings.ToLower(headers.Get("Content-Type")), "multipart/related") || len(body) == 0 {
		t.Fatalf("multipart status=%d content-type=%q bytes=%d", status, headers.Get("Content-Type"), len(body))
	}

	exception(t, baseGetCoverage("2.0.1", "does_not_exist"), "NoSuchCoverage")
	invalidFormat := baseGetCoverage("2.0.1", id)
	invalidFormat.Set("FORMAT", "bad/type")
	exception(t, invalidFormat, "InvalidParameterValue")
	unknownAxis := baseGetCoverage("2.0.1", id)
	unknownAxis.Add("SUBSET", "unknown(1)")
	exception(t, unknownAxis, "InvalidAxisLabel")
	duplicateAxis := baseGetCoverage("2.0.1", id)
	duplicateAxis.Add("SUBSET", "x(10.1,10.2)")
	duplicateAxis.Add("SUBSET", "x(10.2,10.3)")
	exception(t, duplicateAxis, "InvalidAxisLabel")
}

func TestWCS20Extensions(t *testing.T) {
	document := capabilities(t, "2.0.1")
	requireProfiles(t, document.Profiles,
		"http://www.opengis.net/spec/WCS_protocol-binding_post-xml/1.0/conf/post-xml",
		"http://www.opengis.net/spec/WCS_service-extension_range-subsetting/1.0/conf/record-subsetting",
		"http://www.opengis.net/spec/WCS_service-extension_scaling/1.0/conf/scaling",
		"http://www.opengis.net/spec/WCS_service-extension_crs/1.0/conf/crs",
		"http://www.opengis.net/spec/WCS_service-extension_interpolation/1.0/conf/interpolation",
		"http://www.opengis.net/spec/WCS_service-extension_interpolation/1.0/conf/interpolation-nearest-neighbor",
		"http://www.opengis.net/spec/WCS_service-extension_interpolation/1.0/conf/interpolation-linear",
	)
	id := coverageID(t, document, "RectifiedGridCoverage")
	base := baseGetCoverage("2.0.1", id)
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"RangeSubsetting", "RANGESUBSET", "band1"},
		{"Scaling", "SCALESIZE", "i(5),j(5)"},
		{"OutputCRS", "OUTPUTCRS", "http://www.opengis.net/def/crs/EPSG/0/3857"},
		{"NearestInterpolation", "INTERPOLATION", "nearest-neighbor"},
		{"LinearInterpolation", "INTERPOLATION", "linear"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := clone(base)
			values.Set(test.key, test.value)
			status, headers, body := get(t, values)
			assertTIFF(t, status, headers, body)
		})
	}
}
