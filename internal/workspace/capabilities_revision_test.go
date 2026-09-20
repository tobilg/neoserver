package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestCapabilitiesRevisionSnapshotsAndReload(t *testing.T) {
	ctx := context.Background()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	registry := NewRegistry(catalog, nil)
	defer registry.Close()
	old, err := registry.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "sequence"})
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.UpdateWMTSSettings(ctx, old.ID, store.WMTSSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	current, _ := registry.GetByID(old.ID)
	if old.CapabilitiesRevision() != 1 || current.CapabilitiesRevision() != 2 {
		t.Fatal("snapshot revision changed or runtime sequence did not advance")
	}
	if err = registry.Load(ctx); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := registry.GetByID(old.ID)
	if reloaded.CapabilitiesRevision() != current.CapabilitiesRevision() {
		t.Fatal("reload changed the durable revision")
	}
	name := "renamed"
	if _, err = registry.UpdateWorkspace(ctx, old.ID, store.UpdateWorkspaceInput{Name: &name}); err != nil {
		t.Fatal(err)
	}
	renamed, _ := registry.GetByID(old.ID)
	if renamed.CapabilitiesRevision() != 3 || current.CapabilitiesRevision() != 2 {
		t.Fatal("rename or snapshot sequence is incorrect")
	}
}
