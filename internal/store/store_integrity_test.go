package store

import (
	"context"
	"testing"
)

func TestCatalogIntegrityAuditAndExplicitRepair(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	valid, err := catalog.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "valid"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.db.ExecContext(ctx, `INSERT INTO services(id,workspace_id,name,type,connection_info,enabled) VALUES ('orphan-service','missing','source','postgis','{}',true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.db.ExecContext(ctx, `INSERT INTO layers(id,service_id,source_layer,public_id) VALUES ('orphan-layer','orphan-service','roads','roads')`); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.db.ExecContext(ctx, `INSERT INTO styles(id,workspace_id,name,sld_body) VALUES ('orphan-style','missing','style','<StyledLayerDescriptor/>')`); err != nil {
		t.Fatal(err)
	}
	orphans, err := catalog.AuditCatalogOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 2 { // service and style; the layer still has a service until repair.
		t.Fatalf("orphans = %+v", orphans)
	}
	removed, err := catalog.RepairCatalogOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	orphans, err = catalog.AuditCatalogOrphans(ctx)
	if err != nil || len(orphans) != 0 {
		t.Fatalf("orphans after repair = %+v, %v", orphans, err)
	}
	if _, err := catalog.GetWorkspace(ctx, valid.ID); err != nil {
		t.Fatalf("valid workspace was damaged: %v", err)
	}
}
