package mgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
)

func TestAuthorizeServiceEndpoint(t *testing.T) {
	superAdmin := map[string]string{"*": rbac.RoleSuperAdmin}
	workspaceAdmin := map[string]string{"ws-1": "admin"}
	stored := json.RawMessage(`{"host":"db.internal","database":"gis"}`)

	cases := []struct {
		name        string
		roles       map[string]string
		serviceType store.ServiceType
		allowlist   []string
		previous    json.RawMessage
		candidate   string
		want        bool
	}{
		{"super admin may use any host", superAdmin, store.ServiceTypePostGIS, nil, nil, `{"host":"10.0.0.5","port":6432}`, true},
		{"workspace admin denied a new host", workspaceAdmin, store.ServiceTypePostGIS, nil, nil, `{"host":"10.0.0.5"}`, false},
		{"anonymous denied a new host", nil, store.ServiceTypePostGIS, nil, nil, `{"host":"127.0.0.1"}`, false},
		{"workspace admin keeps the stored endpoint", workspaceAdmin, store.ServiceTypePostGIS, nil, stored, `{"host":"DB.internal","port":5432,"database":"other"}`, true},
		{"workspace admin cannot move the port", workspaceAdmin, store.ServiceTypePostGIS, nil, stored, `{"host":"db.internal","port":22}`, false},
		{"workspace admin cannot move the host", workspaceAdmin, store.ServiceTypePostGIS, nil, stored, `{"host":"169.254.169.254"}`, false},
		{"allowlisted host with any port", workspaceAdmin, store.ServiceTypePostGIS, []string{"pg.example.com"}, nil, `{"host":"pg.example.com","port":6432}`, true},
		{"allowlisted host with matching port", workspaceAdmin, store.ServiceTypePostGIS, []string{"pg.example.com:5432"}, nil, `{"host":"pg.example.com"}`, true},
		{"allowlisted host with other port", workspaceAdmin, store.ServiceTypePostGIS, []string{"pg.example.com:5432"}, nil, `{"host":"pg.example.com","port":6432}`, false},
		{"allowlisted IPv6 host", workspaceAdmin, store.ServiceTypePostGIS, []string{"[::1]:5432"}, nil, `{"host":"::1"}`, true},
		{"file services are governed by AllowedPaths", workspaceAdmin, store.ServiceTypeVectorFile, nil, nil, `{"path":"./data/sources/a.gpkg"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &handler{}
			h.cfg.Datasource.DatabaseHosts = tc.allowlist
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			if tc.roles != nil {
				r = r.WithContext(identity.WithIdentity(r.Context(), &identity.Identity{Subject: "t", Roles: tc.roles}))
			}
			w := httptest.NewRecorder()
			got := h.authorizeServiceEndpoint(w, r, tc.serviceType, tc.previous, json.RawMessage(tc.candidate))
			if got != tc.want {
				t.Fatalf("authorizeServiceEndpoint() = %v, want %v", got, tc.want)
			}
			if !got && w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", w.Code)
			}
		})
	}
}

func TestWorkspaceAdminCannotProbeNewPostGISHost(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "Workspace 1"}
	h := newTestHandlerWithEnforcer(t, mockSt)
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"create": h.createService,
		"test":   h.testNewServiceConnection,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"name":"probe","type":"postgis","connection_info":{"host":"127.0.0.1","port":22}}`
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("workspace", "ws-1")
			ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
			ctx = withIdentity(ctx, &identity.Identity{Subject: "t", Roles: map[string]string{"ws-1": "admin"}})
			w := httptest.NewRecorder()
			call(w, r.WithContext(ctx))
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
			}
		})
	}
}
