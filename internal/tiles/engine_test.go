package tiles

import (
	"testing"

	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestEngineIdentityChangesWithWorkspaceAndGlobalRenderRevisions(t *testing.T) {
	cfg := newTestConfig()
	ws := newTestWorkspace()
	ws.TileRevision = 3
	layer := addTestLayer(ws, "roads", &fakeDataSource{})
	layer.TileCacheGeneration = 7
	resource := ws.GetResource("roads")
	request := EngineRequest{Workspace: ws, Resource: resource, TileType: "vector", MatrixSet: TMSWebMercatorQuad, Zoom: 0, Column: 0, Row: 0, Format: MediaTypeMVT}

	first, err := NewEngine(cfg, newTestLogger(), nil, nil).ResolveIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	ws.TileRevision++
	second, err := NewEngine(cfg, newTestLogger(), nil, nil).ResolveIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalKey() == second.CanonicalKey() {
		t.Fatal("workspace rendering revision did not change the canonical identity")
	}

	cfg.Tiles.MaxFeatures++
	third, err := NewEngine(cfg, newTestLogger(), nil, nil).ResolveIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	if second.CanonicalKey() == third.CanonicalKey() {
		t.Fatal("global rendering configuration did not change the canonical identity")
	}
	ws.StyleAssetDigest = "new-asset-manifest"
	fourth, err := NewEngine(cfg, newTestLogger(), nil, nil).ResolveIdentity(request)
	if err != nil || fourth.CanonicalKey() == third.CanonicalKey() {
		t.Fatalf("asset manifest did not invalidate persistent identity: %v", err)
	}
}

func TestGroupIdentityIncludesChildGeneration(t *testing.T) {
	cfg := newTestConfig()
	ws := newTestWorkspace()
	layer := addTestLayer(ws, "roads", &fakeDataSource{})
	layer.TileCacheGeneration = 4
	ws.Groups = make(map[string]*workspace.LayerGroup)
	ws.Groups["base"] = &workspace.LayerGroup{ID: "group-id", PublicID: "base", Enabled: true, Public: true, TileCacheGeneration: 2, Members: []store.LayerGroupMember{{Resource: "roads"}}}
	request := EngineRequest{Workspace: ws, Resource: ws.GetResource("base"), TileType: "map", MatrixSet: TMSWebMercatorQuad, Zoom: 0, Column: 0, Row: 0, Format: MediaTypePNG}
	engine := NewEngine(cfg, newTestLogger(), nil, nil)

	first, err := engine.ResolveIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	layer.TileCacheGeneration++
	second, err := engine.ResolveIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalKey() == second.CanonicalKey() {
		t.Fatal("child generation did not change the group cache identity")
	}
}
