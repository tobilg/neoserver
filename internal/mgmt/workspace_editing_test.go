package mgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestWorkspaceDescriptionOmissionEmptyAndConflicts(t *testing.T) {
	h, router, ws, key := releaseWorkflowFixture(t)
	for _, tc := range []struct{ body, want string }{
		{`{"description":"Original"}`, "Original"}, {`{}`, "Original"},
		{`{"description":null}`, "Original"}, {`{"description":""}`, ""},
	} {
		response := workflowRequest(router, key, http.MethodPut, "/workspaces/workflow", tc.body)
		if response.Code != 200 {
			t.Fatalf("update: %d %s", response.Code, response.Body.String())
		}
		var result WorkspaceResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		stored, err := h.store.GetWorkspace(context.Background(), ws)
		if err != nil || stored.Description != tc.want || result.Description != tc.want || result.UpdatedAt.IsZero() {
			t.Fatalf("stored=%+v response=%+v err=%v", stored, result, err)
		}
	}
	other, err := h.registry.CreateWorkspace(context.Background(), store.CreateWorkspaceInput{Name: "taken"})
	if err != nil {
		t.Fatal(err)
	}
	response := workflowRequest(router, key, http.MethodPut, "/workspaces/workflow", `{"name":"taken"}`)
	if response.Code != 409 {
		t.Fatalf("rename: %d %s", response.Code, response.Body.String())
	}
	if current, _ := h.store.GetWorkspace(context.Background(), ws); current.Name != "workflow" {
		t.Fatal("conflicting rename changed source")
	}
	if current, _ := h.store.GetWorkspace(context.Background(), other.ID); current.Name != "taken" {
		t.Fatal("conflicting rename changed destination")
	}
	admin, err := h.store.CreateAPIKey(context.Background(), store.CreateAPIKeyInput{RoleID: "super_admin", Name: "bootstrap"})
	if err != nil {
		t.Fatal(err)
	}
	response = workflowRequest(router, admin.Key, http.MethodPost, "/workspaces", `{"name":"workflow"}`)
	if response.Code != 409 {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
}
