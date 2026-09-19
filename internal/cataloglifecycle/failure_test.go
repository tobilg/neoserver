package cataloglifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tobilg/neoserver/internal/mosaiccatalog"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/workspace"
)

var errInjectedLifecycle = errors.New("injected lifecycle failure")

type faultOnce struct {
	mu     sync.Mutex
	armed  bool
	called int
}

func (f *faultOnce) trip() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called++
	if f.armed {
		f.armed = false
		return errInjectedLifecycle
	}
	return nil
}

type fakeTileLifecycle struct{ purge faultOnce }

func (*fakeTileLifecycle) LifecycleStats(context.Context, string, []string) (int64, int64, int64, error) {
	return 1, 10, 1, nil
}
func (f *fakeTileLifecycle) PurgeLifecycle(context.Context, string, []string) (tilecache.DeleteResult, int64, error) {
	if err := f.purge.trip(); err != nil {
		return tilecache.DeleteResult{}, 0, err
	}
	return tilecache.DeleteResult{Entries: 1, Bytes: 10}, 1, nil
}
func (*fakeTileLifecycle) LifecycleInventory(context.Context) ([]tilecache.LifecycleOwner, error) {
	return nil, nil
}

type fakeJobLifecycle struct{ quiesce faultOnce }

func (f *fakeJobLifecycle) QuiesceLifecycle(context.Context, string, []string) error {
	return f.quiesce.trip()
}

type fakeMosaicLifecycle struct{ remove faultOnce }

func (*fakeMosaicLifecycle) LifecycleStats(context.Context, string, []string) (int64, int64, int64, error) {
	return 1, 1, 1, nil
}
func (f *fakeMosaicLifecycle) QuiesceAndDeleteLifecycle(context.Context, string, []string) error {
	return f.remove.trip()
}
func (*fakeMosaicLifecycle) LifecycleInventory(context.Context) ([]mosaiccatalog.LifecycleOwner, error) {
	return nil, nil
}

type fakeAssetStore struct {
	stage  faultOnce
	remove faultOnce
}

func (*fakeAssetStore) Count(string) (int64, error)  { return 1, nil }
func (f *fakeAssetStore) Stage(string, string) error { return f.stage.trip() }
func (f *fakeAssetStore) RemoveStaged(string) error  { return f.remove.trip() }

type faultDeletionStore struct {
	store.CatalogDeletionStore
	mu               sync.Mutex
	failUpdateStatus store.DeletionStatus
	failUpdatePhase  store.DeletionPhase
	failCommit       bool
}

func (s *faultDeletionStore) UpdateCatalogDeletion(ctx context.Context, id string, statusValue store.DeletionStatus, phase store.DeletionPhase, message string) error {
	s.mu.Lock()
	if s.failUpdatePhase == phase && (s.failUpdateStatus == "" || s.failUpdateStatus == statusValue) {
		s.failUpdatePhase = ""
		s.failUpdateStatus = ""
		s.mu.Unlock()
		return errInjectedLifecycle
	}
	s.mu.Unlock()
	return s.CatalogDeletionStore.UpdateCatalogDeletion(ctx, id, statusValue, phase, message)
}

func (s *faultDeletionStore) CommitCatalogDeletion(ctx context.Context, operationID string) error {
	s.mu.Lock()
	if s.failCommit {
		s.failCommit = false
		s.mu.Unlock()
		return errInjectedLifecycle
	}
	s.mu.Unlock()
	return s.CatalogDeletionStore.CommitCatalogDeletion(ctx, operationID)
}

