package vectorfile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGeoPackageDiscoveryUsesActualLayerNames(t *testing.T) {
	data, err := os.ReadFile("../../../web/admin/e2e/fixtures/two-layers.gpkg")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "two-layers.gpkg")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ds, err := New("multi", Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	// Metadata enumeration must not reserve a second DuckDB connection.
	ds.db.SetMaxOpenConns(1)
	layers, err := ds.DiscoverLayers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 2 {
		t.Fatalf("expected both layers, got %+v", layers)
	}
	seen := map[string]bool{}
	for _, layer := range layers {
		seen[layer.Name] = true
		if layer.GeometryColumn == "" || layer.SRID != 4326 {
			t.Fatalf("invalid metadata: %+v", layer)
		}
	}
	if !seen["first_source"] || !seen["second_source"] {
		t.Fatalf("wrong layer names: %+v", seen)
	}
}
