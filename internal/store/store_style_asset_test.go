package store

import (
	"context"
	"testing"
)

func TestStyleAssetMetadataCRUD(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	workspace, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "assets"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.UpsertStyleAsset(ctx, UpsertStyleAssetInput{WorkspaceID: workspace.ID, Name: "marker.png", ContentType: "image/png", SizeBytes: 12, SHA256: "first"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpsertStyleAsset(ctx, UpsertStyleAssetInput{WorkspaceID: workspace.ID, Name: "marker.png", ContentType: "image/png", SizeBytes: 24, SHA256: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.SizeBytes != 24 {
		t.Fatalf("updated=%+v", updated)
	}
	if revision, err := s.GetTileRevision(ctx, workspace.ID); err != nil || revision != 3 {
		t.Fatalf("asset create/replace must advance durable render revision: %d %v", revision, err)
	}
	listed, err := s.ListStyleAssets(ctx, workspace.ID)
	if err != nil || len(listed) != 1 || listed[0].SHA256 != "second" {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	if err = s.DeleteStyleAsset(ctx, workspace.ID, "marker.png"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetStyleAsset(ctx, workspace.ID, "marker.png"); err != ErrNotFound {
		t.Fatalf("get after delete=%v", err)
	}
	if revision, err := s.GetTileRevision(ctx, workspace.ID); err != nil || revision != 4 {
		t.Fatalf("delete revision: %d %v", revision, err)
	}
}