func TestDeletionFailureMatrixIsDurableAndRetryable(t *testing.T) {
	tests := []struct {
		name          string
		expectedPhase store.DeletionPhase
		arm           func(*fakeJobLifecycle, *fakeMosaicLifecycle, *fakeTileLifecycle, *fakeAssetStore, *faultDeletionStore)
	}{
		{name: "job quiesce", expectedPhase: store.DeletionPhaseTombstoned, arm: func(j *fakeJobLifecycle, _ *fakeMosaicLifecycle, _ *fakeTileLifecycle, _ *fakeAssetStore, _ *faultDeletionStore) {
			j.quiesce.armed = true
		}},
		{name: "persist jobs quiesced", expectedPhase: store.DeletionPhaseTombstoned, arm: func(_ *fakeJobLifecycle, _ *fakeMosaicLifecycle, _ *fakeTileLifecycle, _ *fakeAssetStore, d *faultDeletionStore) {
			d.failUpdateStatus, d.failUpdatePhase = store.DeletionRunning, store.DeletionPhaseJobsQuiesced
		}},
		{name: "mosaic cleanup", expectedPhase: store.DeletionPhaseJobsQuiesced, arm: func(_ *fakeJobLifecycle, m *fakeMosaicLifecycle, _ *fakeTileLifecycle, _ *fakeAssetStore, _ *faultDeletionStore) {
			m.remove.armed = true
		}},
		{name: "tile cache purge", expectedPhase: store.DeletionPhaseJobsQuiesced, arm: func(_ *fakeJobLifecycle, _ *fakeMosaicLifecycle, c *fakeTileLifecycle, _ *fakeAssetStore, _ *faultDeletionStore) {
			c.purge.armed = true
		}},
		{name: "style asset staging", expectedPhase: store.DeletionPhaseJobsQuiesced, arm: func(_ *fakeJobLifecycle, _ *fakeMosaicLifecycle, _ *fakeTileLifecycle, a *fakeAssetStore, _ *faultDeletionStore) {
			a.stage.armed = true
		}},
		{name: "persist auxiliary removal", expectedPhase: store.DeletionPhaseJobsQuiesced, arm: func(_ *fakeJobLifecycle, _ *fakeMosaicLifecycle, _ *fakeTileLifecycle, _ *fakeAssetStore, d *faultDeletionStore) {
			d.failUpdateStatus, d.failUpdatePhase = store.DeletionRunning, store.DeletionPhaseAuxiliaryRemoved
		}},
		{name: "catalog commit", expectedPhase: store.DeletionPhaseAuxiliaryRemoved, arm: func(_ *fakeJobLifecycle, _ *fakeMosaicLifecycle, _ *fakeTileLifecycle, _ *fakeAssetStore, d *faultDeletionStore) {
			d.failCommit = true
		}},
		{name: "staged asset removal", expectedPhase: store.DeletionPhaseCatalogCommitted, arm: func(_ *fakeJobLifecycle, _ *fakeMosaicLifecycle, _ *fakeTileLifecycle, a *fakeAssetStore, _ *faultDeletionStore) {
			a.remove.armed = true
		}},
		{name: "persist runtime cleared", expectedPhase: store.DeletionPhaseCatalogCommitted, arm: func(_ *fakeJobLifecycle, _ *fakeMosaicLifecycle, _ *fakeTileLifecycle, _ *fakeAssetStore, d *faultDeletionStore) {
			d.failUpdateStatus, d.failUpdatePhase = store.DeletionRunning, store.DeletionPhaseRuntimeCleared
		}},
		{name: "persist completion", expectedPhase: store.DeletionPhaseRuntimeCleared, arm: func(_ *fakeJobLifecycle, _ *fakeMosaicLifecycle, _ *fakeTileLifecycle, _ *fakeAssetStore, d *faultDeletionStore) {
			d.failUpdateStatus, d.failUpdatePhase = store.DeletionCompleted, store.DeletionPhaseCompleted
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db"), EncryptionKey: "abc123"})
			if err != nil {
				t.Fatal(err)
			}
			defer catalog.Close()
			workspaceRecord, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "target"})
			if err != nil {
				t.Fatal(err)
			}
			sourceMarker := filepath.Join(root, "source-data.must-survive")
			if err := os.WriteFile(sourceMarker, []byte("source"), 0o600); err != nil {
				t.Fatal(err)
			}
			registry := workspace.NewRegistry(catalog, nil)
			if err := registry.Load(ctx); err != nil {
				t.Fatal(err)
			}
			defer registry.Close()
			jobs := &fakeJobLifecycle{}
			mosaic := &fakeMosaicLifecycle{}
			tiles := &fakeTileLifecycle{}
			assets := &fakeAssetStore{}
			deletions := &faultDeletionStore{CatalogDeletionStore: catalog}
			test.arm(jobs, mosaic, tiles, assets, deletions)
			coordinator, err := New(ctx, Dependencies{
				Catalog: catalog, Deletions: deletions, Registry: registry, TileJobs: jobs, Mosaic: mosaic,
				TileCache: tiles, Assets: assets, StyleAssetRoot: filepath.Join(root, "assets"),
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer coordinator.Close()

			operation, err := coordinator.DeleteWorkspace(ctx, workspaceRecord.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			waitForDeletionStatus(t, coordinator, operation.ID, store.DeletionFailed)
			failed, err := coordinator.Get(ctx, operation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if failed.Phase != test.expectedPhase || failed.AttemptCount != 1 || !strings.Contains(failed.LastError, errInjectedLifecycle.Error()) {
				t.Fatalf("failed operation = %+v", failed)
			}
			if len(failed.LastError) > 2048 {
				t.Fatalf("error was not bounded: %d", len(failed.LastError))
			}
			if _, ok := registry.GetByID(workspaceRecord.ID); ok {
				t.Fatal("failed deletion target remained in runtime registry")
			}
			if _, err := catalog.GetWorkspace(ctx, workspaceRecord.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("failed deletion target became visible: %v", err)
			}
			if body, err := os.ReadFile(sourceMarker); err != nil || string(body) != "source" {
				t.Fatalf("source data changed: %q %v", body, err)
			}

			if _, err := coordinator.Retry(ctx, operation.ID); err != nil {
				t.Fatal(err)
			}
			waitForDeletionStatus(t, coordinator, operation.ID, store.DeletionCompleted)
			completed, err := coordinator.Get(ctx, operation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if completed.AttemptCount != 2 || completed.Phase != store.DeletionPhaseCompleted {
				t.Fatalf("completed operation = %+v", completed)
			}
			if _, err := os.Stat(sourceMarker); err != nil {
				t.Fatalf("source data was removed: %v", err)
			}
		})
	}
}
