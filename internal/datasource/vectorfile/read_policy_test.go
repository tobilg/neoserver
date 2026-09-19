package vectorfile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/store"
)

func TestIndirectDatasetsCannotEscapePolicy(t *testing.T) {
	allowed, denied := t.TempDir(), t.TempDir()
	fixture := filepath.Join(denied, "fixture.geojson")
	if err := os.WriteFile(fixture, []byte(`{"type":"FeatureCollection","name":"fixture","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"marker":"denied"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	pathpolicy.Configure([]string{filepath.Join(allowed, "**")})
	t.Cleanup(func() { pathpolicy.Configure([]string{"**"}) })
	if err := pathpolicy.Check(fixture); err == nil {
		t.Fatal("fixture must be denied directly")
	}
	var requests atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.ServeFile(w, r, fixture) }))
	defer remote.Close()
	for _, target := range []string{fixture, remote.URL + "/fixture.geojson"} {
		for _, extension := range []string{".vrt", ".geojson", ".gpkg", ".shp", ".fgb"} {
			path := filepath.Join(allowed, "proxy"+extension)
			body := fmt.Sprintf(`<OGRVRTDataSource><OGRVRTLayer name="proxy"><SrcDataSource>%s</SrcDataSource><SrcLayer>fixture</SrcLayer></OGRVRTLayer></OGRVRTDataSource>`, target)
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			config, _ := json.Marshal(Config{Path: path})
			if source, err := NewFromService(&store.Service{ID: "probe", ConnectionInfo: config}); err == nil {
				source.Close()
				t.Fatalf("indirect %s accepted", extension)
			}
			// The upload path calls New directly, so it must enforce the same policy.
			if source, err := New("upload", Config{Path: path}); err == nil {
				source.Close()
				t.Fatalf("upload %s accepted", extension)
			}
		}
	}
	if requests.Load() != 0 {
		t.Fatal("GDAL contacted an indirect remote source")
	}
	if _, err := New("options", Config{Path: fixture, OpenOptions: map[string]string{"XSD": "https://invalid/schema"}}); err == nil {
		t.Fatal("file-bearing option accepted")
	}
}

func TestSafeGeoJSONOpenOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "simple.geojson")
	if err := os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"nested":{"value":1}},"geometry":{"type":"Point","coordinates":[1,2]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := New("safe", Config{Path: path, OpenOptions: map[string]string{"FLATTEN_NESTED_ATTRIBUTES": "YES"}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if layers, err := source.DiscoverLayers(context.Background()); err != nil || len(layers) != 1 {
		t.Fatalf("layers=%v err=%v", layers, err)
	}
}
