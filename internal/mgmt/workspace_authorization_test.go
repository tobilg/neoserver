package mgmt

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestManagementWorkspaceNamesAndGlobalRoles(t *testing.T) {
	ctx := context.Background()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	ws, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	enforcer, err := rbac.NewEnforcerWithDefaults(rbac.NewStoreAdapter(catalog))
	if err != nil {
		t.Fatal(err)
	}
	key, err := catalog.CreateAPIKey(ctx, store.CreateAPIKeyInput{RoleID: "admin", WorkspaceID: &ws.ID, Name: "workspace admin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		roles map[string]string
		key   string
		want  int
	}{
		{"workspace API key", nil, key.Key, 200}, {"global admin", map[string]string{"*": "admin"}, "", 200},
		{"workspace admin", map[string]string{ws.ID: "admin"}, "", 200},
		{"global editor", map[string]string{"*": "editor"}, "", 403}, {"global viewer", map[string]string{"*": "viewer"}, "", 403},
		{"explicit viewer wins", map[string]string{"*": "admin", ws.ID: "viewer"}, "", 403},
		{"different workspace", map[string]string{"other": "admin"}, "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := chi.NewRouter()
			router.Use(identity.Middleware(identity.MiddlewareConfig{Store: catalog}))
			if tc.roles != nil {
				router.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						next.ServeHTTP(w, r.WithContext(identity.WithIdentity(r.Context(), &identity.Identity{Subject: "test", Roles: tc.roles})))
					})
				})
			}
			RegisterRoutes(router, Dependencies{Store: catalog, Enforcer: enforcer, Registry: workspace.NewRegistry(catalog, nil), Config: conf.Config{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			for _, name := range []string{ws.ID, ws.Name} {
				for _, resource := range []string{"services", "styles"} {
					request := httptest.NewRequest("GET", "/workspaces/"+name+"/"+resource, nil)
					if tc.key != "" {
						request.Header.Set("X-API-Key", tc.key)
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					if response.Code != tc.want {
						t.Fatalf("%s/%s: %d %s", name, resource, response.Code, response.Body.String())
					}
					var body map[string]any
					if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					if tc.want == 200 && body[resource] == nil {
						t.Fatalf("missing real %s collection: %v", resource, body)
					}
					if tc.want == 403 && body["message"] == nil {
						t.Fatal("middleware error violates API contract")
					}
				}
			}
		})
	}
}
