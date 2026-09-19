package store

import (
	"context"
	"errors"
	"testing"
)

func TestWorkspaceDeletionIsSafeAndReferentiallyComplete(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	workspace, err := catalog.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "delete-me"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.CreateService(ctx, CreateServiceInput{WorkspaceID: workspace.ID, Name: "source", Type: ServiceTypePostGIS, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	layer, err := catalog.CreateLayer(ctx, CreateLayerInput{ServiceID: service.ID, SourceLayer: "roads", PublicID: "roads", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.CreateStyle(ctx, CreateStyleInput{WorkspaceID: workspace.ID, Name: "roads", SLDBody: "<StyledLayerDescriptor/>", Format: "sld_1.1.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.UpsertStyleAsset(ctx, UpsertStyleAssetInput{WorkspaceID: workspace.ID, Name: "marker.png", ContentType: "image/png", SizeBytes: 4, SHA256: "abcd"}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.CreateLayerGroup(ctx, CreateLayerGroupInput{WorkspaceID: workspace.ID, PublicID: "base", Enabled: true, Members: []LayerGroupMember{{Resource: layer.PublicID}}}); err != nil {
		t.Fatal(err)
	}

	err = catalog.DeleteWorkspace(ctx, workspace.ID)
	var conflict *DeletionConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrResourceNotEmpty) {
		t.Fatalf("DeleteWorkspace error = %v, want deletion conflict", err)
	}
	if len(conflict.Plan.Services) != 1 || len(conflict.Plan.Layers) != 1 || len(conflict.Plan.Styles) != 1 || len(conflict.Plan.StyleAssets) != 1 || len(conflict.Plan.LayerGroups) != 1 {
		t.Fatalf("incomplete dependency plan: %+v", conflict.Plan)
	}

	operation, created, err := catalog.BeginCatalogDeletion(ctx, conflict.Plan)
	if err != nil || !created {
		t.Fatalf("BeginCatalogDeletion = %+v, %t, %v", operation, created, err)
	}
	duplicate, created, err := catalog.BeginCatalogDeletion(ctx, conflict.Plan)
	if err != nil || created || duplicate.ID != operation.ID {
		t.Fatalf("duplicate operation = %+v, %t, %v", duplicate, created, err)
	}
	if _, err := catalog.GetWorkspace(ctx, workspace.ID); err != ErrNotFound {
		t.Fatalf("tombstoned workspace remained visible: %v", err)
	}
	if err := catalog.CommitCatalogDeletion(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpdateCatalogDeletion(ctx, operation.ID, DeletionCompleted, DeletionPhaseCompleted, ""); err != nil {
		t.Fatal(err)
	}
	for table, predicate := range map[string]string{
		"workspaces": "id='" + workspace.ID + "'", "services": "workspace_id='" + workspace.ID + "'",
		"styles": "workspace_id='" + workspace.ID + "'", "style_assets": "workspace_id='" + workspace.ID + "'",
		"layer_groups": "workspace_id='" + workspace.ID + "'",
	} {
		var count int
		if err := catalog.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE "+predicate).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d deleted rows", table, count)
		}
	}
	if completed, err := catalog.GetCatalogDeletion(ctx, operation.ID); err != nil || completed.Status != DeletionCompleted {
		t.Fatalf("completed operation = %+v, %v", completed, err)
	}
}

func TestServiceDeletionIncludesTransitiveGroupsButPreservesSiblings(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	workspace, _ := catalog.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "service-delete"})
	target, _ := catalog.CreateService(ctx, CreateServiceInput{WorkspaceID: workspace.ID, Name: "target", Type: ServiceTypePostGIS, Enabled: true})
	sibling, _ := catalog.CreateService(ctx, CreateServiceInput{WorkspaceID: workspace.ID, Name: "sibling", Type: ServiceTypePostGIS, Enabled: true})
	roads, _ := catalog.CreateLayer(ctx, CreateLayerInput{ServiceID: target.ID, SourceLayer: "roads", PublicID: "roads", Enabled: true})
	labels, _ := catalog.CreateLayer(ctx, CreateLayerInput{ServiceID: sibling.ID, SourceLayer: "labels", PublicID: "labels", Enabled: true})
	base, err := catalog.CreateLayerGroup(ctx, CreateLayerGroupInput{WorkspaceID: workspace.ID, PublicID: "base", Enabled: true, Members: []LayerGroupMember{{Resource: roads.PublicID}}})
	if err != nil {
		t.Fatal(err)
	}
	composite, err := catalog.CreateLayerGroup(ctx, CreateLayerGroupInput{WorkspaceID: workspace.ID, PublicID: "composite", Enabled: true, Members: []LayerGroupMember{{Resource: base.PublicID}, {Resource: labels.PublicID}}})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := catalog.PlanServiceDeletion(ctx, workspace.ID, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.LayerGroups) != 2 {
		t.Fatalf("group closure = %+v, want base and composite", plan.LayerGroups)
	}
	operation, _, err := catalog.BeginCatalogDeletion(ctx, *plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.CommitCatalogDeletion(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.GetService(ctx, target.ID); err != ErrNotFound {
		t.Fatalf("target service still exists: %v", err)
	}
	if _, err := catalog.GetLayer(ctx, roads.ID); err != ErrNotFound {
		t.Fatalf("target layer still exists: %v", err)
	}
	if _, err := catalog.GetLayerGroup(ctx, base.ID); err != ErrNotFound {
		t.Fatalf("base group still exists: %v", err)
	}
	if _, err := catalog.GetLayerGroup(ctx, composite.ID); err != ErrNotFound {
		t.Fatalf("composite group still exists: %v", err)
	}
	if _, err := catalog.GetService(ctx, sibling.ID); err != nil {
		t.Fatalf("sibling service deleted: %v", err)
	}
	if _, err := catalog.GetLayer(ctx, labels.ID); err != nil {
		t.Fatalf("sibling layer deleted: %v", err)
	}
}
