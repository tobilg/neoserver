package importer

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type recoveryRegistry struct{ workspaceID string }

func (r recoveryRegistry) GetByID(id string) (*workspace.Workspace, bool) {
	return &workspace.Workspace{}, id == r.workspaceID
}
func (recoveryRegistry) RefreshService(context.Context, string, string) error { return nil }

type recoveryRollback struct{ operation *store.DeletionOperation }

func (r recoveryRollback) DeleteService(context.Context, string, string, bool) (*store.DeletionOperation, error) {
	return r.operation, nil
}
func (r recoveryRollback) Get(context.Context, string) (*store.DeletionOperation, error) {
	return r.operation, nil
}
func (r recoveryRollback) FindServiceDeletion(context.Context, string) (*store.DeletionOperation, error) {
	return r.operation, nil
}
func (r recoveryRollback) Retry(context.Context, string) (*store.DeletionOperation, error) {
	return r.operation, nil
}

func importerTestConfig(root string) conf.Importer {
	return conf.Importer{Root: filepath.Join(root, "managed"), TemporaryDirectory: filepath.Join(root, "tmp"),
		WorkerCount: 0, MaxConcurrentJobs: 1, MaxRetries: 2, ShutdownTimeoutSec: 1}
}

func TestInterruptedPublicationResumesAfterFilePromotion(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.duckdb"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	ws, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "publish-recovery"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := catalog.CreateImportJob(ctx, store.CreateImportJobInput{WorkspaceID: ws.ID, Name: "roads", SourceKind: "upload", SourcePath: filepath.Join(root, "roads.geojson")})
	if err != nil {
		t.Fatal(err)
	}
	plan := store.ImportPlan{ServiceName: "managed-roads", Layers: []store.ImportLayerPlan{{SourceLayer: "roads", PublicID: "roads", SourceSRID: 4326, TargetSRID: 4326, Public: true, Enabled: true}}}
	status, phase := store.ImportPublishing, store.ImportPhasePublish
	if _, err = catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Plan: &plan, Status: &status, Phase: &phase}); err != nil {
		t.Fatal(err)
	}
	cfg := importerTestConfig(root)
	staged := filepath.Join(cfg.Root, ".staging", job.ID+".duckdb")
	final := filepath.Join(cfg.Root, ws.ID, job.ID+".duckdb")
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("promoted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertStagedManagedAsset(ctx, store.ManagedAsset{ImportID: job.ID, WorkspaceID: ws.ID, Path: staged, EncryptionKey: "managed-key"}); err != nil {
		t.Fatal(err)
	}
	manager, err := New(ctx, cfg, catalog, recoveryRegistry{workspaceID: ws.ID}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	recovered, err := catalog.GetImportJob(ctx, job.ID)
	if err != nil || recovered.Status != store.ImportPublished || recovered.ServiceID == "" {
		t.Fatalf("recovered job = %+v, %v", recovered, err)
	}
	asset, err := catalog.GetManagedAssetByImport(ctx, job.ID)
	if err != nil || asset.Path != final || asset.ServiceID != recovered.ServiceID {
		t.Fatalf("recovered asset = %+v, %v", asset, err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("stale staging file remains: %v", err)
	}
}

func TestInterruptedRollbackRecoversLegacyOperationLink(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.duckdb"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	ws, _ := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "rollback-recovery"})
	job, _ := catalog.CreateImportJob(ctx, store.CreateImportJobInput{WorkspaceID: ws.ID, Name: "roads", SourceKind: "upload", SourcePath: filepath.Join(root, "roads.geojson")})
	ready := store.ImportReadyToPublish
	_, _ = catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &ready})
	managedPath := filepath.Join(root, "managed", ws.ID, job.ID+".duckdb")
	_ = catalog.UpsertStagedManagedAsset(ctx, store.ManagedAsset{ImportID: job.ID, WorkspaceID: ws.ID, Path: managedPath, EncryptionKey: "key"})
	service, _, err := catalog.PublishImport(ctx, store.PublishImportInput{ImportID: job.ID, WorkspaceID: ws.ID, ManagedPath: managedPath, EncryptionKey: "key",
		Service: store.CreateServiceInput{Name: "managed", Type: store.ServiceTypeDuckDB, ConnectionInfo: json.RawMessage(`{"managed_import_id":"` + job.ID + `"}`), Enabled: true},
		Layers:  []store.CreateLayerInput{{SourceLayer: "roads", PublicID: "roads", Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	rolling, rollbackPhase := store.ImportRollingBack, store.ImportPhaseRollback
	if _, err := catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &rolling, Phase: &rollbackPhase}); err != nil {
		t.Fatal(err)
	}
	op := &store.DeletionOperation{ID: "delete-1", Scope: store.DeletionScopeService, WorkspaceID: ws.ID, TargetID: service.ID, Status: store.DeletionCompleted}
	manager, err := New(ctx, importerTestConfig(root), catalog, recoveryRegistry{workspaceID: ws.ID}, recoveryRollback{operation: op}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		recovered, getErr := catalog.GetImportJob(ctx, job.ID)
		if getErr == nil && recovered.Status == store.ImportRolledBack {
			if recovered.RollbackOperationID != op.ID {
				t.Fatalf("rollback operation = %q", recovered.RollbackOperationID)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("interrupted rollback did not finish")
}

func TestBuildTransformSQLQuotesMappingsAndTransformsCRS(t *testing.T) {
	source := store.ImportDiscoveredLayer{Name: "roads", GeometryColumn: "geom", Properties: []store.ImportProperty{{Name: "name"}}}
	plan := store.ImportLayerPlan{PublicID: "published", TargetGeometry: "shape", SourceSRID: 4326, TargetSRID: 3857, Fields: []store.ImportFieldMapping{{Source: "name", Target: "label", Include: true}}}
	query, id, err := buildTransformSQL(filepath.Join("tmp", "roads.gpkg"), source, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`layer='roads'`, `AS "label"`, "ST_Transform", `managed."published"`} {
		if !strings.Contains(query, expected) {
			t.Fatalf("query missing %q: %s", expected, query)
		}
	}
	if id != "__neoserver_id" {
		t.Fatalf("generated id = %q", id)
	}
}

func TestExtractArchiveRejectsTraversalAndExpandedLimit(t *testing.T) {
	temporary := t.TempDir()
	manager := &Manager{cfg: conf.Importer{TemporaryDirectory: temporary, MaxArchiveFiles: 10, MaxExpandedBytes: 4}}
	job := &store.ImportJob{ID: "job-1"}

	for _, test := range []struct {
		name, entry, contents string
	}{
		{name: "traversal", entry: "../escape.geojson", contents: `{}`},
		{name: "expanded limit", entry: "roads.geojson", contents: `12345`},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "source.zip")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			entry, err := writer.Create(test.entry)
			if err == nil {
				_, err = entry.Write([]byte(test.contents))
			}
			if closeErr := writer.Close(); err == nil {
				err = closeErr
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = manager.extractArchive(context.Background(), job, archive); err == nil {
				t.Fatal("unsafe archive was accepted")
			}
			if _, err = os.Stat(filepath.Join(temporary, "extract-"+job.ID)); !os.IsNotExist(err) {
				t.Fatalf("failed extraction was not cleaned up: %v", err)
			}
		})
	}
}

func FuzzBuildTransformSQLIdentifiers(f *testing.F) {
	f.Add("roads", "name", "label")
	f.Add("odd'layer", `field"name`, "target")
	f.Fuzz(func(t *testing.T, layer, sourceField, target string) {
		if layer == "" || sourceField == "" || target == "" || len(layer)+len(sourceField)+len(target) > 300 {
			t.Skip()
		}
		_, _, _ = buildTransformSQL("source.gpkg", store.ImportDiscoveredLayer{Name: layer, GeometryColumn: "geom", Properties: []store.ImportProperty{{Name: sourceField}}}, store.ImportLayerPlan{PublicID: "published", TargetGeometry: "geom", SourceSRID: 4326, TargetSRID: 4326, Fields: []store.ImportFieldMapping{{Source: sourceField, Target: target, Include: true}}})
	})
}
