package store

import (
	"context"
	"testing"
	"time"
)

func TestWFSLockCRUD(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	rec := WFSLockRecord{
		LockID:      "lock-1",
		WorkspaceID: "ws1",
		OwnerID:     "apikey:k1",
		FeatureIDs:  map[string][]string{"roads": {"1", "2"}, "rivers": {"9"}},
		CreatedAt:   now,
		ExpiresAt:   now.Add(time.Hour),
	}
	if err := store.PutWFSLock(ctx, rec); err != nil {
		t.Fatalf("put lock: %v", err)
	}

	locks, err := store.ListWFSLocks(ctx)
	if err != nil {
		t.Fatalf("list locks: %v", err)
	}
	if len(locks) != 1 {
		t.Fatalf("expected 1 lock, got %d", len(locks))
	}
	got := locks[0]
	if got.LockID != rec.LockID || got.WorkspaceID != rec.WorkspaceID || got.OwnerID != rec.OwnerID {
		t.Errorf("lock identity mismatch: %+v", got)
	}
	if len(got.FeatureIDs["roads"]) != 2 || got.FeatureIDs["rivers"][0] != "9" {
		t.Errorf("feature ids not round-tripped: %+v", got.FeatureIDs)
	}
	if !got.ExpiresAt.Equal(rec.ExpiresAt) {
		t.Errorf("expiry not round-tripped: got %v want %v", got.ExpiresAt, rec.ExpiresAt)
	}

	// Replacing the same lock ID must not duplicate.
	rec.OwnerID = "apikey:k2"
	if err := store.PutWFSLock(ctx, rec); err != nil {
		t.Fatalf("replace lock: %v", err)
	}
	locks, _ = store.ListWFSLocks(ctx)
	if len(locks) != 1 || locks[0].OwnerID != "apikey:k2" {
		t.Fatalf("expected replaced lock, got %+v", locks)
	}

	if err := store.DeleteWFSLock(ctx, "lock-1"); err != nil {
		t.Fatalf("delete lock: %v", err)
	}
	if locks, _ = store.ListWFSLocks(ctx); len(locks) != 0 {
		t.Fatalf("expected no locks after delete, got %d", len(locks))
	}
	// Deleting a missing lock is not an error.
	if err := store.DeleteWFSLock(ctx, "lock-1"); err != nil {
		t.Fatalf("delete missing lock: %v", err)
	}
}

