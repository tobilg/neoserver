package wfs

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/gdalcap"
)

func exportTestHandler(t *testing.T) *workspaceHandler {
	t.Helper()
	return &workspaceHandler{
		cfg: conf.Config{WFS: conf.WFS{
			MaxOutputBytes: 10 << 20, MaxTemporaryBytes: 10 << 20,
			TemporaryDirectory: t.TempDir(), ExportQueueTimeoutMS: 1000, ExportTimeoutMS: 10000,
		}},
		exports: make(chan struct{}, 1),
	}
}

var exportTestFeatures = [][]byte{
	[]byte(`{"type":"Feature","id":"roads.1","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"Main","very_long_property_alpha":7,"very_long_property_beta":true}}`),
}

func TestCSVOutputIsStableAndBounded(t *testing.T) {
	h := &workspaceHandler{cfg: conf.Config{WFS: conf.WFS{MaxOutputBytes: 1024}}}
	recorder := httptest.NewRecorder()
	features := [][]byte{[]byte(`{"type":"Feature","id":"roads.1","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"Main","lanes":2}}`)}
	if err := h.writeCSV(recorder, features, "roads"); err != nil {
		t.Fatal(err)
	}
	if recorder.Header().Get("Content-Type") != "text/csv; charset=utf-8" || !strings.Contains(recorder.Body.String(), "id,lanes,name,geometry") {
		t.Fatalf("CSV response: %s %q", recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
	h.cfg.WFS.MaxOutputBytes = 8
	if err := h.writeCSV(httptest.NewRecorder(), features, "roads"); err == nil {
		t.Fatal("CSV output limit was not enforced")
	}
}

func TestGeoPackageOutput(t *testing.T) {
	if !gdalcap.Get().GeoPackage {
		t.Skip("GDAL GeoPackage driver is unavailable")
	}
	h := exportTestHandler(t)
	recorder := httptest.NewRecorder()
	info := &datasource.LayerInfo{Name: "roads", GeometryType: "Point", SRID: 4326}
	if err := h.writeGeoPackage(t.Context(), recorder, exportTestFeatures, "roads", info, 4326, nil); err != nil {
		t.Fatal(err)
	}
	if recorder.Header().Get("Content-Type") != geoPackageMediaType || recorder.Body.Len() < 100 || filepath.Ext(strings.Trim(recorder.Header().Get("Content-Disposition"), `"`)) == "" {
		t.Fatalf("unexpected GeoPackage response: headers=%v bytes=%d", recorder.Header(), recorder.Body.Len())
	}
	path := filepath.Join(t.TempDir(), "roads.gpkg")
	if err := os.WriteFile(path, recorder.Body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	dataset, err := godal.Open(path, godal.VectorOnly())
	if err != nil {
		t.Fatalf("open GeoPackage: %v", err)
	}
	defer dataset.Close()
	if len(dataset.Layers()) != 1 || dataset.Layers()[0].Name() != "roads" {
		t.Fatalf("GeoPackage layers = %v", dataset.Layers())
	}
	featureCount, err := dataset.Layers()[0].FeatureCount()
	if err != nil || featureCount != 1 {
		t.Fatalf("feature count = %d, err=%v", featureCount, err)
	}
	feature := dataset.Layers()[0].NextFeature()
	if feature == nil {
		t.Fatal("GeoPackage feature is missing")
	}
	defer feature.Close()
	fields := feature.Fields()
	if fields["feature_id"].String() != "roads.1" || fields["name"].String() != "Main" || fields["very_long_property_alpha"].Int() != 7 {
		t.Fatalf("GeoPackage fields = %v", fields)
	}
	h.cfg.WFS.MaxTemporaryBytes = 1024
	if err := h.writeGeoPackage(t.Context(), httptest.NewRecorder(), exportTestFeatures, "roads", info, 4326, nil); err == nil {
		t.Fatal("GeoPackage aggregate temporary-byte limit was not enforced")
	}
}

func TestShapeZipOutputAndDBFNameCollisions(t *testing.T) {
	if !gdalcap.Get().Shapefile {
		t.Skip("GDAL ESRI Shapefile driver is unavailable")
	}
	h := exportTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/wfs?FORMAT_OPTIONS=filename:download.zip", nil)
	recorder := httptest.NewRecorder()
	info := &datasource.LayerInfo{Name: "roads", GeometryType: "Point", SRID: 4326}
	if err := h.writeShapeZip(t.Context(), recorder, request, exportTestFeatures, "roads", info, 4326, nil); err != nil {
		t.Fatal(err)
	}
	if recorder.Header().Get("Content-Type") != shapeZipMediaType || !strings.Contains(recorder.Header().Get("Content-Disposition"), "download.zip") {
		t.Fatalf("SHAPE-ZIP headers = %v", recorder.Header())
	}
	archive, err := zip.NewReader(bytes.NewReader(recorder.Body.Bytes()), int64(recorder.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(archive.File))
	directory := t.TempDir()
	for _, entry := range archive.File {
		names = append(names, entry.Name)
		if filepath.Base(entry.Name) != entry.Name {
			t.Fatalf("unsafe archive entry %q", entry.Name)
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		payload, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s: %v / %v", entry.Name, readErr, closeErr)
		}
		if err := os.WriteFile(filepath.Join(directory, entry.Name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{".cpg", ".dbf", ".prj", ".shp", ".shx"} {
		if !slices.Contains(names, "roads"+suffix) {
			t.Fatalf("SHAPE-ZIP entries = %v", names)
		}
	}
	dataset, err := godal.Open(filepath.Join(directory, "roads.shp"), godal.VectorOnly())
	if err != nil {
		t.Fatal(err)
	}
	defer dataset.Close()
	feature := dataset.Layers()[0].NextFeature()
	if feature == nil {
		t.Fatal("Shapefile feature is missing")
	}
	defer feature.Close()
	fields := feature.Fields()
	if fields["FEATURE_ID"].String() != "roads.1" || fields["NAME"].String() != "Main" || fields["VERY_LONG_"].Int() == fields["VERY_LON_2"].Int() {
		t.Fatalf("Shapefile fields = %v", fields)
	}
}

func TestShapeZipRejectsUnsafeFilename(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/wfs?FORMAT_OPTIONS=filename:../escape.zip", nil)
	if _, err := shapeZipFilename(request, "roads"); err == nil {
		t.Fatal("unsafe SHAPE-ZIP filename was accepted")
	}
}

func TestWFSFormatsReflectGDALCapabilities(t *testing.T) {
	formats := availableWFSOutputFormats()
	capabilities := gdalcap.Get()
	if slices.Contains(formats, geoPackageMediaType) != capabilities.GeoPackage || slices.Contains(formats, shapeZipFormat) != capabilities.Shapefile {
		t.Fatalf("formats %v do not match GDAL capabilities %+v", formats, capabilities)
	}
}
