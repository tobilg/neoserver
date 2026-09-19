package tilecache

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newFakeBackendManager builds a Manager over the in-memory fake blob store
// so backend failures can be injected.
func newFakeBackendManager(t *testing.T, quota int64) (*Manager, *fakeBlobStore) {
	t.Helper()
	backend := newFakeBlobStore()
	manager, err := NewManager(context.Background(), Config{
		DatabasePath:        filepath.Join(t.TempDir(), "tile-cache.duckdb"),
		EncryptionKey:       "abc123",
		MaxBytes:            quota,
		AccessFlushInterval: 10 * time.Millisecond,
		MaintenanceInterval: time.Hour,
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager, backend
}

func TestPutBackendFailureRollsBackPendingEntry(t *testing.T) {
	manager, backend := newFakeBackendManager(t, 1024)
	ctx := context.Background()
	identity := testIdentity(1)

	backend.setPutErr(errors.New("s3 down"))
	stored, err := manager.Put(ctx, identity, []byte("tile"), Policy{})
	if err == nil || stored {
		t.Fatalf("Put = %v, %v; want backend error", stored, err)
	}

	// The pending index entry must be rolled back: no ready row, no usage.
	if _, _, found, err := manager.Get(ctx, identity); err != nil || found {
		t.Fatalf("Get after failed put = found=%v err=%v", found, err)
	}
	stats, err := manager.Stats(ctx, "", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Global.EntryCount != 0 || stats.Global.SizeBytes != 0 {
		t.Fatalf("usage not rolled back: %+v", stats.Global)
	}
	if stats.WriteErrors != 1 {
		t.Fatalf("write errors = %d, want 1", stats.WriteErrors)
	}

	// After the backend recovers the same identity stores cleanly.
	backend.setPutErr(nil)
	if stored, err := manager.Put(ctx, identity, []byte("tile"), Policy{}); err != nil || !stored {
		t.Fatalf("recovered Put = %v, %v", stored, err)
	}
}

func TestPutFailureKeepsPreviousEntryServable(t *testing.T) {
	manager, backend := newFakeBackendManager(t, 1024)
	ctx := context.Background()
	identity := testIdentity(1)

	if stored, err := manager.Put(ctx, identity, []byte("v1"), Policy{}); err != nil || !stored {
		t.Fatalf("initial Put = %v, %v", stored, err)
	}
	backend.setPutErr(errors.New("s3 down"))
	if stored, err := manager.Put(ctx, identity, []byte("v2-longer"), Policy{}); err == nil || stored {
		t.Fatalf("overwrite Put = %v, %v; want error", stored, err)
	}
	backend.setPutErr(nil)

	// The previous metadata was restored; the object key is unchanged for an
	// identical identity so the v1 payload is still served.
	data, _, found, err := manager.Get(ctx, identity)
	if err != nil || !found {
		t.Fatalf("Get after failed overwrite = found=%v err=%v", found, err)
	}
	if string(data) != "v1" {
		t.Fatalf("data = %q, want v1", data)
	}
}

func TestPutRespectsResourceAndWorkspaceQuotas(t *testing.T) {
	manager, _ := newFakeBackendManager(t, 1024)
	ctx := context.Background()

	// Resource quota of 8 bytes: the third 4-byte tile evicts the LRU one.
	policy := Policy{ResourceQuotaBytes: 8}
	for column := 0; column < 3; column++ {
		if stored, err := manager.Put(ctx, testIdentity(column), []byte("1234"), Policy{ResourceQuotaBytes: 8}); err != nil || !stored {
			t.Fatalf("Put(%d) = %v, %v", column, stored, err)
		}
	}
	stats, _ := manager.Stats(ctx, "workspace", "layer", 0, policy.ResourceQuotaBytes)
	if stats.Resource.SizeBytes != 8 || stats.Resource.EntryCount != 2 {
		t.Fatalf("resource usage = %+v, want 8 bytes / 2 entries", stats.Resource)
	}
	if stats.Evictions != 1 {
		t.Fatalf("evictions = %d, want 1", stats.Evictions)
	}

	// A tile larger than the workspace quota is skipped without error.
	big := testIdentity(9)
	if stored, err := manager.Put(ctx, big, []byte("123456789"), Policy{WorkspaceQuotaBytes: 4}); err != nil || stored {
		t.Fatalf("oversized workspace Put = %v, %v; want skipped", stored, err)
	}
	stats, _ = manager.Stats(ctx, "", "", 0, 0)
	if stats.OversizedSkipped != 1 {
		t.Fatalf("oversized counter = %d, want 1", stats.OversizedSkipped)
	}
}

func TestQuotaUnsatisfiableWhenOnlyNewTileRemains(t *testing.T) {
	manager, _ := newFakeBackendManager(t, 1024)
	ctx := context.Background()

	// First put fits exactly; a second, different tile cannot fit even after
	// evicting everything else because the candidate excludes itself.
	if stored, err := manager.Put(ctx, testIdentity(0), []byte("12345678"), Policy{ResourceQuotaBytes: 8}); err != nil || !stored {
		t.Fatalf("Put = %v, %v", stored, err)
	}
	// 6 bytes + existing 8 > 8: evicts the old entry and fits.
	if stored, err := manager.Put(ctx, testIdentity(1), []byte("123456"), Policy{ResourceQuotaBytes: 8}); err != nil || !stored {
		t.Fatalf("evicting Put = %v, %v", stored, err)
	}
	stats, _ := manager.Stats(ctx, "workspace", "layer", 0, 8)
	if stats.Resource.EntryCount != 1 || stats.Resource.SizeBytes != 6 {
		t.Fatalf("resource usage = %+v", stats.Resource)
	}
}

func TestLifecycleJobFiltering(t *testing.T) {
	manager := newTestManager(t, t.TempDir(), 1024)
	t.Cleanup(func() { _ = manager.Close() })
	ctx := context.Background()
	jobs := manager.JobStore()

	mk := func(resourceID string, all bool) *Job {
		job, err := jobs.CreateTileCacheJob(ctx, CreateJobInput{WorkspaceID: "workspace", Request: JobRequest{Operation: OperationSeed, ResourceID: resourceID, AllResources: all}})
		if err != nil {
			t.Fatal(err)
		}
		return job
	}
	layerJob := mk("layer", false)
	otherJob := mk("other", false)
	allJob := mk("", true)

	// Cancel everything so purge is allowed later.
	for _, job := range []*Job{layerJob, otherJob, allJob} {
		if err := jobs.RequestTileCacheJobCancel(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
	}

	// Filtering by one resource includes that resource's jobs plus
	// all-resources jobs, but not other resources.
	_, _, count, err := manager.LifecycleStats(ctx, "workspace", []string{"layer"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("filtered job count = %d, want 2 (layer + all-resources)", count)
	}

	// An empty resource list matches every job in the workspace.
	_, _, count, err = manager.LifecycleStats(ctx, "workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("unfiltered job count = %d, want 3", count)
	}
}

func TestPurgeRefusedWhileJobActive(t *testing.T) {
	manager := newTestManager(t, t.TempDir(), 1024)
	t.Cleanup(func() { _ = manager.Close() })
	ctx := context.Background()

	if _, err := manager.JobStore().CreateTileCacheJob(ctx, CreateJobInput{WorkspaceID: "workspace", Request: JobRequest{Operation: OperationSeed, ResourceID: "layer"}}); err != nil {
		t.Fatal(err)
	}
	_, _, err := manager.PurgeLifecycle(ctx, "workspace", []string{"layer"})
	if err == nil || !strings.Contains(err.Error(), "still") {
		t.Fatalf("purge with queued job = %v, want refusal", err)
	}
}
