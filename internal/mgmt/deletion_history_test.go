package mgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
)

func TestDeletionHistoryAuthorizationAndValidation(t *testing.T) {
	s, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 202; i++ {
		ws := "other"
		if i < 2 {
			ws = "mine"
		}
		op, _, err := s.BeginCatalogDeletion(context.Background(), store.DeletionPlan{Scope: store.DeletionScopeService, WorkspaceID: ws, Target: store.DeletionRef{ID: fmt.Sprint(i), Name: fmt.Sprint(i)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateCatalogDeletion(context.Background(), op.ID, store.DeletionFailed, store.DeletionPhasePlanned, "retry"); err != nil {
			t.Fatal(err)
		}
	}
	h := &handler{store: s}
	for _, tc := range []struct {
		name, query   string
		roles         map[string]string
		status, count int
	}{
		{"workspace admin", "", map[string]string{"mine": "admin"}, 200, 2},
		{"viewer", "", map[string]string{"mine": "viewer"}, 200, 0},
		{"global admin with override", "", map[string]string{"*": "admin", "other": "viewer"}, 200, 2},
		{"super admin", "?limit=1", map[string]string{"*": "super_admin"}, 200, 1},
		{"filter", "?workspace=mine&status=failed&limit=1", map[string]string{"mine": "admin"}, 200, 1},
		{"cross workspace", "?workspace=other", map[string]string{"mine": "admin"}, 403, 0},
		{"invalid limit", "?limit=no", nil, 400, 0}, {"invalid zero", "?limit=0", nil, 400, 0},
		{"invalid status", "?status=oops", nil, 400, 0}, {"invalid cursor", "?cursor=oops", nil, 400, 0}, {"offset", "?offset=1", nil, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/deletions"+tc.query, nil)
			r = r.WithContext(identity.WithIdentity(r.Context(), &identity.Identity{Roles: tc.roles}))
			w := httptest.NewRecorder()
			h.listDeletionOperations(w, r)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if tc.status == 200 {
				var page store.DeletionPage
				if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Deletions) != tc.count {
					t.Fatalf("%+v %v", page, err)
				}
				if tc.count == 1 && page.NextCursor == "" {
					t.Fatal("missing continuation")
				}
			}
		})
	}
}
