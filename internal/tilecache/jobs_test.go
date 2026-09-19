package tilecache

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestJobsPersistAndRecoverAcrossRestart(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, 1<<20)
	jobs := manager.JobStore()
	ctx := context.Background()

	job, err := jobs.CreateTileCacheJob(ctx, CreateJobInput{
		WorkspaceID: "workspace",
		Request: JobRequest{Operation: OperationSeed, Resource: "roads", ResourceID: "layer",
			TileType: "vector", TileMatrixSet: "WebMercatorQuad", Format: "application/vnd.mapbox-vector-tile"},
		TotalTiles: 4,
		CreatedBy:  "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.ReplaceTileCacheJobChunks(ctx, job.ID, []JobChunk{{Zoom: 1, MinCol: 0, MaxCol: 1, MinRow: 0, MaxRow: 1}}); err != nil {
		t.Fatal(err)
	}
	chunks, err := jobs.ListTileCacheJobChunks(ctx, job.ID)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("chunks = %+v, err = %v", chunks, err)
	}
	running, processed := JobRunning, int64(2)
	if _, err := jobs.UpdateTileCacheJob(ctx, job.ID, JobUpdate{Status: &running, ProcessedTiles: &processed}); err != nil {
		t.Fatal(err)
	}
	chunks[0].Status, chunks[0].NextOffset = "running", 2
	if err := jobs.UpdateTileCacheJobChunk(ctx, chunks[0]); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	manager = newTestManager(t, root, 1<<20)
	t.Cleanup(func() { _ = manager.Close() })
	jobs = manager.JobStore()
	if err := jobs.ResetInterruptedTileCacheJobs(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := jobs.GetTileCacheJob(ctx, job.ID)
	if err != nil || got.Status != JobQueued || got.ProcessedTiles != processed {
		t.Fatalf("recovered job = %+v, err = %v", got, err)
	}
	chunks, err = jobs.ListTileCacheJobChunks(ctx, job.ID)
	if err != nil || len(chunks) != 1 || chunks[0].Status != "queued" || chunks[0].NextOffset != 2 {
		t.Fatalf("recovered chunks = %+v, err = %v", chunks, err)
	}
	if err := jobs.RequestTileCacheJobCancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	got, err = jobs.GetTileCacheJob(ctx, job.ID)
	if err != nil || got.Status != JobCancelled || !got.CancelRequested || got.CompletedAt == nil {
		t.Fatalf("cancelled job = %+v, err = %v", got, err)
	}
	if err := jobs.RequestTileCacheJobCancel(ctx, job.ID); err == nil {
		t.Fatal("cancelling a terminal job should fail")
	}
	if _, err := jobs.GetTileCacheJob(ctx, "missing"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("missing job error = %v", err)
	}
}

func TestCacheDatabaseRejectsWrongEncryptionKey(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, 1024)
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFilesystemStore(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewManager(context.Background(), Config{
		DatabasePath: filepath.Join(root, "tile-cache.duckdb"), EncryptionKey: "wrong-key", MaxBytes: 1024,
	}, backend)
	if err == nil {
		t.Fatal("opening the encrypted cache database with the wrong key succeeded")
	}
	_ = backend.Close()
}

func TestConcurrentTileAndJobWritesAreSerialized(t *testing.T) {
	manager := newTestManager(t, t.TempDir(), 1<<20)
	t.Cleanup(func() { _ = manager.Close() })
	ctx := context.Background()
	job, err := manager.JobStore().CreateTileCacheJob(ctx, CreateJobInput{
		WorkspaceID: "workspace", Request: JobRequest{Operation: OperationSeed}, TotalTiles: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks := make([]JobChunk, 20)
	for index := range chunks {
		chunks[index] = JobChunk{Zoom: 1, MinCol: index, MaxCol: index, MinRow: 0, MaxRow: 0}
	}
	if err := manager.JobStore().ReplaceTileCacheJobChunks(ctx, job.ID, chunks); err != nil {
		t.Fatal(err)
	}
	chunks, err = manager.JobStore().ListTileCacheJobChunks(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errorsCh := make(chan error, 40)
	for index := range 20 {
		wg.Add(2)
		go func(column int) {
			defer wg.Done()
			_, err := manager.Put(ctx, testIdentity(column), []byte("tile"), Policy{})
			errorsCh <- err
		}(index)
		go func(chunk JobChunk) {
			defer wg.Done()
			chunk.Status = "running"
			chunk.NextOffset = 1
			errorsCh <- manager.JobStore().UpdateTileCacheJobChunk(ctx, chunk)
		}(chunks[index])
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	stats, err := manager.Stats(ctx, "", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Global.EntryCount != 20 {
		t.Fatalf("entry count = %d, want 20", stats.Global.EntryCount)
	}
}
