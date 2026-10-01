package stacsource

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type rasterSTAC struct {
	registry *workspace.Registry
	manager  *Manager
	ws       *workspace.Workspace
	service  *workspace.Service
	coverage *workspace.Coverage
	cfg      conf.Config
	logger   *slog.Logger
}

// newRasterSTAC publishes one GeoTIFF coverage and binds a STAC Collection
// "scenes" to it in raster mode. The Collection is not yet refreshed.
func newRasterSTAC(t *testing.T) *rasterSTAC {
	t.Helper()
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
	t.Cleanup(func() { catalog.Close() })
	registry := workspace.NewRegistry(catalog, datasource.CreateFromService)
	t.Cleanup(func() { registry.Close() })
	ws, err := registry.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "raster"})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := json.Marshal(store.RasterFileConnectionInfo{Path: fixture})
	service, err := registry.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "raster", Type: store.ServiceTypeRasterFile, ConnectionInfo: connection, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	coverages, err := service.CoverageSource.DiscoverCoverages(ctx)
	if err != nil || len(coverages) == 0 {
		t.Fatalf("coverage discovery: %v", err)
	}
	coverage, err := registry.CreateCoverage(ctx, ws.ID, store.CreateCoverageInput{WorkspaceID: ws.ID, ServiceID: service.ID, SourceCoverage: coverages[0].SourceCoverage, PublicID: "raster", Enabled: true, Public: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg := conf.Config{STAC: conf.STAC{Enabled: true, DatabasePath: filepath.Join(root, "stac.duckdb"), MaxItems: 1000, WorkerCount: 1, RefreshIntervalSec: 900}, Store: conf.Store{EncryptionKey: "abc123"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := New(ctx, cfg, registry, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Catalog.PutCollection(ctx, ws.ID, staccatalog.Collection{Document: stacmodel.Collection("scenes", "Scenes", "Acquisitions", "other", nil), Public: true}, true); err != nil {
		t.Fatal(err)
	}
	binding := &staccatalog.Binding{ID: "binding", ServiceID: service.ID, ResourceID: coverage.ID, ResourceKind: "coverage", Mode: "raster", RefreshIntervalSec: 900, Mapping: staccatalog.Mapping{Datetime: staccatalog.Value{Constant: "2026-01-01T00:00:00Z"}}}
	if err = manager.Catalog.PutBinding(ctx, ws.ID, "scenes", binding, 1); err != nil {
		t.Fatal(err)
	}
	return &rasterSTAC{registry: registry, manager: manager, ws: ws, service: service, coverage: coverage, cfg: cfg, logger: logger}
}

func TestReconcileRefreshesOnlyChangedSourcesAndKeepsDisabledOnes(t *testing.T) {
	ctx := context.Background()
	fixture := newRasterSTAC(t)
	registry, manager, ws, service, coverage, cfg, logger := fixture.registry, fixture.manager, fixture.ws, fixture.service, fixture.coverage, fixture.cfg, fixture.logger
	var err error
	if err = manager.reconcile(); err != nil {
		t.Fatal(err)
	}
	awaitSTAC(t, func() bool {
		c, e := manager.Catalog.GetCollection(ctx, ws.ID, "scenes")
		if e != nil {
			t.Fatal(e)
		}
		return c.ItemCount == 1 && c.SourceFingerprint != ""
	})
	jobs := func(m *Manager) int {
		t.Helper()
		list, e := m.Catalog.Jobs(ctx, ws.ID)
		if e != nil {
			t.Fatal(e)
		}
		return len(list)
	}
	published := jobs(manager)
	if err = manager.reconcile(); err != nil {
		t.Fatal(err)
	}
	if err = registry.UpdateWMSSettings(ctx, ws.ID, store.WMSSettings{Enabled: false, Title: "unrelated"}); err != nil {
		t.Fatal(err)
	}
	if err = registry.UpdateSTACSettings(ctx, ws.ID, store.STACSettings{Enabled: true, Title: "renamed"}); err != nil {
		t.Fatal(err)
	}
	if err = manager.reconcile(); err != nil {
		t.Fatal(err)
	}
	if got := jobs(manager); got != published {
		t.Fatalf("unchanged source rescanned: %d jobs, want %d", got, published)
	}
	if err = manager.Close(); err != nil {
		t.Fatal(err)
	}
	manager, err = New(ctx, cfg, registry, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err = manager.reconcile(); err != nil {
		t.Fatal(err)
	}
	if got := jobs(manager); got != published {
		t.Fatalf("restart rescanned an unchanged source: %d jobs, want %d", got, published)
	}
	disabled := false
	if _, err = registry.UpdateCoverage(ctx, ws.ID, service.ID, coverage.ID, store.UpdateCoverageInput{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if err = manager.reconcile(); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Catalog.GetCollection(ctx, ws.ID, "scenes"); err != nil {
		t.Fatalf("disabled source deleted its Collection: %v", err)
	}
	current, release, ok := registry.AcquireByID(ws.ID)
	if !ok {
		t.Fatal("workspace missing")
	}
	c, err := manager.Catalog.GetCollection(ctx, ws.ID, "scenes")
	if err != nil {
		t.Fatal(err)
	}
	visible := Visible(current, c, "super_admin")
	release()
	if visible {
		t.Fatal("disabled source remains visible")
	}
	if err = registry.DeleteCoverage(ctx, ws.ID, service.ID, coverage.ID); err != nil {
		t.Fatal(err)
	}
	if err = manager.reconcile(); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Catalog.GetCollection(ctx, ws.ID, "scenes"); err != staccatalog.ErrNotFound {
		t.Fatalf("deleted source kept its Collection: %v", err)
	}
}

func TestCoverageRefreshIgnoresPendingFeatureWrites(t *testing.T) {
	ctx := context.Background()
	fixture := newRasterSTAC(t)
	defer fixture.manager.Close()
	// Hold a workspace feature write open until the refresh has published.
	finish, err := fixture.registry.BeginDataWrite(ctx, fixture.ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if _, pending := fixture.ws.DataCacheState(); !pending {
		t.Fatal("feature write not pending")
	}
	if err = fixture.manager.reconcile(); err != nil {
		t.Fatal(err)
	}
	awaitSTAC(t, func() bool {
		c, e := fixture.manager.Catalog.GetCollection(ctx, fixture.ws.ID, "scenes")
		if e != nil {
			t.Fatal(e)
		}
		return c.ItemCount == 1
	})
}
