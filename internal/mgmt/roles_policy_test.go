package mgmt

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestOperationPoliciesResolveScopesAndRejectIneffectiveGrants(t *testing.T) {
	catalog := newMockStore()
	catalog.workspaces["ws-id"] = &store.Workspace{ID: "ws-id", Name: "Test"}
	catalog.roles["custom"] = &store.Role{ID: "custom", Name: "Custom"}
	h := newTestHandlerWithEnforcer(t, catalog)
	for _, tc := range []struct {
		workspace, service, operation, action string
		status                                int
	}{
		{"Test", "wfs", " GetFeature ", "read", 201},
		{"ws-id", "wfs", "Transaction", "write", 201},
		{"*", "wfs", "DropStoredQuery", "manage", 201},
		{"missing", "wfs", "GetFeature", "read", 422},
		{"ws-id", "wfs", "GetMap", "read", 422},
		{"ws-id", "wfs", "DropStoredQuery", "write", 422},
		{"ws-id", "wms", "*", "write", 422},
		{"ws-id", "wfs", "*", "delete", 422},
	} {
		body, _ := json.Marshal(OperationPolicyRequest{Workspace: tc.workspace, Service: tc.service, Operation: tc.operation, Action: tc.action})
		r := httptest.NewRequest("POST", "/roles/custom/policies", bytes.NewReader(body))
		r = r.WithContext(withChiContext(r.Context(), map[string]string{"roleId": "custom"}))
		w := httptest.NewRecorder()
		h.addRolePolicy(w, r)
		if w.Code != tc.status {
			t.Fatalf("%+v: status=%d %s", tc, w.Code, w.Body.String())
		}
	}
	if allowed, err := h.enforcer.CanAccessOperation("custom", "ws-id", "wfs", "GETFEATURE", "read"); err != nil || !allowed {
		t.Fatalf("name-scoped grant did not match UUID: %t %v", allowed, err)
	}
	// Upgrade cleanup must still remove legacy unmatched scopes and actions.
	if err := h.enforcer.AddOperationPolicy("custom", "missing", "wms", "GetMap", "write"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/roles/custom/policies/remove", bytes.NewBufferString(`{"workspace":"missing","service":"wms","operation":"GetMap","action":"write"}`))
	r = r.WithContext(withChiContext(r.Context(), map[string]string{"roleId": "custom"}))
	w := httptest.NewRecorder()
	h.removeRolePolicy(w, r)
	if w.Code != 204 {
		t.Fatalf("legacy removal: %d %s", w.Code, w.Body.String())
	}
	policies, _ := h.enforcer.GetPoliciesForRole("custom")
	for _, policy := range policies {
		if policy[1] == "missing" || policy[1] == "Test" {
			t.Fatalf("ineffective stored scope: %v", policy)
		}
	}
}
