package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestManagedResolverRequiresPublishedOwnership(t *testing.T) {
	ctx := context.Background()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	owner, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := catalog.CreateImportJob(ctx, store.CreateImportJobInput{WorkspaceID: owner.ID, Name: "private", SourceKind: "upload"})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertStagedManagedAsset(ctx, store.ManagedAsset{ImportID: job.ID, WorkspaceID: owner.ID, Path: "synthetic.duckdb", EncryptionKey: "abc123"}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Plan: &store.ImportPlan{Layers: []store.ImportLayerPlan{{PublicID: "projected", SourceSRID: 4326, TargetSRID: 3857}}}}); err != nil {
		t.Fatal(err)
	}
	resolve := managedAssetResolver(catalog)
	if _, _, _, err := resolve(ctx, owner.ID, "unpublished", job.ID); err == nil {
		t.Fatal("staged asset disclosed")
	}
	connection, _ := json.Marshal(store.DuckDBConnectionInfo{ManagedImportID: job.ID})
	svc, _, err := catalog.PublishImport(ctx, store.PublishImportInput{WorkspaceID: owner.ID, ImportID: job.ID, ManagedPath: "synthetic.duckdb", EncryptionKey: "abc123", Service: store.CreateServiceInput{Name: "managed", Type: store.ServiceTypeDuckDB, ConnectionInfo: connection, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][3]string{{other.ID, svc.ID, job.ID}, {owner.ID, "different-service", job.ID}, {owner.ID, svc.ID, "different-import"}, {"", svc.ID, job.ID}} {
		path, key, _, err := resolve(ctx, ids[0], ids[1], ids[2])
		if err == nil || path != "" || key != "" {
			t.Fatal("foreign or invalid binding disclosed a secret")
		}
	}
	if path, key, srids, err := resolve(ctx, owner.ID, svc.ID, job.ID); err != nil || path != "synthetic.duckdb" || key != "abc123" || srids["projected"] != 3857 {
		t.Fatalf("own published asset failed: %v", err)
	}
	status := store.ImportRolledBack
	if _, err := catalog.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := resolve(ctx, owner.ID, svc.ID, job.ID); err == nil {
		t.Fatal("rolled-back import remained attachable")
	}
}
