package wfs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/store"
)

func newPersistenceTestStore(t *testing.T) *store.DuckDBStore {
	t.Helper()
	s, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db")})
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newPersistedRuntime(t *testing.T, s *store.DuckDBStore) *RuntimeState {
	t.Helper()
	state := NewRuntimeState(conf.WFS{}, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(state.Close)
	return state
}

func TestLockPersistsAcrossRestart(t *testing.T) {
	s := newPersistenceTestStore(t)

	first := newPersistedRuntime(t, s)
	lock, _, err := first.Locks.AcquireLockOwned("ws1", map[string][]string{"roads": {"1", "2"}}, 600, LockActionAll, "apikey:k1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// A second runtime over the same store simulates a restart.
	second := newPersistedRuntime(t, s)
	restored := second.Locks.GetLock(lock.LockID)
	if restored == nil {
		t.Fatal("lock did not survive restart")
	}
	if restored.OwnerID != "apikey:k1" || restored.WorkspaceID != "ws1" {
		t.Errorf("restored lock identity mismatch: %+v", restored)
	}
	if len(restored.FeatureIDs["roads"]) != 2 {
		t.Errorf("restored feature ids mismatch: %+v", restored.FeatureIDs)
	}
	if d := lock.ExpiresAt.Sub(restored.ExpiresAt); d > time.Second || d < -time.Second {
		t.Errorf("restored expiry drifted: %v vs %v", restored.ExpiresAt, lock.ExpiresAt)
	}
	if err := second.Locks.ValidateLockOwner(lock.LockID, "ws1", "apikey:k1"); err != nil {
		t.Errorf("restored lock must validate for its owner: %v", err)
	}
	if second.Locks.ValidateLockOwner(lock.LockID, "ws1", "apikey:other") == nil {
		t.Error("restored lock must reject a different owner")
	}
}

func TestExpiredLockNotRestored(t *testing.T) {
	s := newPersistenceTestStore(t)
	now := time.Now().UTC()
	if err := s.PutWFSLock(context.Background(), store.WFSLockRecord{
		LockID: "stale", WorkspaceID: "ws1", FeatureIDs: map[string][]string{"t": {"1"}},
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed expired lock: %v", err)
	}

	state := newPersistedRuntime(t, s)
	if state.Locks.GetLock("stale") != nil {
		t.Fatal("expired lock must not be restored")
	}
}

func TestReleaseRemovesPersistedLock(t *testing.T) {
	s := newPersistenceTestStore(t)
	first := newPersistedRuntime(t, s)
	lock, _, err := first.Locks.AcquireLockOwned("ws1", map[string][]string{"roads": {"1"}}, 600, LockActionAll, "u")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := first.Locks.ReleaseLockOwned(lock.LockID, "u"); err != nil {
		t.Fatalf("release: %v", err)
	}

	second := newPersistedRuntime(t, s)
	if second.Locks.GetLock(lock.LockID) != nil {
		t.Fatal("released lock must not be restored")
	}
}

func TestClearWorkspacePurgesPersistedState(t *testing.T) {
	s := newPersistenceTestStore(t)
	first := newPersistedRuntime(t, s)
	if _, _, err := first.Locks.AcquireLockOwned("ws1", map[string][]string{"roads": {"1"}}, 600, LockActionAll, "u"); err != nil {
		t.Fatalf("acquire ws1: %v", err)
	}
	keep, _, err := first.Locks.AcquireLockOwned("ws2", map[string][]string{"roads": {"2"}}, 600, LockActionAll, "u")
	if err != nil {
		t.Fatalf("acquire ws2: %v", err)
	}
	first.Versions.RecordInsert("ws1", "roads", "1", "u")

	first.ClearWorkspace("ws1")

	second := newPersistedRuntime(t, s)
	locks, err := s.ListWFSLocks(context.Background())
	if err != nil {
		t.Fatalf("list locks: %v", err)
	}
	if len(locks) != 1 || locks[0].LockID != keep.LockID {
		t.Fatalf("expected only the ws2 lock to survive, got %+v", locks)
	}
	if got := second.Versions.GetAllVersions("ws1", "roads", "1"); len(got) != 0 {
		t.Fatalf("ws1 version metadata must be purged, got %+v", got)
	}
}

func TestVersionMetadataSurvivesRestart(t *testing.T) {
	s := newPersistenceTestStore(t)
	first := newPersistedRuntime(t, s)
	first.Versions.RecordInsert("ws1", "roads", "7", "jwt:alice")
	first.Versions.RecordUpdate("ws1", "roads", "7", "jwt:bob")
	first.Versions.RecordDelete("ws1", "roads", "7")

	second := newPersistedRuntime(t, s)
	versions := second.Versions.GetAllVersions("ws1", "roads", "7")
	if len(versions) != 2 {
		t.Fatalf("expected 2 restored versions, got %+v", versions)
	}
	if versions[0].Version != 1 || versions[0].State != VersionStateSuperseded {
		t.Errorf("v1 = %+v, want superseded version 1", versions[0])
	}
	if versions[1].Version != 2 || versions[1].State != VersionStateRetired {
		t.Errorf("v2 = %+v, want retired version 2", versions[1])
	}
	if versions[1].PreviousRid != "roads.7@1" {
		t.Errorf("previousRid = %q, want roads.7@1", versions[1].PreviousRid)
	}
	if versions[1].ModifiedBy != "jwt:bob" {
		t.Errorf("modifiedBy = %q, want jwt:bob", versions[1].ModifiedBy)
	}
}

// failingPersistence rejects lock writes but accepts version writes, so the
// contrasting failure semantics (consistency-first locks, best-effort
// versions) can be asserted.
type failingPersistence struct {
	putLockErr error
}

func (f *failingPersistence) PutWFSLock(context.Context, store.WFSLockRecord) error {
	return f.putLockErr
}
func (f *failingPersistence) DeleteWFSLock(context.Context, string) error            { return nil }
func (f *failingPersistence) DeleteExpiredWFSLocks(context.Context, time.Time) error { return nil }
func (f *failingPersistence) DeleteWorkspaceWFSLocks(context.Context, string) error  { return nil }
func (f *failingPersistence) ListWFSLocks(context.Context) ([]store.WFSLockRecord, error) {
	return nil, nil
}
func (f *failingPersistence) PutWFSFeatureVersion(context.Context, store.WFSFeatureVersionRecord) error {
	return errors.New("version write rejected")
}
func (f *failingPersistence) ListWFSFeatureVersions(context.Context) ([]store.WFSFeatureVersionRecord, error) {
	return nil, nil
}
func (f *failingPersistence) DeleteWFSFeatureVersions(context.Context, string, string, string) error {
	return nil
}
func (f *failingPersistence) DeleteWFSFeatureVersionsBelow(context.Context, string, string, string, int) error {
	return nil
}
func (f *failingPersistence) DeleteWorkspaceWFSFeatureVersions(context.Context, string) error {
	return nil
}

func TestAcquireFailsWhenLockPersistenceFails(t *testing.T) {
	persistence := &failingPersistence{putLockErr: errors.New("disk full")}
	state := NewRuntimeState(conf.WFS{}, persistence, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer state.Close()

	lock, _, err := state.Locks.AcquireLockOwned("ws1", map[string][]string{"roads": {"1"}}, 600, LockActionAll, "u")
	if err == nil {
		t.Fatal("acquire must fail when the lock cannot be persisted")
	}
	if lock != nil {
		t.Fatalf("no lock must be granted, got %+v", lock)
	}
	if state.Locks.IsFeatureLocked("ws1", "roads", "1") {
		t.Fatal("in-memory lock entry must be rolled back")
	}

	// Version recording is best-effort: a failing persistence must not
	// prevent the in-memory record.
	v := state.Versions.RecordInsert("ws1", "roads", "1", "u")
	if v == nil || v.Version != 1 {
		t.Fatalf("version recording must succeed in-memory, got %+v", v)
	}
}
