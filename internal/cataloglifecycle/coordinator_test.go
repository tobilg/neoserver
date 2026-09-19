package cataloglifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestRecursiveWorkspaceDeletionTombstonesAndRemovesManagedState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	workspaceRecord, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.CreateService(ctx, store.CreateServiceInput{WorkspaceID: workspaceRecord.ID, Name: "source", Type: store.ServiceTypePostGIS, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.CreateLayer(ctx, store.CreateLayerInput{ServiceID: service.ID, SourceLayer: "roads", PublicID: "roads", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	assetRoot := filepath.Join(root, "style-assets")
	if err := os.MkdirAll(filepath.Join(assetRoot, workspaceRecord.ID), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetRoot, workspaceRecord.ID, "marker.png"), []byte("png"), 0o640); err != nil {
		t.Fatal(err)
	}

	registry := workspace.NewRegistry(catalog, nil)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}
	cacheManager, err := cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer cacheManager.Close()
	enforcer, err := rbac.NewEnforcerWithDefaults(rbac.NewMemoryAdapter())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := New(ctx, Dependencies{Catalog: catalog, Deletions: catalog, Registry: registry,
		Cache: cacheManager, Enforcer: enforcer, StyleAssetRoot: assetRoot,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()

	if _, err := coordinator.DeleteWorkspace(ctx, workspaceRecord.ID, false); !errors.Is(err, store.ErrResourceNotEmpty) {
		t.Fatalf("non-recursive delete error = %v", err)
	}
	operation, err := coordinator.DeleteWorkspace(ctx, workspaceRecord.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if operation == nil || operation.Phase != store.DeletionPhaseTombstoned {
		t.Fatalf("accepted operation = %+v", operation)
	}
	if _, ok := registry.GetByID(workspaceRecord.ID); ok {
		t.Fatal("workspace remained in live registry after deletion was accepted")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		current, err := coordinator.Get(ctx, operation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == store.DeletionCompleted {
			break
		}
		if current.Status == store.DeletionFailed {
			t.Fatalf("deletion failed in %s: %s", current.Phase, current.LastError)
		}
		if time.Now().After(deadline) {
			t.Fatalf("deletion did not complete: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(assetRoot, workspaceRecord.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed asset directory still exists: %v", err)
	}
	if _, err := catalog.GetWorkspace(ctx, workspaceRecord.ID); err != store.ErrNotFound {
		t.Fatalf("workspace still exists: %v", err)
	}
}

func TestFailedDeletionRemainsTombstonedAndCanBeRetried(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	workspaceRecord, _ := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "retry"})
	assetRoot := filepath.Join(root, "assets")
	activeAssets := filepath.Join(assetRoot, workspaceRecord.ID)
	if err := os.MkdirAll(activeAssets, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeAssets, "marker.png"), []byte("png"), 0o640); err != nil {
		t.Fatal(err)
	}
	registry := workspace.NewRegistry(catalog, nil)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())
	defer cacheManager.Close()
	coordinator, err := New(ctx, Dependencies{Catalog: catalog, Deletions: catalog, Registry: registry,
		Cache: cacheManager, StyleAssetRoot: assetRoot, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	plan, err := coordinator.PlanWorkspace(ctx, workspaceRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	operation, _, err := catalog.BeginCatalogDeletion(ctx, *plan)
	if err != nil {
		t.Fatal(err)
	}
	conflictingStage := filepath.Join(assetRoot, ".deleting", operation.ID)
	if err := os.MkdirAll(conflictingStage, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	waitForDeletionStatus(t, coordinator, operation.ID, store.DeletionFailed)
	failed, err := coordinator.Get(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.AttemptCount != 1 {
		t.Fatalf("failed attempt count = %d, want 1", failed.AttemptCount)
	}
	if _, err := catalog.GetWorkspace(ctx, workspaceRecord.ID); err != store.ErrNotFound {
		t.Fatalf("failed deletion target became visible: %v", err)
	}
	if err := os.RemoveAll(conflictingStage); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Retry(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	waitForDeletionStatus(t, coordinator, operation.ID, store.DeletionCompleted)
	completed, err := coordinator.Get(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.AttemptCount != 2 {
		t.Fatalf("completed attempt count = %d, want 2", completed.AttemptCount)
	}
	if _, err := os.Stat(activeAssets); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("assets remained after retry: %v", err)
	}
}

func waitForDeletionStatus(t *testing.T, coordinator *Coordinator, operationID string, wanted store.DeletionStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		operation, err := coordinator.Get(context.Background(), operationID)
		if err != nil {
			t.Fatal(err)
		}
		if operation.Status == wanted {
			return
		}
		if operation.Status == store.DeletionFailed && wanted != store.DeletionFailed {
			t.Fatalf("operation failed in %s: %s", operation.Phase, operation.LastError)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("operation %s did not reach %s", operationID, wanted)
}
