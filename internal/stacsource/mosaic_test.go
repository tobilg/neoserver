package stacsource

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/datasource/rastermosaic"
	"github.com/tobilg/neoserver/internal/mosaiccatalog"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestMosaicHarvestAutomaticallyRefreshesSTACAndPreservesIdentity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fixture, err := filepath.Abs("../../testing/fixtures/raster/rectified-grid-coverage.tif")
	if err != nil {
		t.Fatal(err)
	}
	pathpolicy.Configure([]string{fixture})
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.duckdb"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	registry := workspace.NewRegistry(catalog, datasource.CreateFromService)
	defer registry.Close()
	ws, err := registry.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "mosaic"})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := json.Marshal(store.RasterMosaicConnectionInfo{Name: "series", Granules: []store.RasterMosaicGranule{{Path: fixture}}})
	service, err := registry.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "series", Type: store.ServiceTypeRasterMosaic, ConnectionInfo: connection, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	coverage, err := registry.CreateCoverage(ctx, ws.ID, store.CreateCoverageInput{WorkspaceID: ws.ID, ServiceID: service.ID, SourceCoverage: "series", PublicID: "series", Enabled: true, Public: true})
	if err != nil {
		t.Fatal(err)
	}
	mosaic, err := mosaiccatalog.Open(ctx, conf.MosaicCatalog{Enabled: true, DatabasePath: filepath.Join(root, "mosaic.duckdb"), WorkerCount: 1, MaxConcurrentJobs: 1, MaxGranulesPerJob: 10, BatchSize: 10, ShutdownTimeoutSec: 5}, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	rastermosaic.SetCatalog(mosaic)
	defer rastermosaic.SetCatalog(nil)
	if err = mosaic.Start(registry); err != nil {
		t.Fatal(err)
	}
	defer mosaic.Close(ctx)
	harvest := func(instant string) {
		t.Helper()
		j, err := mosaic.CreateJob(ctx, ws.ID, service.ID, "test", mosaiccatalog.HarvestRequest{Mode: mosaiccatalog.HarvestSynchronize, Granules: []mosaiccatalog.HarvestGranule{{Path: fixture, Time: instant}}})
		if err != nil {
			t.Fatal(err)
		}
		awaitSTAC(t, func() bool {
			job, e := mosaic.GetJob(ctx, ws.ID, service.ID, j.ID)
			if e != nil {
				t.Fatal(e)
			}
			if job.Status == mosaiccatalog.JobFailed {
				t.Fatalf("harvest failed: %+v", job)
			}
			return job.Status == mosaiccatalog.JobSucceeded
		})
	}
	harvest("2026-01-01T00:00:00Z")
	cfg := conf.Config{STAC: conf.STAC{Enabled: true, DatabasePath: filepath.Join(root, "stac.duckdb"), MaxItems: 1000, WorkerCount: 1, RefreshIntervalSec: 900}, Store: conf.Store{EncryptionKey: "abc123"}}
	manager, err := New(ctx, cfg, registry, mosaic, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err = manager.Catalog.PutCollection(ctx, ws.ID, staccatalog.Collection{Document: stacmodel.Collection("scenes", "Scenes", "Acquisitions", "other", nil), Public: true}, true); err != nil {
		t.Fatal(err)
	}
	binding := &staccatalog.Binding{ID: "binding", ServiceID: service.ID, ResourceID: coverage.ID, ResourceKind: "coverage", Mode: "raster", RefreshIntervalSec: 900}
	if err = manager.Catalog.PutBinding(ctx, ws.ID, "scenes", binding, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Catalog.CreateJob(ctx, ws.ID, "scenes", "refresh", "queued", nil); err != nil {
		t.Fatal(err)
	}
	var first stacmodel.Document
	awaitSTAC(t, func() bool {
		page, e := manager.Catalog.Search(ctx, ws.ID, staccatalog.Search{}, []string{"scenes"})
		if e != nil {
			t.Fatal(e)
		}
		if len(page.Items) != 1 {
			return false
		}
		first = page.Items[0]
		return first.Object("properties").String("datetime") == "2026-01-01T00:00:00Z"
	})
	harvest("2026-02-01T00:00:00Z")
	awaitSTAC(t, func() bool {
		page, e := manager.Catalog.Search(ctx, ws.ID, staccatalog.Search{}, []string{"scenes"})
		if e != nil {
			t.Fatal(e)
		}
		if len(page.Items) != 1 {
			return false
		}
		if page.Items[0].String("id") != first.String("id") {
			t.Fatal("harvest changed stable Item ID")
		}
		return page.Items[0].Object("properties").String("datetime") == "2026-02-01T00:00:00Z"
	})
	if err = registry.DeleteCoverage(ctx, ws.ID, service.ID, coverage.ID); err != nil {
		t.Fatal(err)
	}
	awaitSTAC(t, func() bool {
		_, e := manager.Catalog.GetCollection(ctx, ws.ID, "scenes")
		return e == staccatalog.ErrNotFound
	})
	if _, err = os.Stat(fixture); err != nil {
		t.Fatal("source fixture was not retained")
	}
}

func awaitSTAC(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for asynchronous STAC publication")
}
