package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func datasetMapFixture(t *testing.T, s *DuckDBStore, name string) (*Workspace, *Service, *LayerGroup) {
	t.Helper()
	ctx := context.Background()
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := s.CreateService(ctx, CreateServiceInput{WorkspaceID: ws.ID, Name: "source", Type: ServiceTypePostGIS, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	layer, err := s.CreateLayer(ctx, CreateLayerInput{ServiceID: svc.ID, SourceLayer: "roads", PublicID: "roads", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.CreateLayerGroup(ctx, CreateLayerGroupInput{WorkspaceID: ws.ID, PublicID: "base", Enabled: true, Members: []LayerGroupMember{{Resource: layer.PublicID}}})
	if err != nil {
		t.Fatal(err)
	}
	group, err := s.CreateLayerGroup(ctx, CreateLayerGroupInput{WorkspaceID: ws.ID, PublicID: "map", Enabled: true, Members: []LayerGroupMember{{Resource: base.PublicID}}})
	if err != nil {
		t.Fatal(err)
	}
	return ws, svc, group
}

func TestDatasetMapSelectionLifecycle(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, svc, group := datasetMapFixture(t, s, "map")
	other, _, _ := datasetMapFixture(t, s, "other")
	settings := DefaultOGCTilesAPISettings()
	for _, id := range []string{"missing", svc.ID} {
		settings.Settings.DatasetMapLayerGroupID = id
		if err := s.UpdateOGCTilesAPISettings(ctx, ws.ID, settings); !errors.Is(err, ErrInvalidDatasetMap) {
			t.Fatalf("invalid selection: %v", err)
		}
	}
	settings.Settings.DatasetMapLayerGroupID = group.ID
	if err := s.UpdateOGCTilesAPISettings(ctx, other.ID, settings); !errors.Is(err, ErrInvalidDatasetMap) {
		t.Fatalf("foreign group: %v", err)
	}
	if err := s.UpdateOGCTilesAPISettings(ctx, ws.ID, settings); err != nil {
		t.Fatal(err)
	}
	rename := "renamed"
	if _, err := s.UpdateLayerGroup(ctx, group.ID, UpdateLayerGroupInput{PublicID: &rename}); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetOGCTilesAPISettings(ctx, ws.ID)
	if err != nil || loaded.Settings.DatasetMapLayerGroupID != group.ID {
		t.Fatalf("selection lost on rename: %+v %v", loaded, err)
	}
	if err := s.DeleteLayerGroup(ctx, group.ID); !errors.Is(err, ErrDatasetMapInUse) {
		t.Fatalf("direct delete: %v", err)
	}
	plan, err := s.PlanServiceDeletion(ctx, ws.ID, svc.ID)
	if err != nil || len(plan.Blockers) != 1 {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	if _, _, err := s.BeginCatalogDeletion(ctx, *plan); !errors.Is(err, ErrDatasetMapInUse) {
		t.Fatalf("cascade: %v", err)
	}
	if _, err := s.GetService(ctx, svc.ID); err != nil {
		t.Fatalf("blocked deletion tombstoned service: %v", err)
	}
	settings.Settings.DatasetMapLayerGroupID = ""
	if err := s.UpdateOGCTilesAPISettings(ctx, ws.ID, settings); err != nil {
		t.Fatal(err)
	}
	op, _, err := s.BeginCatalogDeletion(ctx, *plan) // A stale preview must be rechecked.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLayerGroup(ctx, CreateLayerGroupInput{WorkspaceID: ws.ID, PublicID: "late-group", Members: []LayerGroupMember{{Resource: "roads"}}}); !errors.Is(err, ErrGroupResourceDeleting) {
		t.Fatalf("new reference during deletion: %v", err)
	}
	rename = "cannot-escape-deletion"
	if _, err := s.UpdateLayerGroup(ctx, group.ID, UpdateLayerGroupInput{PublicID: &rename, Members: &[]LayerGroupMember{{Resource: "unrelated"}}}); !errors.Is(err, ErrGroupResourceDeleting) {
		t.Fatalf("group update during deletion: %v", err)
	}
	settings.Settings.DatasetMapLayerGroupID = group.ID
	if err := s.UpdateOGCTilesAPISettings(ctx, ws.ID, settings); !errors.Is(err, ErrInvalidDatasetMap) {
		t.Fatalf("pending deletion selection: %v", err)
	}
	if err := s.CommitCatalogDeletion(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetLayerGroup(ctx, group.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group remains: %v", err)
	}
}

func TestDatasetMapAllowsWholeWorkspaceDeletion(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, _, group := datasetMapFixture(t, s, "whole")
	settings := DefaultOGCTilesAPISettings()
	settings.Settings.DatasetMapLayerGroupID = group.ID
	if err := s.UpdateOGCTilesAPISettings(ctx, ws.ID, settings); err != nil {
		t.Fatal(err)
	}
	plan, err := s.PlanWorkspaceDeletion(ctx, ws.ID)
	if err != nil || len(plan.Blockers) != 0 {
		t.Fatalf("workspace plan: %+v %v", plan, err)
	}
	op, _, err := s.BeginCatalogDeletion(ctx, *plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CommitCatalogDeletion(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDatasetMapIntegrityRepair(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, _, group := datasetMapFixture(t, s, "repair")
	settings := DefaultOGCTilesAPISettings()
	settings.Settings.DatasetMapLayerGroupID = group.ID
	if err := s.UpdateOGCTilesAPISettings(ctx, ws.ID, settings); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetTileRevision(ctx, ws.ID)
	// Simulate catalog damage outside the supported management API.
	if _, err := s.db.ExecContext(ctx, "DELETE FROM layer_groups WHERE id=?", group.ID); err != nil {
		t.Fatal(err)
	}
	issues, err := s.AuditCatalogOrphans(ctx)
	if err != nil || len(issues) != 1 || issues[0].Kind != "dataset_map" {
		t.Fatalf("audit: %+v %v", issues, err)
	}
	if count, err := s.RepairCatalogOrphans(ctx); err != nil || count != 1 {
		t.Fatalf("repair: %d %v", count, err)
	}
	loaded, _ := s.GetOGCTilesAPISettings(ctx, ws.ID)
	after, _ := s.GetTileRevision(ctx, ws.ID)
	if loaded.Settings.DatasetMapLayerGroupID != "" || after <= before || len(loaded.Settings.TileMatrixSets) != 2 {
		t.Fatalf("repair lost unrelated settings or revision: %+v %d %d", loaded, before, after)
	}
	if count, err := s.RepairCatalogOrphans(ctx); err != nil || count != 0 {
		t.Fatalf("repeat repair: %d %v", count, err)
	}
}

func TestDatasetMapConcurrentSelectionAndDeletion(t *testing.T) {
	for _, cascade := range []bool{false, true} {
		t.Run(map[bool]string{false: "group", true: "service"}[cascade], func(t *testing.T) {
			s, cleanup := createTestStore(t)
			defer cleanup()
			ctx := context.Background()
			for i := 0; i < 8; i++ {
				ws, svc, group := datasetMapFixture(t, s, string(rune('a'+i)))
				settings := DefaultOGCTilesAPISettings()
				settings.Settings.DatasetMapLayerGroupID = group.ID
				plan, err := s.PlanServiceDeletion(ctx, ws.ID, svc.ID)
				if err != nil {
					t.Fatal(err)
				}
				start := make(chan struct{})
				var wg sync.WaitGroup
				wg.Add(2)
				var selectionErr, deletionErr error
				go func() { defer wg.Done(); <-start; selectionErr = s.UpdateOGCTilesAPISettings(ctx, ws.ID, settings) }()
				go func() {
					defer wg.Done()
					<-start
					if cascade {
						_, _, deletionErr = s.BeginCatalogDeletion(ctx, *plan)
					} else {
						deletionErr = s.DeleteLayerGroup(ctx, group.ID)
					}
				}()
				close(start)
				wg.Wait()
				if selectionErr == nil && deletionErr == nil {
					t.Fatal("both selection and deletion succeeded")
				}
				if selectionErr != nil && !errors.Is(selectionErr, ErrInvalidDatasetMap) {
					t.Fatal(selectionErr)
				}
				if deletionErr != nil && !errors.Is(deletionErr, ErrDatasetMapInUse) {
					t.Fatal(deletionErr)
				}
			}
		})
	}
}
