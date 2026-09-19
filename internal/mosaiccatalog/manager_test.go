package mosaiccatalog_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/datasource/rastermosaic"
	"github.com/tobilg/neoserver/internal/mosaiccatalog"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestHarvestActivatesAtomicGenerationAndRefreshesMosaic(t *testing.T) {
	ctx := context.Background()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testing", "fixtures", "raster", "rectified-grid-coverage.tif"))
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.tif")
	pathpolicy.Configure([]string{fixture, missing})
	catalogStore, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.duckdb")})
	if err != nil {
		t.Fatal(err)
	}
	defer catalogStore.Close()
	registry := workspace.NewRegistry(catalogStore, datasource.CreateFromService)
	defer registry.Close()
	ws, err := registry.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "mosaic-test"})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := json.Marshal(store.RasterMosaicConnectionInfo{Name: "series", Granules: []store.RasterMosaicGranule{{Path: fixture}}})
	service, err := registry.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "series", Type: store.ServiceTypeRasterMosaic, ConnectionInfo: connection, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := mosaiccatalog.Open(ctx, conf.MosaicCatalog{
		Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "mosaic-index.duckdb"), WorkerCount: 1,
		MaxConcurrentJobs: 1, MaxGranulesPerJob: 100, BatchSize: 10, MaxRetries: 0, ShutdownTimeoutSec: 5,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	rastermosaic.SetCatalog(manager)
	t.Cleanup(func() { rastermosaic.SetCatalog(nil) })
	if err := manager.Start(registry); err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Close(closeCtx); err != nil {
			t.Error(err)
		}
	}()
	elevation := 250.0
	job, err := manager.CreateJob(ctx, ws.ID, service.ID, "test", mosaiccatalog.HarvestRequest{
		Mode:     mosaiccatalog.HarvestSynchronize,
		Granules: []mosaiccatalog.HarvestGranule{{Path: fixture, Time: "2026-01-01T00:00:00Z", Elevation: &elevation, Priority: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err = manager.GetJob(ctx, ws.ID, service.ID, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == mosaiccatalog.JobSucceeded || job.Status == mosaiccatalog.JobFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != mosaiccatalog.JobSucceeded || job.Generation != 1 || job.Succeeded != 1 {
		t.Fatalf("unexpected harvest result: %+v", job)
	}
	items, err := manager.ListGranules(ctx, ws.ID, service.ID, mosaiccatalog.GranuleFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Time != "2026-01-01T00:00:00Z" || items[0].Elevation == nil || *items[0].Elevation != elevation || len(items[0].FootprintWKB) == 0 {
		t.Fatalf("unexpected indexed granule: %+v", items)
	}
	indexed, err := manager.ListRasterMosaicGranules(ctx, service.ID)
	if err != nil || len(indexed) != 1 || indexed[0].Priority != 2 {
		t.Fatalf("unexpected runtime granules: %+v err=%v", indexed, err)
	}
	current, _ := registry.GetByID(ws.ID)
	if runtime := current.ResolveService(service.ID); runtime == nil || runtime.CoverageSource == nil {
		t.Fatal("harvest did not refresh the runtime mosaic")
	}
	failed, err := manager.CreateJob(ctx, ws.ID, service.ID, "test", mosaiccatalog.HarvestRequest{
		Mode: mosaiccatalog.HarvestSynchronize, Granules: []mosaiccatalog.HarvestGranule{{Path: missing}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		failed, err = manager.GetJob(ctx, ws.ID, service.ID, failed.ID)
		if err != nil {
			t.Fatal(err)
		}
		if failed.Status == mosaiccatalog.JobFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if failed.Status != mosaiccatalog.JobFailed {
		t.Fatalf("invalid harvest should fail: %+v", failed)
	}
	items, err = manager.ListGranules(ctx, ws.ID, service.ID, mosaiccatalog.GranuleFilter{})
	if err != nil || len(items) != 1 || items[0].SourceURI != fixture {
		t.Fatalf("failed harvest replaced the active generation: items=%+v err=%v", items, err)
	}
}

func TestLifecycleQuiescePurgesJobsWithoutDeletingSourceFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "source.tif")
	if err := os.WriteFile(source, []byte("operator-owned raster bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	pathpolicy.Configure([]string{source})
	catalogStore, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.duckdb")})
	if err != nil {
		t.Fatal(err)
	}
	defer catalogStore.Close()
	registry := workspace.NewRegistry(catalogStore, nil)
	defer registry.Close()
	ws, err := registry.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "mosaic-delete"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := registry.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "series", Type: store.ServiceTypeRasterMosaic, ConnectionInfo: json.RawMessage(`{"name":"series"}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := mosaiccatalog.Open(ctx, conf.MosaicCatalog{Enabled: true, DatabasePath: filepath.Join(root, "mosaic.duckdb"), WorkerCount: 1, MaxConcurrentJobs: 1, MaxGranulesPerJob: 10, BatchSize: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(registry); err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Close(closeCtx)
	}()
	if _, err := manager.CreateJob(ctx, ws.ID, service.ID, "test", mosaiccatalog.HarvestRequest{Mode: mosaiccatalog.HarvestSynchronize, Granules: []mosaiccatalog.HarvestGranule{{Path: source}}}); err != nil {
		t.Fatal(err)
	}
	_, _, jobs, err := manager.LifecycleStats(ctx, ws.ID, []string{service.ID})
	if err != nil || jobs != 1 {
		t.Fatalf("lifecycle stats jobs = %d, %v", jobs, err)
	}
	deleteCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := manager.QuiesceAndDeleteLifecycle(deleteCtx, ws.ID, []string{service.ID}); err != nil {
		t.Fatal(err)
	}
	services, granules, jobs, err := manager.LifecycleStats(ctx, ws.ID, []string{service.ID})
	if err != nil || services != 0 || granules != 0 || jobs != 0 {
		t.Fatalf("state after lifecycle purge = services %d granules %d jobs %d err %v", services, granules, jobs, err)
	}
	if data, err := os.ReadFile(source); err != nil || string(data) != "operator-owned raster bytes" {
		t.Fatalf("source raster was modified: %q, %v", data, err)
	}
}
