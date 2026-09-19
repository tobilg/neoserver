package wcs

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airbusgeo/godal"
)

var registerGDAL sync.Once

const (
	wcs20Core = "http://www.opengis.net/spec/WCS/2.0/conf/core"
	wcs20KVP  = "http://www.opengis.net/spec/WCS_protocol-binding_get-kvp/1.0/conf/get-kvp"
	wcs21Core = "http://www.opengis.net/spec/WCS/2.1/conf/core"
	wcs21KVP  = "http://www.opengis.net/spec/WCS/2.1/conf/kvp-protocol-binding"
)

type capabilitiesDocument struct {
	Version  string   `xml:"version,attr"`
	Profiles []string `xml:"ServiceIdentification>Profile"`
	Formats  []string `xml:"ServiceMetadata>formatSupported"`
	Coverage []struct {
		ID      string `xml:"CoverageId"`
		Subtype string `xml:"CoverageSubtype"`
	} `xml:"Contents>CoverageSummary"`
}

func endpoint(t *testing.T) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv("WCS_TEST_URL"))
	if value == "" {
		t.Skip("WCS_TEST_URL is not set")
	}
	return strings.SplitN(value, "?", 2)[0]
}

func get(t *testing.T, values url.Values) (int, http.Header, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(endpoint(t) + "?" + values.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, body
}

func capabilities(t *testing.T, version string) capabilitiesDocument {
	t.Helper()
	status, headers, body := get(t, url.Values{
		"SERVICE": {"WCS"}, "REQUEST": {"GetCapabilities"}, "ACCEPTVERSIONS": {version},
	})
	if status != http.StatusOK || !strings.Contains(strings.ToLower(headers.Get("Content-Type")), "xml") {
		t.Fatalf("GetCapabilities status=%d content-type=%q body=%s", status, headers.Get("Content-Type"), body)
	}
	var document capabilitiesDocument
	if err := xml.Unmarshal(body, &document); err != nil {
		t.Fatalf("parse GetCapabilities: %v", err)
	}
	if document.Version != version {
		t.Fatalf("capabilities version=%q, want %s", document.Version, version)
	}
	if len(document.Coverage) == 0 || document.Coverage[0].ID == "" {
		t.Fatal("capabilities contain no usable coverage")
	}
	return document
}

func coverageID(t *testing.T, document capabilitiesDocument, subtype string) string {
	t.Helper()
	for _, coverage := range document.Coverage {
		if subtype == "" || coverage.Subtype == subtype {
			return coverage.ID
		}
	}
	t.Fatalf("no coverage with subtype %q: %+v", subtype, document.Coverage)
	return ""
}

func requireProfiles(t *testing.T, actual []string, expected ...string) {
	t.Helper()
	for _, profile := range expected {
		if !contains(actual, profile) {
			t.Errorf("capabilities do not advertise %s", profile)
		}
	}
}

func assertXML(t *testing.T, document []byte) {
	t.Helper()
	if err := xml.Unmarshal(document, &struct{ XMLName xml.Name }{}); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
}

func assertTIFF(t *testing.T, status int, headers http.Header, body []byte) {
	t.Helper()
	if status != http.StatusOK || !strings.HasPrefix(strings.ToLower(headers.Get("Content-Type")), "image/tiff") {
		t.Fatalf("GetCoverage status=%d content-type=%q body=%s", status, headers.Get("Content-Type"), body)
	}
	registerGDAL.Do(func() { godal.RegisterRaster(godal.GTiff) })
	path := filepath.Join(t.TempDir(), "coverage.tif")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("stage GeoTIFF: %v", err)
	}
	dataset, err := godal.Open(path, godal.RasterOnly(), godal.Drivers("GTiff"))
	if err != nil {
		t.Fatalf("open GeoTIFF with GDAL: %v", err)
	}
	defer dataset.Close()
	structure := dataset.Structure()
	if structure.SizeX <= 0 || structure.SizeY <= 0 || structure.NBands <= 0 {
		t.Fatalf("invalid GeoTIFF dimensions=%dx%d bands=%d", structure.SizeX, structure.SizeY, structure.NBands)
	}
	pixel := make([]float64, 1)
	if err := dataset.Bands()[0].Read(0, 0, pixel, 1, 1); err != nil {
		t.Fatalf("decode GeoTIFF pixel data: %v", err)
	}
}

func baseGetCoverage(version, id string) url.Values {
	return url.Values{
		"SERVICE": {"WCS"}, "VERSION": {version}, "REQUEST": {"GetCoverage"},
		"COVERAGEID": {id}, "FORMAT": {"image/tiff"},
	}
}

func clone(values url.Values) url.Values {
	copy := make(url.Values, len(values))
	for key, value := range values {
		copy[key] = append([]string(nil), value...)
	}
	return copy
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func exception(t *testing.T, values url.Values, expected string) {
	t.Helper()
	status, _, body := get(t, values)
	if status < 400 || !strings.Contains(string(body), expected) {
		t.Fatalf("expected %s exception, status=%d body=%s", expected, status, body)
	}
}

func describe(t *testing.T, version, id string) []byte {
	t.Helper()
	status, _, body := get(t, url.Values{
		"SERVICE": {"WCS"}, "VERSION": {version}, "REQUEST": {"DescribeCoverage"}, "COVERAGEID": {id},
	})
	if status != http.StatusOK {
		t.Fatalf("DescribeCoverage status=%d body=%s", status, body)
	}
	assertXML(t, body)
	return body
}
