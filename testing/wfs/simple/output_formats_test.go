package simple

import (
	"archive/zip"
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/testing/wfs"
)

func advertisedGetFeatureFormat(t *testing.T, expected string) bool {
	t.Helper()
	operation := getTestContext(t).Capabilities.GetOperation("GetFeature")
	if operation == nil {
		return false
	}
	for _, parameter := range operation.Parameters {
		if !strings.EqualFold(parameter.Name, "outputFormat") {
			continue
		}
		for _, value := range parameter.AllowedValues {
			if strings.EqualFold(value, expected) {
				return true
			}
		}
	}
	return false
}

func binaryOutputRequest(t *testing.T, outputFormat string, extra url.Values) *wfs.Response {
	t.Helper()
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	featureType := ctx.GetFirstFeatureType()
	params := url.Values{
		"SERVICE": {"WFS"}, "REQUEST": {"GetFeature"}, "VERSION": {"2.0.0"},
		"TYPENAMES": {featureType.Name}, "COUNT": {"2"}, "OUTPUTFORMAT": {outputFormat},
	}
	for name, values := range extra {
		params[name] = values
	}
	response, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatal(err)
	}
	wfs.AssertStatusCode(t, response, 200)
	wfs.AssertNotException(t, response)
	return response
}

func TestGetFeature_GeoPackageOutput(t *testing.T) {
	const mediaType = "application/geopackage+sqlite3"
	if !advertisedGetFeatureFormat(t, mediaType) {
		t.Skip("GeoPackage is not advertised")
	}
	response := binaryOutputRequest(t, mediaType, nil)
	if !strings.HasPrefix(strings.ToLower(response.ContentType), mediaType) {
		t.Fatalf("Content-Type = %q", response.ContentType)
	}
	path := filepath.Join(t.TempDir(), "features.gpkg")
	if err := os.WriteFile(path, response.Body, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = godal.RegisterVector(godal.GeoPackage)
	dataset, err := godal.Open(path, godal.VectorOnly())
	if err != nil {
		t.Fatalf("open GeoPackage: %v", err)
	}
	defer dataset.Close()
	if len(dataset.Layers()) != 1 {
		t.Fatalf("GeoPackage layer count = %d", len(dataset.Layers()))
	}
	count, err := dataset.Layers()[0].FeatureCount()
	if err != nil || count == 0 {
		t.Fatalf("GeoPackage feature count = %d, err=%v", count, err)
	}
	feature := dataset.Layers()[0].NextFeature()
	if feature == nil {
		t.Fatal("GeoPackage feature is missing")
	}
	defer feature.Close()
	if _, exists := feature.Fields()["feature_id"]; !exists {
		t.Fatalf("GeoPackage fields = %v", feature.Fields())
	}
}

func TestGetFeature_ShapeZipOutput(t *testing.T) {
	if !advertisedGetFeatureFormat(t, "shape-zip") {
		t.Skip("SHAPE-ZIP is not advertised")
	}
	response := binaryOutputRequest(t, "shape-zip", url.Values{"FORMAT_OPTIONS": {"filename:live-export.zip"}})
	if !strings.HasPrefix(strings.ToLower(response.ContentType), "application/zip") || !strings.Contains(response.Headers.Get("Content-Disposition"), "live-export.zip") {
		t.Fatalf("SHAPE-ZIP headers = %v", response.Headers)
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body), int64(len(response.Body)))
	if err != nil {
		t.Fatal(err)
	}
	suffixes := make([]string, 0, len(archive.File))
	for _, entry := range archive.File {
		if filepath.Base(entry.Name) != entry.Name {
			t.Fatalf("unsafe ZIP entry %q", entry.Name)
		}
		suffixes = append(suffixes, strings.ToLower(filepath.Ext(entry.Name)))
	}
	for _, suffix := range []string{".cpg", ".dbf", ".prj", ".shp", ".shx"} {
		if !slices.Contains(suffixes, suffix) {
			t.Fatalf("SHAPE-ZIP suffixes = %v", suffixes)
		}
	}
}