func TestWFSLockExpiryFiltering(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC()
	expired := WFSLockRecord{
		LockID: "expired", WorkspaceID: "ws1", FeatureIDs: map[string][]string{"a": {"1"}},
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
	}
	live := WFSLockRecord{
		LockID: "live", WorkspaceID: "ws1", FeatureIDs: map[string][]string{"a": {"2"}},
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	for _, rec := range []WFSLockRecord{expired, live} {
		if err := store.PutWFSLock(ctx, rec); err != nil {
			t.Fatalf("put %s: %v", rec.LockID, err)
		}
	}

	// List excludes expired rows without deleting them.
	locks, err := store.ListWFSLocks(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(locks) != 1 || locks[0].LockID != "live" {
		t.Fatalf("expected only the live lock, got %+v", locks)
	}

	// DeleteExpired removes only the expired row.
	if err := store.DeleteExpiredWFSLocks(ctx, now); err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	locks, _ = store.ListWFSLocks(ctx)
	if len(locks) != 1 || locks[0].LockID != "live" {
		t.Fatalf("live lock must survive expiry pruning, got %+v", locks)
	}
}

func TestWFSLockWorkspaceScopedDelete(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC()
	for _, rec := range []WFSLockRecord{
		{LockID: "a", WorkspaceID: "ws1", FeatureIDs: map[string][]string{"t": {"1"}}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		{LockID: "b", WorkspaceID: "ws2", FeatureIDs: map[string][]string{"t": {"2"}}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
	} {
		if err := store.PutWFSLock(ctx, rec); err != nil {
			t.Fatalf("put %s: %v", rec.LockID, err)
		}
	}
	if err := store.DeleteWorkspaceWFSLocks(ctx, "ws1"); err != nil {
		t.Fatalf("delete workspace locks: %v", err)
	}
	locks, _ := store.ListWFSLocks(ctx)
	if len(locks) != 1 || locks[0].WorkspaceID != "ws2" {
		t.Fatalf("expected only ws2 lock to remain, got %+v", locks)
	}
}

// Reopening a released baseline preserves the WFS runtime tables.
func TestBaselineWFSRuntimeTablesSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/catalog.db"
	s, _, err := Init(Config{Path: dbPath})
	if err != nil {
		t.Fatalf("init store: %v", err)
	}

	s.Close()

	reopened, err := Open(Config{Path: dbPath})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	if err := reopened.PutWFSLock(ctx, WFSLockRecord{
		LockID: "l", WorkspaceID: "ws", FeatureIDs: map[string][]string{"t": {"1"}},
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("wfs_locks not migrated: %v", err)
	}
	if err := reopened.PutWFSFeatureVersion(ctx, WFSFeatureVersionRecord{
		WorkspaceID: "ws", LayerID: "t", FeatureID: "1", Version: 1, State: "valid", CreatedAt: now,
	}); err != nil {
		t.Fatalf("wfs_feature_versions not migrated: %v", err)
	}

	var version int
	if err := reopened.db.QueryRow("SELECT max(version) FROM schema_info").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
}

func TestWFSFeatureVersionCRUD(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	records := []WFSFeatureVersionRecord{
		{WorkspaceID: "ws1", LayerID: "roads", FeatureID: "7", Version: 1, State: "superseded", ModifiedBy: "jwt:alice", CreatedAt: now.Add(-time.Minute)},
		{WorkspaceID: "ws1", LayerID: "roads", FeatureID: "7", Version: 2, PreviousRid: "roads.7@1", State: "valid", ModifiedBy: "jwt:alice", CreatedAt: now},
		{WorkspaceID: "ws2", LayerID: "rivers", FeatureID: "3", Version: 1, State: "valid", CreatedAt: now},
	}
	for _, rec := range records {
		if err := store.PutWFSFeatureVersion(ctx, rec); err != nil {
			t.Fatalf("put version: %v", err)
		}
	}

	got, err := store.ListWFSFeatureVersions(ctx)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(got))
	}
	// Ordered by workspace, layer, feature, version.
	if got[0].Version != 1 || got[1].Version != 2 || got[1].PreviousRid != "roads.7@1" {
		t.Errorf("ordering or fields wrong: %+v", got[:2])
	}

	// State update via INSERT OR REPLACE on the same PK.
	rec := records[1]
	rec.State = "retired"
	if err := store.PutWFSFeatureVersion(ctx, rec); err != nil {
		t.Fatalf("replace version: %v", err)
	}
	got, _ = store.ListWFSFeatureVersions(ctx)
	if len(got) != 3 || got[1].State != "retired" {
		t.Fatalf("expected state replacement, got %+v", got)
	}

	// Trim below a minimum version.
	if err := store.DeleteWFSFeatureVersionsBelow(ctx, "ws1", "roads", "7", 2); err != nil {
		t.Fatalf("delete below: %v", err)
	}
	got, _ = store.ListWFSFeatureVersions(ctx)
	if len(got) != 2 {
		t.Fatalf("expected trim to remove v1, got %+v", got)
	}

	// Feature-scoped delete.
	if err := store.DeleteWFSFeatureVersions(ctx, "ws1", "roads", "7"); err != nil {
		t.Fatalf("delete feature versions: %v", err)
	}
	// Workspace-scoped delete.
	if err := store.DeleteWorkspaceWFSFeatureVersions(ctx, "ws2"); err != nil {
		t.Fatalf("delete workspace versions: %v", err)
	}
	if got, _ = store.ListWFSFeatureVersions(ctx); len(got) != 0 {
		t.Fatalf("expected empty version table, got %+v", got)
	}
}
