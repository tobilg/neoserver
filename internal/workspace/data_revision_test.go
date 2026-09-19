package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

type failingDataCompletion struct {
	*store.DuckDBStore
	fail bool
}

func (s *failingDataCompletion) SetDataWritePending(ctx context.Context, id string, pending bool) (int64, error) {
	if !pending && s.fail {
		return 0, errors.New("injected completion failure")
	}
	return s.DuckDBStore.SetDataWritePending(ctx, id, pending)
}

func TestDataWriteBarrierPersistsAndRecoversAfterCompletionFailure(t *testing.T) {
	ctx := context.Background()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	fault := &failingDataCompletion{DuckDBStore: catalog, fail: true}
	r := NewRegistry(fault, nil)
	defer r.Close()
	ws, err := r.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "barrier"})
	if err != nil {
		t.Fatal(err)
	}
	old, release, _ := r.AcquireByID(ws.ID)
	defer release()
	before, _ := old.DataCacheState()
	finish, err := r.BeginDataWrite(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if revision, pending := old.DataCacheState(); !pending || revision <= before {
		t.Fatal("existing job did not observe dirty barrier")
	}
	if err := finish(); err == nil {
		t.Fatal("injected failure ignored")
	}
	if _, pending := old.DataCacheState(); !pending {
		t.Fatal("cache reenabled without durable completion")
	}
	restarted := NewRegistry(catalog, nil)
	defer restarted.Close()
	if err := restarted.Load(ctx); err != nil {
		t.Fatal(err)
	}
	restored, _ := restarted.GetByID(ws.ID)
	dirtyRevision, pending := restored.DataCacheState()
	if !pending {
		t.Fatal("dirty marker was not persisted")
	}
	if err := catalog.RecoverDataWrites(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Load(ctx); err != nil {
		t.Fatal(err)
	}
	restored, _ = restarted.GetByID(ws.ID)
	if revision, pending := restored.DataCacheState(); pending || revision <= dirtyRevision {
		t.Fatal("recovery reused a dirty generation")
	}
}
