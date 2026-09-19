package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internalaudit "github.com/tobilg/neoserver/internal/audit"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	duckdbsource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/datasource/rastermosaic"
	"github.com/tobilg/neoserver/internal/mosaiccatalog"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

func recoveryConfig(t *testing.T, root, key, source string) conf.Config {
	t.Helper()
	cfg, err := conf.Load("", false, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Store.Path = filepath.Join(root, "catalog.duckdb")
	cfg.Store.EncryptionKey = key
	cfg.Server.HttpHost = "127.0.0.1"
	cfg.Server.HttpPort = 0
	cfg.Server.UrlBase = "http://neoserver.test/base"
	cfg.Server.BasePath = "/base"
	cfg.Server.DisableUI = true
	cfg.Auth.RequireHTTPS = false
	cfg.Cache.Enabled = false
	cfg.Datasource.AllowedPaths = []string{source}
	cfg.PersistentCache.Enabled = true
	cfg.PersistentCache.Backend = "filesystem"
	cfg.PersistentCache.DatabasePath = filepath.Join(root, "tile-cache.duckdb")
	cfg.PersistentCache.Filesystem.Root = filepath.Join(root, "tile-payloads")
	cfg.PersistentCache.MaxBytes = 16 << 20
	cfg.PersistentCache.AccessFlushIntervalSec = 1
	cfg.PersistentCache.MaintenanceIntervalSec = 60
	cfg.PersistentCache.Jobs.WorkerCount = 1
	cfg.PersistentCache.Jobs.MaxConcurrentJobs = 1
	cfg.PersistentCache.Jobs.MaxConcurrentRenders = 1
	cfg.PersistentCache.Jobs.MaxTilesPerJob = 100
	cfg.PersistentCache.Jobs.MaxRetries = 0
	cfg.PersistentCache.Jobs.ShutdownTimeoutSec = 5
	cfg.Audit.DatabasePath = filepath.Join(root, "audit.duckdb")
	cfg.Importer.Enabled = true
	cfg.Importer.Root = filepath.Join(root, "managed-imports")
	cfg.Importer.TemporaryDirectory = filepath.Join(root, "import-tmp")
	cfg.MosaicCatalog.Enabled = true
	cfg.MosaicCatalog.DatabasePath = filepath.Join(root, "mosaic-catalog.duckdb")
	cfg.MosaicCatalog.WorkerCount = 1
	cfg.MosaicCatalog.MaxConcurrentJobs = 1
	cfg.MosaicCatalog.MaxGranulesPerJob = 10
	cfg.MosaicCatalog.BatchSize = 2
	cfg.MosaicCatalog.MaxRetries = 0
	cfg.MosaicCatalog.ShutdownTimeoutSec = 5
	cfg.WMS.StyleAssetPath = filepath.Join(root, "style-assets")
	return cfg
}

func TestRestoreConsistencySetRecoversDurableState(t *testing.T) {
	ctx := context.Background()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testing", "fixtures", "raster", "rectified-grid-coverage.tif"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("source fixture: %v", err)
	}
	pathpolicy.Configure([]string{fixture})
	key := "abc123"
	originalRoot := filepath.Join(t.TempDir(), "original")
	if err := os.MkdirAll(originalRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	originalCfg := recoveryConfig(t, originalRoot, key, fixture)
	catalog, _, err := store.Init(store.Config{Path: originalCfg.Store.Path, EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	target, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "restore-target"})
	if err != nil {
		t.Fatal(err)
	}
	control, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "restore-control"})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpdateOGCAPISettings(ctx, control.ID, store.OGCAPISettings{Enabled: true, Public: true, LimitDefault: 10, LimitMax: 100}); err != nil {
		t.Fatal(err)
	}
	customGrid := &tiles.TileMatrixSetDefinition{ID: "RestoreCRS84", CRS: tiles.CRS4326URI, TileMatrices: []tiles.TileMatrix{{ID: "0", ScaleDenominator: 1000, CellSize: 1, CornerOfOrigin: "topLeft", PointOfOrigin: []float64{-10, 10}, TileWidth: 10, TileHeight: 10, MatrixWidth: 2, MatrixHeight: 2}}}
	t.Cleanup(func() { _ = tiles.ReplaceCustomTileMatrixSets(nil) })
	customDefinition, err := json.Marshal(customGrid)
	if err != nil {
		t.Fatal(err)
	}
	customRecord, err := catalog.UpsertTileMatrixSet(ctx, customGrid.ID, customDefinition, "restore-grid-digest")
	if err != nil {
		t.Fatal(err)
	}
	survivingImport, survivingService, survivingPath := seedPublishedRecoveryImport(t, catalog, originalCfg, control.ID, "restore-survivor")
	interruptedPublish, interruptedFinal, interruptedStaged := seedInterruptedPublication(t, catalog, originalCfg, control.ID, "restore-publishing")
	rollbackImport, rollbackService, rollbackPath, rollbackOperation := seedInterruptedRollback(t, catalog, originalCfg, control.ID, "restore-rollback")
	pendingSource := filepath.Join(originalCfg.Importer.TemporaryDirectory, "upload-pending", "points.geojson")
	if err := os.MkdirAll(filepath.Dir(pendingSource), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingSource, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"restored"},"geometry":{"type":"Point","coordinates":[10,20]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	pendingImport, err := catalog.CreateImportJob(ctx, store.CreateImportJobInput{WorkspaceID: control.ID, Name: "restore-pending", SourceKind: "upload", SourcePath: pendingSource, SourceFilename: "points.geojson"})
	if err != nil {
		t.Fatal(err)
	}
	pendingStatus, pendingPhase := store.ImportAwaitingPlan, store.ImportPhaseValidate
	if _, err = catalog.UpdateImportJob(ctx, pendingImport.ID, store.ImportJobUpdate{Status: &pendingStatus, Phase: &pendingPhase, Discovery: &store.ImportDiscovery{Layers: []store.ImportDiscoveredLayer{{Name: "points", GeometryColumn: "geom", SRID: 4326, Properties: []store.ImportProperty{{Name: "name", Type: "VARCHAR"}}}}}}); err != nil {
		t.Fatal(err)
	}
	auditManager, err := internalaudit.Open(ctx, originalCfg.Audit, key, testLogger(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	freshAudit := internalaudit.Event{ID: "restore-audit-fresh", Timestamp: time.Now().UTC(), Method: http.MethodPost, Path: "/api/v1/workspaces", Action: "change", Status: http.StatusCreated}
	expiredAudit := internalaudit.Event{ID: "restore-audit-expired", Timestamp: time.Now().UTC().AddDate(0, 0, -originalCfg.Audit.RetentionDays-1), Method: http.MethodDelete, Path: "/api/v1/workspaces/old", Action: "change", Status: http.StatusNoContent}
	if err := auditManager.Record(ctx, freshAudit); err != nil {
		t.Fatal(err)
	}
	if err := auditManager.Record(ctx, expiredAudit); err != nil {
		t.Fatal(err)
	}
	if err := auditManager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	queuedAudit := internalaudit.Event{ID: "restore-audit-queued", Timestamp: time.Now().UTC(), Method: http.MethodPut, Path: "/api/v1/roles/operator", Action: "change", Status: http.StatusOK}
	queuedPayload, err := json.Marshal(queuedAudit)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.EnqueueAuditEvent(ctx, queuedAudit.ID, queuedAudit.Timestamp, queuedPayload); err != nil {
		t.Fatal(err)
	}
	connection, err := json.Marshal(store.RasterMosaicConnectionInfo{Name: "series", Granules: []store.RasterMosaicGranule{{Path: fixture}}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.CreateService(ctx, store.CreateServiceInput{
		WorkspaceID: target.ID, Name: "mosaic", Type: store.ServiceTypeRasterMosaic, ConnectionInfo: connection, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	duckdbsource.ConfigureManagedResolver(managedAssetResolver(catalog))
	t.Cleanup(func() { duckdbsource.ConfigureManagedResolver(nil) })

	registry := workspace.NewRegistry(catalog, datasource.CreateFromService)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}
	mosaic, err := mosaiccatalog.Open(ctx, originalCfg.MosaicCatalog, key)
	if err != nil {
		t.Fatal(err)
	}
	rastermosaic.SetCatalog(mosaic)
	if err := mosaic.Start(registry); err != nil {
		t.Fatal(err)
	}
	harvest, err := mosaic.CreateJob(ctx, target.ID, service.ID, "restore-test", mosaiccatalog.HarvestRequest{
		Mode: mosaiccatalog.HarvestSynchronize, Granules: []mosaiccatalog.HarvestGranule{{Path: fixture}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForMosaicJob(t, mosaic, target.ID, service.ID, harvest.ID)

	persistent, err := newPersistentTileCache(ctx, originalCfg.PersistentCache, key, "")
	if err != nil {
		t.Fatal(err)
	}
	identity := tilecache.Identity{
		WorkspaceID: target.ID, WorkspaceRevision: 1, ResourceID: service.ID, ResourceKind: "coverage",
		Generation: 1, TileType: "map", MatrixSet: "WebMercatorQuad", Zoom: 0, Column: 0, Row: 0, Format: "image/png",
	}
	if stored, err := persistent.Put(ctx, identity, []byte("durable-tile"), tilecache.Policy{}); err != nil || !stored {
		t.Fatalf("seed durable tile = %t, %v", stored, err)
	}
	job, err := persistent.JobStore().CreateTileCacheJob(ctx, tilecache.CreateJobInput{WorkspaceID: target.ID, Request: tilecache.JobRequest{Operation: tilecache.OperationTruncate, ResourceID: service.ID}})
	if err != nil {
		t.Fatal(err)
	}
	succeeded, completedAt := tilecache.JobSucceeded, time.Now().UTC()
	if _, err := persistent.JobStore().UpdateTileCacheJob(ctx, job.ID, tilecache.JobUpdate{Status: &succeeded, CompletedAt: &completedAt}); err != nil {
		t.Fatal(err)
	}
	assetDir := filepath.Join(originalCfg.WMS.StyleAssetPath, target.ID)
	if err := os.MkdirAll(assetDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "marker.png"), []byte("style"), 0o640); err != nil {
		t.Fatal(err)
	}

	plan, err := catalog.PlanWorkspaceDeletion(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	operation, _, err := catalog.BeginCatalogDeletion(ctx, *plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpdateCatalogDeletion(ctx, operation.ID, store.DeletionRunning, store.DeletionPhaseTombstoned, ""); err != nil {
		t.Fatal(err)
	}

	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := persistent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mosaic.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	cancel()
	rastermosaic.SetCatalog(nil)
	registry.Close()
	if err := catalog.Close(); err != nil {
		t.Fatal(err)
	}

	backupRoot := filepath.Join(t.TempDir(), "backup")
	if err := copyConsistencyTree(originalRoot, backupRoot); err != nil {
		t.Fatal(err)
	}
	restoredRoot := filepath.Join(t.TempDir(), "restored")
	if err := copyConsistencyTree(backupRoot, restoredRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(originalCfg.Importer.Root, originalCfg.Importer.Root+".offline"); err != nil {
		t.Fatal(err)
	}
	restoredCfg := recoveryConfig(t, restoredRoot, key, fixture)

	if wrong, err := store.Open(store.Config{Path: restoredCfg.Store.Path, EncryptionKey: "wrong-key"}); err == nil {
		_ = wrong.Close()
		t.Fatal("restored encrypted catalog opened with the wrong key")
	}
	incompleteRoot := filepath.Join(t.TempDir(), "incomplete")
	if err := copyConsistencyTree(backupRoot, incompleteRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(incompleteRoot, "catalog.duckdb")); err != nil {
		t.Fatal(err)
	}
	if incomplete, err := store.Open(store.Config{Path: filepath.Join(incompleteRoot, "catalog.duckdb"), EncryptionKey: key}); err == nil {
		_ = incomplete.Close()
		t.Fatal("incomplete consistency set unexpectedly opened")
	}

	restoredCatalog, err := store.Open(store.Config{Path: restoredCfg.Store.Path, EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	defer restoredCatalog.Close()
	srv, err := New(ctx, restoredCfg, testLogger(), restoredCatalog)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	}()

	waitForRecoveredDeletion(t, restoredCatalog, operation.ID)
	// A retained input without a managed output must relocate too. This uses
	// the pre-v24 ownership fallback and a real transform after restoration.
	pendingPlan := store.ImportPlan{ServiceName: "restored-pending", Layers: []store.ImportLayerPlan{{SourceLayer: "points", PublicID: "restored-pending", GeometryColumn: "geom", SourceSRID: 4326, TargetSRID: 4326, Enabled: true}}}
	if _, err := srv.importer.SetPlan(ctx, pendingImport.ID, pendingPlan); err != nil {
		t.Fatalf("restored source cannot be revised: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		pending, err := srv.importer.Get(ctx, pendingImport.ID)
		if err != nil {
			t.Fatal(err)
		}
		if pending.Status == store.ImportReadyToPublish {
			if !strings.HasPrefix(pending.SourcePath, restoredCfg.Importer.TemporaryDirectory+string(filepath.Separator)) {
				t.Fatal("retained input still uses old mount")
			}
			break
		}
		if pending.Status == store.ImportFailed || time.Now().After(deadline) {
			t.Fatalf("restored input transform: %+v", pending)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := restoredCatalog.GetWorkspace(ctx, target.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted target is visible: %v", err)
	}
	if restoredControl, err := restoredCatalog.GetWorkspace(ctx, control.ID); err != nil || restoredControl.Name != control.Name {
		t.Fatalf("control workspace missing: %+v %v", restoredControl, err)
	}
	relocated := func(original string) string {
		relative, relErr := filepath.Rel(originalCfg.Importer.Root, original)
		if relErr != nil {
			t.Fatal(relErr)
		}
		return filepath.Join(restoredCfg.Importer.Root, relative)
	}
	if restoredJob := waitForImportStatus(t, restoredCatalog, survivingImport.ID, store.ImportPublished); restoredJob.ServiceID != survivingService.ID {
		t.Fatalf("surviving import service = %q, want %q", restoredJob.ServiceID, survivingService.ID)
	}
	survivingAsset, err := restoredCatalog.GetManagedAssetByImport(ctx, survivingImport.ID)
	if err != nil || survivingAsset.Path != relocated(survivingPath) {
		t.Fatalf("surviving managed asset = %+v, %v", survivingAsset, err)
	}
	if _, err := os.Stat(survivingAsset.Path); err != nil {
		t.Fatalf("surviving managed file: %v", err)
	}
	republished := waitForImportStatus(t, restoredCatalog, interruptedPublish.ID, store.ImportPublished)
	if republished.ServiceID == "" {
		t.Fatal("recovered publication has no service")
	}
	republishedAsset, err := restoredCatalog.GetManagedAssetByImport(ctx, interruptedPublish.ID)
	if err != nil || republishedAsset.Path != relocated(interruptedFinal) {
		t.Fatalf("republished managed asset = %+v, %v", republishedAsset, err)
	}
	if _, err := os.Stat(relocated(interruptedStaged)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale restored staging file remains: %v", err)
	}
	rolledBack := waitForImportStatus(t, restoredCatalog, rollbackImport.ID, store.ImportRolledBack)
	if rolledBack.RollbackOperationID != rollbackOperation.ID {
		t.Fatalf("rollback operation = %q, want %q", rolledBack.RollbackOperationID, rollbackOperation.ID)
	}
	if _, err := restoredCatalog.GetService(ctx, rollbackService.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rolled-back service remains: %v", err)
	}
	if _, err := restoredCatalog.GetManagedAssetByImport(ctx, rollbackImport.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rolled-back ownership remains: %v", err)
	}
	for _, path := range []string{relocated(rollbackPath), relocated(rollbackPath) + ".deleting-" + rollbackOperation.ID} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rolled-back managed file remains at %s: %v", path, err)
		}
	}
	waitForRecoveredAudit(t, srv.audit, restoredCatalog, freshAudit.ID, queuedAudit.ID, expiredAudit.ID)
	restoredGrid, err := restoredCatalog.GetTileMatrixSet(ctx, customGrid.ID)
	if err != nil || restoredGrid.Revision != customRecord.Revision || restoredGrid.Digest != customRecord.Digest {
		t.Fatalf("restored tile matrix record = %+v, %v", restoredGrid, err)
	}
	runtimeGrid, err := tiles.GetTileMatrixSetDefinition(customGrid.ID)
	if err != nil || runtimeGrid.ID != customGrid.ID || len(runtimeGrid.TileMatrices) != 1 {
		t.Fatalf("runtime tile matrix definition = %+v, %v", runtimeGrid, err)
	}
	stats, err := srv.tileCache.Stats(ctx, target.ID, "", 0, 0)
	if err != nil || stats.Workspace == nil || stats.Workspace.EntryCount != 0 {
		t.Fatalf("tile state not reconciled: %+v %v", stats, err)
	}
	services, granules, jobs, err := srv.mosaicCatalog.LifecycleStats(ctx, target.ID, nil)
	if err != nil || services != 0 || granules != 0 || jobs != 0 {
		t.Fatalf("mosaic state not reconciled: %d %d %d %v", services, granules, jobs, err)
	}
	if _, err := os.Stat(filepath.Join(restoredCfg.WMS.StyleAssetPath, target.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed assets were not removed: %v", err)
	}
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("source raster was removed: %v", err)
	}

	for _, requestPath := range []string{"/base/ready", "/base/workspaces/restore-control/ogc/",
		"/base/workspaces/restore-control/ogc/collections/restore-survivor/items",
		"/base/workspaces/restore-control/ogc/collections/restore-publishing/items"} {
		recorder := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", requestPath, recorder.Code, recorder.Body.String())
		}
	}
}

func waitForMosaicJob(t *testing.T, manager *mosaiccatalog.Manager, workspaceID, serviceID, jobID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.GetJob(context.Background(), workspaceID, serviceID, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == mosaiccatalog.JobSucceeded {
			return
		}
		if job.Status == mosaiccatalog.JobFailed {
			t.Fatalf("mosaic harvest failed: %s", job.ErrorMessage)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("mosaic harvest did not complete")
}

func seedPublishedRecoveryImport(t *testing.T, catalog *store.DuckDBStore, cfg conf.Config, workspaceID, name string) (*store.ImportJob, *store.Service, string) {
	t.Helper()
	ctx := context.Background()
	job, err := catalog.CreateImportJob(ctx, store.CreateImportJobInput{WorkspaceID: workspaceID, Name: name, SourceKind: "upload", SourcePath: filepath.Join(cfg.Importer.TemporaryDirectory, name+".geojson"), SourceFilename: name + ".geojson", CreatedBy: "recovery-test"})
	if err != nil {
		t.Fatal(err)
	}
	plan := store.ImportPlan{ServiceName: name, Layers: []store.ImportLayerPlan{{SourceLayer: "roads", PublicID: name, Title: name, Enabled: true, Public: true, SourceSRID: 4326, TargetSRID: 4326}}}
	ready := store.ImportReadyToPublish
	if _, err = catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Plan: &plan, Status: &ready}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.Importer.Root, workspaceID, job.ID+".duckdb")
	// Real managed imports name physical output tables after PublicID.
	createManagedRecoveryDatabase(t, path, "managed-"+job.ID, name)
	if err = catalog.UpsertStagedManagedAsset(ctx, store.ManagedAsset{ImportID: job.ID, WorkspaceID: workspaceID, Path: path, EncryptionKey: "managed-" + job.ID}); err != nil {
		t.Fatal(err)
	}
	connection, _ := json.Marshal(store.DuckDBConnectionInfo{ManagedImportID: job.ID})
	service, _, err := catalog.PublishImport(ctx, store.PublishImportInput{ImportID: job.ID, WorkspaceID: workspaceID, ManagedPath: path, EncryptionKey: "managed-" + job.ID,
		Service: store.CreateServiceInput{WorkspaceID: workspaceID, Name: name, Type: store.ServiceTypeDuckDB, ConnectionInfo: connection, Enabled: true},
		Layers:  []store.CreateLayerInput{{SourceLayer: name, PublicID: name, Title: name, Enabled: true, Public: true, CRSDefault: 4326}}})
	if err != nil {
		t.Fatal(err)
	}
	job, err = catalog.GetImportJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	return job, service, path
}

func seedInterruptedPublication(t *testing.T, catalog *store.DuckDBStore, cfg conf.Config, workspaceID, name string) (*store.ImportJob, string, string) {
	t.Helper()
	ctx := context.Background()
	job, err := catalog.CreateImportJob(ctx, store.CreateImportJobInput{WorkspaceID: workspaceID, Name: name, SourceKind: "upload", SourcePath: filepath.Join(cfg.Importer.TemporaryDirectory, name+".geojson"), SourceFilename: name + ".geojson", CreatedBy: "recovery-test"})
	if err != nil {
		t.Fatal(err)
	}
	plan := store.ImportPlan{ServiceName: name, Layers: []store.ImportLayerPlan{{SourceLayer: "roads", PublicID: name, Title: name, Enabled: true, Public: true, SourceSRID: 4326, TargetSRID: 4326}}}
	publishing, phase := store.ImportPublishing, store.ImportPhasePublish
	if _, err = catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Plan: &plan, Status: &publishing, Phase: &phase}); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(cfg.Importer.Root, ".staging", job.ID+".duckdb")
	final := filepath.Join(cfg.Importer.Root, workspaceID, job.ID+".duckdb")
	createManagedRecoveryDatabase(t, final, "managed-"+job.ID, name)
	if err = catalog.UpsertStagedManagedAsset(ctx, store.ManagedAsset{ImportID: job.ID, WorkspaceID: workspaceID, Path: staged, EncryptionKey: "managed-" + job.ID}); err != nil {
		t.Fatal(err)
	}
	return job, final, staged
}

func seedInterruptedRollback(t *testing.T, catalog *store.DuckDBStore, cfg conf.Config, workspaceID, name string) (*store.ImportJob, *store.Service, string, *store.DeletionOperation) {
	t.Helper()
	job, service, path := seedPublishedRecoveryImport(t, catalog, cfg, workspaceID, name)
	ctx := context.Background()
	plan, err := catalog.PlanServiceDeletion(ctx, workspaceID, service.ID)
	if err != nil {
		t.Fatal(err)
	}
	operation, _, err := catalog.BeginCatalogDeletion(ctx, *plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.UpdateCatalogDeletion(ctx, operation.ID, store.DeletionRunning, store.DeletionPhaseTombstoned, ""); err != nil {
		t.Fatal(err)
	}
	rolling, phase := store.ImportRollingBack, store.ImportPhaseRollback
	if _, err = catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &rolling, Phase: &phase, RollbackOperationID: &operation.ID}); err != nil {
		t.Fatal(err)
	}
	job, _ = catalog.GetImportJob(ctx, job.ID)
	return job, service, path, operation
}

func createManagedRecoveryDatabase(t *testing.T, path, key, table string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	statement := fmt.Sprintf("INSTALL spatial; LOAD spatial; ATTACH '%s' AS managed (ENCRYPTION_KEY '%s'); USE managed", escape(path), escape(key))
	if _, err = db.Exec(statement); err != nil {
		db.Close()
		t.Fatal(err)
	}
	table = `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
	if _, err = db.Exec(fmt.Sprintf(`CREATE TABLE %s(id BIGINT PRIMARY KEY,name VARCHAR,geom GEOMETRY); INSERT INTO %s VALUES (1,'restored',ST_Point(7,52)); CHECKPOINT`, table, table)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
}

func waitForImportStatus(t *testing.T, catalog *store.DuckDBStore, importID string, expected store.ImportStatus) *store.ImportJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err := catalog.GetImportJob(context.Background(), importID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == expected {
			return job
		}
		if job.Status == store.ImportFailed {
			t.Fatalf("import %s failed during recovery: %s", importID, job.ErrorMessage)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("import %s did not reach %s", importID, expected)
	return nil
}

func waitForRecoveredAudit(t *testing.T, manager *internalaudit.Manager, catalog *store.DuckDBStore, freshID, queuedID, expiredID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := catalog.CountAuditOutbox(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		events, err := manager.List(context.Background(), internalaudit.Query{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		for _, event := range events {
			counts[event.ID]++
		}
		if pending == 0 && counts[freshID] == 1 && counts[queuedID] == 1 && counts[expiredID] == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("restored audit events, retention, and outbox did not reconcile")
}

func waitForRecoveredDeletion(t *testing.T, catalog *store.DuckDBStore, operationID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		operation, err := catalog.GetCatalogDeletion(context.Background(), operationID)
		if err != nil {
			t.Fatal(err)
		}
		if operation.Status == store.DeletionCompleted {
			return
		}
		if operation.Status == store.DeletionFailed {
			t.Fatalf("recovered deletion failed in %s: %s", operation.Phase, operation.LastError)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("recovered deletion did not complete")
}

func copyConsistencyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, info.Mode().Perm())
	})
}
