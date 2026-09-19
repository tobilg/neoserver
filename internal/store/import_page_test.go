package store

import (
	"context"
	"errors"
	"testing"
)

func TestImportPaginationKeepsOlderActionableJobsReachable(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	job, err := s.CreateImportJob(ctx, CreateImportJobInput{WorkspaceID: "ws", Name: "old ready", SourceKind: "upload"})
	if err != nil {
		t.Fatal(err)
	}
	status := ImportReadyToPublish
	if _, err = s.UpdateImportJob(ctx, job.ID, ImportJobUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO import_jobs (id,workspace_id,name,source_kind,source_path,status,phase,created_at) SELECT 'history-'||i,'ws','history','upload','','cancelled','acquire',current_timestamp FROM range(1001) t(i)`)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := s.ListImportJobsPage(ctx, "ws", ImportListOptions{Limit: 37, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Imports {
			if seen[item.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[item.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 1002 || !seen[job.ID] {
		t.Fatalf("older work hidden: %d jobs", len(seen))
	}
	page, err := s.ListImportJobsPage(ctx, "ws", ImportListOptions{Status: "actionable", Search: "OLD"})
	if err != nil || len(page.Imports) != 1 || page.Imports[0].ID != job.ID {
		t.Fatalf("filter: %+v %v", page, err)
	}
	page, err = s.ListImportJobsPage(ctx, "other", ImportListOptions{})
	if err != nil || len(page.Imports) != 0 {
		t.Fatal("workspace boundary failed")
	}
	if _, err = s.ListImportJobsPage(ctx, "ws", ImportListOptions{Cursor: "invalid"}); !errors.Is(err, ErrInvalidImportList) {
		t.Fatal("invalid cursor accepted")
	}
}
