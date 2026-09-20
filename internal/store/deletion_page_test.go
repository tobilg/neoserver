package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestDeletionHistoryScopesBeforePagingAndSurvivesNewRows(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO workspaces(id,name) VALUES ('mine','mine'),('other','other')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 207; i++ {
		ws := "other"
		if i < 7 {
			ws = "mine"
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO services(id,workspace_id,name,type,connection_info) VALUES (?,?,?,?,'{}')`, fmt.Sprint(i), ws, fmt.Sprint(i), "postgis"); err != nil {
			t.Fatal(err)
		}
		op, _, err := s.BeginCatalogDeletion(ctx, DeletionPlan{Scope: DeletionScopeService, WorkspaceID: ws, Target: DeletionRef{ID: fmt.Sprint(i), Name: fmt.Sprint(i)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateCatalogDeletion(ctx, op.ID, DeletionFailed, DeletionPhasePlanned, "retry me"); err != nil {
			t.Fatal(err)
		}
	}
	// Equal timestamps must still yield each record exactly once.
	if _, err := s.db.ExecContext(ctx, "UPDATE catalog_deletions SET created_at='2026-01-01' WHERE workspace_id='mine'"); err != nil {
		t.Fatal(err)
	}
	q := DeletionQuery{WorkspaceIDs: []string{"mine"}, Status: "failed", Limit: 3}
	seen := map[string]bool{}
	for pageNumber := 0; ; pageNumber++ {
		page, err := s.ListCatalogDeletionPage(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Deletions) == 0 {
			t.Fatal("accessible history hidden behind other workspaces")
		}
		for _, op := range page.Deletions {
			if seen[op.ID] || op.WorkspaceID != "mine" {
				t.Fatalf("duplicate or unauthorized operation: %+v", op)
			}
			seen[op.ID] = true
		}
		if pageNumber == 0 {
			if _, err := s.db.ExecContext(ctx, `INSERT INTO services(id,workspace_id,name,type,connection_info) VALUES ('new','mine','new','postgis','{}')`); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.BeginCatalogDeletion(ctx, DeletionPlan{Scope: DeletionScopeService, WorkspaceID: "mine", Target: DeletionRef{ID: "new", Name: "new"}}); err != nil {
				t.Fatal(err)
			}
			changed := q
			changed.Cursor = page.NextCursor
			changed.WorkspaceIDs = []string{"other"}
			if _, err := s.ListCatalogDeletionPage(ctx, changed); !errors.Is(err, ErrInvalidDeletionQuery) {
				t.Fatalf("cursor scope changed: %v", err)
			}
		}
		q.Cursor = page.NextCursor
		if q.Cursor == "" {
			break
		}
	}
	if len(seen) != 7 {
		t.Fatalf("got %d historic records, want 7", len(seen))
	}
	page, err := s.ListCatalogDeletionPage(ctx, DeletionQuery{WorkspaceIDs: []string{}})
	if err != nil || len(page.Deletions) != 0 {
		t.Fatalf("empty authorization: %+v %v", page, err)
	}
	page, err = s.ListCatalogDeletionPage(ctx, DeletionQuery{ExcludedWorkspaceIDs: []string{"other"}, Status: "actionable"})
	if err != nil || len(page.Deletions) != 8 {
		t.Fatalf("exclusion: %+v %v", page, err)
	}
	for _, bad := range []DeletionQuery{{Limit: -1}, {Limit: 1001}, {Cursor: "not-a-cursor"}, {Status: "unknown"}} {
		if _, err := s.ListCatalogDeletionPage(ctx, bad); !errors.Is(err, ErrInvalidDeletionQuery) {
			t.Fatalf("invalid query %+v accepted: %v", bad, err)
		}
	}
}
