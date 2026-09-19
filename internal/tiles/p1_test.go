package tiles

import (
	"bytes"
	"image"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestCustomTileMatrixSetRegistryAndBounds(t *testing.T) {
	definition := &TileMatrixSetDefinition{ID: "LocalCRS84", CRS: CRS4326URI, TileMatrices: []TileMatrix{{ID: "0", ScaleDenominator: 1000, CellSize: 1, CornerOfOrigin: "topLeft", PointOfOrigin: []float64{-10, 10}, TileWidth: 10, TileHeight: 10, MatrixWidth: 2, MatrixHeight: 2}}}
	if err := ReplaceCustomTileMatrixSets([]*TileMatrixSetDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ReplaceCustomTileMatrixSets(nil) })
	bounds, err := TileBBox("LocalCRS84", 0, 1, 1)
	if err != nil || bounds.MinX != 0 || bounds.MinY != -10 || bounds.MaxX != 10 || bounds.MaxY != 0 {
		t.Fatalf("bounds = %+v, %v", bounds, err)
	}
	if err := ValidateTileCoords("LocalCRS84", 0, 2, 0); err == nil {
		t.Fatal("out-of-range custom coordinate accepted")
	}
}

func TestCustomTileMatrixSetChangeInvalidatesCacheIdentity(t *testing.T) {
	definition := &TileMatrixSetDefinition{ID: "LocalCRS84", CRS: CRS4326URI, TileMatrices: []TileMatrix{{ID: "0", ScaleDenominator: 1000, CellSize: 1, CornerOfOrigin: "topLeft", PointOfOrigin: []float64{-10, 10}, TileWidth: 256, TileHeight: 256, MatrixWidth: 1, MatrixHeight: 1}}}
	if err := ReplaceCustomTileMatrixSets([]*TileMatrixSetDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ReplaceCustomTileMatrixSets(nil) })

	cfg := newTestConfig()
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{})
	request := EngineRequest{Workspace: ws, Resource: ws.GetResource("roads"), TileType: "vector", MatrixSet: definition.ID, Zoom: 0, Column: 0, Row: 0, Format: MediaTypeMVT}
	first, err := NewEngine(cfg, newTestLogger(), nil, nil).ResolveIdentity(request)
	if err != nil {
		t.Fatal(err)
	}

	definition.TileMatrices[0].CellSize = 2
	if err := ReplaceCustomTileMatrixSets([]*TileMatrixSetDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	second, err := NewEngine(cfg, newTestLogger(), nil, nil).ResolveIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalKey() == second.CanonicalKey() {
		t.Fatal("custom tile matrix set update did not invalidate the cache identity")
	}
}

func TestCacheParameterPolicyBypassesUnboundedDimensions(t *testing.T) {
	ws := newTestWorkspace()
	layer := addTestLayer(ws, "roads", &fakeDataSource{})
	request := EngineRequest{Resource: ws.GetResource("roads")}
	request.Time = "2026-08-02"
	if cacheParametersAllow(request) {
		t.Fatal("dimension value without an allowlist was cacheable")
	}
	layer.TileCacheParameters = &store.TileCacheParameterPolicy{Times: []string{"2026-08-02"}, Elevations: []string{"10"}}
	request.Elevation = "10"
	if !cacheParametersAllow(request) {
		t.Fatal("allowlisted parameters were not cacheable")
	}
	request.Elevation = "11"
	if cacheParametersAllow(request) {
		t.Fatal("out-of-filter elevation was cacheable")
	}
}

func TestFeatureMetatileWithGutterReturnsOneTile(t *testing.T) {
	cfg := newTestConfig()
	cfg.Tiles.MaxMetatileFactor = 4
	cfg.Tiles.MaxGutterPixels = 32
	ws := newTestWorkspace()
	layer := addTestLayer(ws, "roads", &fakeDataSource{})
	layer.TileCacheParameters = &store.TileCacheParameterPolicy{MetatileFactor: 2, GutterPixels: 8}
	result, err := NewEngine(cfg, newTestLogger(), nil, nil).Fetch(t.Context(), EngineRequest{Workspace: ws, Resource: ws.GetResource("roads"), TileType: "map", MatrixSet: TMSWebMercatorQuad, Zoom: 1, Column: 1, Row: 1, Format: MediaTypePNG})
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(result.Data))
	if err != nil || decoded.Bounds().Dx() != 256 || decoded.Bounds().Dy() != 256 {
		t.Fatalf("metatile crop = %v, %v", decoded, err)
	}
}
