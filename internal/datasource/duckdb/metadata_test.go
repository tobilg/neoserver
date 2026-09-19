package duckdb

import (
	"context"
	"strings"
	"testing"
)

func TestUnknownCRSRequiresOverrideAndCustomPrimaryKey(t *testing.T) {
	ds, err := New("metadata", Config{Path: ":memory:", Extensions: []string{"spatial"}})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	if _, err = ds.db.Exec(`CREATE TABLE points (custom_key INTEGER PRIMARY KEY, id INTEGER, geom GEOMETRY); INSERT INTO points VALUES (42, 3, ST_Point(779236.435553,6621293.722740))`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = ds.GetLayerInfo(ctx, "points"); err == nil || !strings.Contains(err.Error(), "native CRS") {
		t.Fatalf("unknown CRS: %v", err)
	}
	ds.layerSRIDs = map[string]int{"points": 3857}
	info, err := ds.GetLayerInfo(ctx, "points")
	if err != nil || info.SRID != 3857 || info.IDColumn != "custom_key" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	layers, err := ds.DiscoverLayers(ctx)
	if err != nil || len(layers) != 1 || layers[0].IDColumn != "custom_key" || layers[0].SRID != 3857 {
		t.Fatalf("discovery=%+v err=%v", layers, err)
	}
	feature, found, err := ds.QueryByID(ctx, "points", "42", 4326)
	if err != nil || !found || !strings.Contains(string(feature), `"id":42`) {
		t.Fatalf("lookup=%s found=%v err=%v", feature, found, err)
	}
}
