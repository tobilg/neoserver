package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestAssetManifestSurvivesRestartAndTransientRevisions(t *testing.T) {
	ctx := context.Background()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	ws, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "assets"})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(catalog, nil)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	var previous string
	for _, digest := range []string{"red", "blue"} {
		if _, err := catalog.UpsertStyleAsset(ctx, store.UpsertStyleAssetInput{WorkspaceID: ws.ID, Name: "marker.svg", SHA256: digest}); err != nil {
			t.Fatal(err)
		}
		// Runtime-only service reloads must not mask the asset's durable identity.
		registry.workspacesByID[ws.ID].TileRevision = 100
		if err := registry.RefreshStyleAssets(ctx, ws.ID); err != nil {
			t.Fatal(err)
		}
		current, _ := registry.GetByID(ws.ID)
		if current.StyleAssetDigest == "" || current.StyleAssetDigest == previous {
			t.Fatal("asset replacement did not change the manifest")
		}
		previous = current.StyleAssetDigest
	}
	restarted := NewRegistry(catalog, nil)
	if err := restarted.Load(ctx); err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	current, _ := restarted.GetByID(ws.ID)
	if current.StyleAssetDigest != previous {
		t.Fatal("asset identity changed across restart")
	}
}
