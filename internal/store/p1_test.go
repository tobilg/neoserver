package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestP1CatalogPersistence(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, err := catalog.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.CreateService(ctx, CreateServiceInput{WorkspaceID: ws.ID, Name: "source", Type: ServiceTypePostGIS, ConnectionInfo: json.RawMessage(`{}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	policy := &TileCacheParameterPolicy{Styles: []string{"default"}, Times: []string{"2026-08-02"}, MetatileFactor: 2, GutterPixels: 8}
	layer, err := catalog.CreateLayer(ctx, CreateLayerInput{ServiceID: service.ID, SourceLayer: "roads", PublicID: "roads", Enabled: true, TileCacheParameters: policy})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.GetLayer(ctx, layer.ID)
	if err != nil || loaded.TileCacheParameters == nil || loaded.TileCacheParameters.MetatileFactor != 2 {
		t.Fatalf("cache policy did not round-trip: %+v, %v", loaded, err)
	}

	definition := json.RawMessage(`{"id":"Local","crs":"http://www.opengis.net/def/crs/OGC/1.3/CRS84","tileMatrices":[]}`)
	first, err := catalog.UpsertTileMatrixSet(ctx, "Local", definition, "digest-1")
	if err != nil || first.Revision != 1 {
		t.Fatalf("first tile matrix set revision: %+v, %v", first, err)
	}
	second, err := catalog.UpsertTileMatrixSet(ctx, "Local", definition, "digest-2")
	if err != nil || second.Revision != 2 || second.Digest != "digest-2" {
		t.Fatalf("updated tile matrix set revision: %+v, %v", second, err)
	}
}

func TestPublishImportCommitsCatalogAndJobTogether(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, _ := catalog.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "imports"})
	job, err := catalog.CreateImportJob(ctx, CreateImportJobInput{WorkspaceID: ws.ID, Name: "roads", SourceKind: "upload", SourcePath: "/tmp/roads.geojson"})
	if err != nil {
		t.Fatal(err)
	}
	status := ImportReadyToPublish
	if _, err = catalog.UpdateImportJob(ctx, job.ID, ImportJobUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if err = catalog.UpsertStagedManagedAsset(ctx, ManagedAsset{ImportID: job.ID, WorkspaceID: ws.ID, Path: "/tmp/managed.duckdb", EncryptionKey: "key"}); err != nil {
		t.Fatal(err)
	}
	service, layers, err := catalog.PublishImport(ctx, PublishImportInput{ImportID: job.ID, WorkspaceID: ws.ID, ManagedPath: "/tmp/final.duckdb", EncryptionKey: "key",
		Service: CreateServiceInput{Name: "managed", Type: ServiceTypeDuckDB, ConnectionInfo: json.RawMessage(`{"managed_import_id":"` + job.ID + `"}`), Enabled: true},
		Layers:  []CreateLayerInput{{SourceLayer: "roads", PublicID: "roads", Enabled: true}}})
	if err != nil || service == nil || len(layers) != 1 {
		t.Fatalf("publish import: %+v %+v %v", service, layers, err)
	}
	published, err := catalog.GetImportJob(ctx, job.ID)
	if err != nil || published.Status != ImportPublished || published.ServiceID != service.ID {
		t.Fatalf("published job: %+v, %v", published, err)
	}
}

func TestImportCancellationAndPublicationRecoveryStates(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, _ := catalog.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "import-state"})

	queued, err := catalog.CreateImportJob(ctx, CreateImportJobInput{WorkspaceID: ws.ID, Name: "queued", SourceKind: "upload", SourcePath: "/tmp/queued.geojson"})
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.RequestImportCancel(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := catalog.GetImportJob(ctx, queued.ID)
	if err != nil || cancelled.Status != ImportCancelling || cancelled.CompletedAt != nil || !cancelled.CancelRequested {
		t.Fatalf("cancelled job: %+v, %v", cancelled, err)
	}

	publishing, err := catalog.CreateImportJob(ctx, CreateImportJobInput{WorkspaceID: ws.ID, Name: "publishing", SourceKind: "upload", SourcePath: "/tmp/publishing.geojson"})
	if err != nil {
		t.Fatal(err)
	}
	status := ImportPublishing
	if _, err = catalog.UpdateImportJob(ctx, publishing.ID, ImportJobUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if err = catalog.ResetInterruptedImportJobs(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := catalog.GetImportJob(ctx, publishing.ID)
	if err != nil || recovered.Status != ImportPublishing {
		t.Fatalf("recovered publication: %+v, %v", recovered, err)
	}
	interrupted, err := catalog.ListInterruptedImportJobs(ctx)
	if err != nil || len(interrupted) != 2 || interrupted[1].ID != publishing.ID {
		t.Fatalf("interrupted imports: %+v, %v", interrupted, err)
	}
}

func TestAuditOutboxRoundTrip(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	when := time.Now().UTC().Truncate(time.Microsecond)
	if err := catalog.EnqueueAuditEvent(ctx, "event-1", when, []byte(`{"id":"event-1"}`)); err != nil {
		t.Fatal(err)
	}
	// Re-enqueueing the stable event ID is idempotent.
	if err := catalog.EnqueueAuditEvent(ctx, "event-1", when, []byte(`{"id":"event-1"}`)); err != nil {
		t.Fatal(err)
	}
	items, err := catalog.ListAuditOutbox(ctx, 10)
	if err != nil || len(items) != 1 || items[0].ID != "event-1" {
		t.Fatalf("outbox items = %+v, %v", items, err)
	}
	if err := catalog.MarkAuditOutboxAttempt(ctx, "event-1", "temporary"); err != nil {
		t.Fatal(err)
	}
	items, _ = catalog.ListAuditOutbox(ctx, 10)
	if items[0].AttemptCount != 1 || items[0].LastError != "temporary" {
		t.Fatalf("attempt metadata = %+v", items[0])
	}
	if err := catalog.DeleteAuditOutboxEvent(ctx, "event-1"); err != nil {
		t.Fatal(err)
	}
	if count, err := catalog.CountAuditOutbox(ctx); err != nil || count != 0 {
		t.Fatalf("outbox count = %d, %v", count, err)
	}
}

func TestMigrationV19AddsAuditOutboxAndRollbackLink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "catalog.duckdb")
	catalog, _, err := Init(Config{Path: path, EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.db.Exec(`DROP TABLE audit_outbox;
		DROP INDEX import_jobs_workspace; DROP INDEX import_jobs_status;
		ALTER TABLE import_jobs DROP COLUMN rollback_operation_id;
		CREATE INDEX import_jobs_workspace ON import_jobs(workspace_id, created_at);
		CREATE INDEX import_jobs_status ON import_jobs(status, updated_at);
		DELETE FROM schema_info WHERE version>18; INSERT OR REPLACE INTO schema_info(version) VALUES(18)`); err != nil {
		t.Fatal(err)
	}
	if err = catalog.Close(); err != nil {
		t.Fatal(err)
	}
	catalog, err = Open(Config{Path: path, EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	var version int
	if err = catalog.db.QueryRow(`SELECT max(version) FROM schema_info`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	if err = catalog.EnqueueAuditEvent(context.Background(), "migrated", time.Now().UTC(), []byte(`{"id":"migrated"}`)); err != nil {
		t.Fatal(err)
	}
	columns, err := catalog.db.Query(`SELECT rollback_operation_id FROM import_jobs LIMIT 0`)
	if err != nil {
		t.Fatal(err)
	}
	columns.Close()
}
