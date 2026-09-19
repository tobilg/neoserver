package tilecache

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func testIdentity(column int) Identity {
	return Identity{WorkspaceID: "workspace", WorkspaceRevision: 1, ResourceID: "layer", ResourceKind: "feature", Generation: 1, TileType: "map", MatrixSet: "WebMercatorQuad", Zoom: 2, Column: column, Row: 1, StyleDigest: "default", Format: "image/png"}
}

func newTestManager(t *testing.T, root string, quota int64) *Manager {
	t.Helper()
	backend, err := NewFilesystemStore(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(context.Background(), Config{DatabasePath: filepath.Join(root, "tile-cache.duckdb"), EncryptionKey: "abc123", MaxBytes: quota, AccessFlushInterval: 10 * time.Millisecond, MaintenanceInterval: time.Hour}, backend)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestManagerPersistsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, 1024)
	identity := testIdentity(1)
	stored, err := manager.Put(context.Background(), identity, []byte("persistent tile"), Policy{})
	if err != nil || !stored {
		t.Fatalf("Put() = %v, %v", stored, err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	manager = newTestManager(t, root, 1024)
	t.Cleanup(func() { _ = manager.Close() })
	data, entry, found, err := manager.Get(context.Background(), identity)
	if err != nil || !found || string(data) != "persistent tile" {
		t.Fatalf("Get() = %q, found=%v, err=%v", data, found, err)
	}
	if entry.SizeBytes != int64(len(data)) {
		t.Fatalf("entry size = %d", entry.SizeBytes)
	}
	stats, err := manager.Stats(context.Background(), "workspace", "layer", 800, 400)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Global.EntryCount != 1 || stats.Workspace == nil || stats.Resource == nil {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestManagerQuotaEvictsAndSkipsOversized(t *testing.T) {
	manager := newTestManager(t, t.TempDir(), 8)
	t.Cleanup(func() { _ = manager.Close() })
	for column := 0; column < 3; column++ {
		stored, err := manager.Put(context.Background(), testIdentity(column), []byte("1234"), Policy{})
		if err != nil || !stored {
			t.Fatalf("Put(%d) = %v, %v", column, stored, err)
		}
	}
	stats, _ := manager.Stats(context.Background(), "", "", 0, 0)
	if stats.Global.SizeBytes != 8 || stats.Global.EntryCount != 2 || stats.Evictions != 1 {
		t.Fatalf("quota stats = %+v", stats)
	}
	stored, err := manager.Put(context.Background(), testIdentity(4), make([]byte, 9), Policy{})
	if err != nil || stored {
		t.Fatalf("oversized Put() = %v, %v", stored, err)
	}
}

func TestManagerDeleteSelectorsAndExactIdentity(t *testing.T) {
	manager := newTestManager(t, t.TempDir(), 1024)
	t.Cleanup(func() { _ = manager.Close() })
	for column := 0; column < 3; column++ {
		_, _ = manager.Put(context.Background(), testIdentity(column), []byte("tile"), Policy{})
	}
	result, err := manager.DeleteIdentity(context.Background(), testIdentity(0))
	if err != nil || result.Entries != 1 {
		t.Fatalf("DeleteIdentity() = %+v, %v", result, err)
	}
	result, err = manager.Delete(context.Background(), Selector{WorkspaceID: "workspace", ResourceID: "layer", TileType: "map"})
	if err != nil || result.Entries != 2 {
		t.Fatalf("Delete() = %+v, %v", result, err)
	}
	stats, _ := manager.Stats(context.Background(), "", "", 0, 0)
	if stats.Global.EntryCount != 0 || stats.Global.SizeBytes != 0 {
		t.Fatalf("cache was not emptied: %+v", stats.Global)
	}
}

func TestManagerExistsRepairsMissingObject(t *testing.T) {
	manager := newTestManager(t, t.TempDir(), 1024)
	t.Cleanup(func() { _ = manager.Close() })
	identity := testIdentity(0)
	if stored, err := manager.Put(context.Background(), identity, []byte("tile"), Policy{}); err != nil || !stored {
		t.Fatalf("Put() = %v, %v", stored, err)
	}
	if err := manager.backend.Delete(context.Background(), identity.ObjectKey(manager.config.ObjectPrefix)); err != nil {
		t.Fatal(err)
	}
	exists, err := manager.Exists(context.Background(), identity)
	if err != nil || exists {
		t.Fatalf("Exists() = %v, %v", exists, err)
	}
	stats, err := manager.Stats(context.Background(), "", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Global.EntryCount != 0 || stats.Global.SizeBytes != 0 || stats.OrphansRepaired != 1 {
		t.Fatalf("repaired stats = %+v", stats)
	}
}

func TestLifecyclePurgeRemovesResourcePayloadsJobsChunksAndUsage(t *testing.T) {
	manager := newTestManager(t, t.TempDir(), 1024)
	t.Cleanup(func() { _ = manager.Close() })
	ctx := context.Background()
	if stored, err := manager.Put(ctx, testIdentity(1), []byte("target"), Policy{}); err != nil || !stored {
		t.Fatalf("put target = %v, %v", stored, err)
	}
	siblingIdentity := testIdentity(2)
	siblingIdentity.ResourceID = "sibling"
	if stored, err := manager.Put(ctx, siblingIdentity, []byte("sibling"), Policy{}); err != nil || !stored {
		t.Fatalf("put sibling = %v, %v", stored, err)
	}
	job, err := manager.JobStore().CreateTileCacheJob(ctx, CreateJobInput{WorkspaceID: "workspace", Request: JobRequest{Operation: OperationSeed, ResourceID: "layer"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.JobStore().ReplaceTileCacheJobChunks(ctx, job.ID, []JobChunk{{Zoom: 0, MinCol: 0, MaxCol: 0, MinRow: 0, MaxRow: 0}}); err != nil {
		t.Fatal(err)
	}
	if err := manager.JobStore().RequestTileCacheJobCancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	entries, bytes, jobs, err := manager.LifecycleStats(ctx, "workspace", []string{"layer"})
	if err != nil || entries != 1 || bytes != int64(len("target")) || jobs != 1 {
		t.Fatalf("lifecycle stats = %d, %d, %d, %v", entries, bytes, jobs, err)
	}
	deleted, purgedJobs, err := manager.PurgeLifecycle(ctx, "workspace", []string{"layer"})
	if err != nil || deleted.Entries != 1 || purgedJobs != 1 {
		t.Fatalf("lifecycle purge = %+v, %d, %v", deleted, purgedJobs, err)
	}
	if _, err := manager.JobStore().GetTileCacheJob(ctx, job.ID); err != ErrJobNotFound {
		t.Fatalf("purged job lookup = %v", err)
	}
	stats, err := manager.Stats(ctx, "workspace", "layer", 0, 0)
	if err != nil || stats.Resource == nil || stats.Resource.EntryCount != 0 || stats.Resource.SizeBytes != 0 {
		t.Fatalf("target usage after purge = %+v, %v", stats.Resource, err)
	}
	if _, _, found, err := manager.Get(ctx, siblingIdentity); err != nil || !found {
		t.Fatalf("sibling cache entry was removed: found=%v err=%v", found, err)
	}
}
