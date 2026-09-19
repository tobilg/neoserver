package audit

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCursorTraversesRetainedHistoryWithTiedTimestamps(t *testing.T) {
	ctx := context.Background()
	m, err := Open(ctx, auditTestConfig(t.TempDir()), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	// A single transaction keeps a >1000-event regression inexpensive.
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := range 1107 {
		_, err := tx.ExecContext(ctx, `INSERT INTO audit_events(id,occurred_at,principal,credential_id,workspace,path,operation,status,request_id,auth_method,protocol,action,method,duration_ms,security_event) VALUES(?,?,?,?,?,?,?,?,'','jwt','management','change','POST',0,false)`, fmt.Sprintf("event-%04d", i), stamp, "operator", "key-id", "demo", "/api/v1/workspaces", "publish", 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	query := Query{Limit: 100, Workspace: "demo", Principal: "operator", CredentialID: "key-id", Search: "PUBLISH"}
	seen := map[string]bool{}
	for {
		page, err := m.ListPage(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range page.Events {
			if seen[event.ID] {
				t.Fatalf("duplicate event %s", event.ID)
			}
			seen[event.ID] = true
		}
		if query.Cursor == "" {
			if err := m.Record(ctx, Event{ID: "new-write", Timestamp: stamp.Add(time.Second), Principal: "operator", Workspace: "demo", CredentialID: "key-id", Operation: "publish"}); err != nil {
				t.Fatal(err)
			}
		}
		if page.NextCursor == "" {
			break
		}
		query.Cursor = page.NextCursor
	}
	if len(seen) != 1107 || seen["new-write"] {
		t.Fatalf("incorrect traversal: %d", len(seen))
	}
	page, err := m.ListPage(ctx, Query{Limit: 1000})
	if err != nil || len(page.Events) != 1000 || page.NextCursor == "" {
		t.Fatalf("maximum page: %d %v", len(page.Events), err)
	}
	if _, err := m.ListPage(ctx, Query{Cursor: "not-a-cursor"}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid cursor: %v", err)
	}
}
