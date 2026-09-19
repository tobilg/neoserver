package store

import (
	"context"
	"testing"
)

func TestLayerGroupCRUD(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "groups"})
	if err != nil {
		t.Fatal(err)
	}
	opacity := .5
	created, err := s.CreateLayerGroup(ctx, CreateLayerGroupInput{WorkspaceID: ws.ID, PublicID: "base", Enabled: true, Members: []LayerGroupMember{{Resource: "roads", Opacity: &opacity}}})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListLayerGroups(ctx, ws.ID)
	if err != nil || len(listed) != 1 || listed[0].Members[0].EffectiveOpacity() != .5 {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	title := "Base map"
	members := []LayerGroupMember{{Resource: "roads", Style: "line"}}
	updated, err := s.UpdateLayerGroup(ctx, created.ID, UpdateLayerGroupInput{Title: &title, Members: &members})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != title || updated.TileCacheGeneration != 2 {
		t.Fatalf("updated=%+v", updated)
	}
	if err = s.DeleteLayerGroup(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
}
