package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// withChiContext adds chi URL parameters to the request context.
func withChiContext(ctx context.Context, params map[string]string) context.Context {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

// withIdentity adds an identity to the request context.
func withIdentity(ctx context.Context, id *identity.Identity) context.Context {
	return identity.WithIdentity(ctx, id)
}

// newTestHandlerWithEnforcer creates a test handler with a real enforcer.
func newTestHandlerWithEnforcer(t *testing.T, mockStore *mockStore, factories ...workspace.DataSourceFactory) *handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testConfig()
	var factory workspace.DataSourceFactory
	if len(factories) > 0 {
		factory = factories[0]
	}
	registry := workspace.NewRegistry(mockStore, factory)
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	// Load registry from mock store to populate in-memory cache
	if err := registry.Load(context.Background()); err != nil {
		t.Fatalf("failed to load registry: %v", err)
	}

	// Create an in-memory enforcer
	adapter := rbac.NewMemoryAdapter()
	enforcer, err := rbac.NewEnforcerWithDefaults(adapter)
	if err != nil {
		t.Fatalf("failed to create enforcer: %v", err)
	}

	return &handler{
		cfg:      cfg,
		store:    mockStore,
		registry: registry,
		enforcer: enforcer,
		logger:   logger,
		cache:    cacheManager,
	}
}

// ============================================================================
// Workspace Handler Tests
// ============================================================================

func TestListWorkspaces_Unauthorized(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces", nil)
	w := httptest.NewRecorder()

	h.listWorkspaces(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestListWorkspaces_SuperAdmin(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "Workspace 1"}
	mockSt.workspaces["ws-2"] = &store.Workspace{ID: "ws-2", Name: "Workspace 2"}

	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces", nil)
	ctx := withIdentity(r.Context(), &identity.Identity{
		Subject: "admin",
		Roles:   map[string]string{"*": rbac.RoleSuperAdmin},
	})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.listWorkspaces(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string][]WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result["workspaces"]) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(result["workspaces"]))
	}
}

func TestListWorkspaces_RegularUserForbidden(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "Workspace 1"}
	mockSt.workspaces["ws-2"] = &store.Workspace{ID: "ws-2", Name: "Workspace 2"}

	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces", nil)
	ctx := withIdentity(r.Context(), &identity.Identity{
		Subject: "user",
		Roles:   map[string]string{"ws-1": rbac.RoleViewer}, // Only access to ws-1
	})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.listWorkspaces(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestListWorkspaces_Empty(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces", nil)
	ctx := withIdentity(r.Context(), &identity.Identity{
		Subject: "admin",
		Roles:   map[string]string{"*": rbac.RoleSuperAdmin},
	})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.listWorkspaces(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string][]WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["workspaces"] == nil {
		t.Error("expected empty array, got nil")
	}
	if len(result["workspaces"]) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(result["workspaces"]))
	}
}

func TestCreateWorkspace_Success(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"test-workspace","description":"A test workspace"}`)
	r := httptest.NewRequest("POST", "/workspaces", body)
	w := httptest.NewRecorder()

	h.createWorkspace(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var result WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Name != "test-workspace" {
		t.Errorf("expected name 'test-workspace', got %q", result.Name)
	}
	if result.Description != "A test workspace" {
		t.Errorf("expected description 'A test workspace', got %q", result.Description)
	}
}

func TestCreateWorkspace_MissingName(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"description":"A test workspace"}`)
	r := httptest.NewRequest("POST", "/workspaces", body)
	w := httptest.NewRecorder()

	h.createWorkspace(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateWorkspace_InvalidJSON(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{invalid json}`)
	r := httptest.NewRequest("POST", "/workspaces", body)
	w := httptest.NewRecorder()

	h.createWorkspace(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGetWorkspace_Success(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-test"] = &store.Workspace{
		ID:          "ws-test",
		Name:        "Test Workspace",
		Description: "A test workspace",
	}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-test", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getWorkspace(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.ID != "ws-test" {
		t.Errorf("expected ID 'ws-test', got %q", result.ID)
	}
}

func TestGetWorkspace_NotFound(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getWorkspace(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestUpdateWorkspace_Success(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-test"] = &store.Workspace{
		ID:   "ws-test",
		Name: "Old Name",
	}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"new-name","description":"Updated description"}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-test", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateWorkspace(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Name != "new-name" {
		t.Errorf("expected name 'new-name', got %q", result.Name)
	}
}

func TestUpdateWorkspace_NotFound(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"new-name"}`)
	r := httptest.NewRequest("PUT", "/workspaces/nonexistent", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateWorkspace(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestUpdateWorkspace_InvalidJSON(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-test"] = &store.Workspace{ID: "ws-test", Name: "Test"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{invalid}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-test", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateWorkspace(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestDeleteWorkspace_Success(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-test"] = &store.Workspace{ID: "ws-test", Name: "Test"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/workspaces/ws-test", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.deleteWorkspace(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}

	// Verify workspace was deleted
	if _, ok := mockSt.workspaces["ws-test"]; ok {
		t.Error("expected workspace to be deleted")
	}
}

func TestDeleteWorkspace_NotFound(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/workspaces/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.deleteWorkspace(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// ============================================================================
// Service Handler Tests
// ============================================================================

func TestListServices_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspaces first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.workspaces["ws-2"] = &store.Workspace{ID: "ws-2", Name: "workspace-2"}
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "Service 1", Type: "postgis"}
	mockSt.services["svc-2"] = &store.Service{ID: "svc-2", WorkspaceID: "ws-1", Name: "Service 2", Type: "duckdb"}
	mockSt.services["svc-3"] = &store.Service{ID: "svc-3", WorkspaceID: "ws-2", Name: "Service 3", Type: "postgis"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/services", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.listServices(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string][]ServiceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result["services"]) != 2 {
		t.Errorf("expected 2 services, got %d", len(result["services"]))
	}
}

func TestListServices_Empty(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/services", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.listServices(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string][]ServiceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result["services"]) != 0 {
		t.Errorf("expected 0 services, got %d", len(result["services"]))
	}
}

func TestCreateService_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"test-service","type":"postgis","connection_info":{"host":"localhost"}}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/services", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createService(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var result ServiceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Name != "test-service" {
		t.Errorf("expected name 'test-service', got %q", result.Name)
	}
	if result.Type != "postgis" {
		t.Errorf("expected type 'postgis', got %q", result.Type)
	}
	if !result.Enabled {
		t.Error("expected enabled to be true by default")
	}
}

func TestCreateService_MissingType(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"test-service"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/services", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createService(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateService_MissingName(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"type":"postgis"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/services", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createService(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGetService_Success(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-test"] = &store.Service{
		ID:          "svc-test",
		WorkspaceID: "ws-1",
		Name:        "Test Service",
		Type:        "postgis",
		Enabled:     true,
	}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/services/svc-test", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getService(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result ServiceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.ID != "svc-test" {
		t.Errorf("expected ID 'svc-test', got %q", result.ID)
	}
}

func TestGetService_NotFound(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/services/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getService(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGetService_RejectsCrossWorkspaceID(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-other"] = &store.Service{ID: "svc-other", WorkspaceID: "ws-2", Name: "other"}
	h := newTestHandlerWithEnforcer(t, mockSt)
	r := httptest.NewRequest("GET", "/workspaces/ws-1/services/svc-other", nil)
	r = r.WithContext(withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-other"}))
	w := httptest.NewRecorder()

	h.getService(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestUpdateService_Success(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "Test Workspace"}
	mockSt.services["svc-test"] = &store.Service{
		ID:          "svc-test",
		WorkspaceID: "ws-1",
		Name:        "Old Name",
		Enabled:     true,
	}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"new-name","enabled":false}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/services/svc-test", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateService(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result ServiceResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Name != "new-name" {
		t.Errorf("expected name 'new-name', got %q", result.Name)
	}
	if result.Enabled {
		t.Error("expected enabled to be false")
	}
}

func TestUpdateService_NotFound(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"new-name"}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/services/nonexistent", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateService(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDeleteService_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-test"] = &store.Service{ID: "svc-test", WorkspaceID: "ws-1", Name: "Test"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/workspaces/ws-1/services/svc-test", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.deleteService(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestDeleteService_NotFound(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first (service will be not found)
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/workspaces/ws-1/services/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.deleteService(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// ============================================================================
// Layer Handler Tests
// ============================================================================

func TestListLayers_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	// Create services
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1"}
	mockSt.services["svc-2"] = &store.Service{ID: "svc-2", WorkspaceID: "ws-1", Name: "service-2"}
	// Create layers
	mockSt.layers["layer-1"] = &store.Layer{ID: "layer-1", ServiceID: "svc-1", PublicID: "cities", SourceLayer: "public.cities"}
	mockSt.layers["layer-2"] = &store.Layer{ID: "layer-2", ServiceID: "svc-1", PublicID: "roads", SourceLayer: "public.roads"}
	mockSt.layers["layer-3"] = &store.Layer{ID: "layer-3", ServiceID: "svc-2", PublicID: "buildings", SourceLayer: "public.buildings"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/services/svc-1/layers", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.listLayers(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	layers := result["layers"].([]interface{})
	if len(layers) != 2 {
		t.Errorf("expected 2 layers, got %d", len(layers))
	}
}

func TestCreateLayer_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace and service first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1", Enabled: true}
	h := newTestHandlerWithEnforcer(t, mockSt, publicationTestFactory)

	body := bytes.NewBufferString(`{"source_layer":"public.cities","public_id":"cities","title":"World Cities"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/services/svc-1/layers", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createLayer(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var result LayerResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.SourceLayer != "public.cities" {
		t.Errorf("expected source_layer 'public.cities', got %q", result.SourceLayer)
	}
	if result.PublicID != "cities" {
		t.Errorf("expected public_id 'cities', got %q", result.PublicID)
	}
	if result.CRSDefault != 4326 {
		t.Errorf("expected default crs_default 4326, got %d", result.CRSDefault)
	}
}

func TestCreateLayer_DefaultPublicID(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace and service first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1", Enabled: true}
	h := newTestHandlerWithEnforcer(t, mockSt, publicationTestFactory)

	body := bytes.NewBufferString(`{"source_layer":"public.cities"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/services/svc-1/layers", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createLayer(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var result LayerResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// public_id should default to source_layer
	if result.PublicID != "public.cities" {
		t.Errorf("expected public_id 'public.cities', got %q", result.PublicID)
	}
	// title should default to public_id
	if result.Title != "public.cities" {
		t.Errorf("expected title 'public.cities', got %q", result.Title)
	}
}

func TestCreateLayer_MissingSourceLayer(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace and service first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"public_id":"cities"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/services/svc-1/layers", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createLayer(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGetLayer_Success(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1"}
	mockSt.layers["layer-test"] = &store.Layer{
		ID:          "layer-test",
		ServiceID:   "svc-1",
		PublicID:    "cities",
		SourceLayer: "public.cities",
		Title:       "World Cities",
		Enabled:     true,
		CRSDefault:  4326,
	}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/services/svc-1/layers/layer-test", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1", "layer": "layer-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getLayer(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result LayerResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.ID != "layer-test" {
		t.Errorf("expected ID 'layer-test', got %q", result.ID)
	}
	if result.Title != "World Cities" {
		t.Errorf("expected title 'World Cities', got %q", result.Title)
	}
}

func TestGetLayer_NotFound(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/services/svc-1/layers/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1", "layer": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getLayer(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGetLayer_RejectsCrossServiceID(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1"}
	mockSt.layers["layer-other"] = &store.Layer{ID: "layer-other", ServiceID: "svc-2", PublicID: "other"}
	h := newTestHandlerWithEnforcer(t, mockSt)
	r := httptest.NewRequest("GET", "/workspaces/ws-1/services/svc-1/layers/layer-other", nil)
	r = r.WithContext(withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1", "layer": "layer-other"}))
	w := httptest.NewRecorder()

	h.getLayer(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestUpdateLayer_Success(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "Test Workspace"}
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "Test Service", Enabled: true}
	mockSt.layers["layer-test"] = &store.Layer{
		ID:        "layer-test",
		ServiceID: "svc-1",
		Title:     "Old Title",
		Enabled:   true,
		PublicID:  "cities",
	}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"title":"New Title","enabled":false}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/services/svc-1/layers/layer-test", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1", "layer": "layer-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateLayer(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result LayerResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Title != "New Title" {
		t.Errorf("expected title 'New Title', got %q", result.Title)
	}
	if result.Enabled {
		t.Error("expected enabled to be false")
	}
}

func TestUpdateLayer_NotFound(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first (layer will be not found)
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"title":"New Title"}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/services/svc-1/layers/nonexistent", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1", "layer": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateLayer(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDeleteLayer_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace and service first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1"}
	mockSt.layers["layer-test"] = &store.Layer{ID: "layer-test", ServiceID: "svc-1", PublicID: "cities"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/workspaces/ws-1/services/svc-1/layers/layer-test", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1", "layer": "layer-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.deleteLayer(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestDeleteLayer_NotFound(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace and service first (layer will be not found)
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/workspaces/ws-1/services/svc-1/layers/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1", "layer": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.deleteLayer(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// ============================================================================
// API Key Handler Tests
// ============================================================================

func TestListAPIKeys_Success(t *testing.T) {
	mockSt := newMockStore()
	wsID := "ws-1"
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.apiKeys["key-1"] = &store.APIKey{ID: "key-1", WorkspaceID: &wsID, Name: "Key 1", KeyPrefix: "abc"}
	mockSt.apiKeys["key-2"] = &store.APIKey{ID: "key-2", WorkspaceID: &wsID, Name: "Key 2", KeyPrefix: "def"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/apikeys", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.listAPIKeys(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string][]APIKeyResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result["api_keys"]) != 2 {
		t.Errorf("expected 2 API keys, got %d", len(result["api_keys"]))
	}
}

func TestRevokeAPIKey_Success(t *testing.T) {
	mockSt := newMockStore()
	wsID := "ws-1"
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "Workspace 1"}
	mockSt.apiKeys["key-test"] = &store.APIKey{ID: "key-test", WorkspaceID: &wsID, Name: "Test Key"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/workspaces/ws-1/apikeys/key-test", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "keyId": "key-test"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.revokeAPIKey(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestRevokeAPIKey_NotFound(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "Workspace 1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/workspaces/ws-1/apikeys/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "keyId": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.revokeAPIKey(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// ============================================================================
// OpenAPI Handler Tests
// ============================================================================

func TestAPIHandler(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)
	cfg := testConfig()

	r := httptest.NewRequest("GET", "/api", nil)
	w := httptest.NewRecorder()

	h.api(w, r, cfg)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["openapi"] != "3.0.3" {
		t.Errorf("expected openapi 3.0.3, got %v", result["openapi"])
	}
}

func TestAPIHTMLHandler(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/api.html", nil)
	w := httptest.NewRecorder()

	h.apiHTML(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type text/html; charset=utf-8, got %s", ct)
	}

	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("swagger-ui")) {
		t.Error("expected response to contain swagger-ui")
	}
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestBuildOpenAPI(t *testing.T) {
	cfg := testConfig()
	doc := buildOpenAPI(cfg)

	if doc.OpenAPI != "3.0.3" {
		t.Errorf("expected OpenAPI 3.0.3, got %q", doc.OpenAPI)
	}

	if doc.Info == nil {
		t.Fatal("expected Info to be set")
	}

	if doc.Info.Title != "neoserver Management API" {
		t.Errorf("expected title 'neoserver Management API', got %q", doc.Info.Title)
	}

	if doc.Components == nil {
		t.Fatal("expected Components to be set")
	}

	// Check that schemas are defined
	schemas := doc.Components.Schemas
	expectedSchemas := []string{
		"Error", "Workspace", "Service", "ServiceCacheSettings", "Layer", "Dimension",
		"SQLView", "ValidateSQLResponse", "APIKey", "Role", "OGCTilesSettings",
	}
	for _, name := range expectedSchemas {
		if _, ok := schemas[name]; !ok {
			t.Errorf("expected schema %q to be defined", name)
		}
	}

	// Check that paths are defined
	if doc.Paths == nil {
		t.Fatal("expected Paths to be set")
	}

	expectedPaths := []string{
		"/workspaces",
		"/workspaces/{workspace}",
		"/workspaces/{workspace}/services/{service}/validate-sql",
		"/workspaces/{workspace}/settings/ogc-tiles",
		"/roles",
	}
	for _, path := range expectedPaths {
		if doc.Paths.Find(path) == nil {
			t.Errorf("expected path %q to be defined", path)
		}
	}

	serviceType := schemas["Service"].Value.Properties["type"].Value
	foundVectorFile := false
	for _, value := range serviceType.Enum {
		if value == "vectorfile" {
			foundVectorFile = true
			break
		}
	}
	if !foundVectorFile {
		t.Error("expected Service.type enum to include vectorfile")
	}

	layerProperties := schemas["Layer"].Value.Properties
	for _, property := range []string{"dimensions", "is_sql_view", "sql_view_config", "public", "allowed_roles"} {
		if layerProperties[property] == nil {
			t.Errorf("expected Layer schema property %q", property)
		}
	}

	coverageSubtype := schemas["Coverage"].Value.Properties["wcs20_coverage_subtype"]
	if coverageSubtype == nil || len(coverageSubtype.Value.Enum) != 2 {
		t.Fatalf("expected Coverage.wcs20_coverage_subtype enum")
	}

	tilesProperties := schemas["OGCTilesSettings"].Value.Properties
	for _, property := range []string{"enabled", "public", "versions", "settings"} {
		if tilesProperties[property] == nil {
			t.Errorf("expected OGCTilesSettings schema property %q", property)
		}
	}

	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal generated OpenAPI document: %v", err)
	}
	loaded, err := openapi3.NewLoader().LoadFromData(encoded)
	if err != nil {
		t.Fatalf("reload generated OpenAPI document: %v", err)
	}
	if err := loaded.Validate(context.Background()); err != nil {
		t.Fatalf("generated OpenAPI document is invalid: %v", err)
	}
}

func TestSwaggerUIHTML(t *testing.T) {
	html := swaggerUIHTML("")

	if !bytes.Contains([]byte(html), []byte("<!doctype html>")) {
		t.Error("expected HTML to contain doctype")
	}
	if !bytes.Contains([]byte(html), []byte("swagger-ui")) {
		t.Error("expected HTML to contain swagger-ui reference")
	}

	// The server sends `default-src 'self'; script-src 'self'`, so every asset
	// the page pulls must be same-origin and the initialiser cannot be inline.
	// Regressing any of these renders the docs page blank.
	if bytes.Contains([]byte(html), []byte("//unpkg.com")) ||
		bytes.Contains([]byte(html), []byte("http://")) ||
		bytes.Contains([]byte(html), []byte("https://")) {
		t.Error("Swagger UI must not load assets from a remote origin; the CSP blocks them")
	}
	if bytes.Contains([]byte(html), []byte("SwaggerUIBundle(")) {
		t.Error("the initialiser must be an external script, not inline; the CSP blocks inline scripts")
	}
	for _, asset := range []string{
		"/admin/vendor/swagger/swagger-ui.css",
		"/admin/vendor/swagger/swagger-ui-bundle.js",
		"/api/v1/api.js",
	} {
		if !bytes.Contains([]byte(html), []byte(asset)) {
			t.Errorf("expected HTML to reference %s", asset)
		}
	}
}

func TestSwaggerUIHTMLHonorsBasePath(t *testing.T) {
	html := swaggerUIHTML("/gis")
	for _, asset := range []string{
		"/gis/admin/vendor/swagger/swagger-ui-bundle.js",
		"/gis/api/v1/api.js",
	} {
		if !bytes.Contains([]byte(html), []byte(asset)) {
			t.Errorf("expected HTML to reference %s under a base path", asset)
		}
	}
}

func TestAPIInitJSDegradesWithoutRuntime(t *testing.T) {
	// A binary built without Node, or a server with the console disabled, has
	// no Swagger runtime. The page must say so rather than render empty.
	if !bytes.Contains([]byte(apiInitJS), []byte(`typeof SwaggerUIBundle === "undefined"`)) {
		t.Error("expected the initialiser to detect a missing Swagger UI runtime")
	}
	if !bytes.Contains([]byte(apiInitJS), []byte("Server.AdminUI=false")) {
		t.Error("expected the fallback message to name the config that disables the console")
	}
}

func TestPtrString(t *testing.T) {
	result := ptrString("test")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if *result != "test" {
		t.Errorf("expected 'test', got %q", *result)
	}
}

func TestMkResponses(t *testing.T) {
	// mkResponses is tested indirectly through buildOpenAPI
	// Just verify it handles empty map
	responses := mkResponses(map[string]*openapi3.ResponseRef{})
	if responses == nil {
		t.Fatal("expected non-nil responses")
	}
}

// ============================================================================
// Request/Response Types Tests
// ============================================================================

func TestWorkspaceResponseJSON(t *testing.T) {
	resp := WorkspaceResponse{
		ID:          "ws-123",
		Name:        "Test",
		Description: "A workspace",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded WorkspaceResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != resp.ID {
		t.Errorf("expected ID %q, got %q", resp.ID, decoded.ID)
	}
}

func TestServiceResponseJSON(t *testing.T) {
	resp := ServiceResponse{
		ID:          "svc-123",
		WorkspaceID: "ws-123",
		Name:        "Test Service",
		Type:        "postgis",
		Enabled:     true,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ServiceResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != "postgis" {
		t.Errorf("expected type 'postgis', got %q", decoded.Type)
	}
}

func TestLayerResponseJSON(t *testing.T) {
	resp := LayerResponse{
		ID:          "layer-123",
		ServiceID:   "svc-123",
		SourceLayer: "public.cities",
		PublicID:    "cities",
		Title:       "World Cities",
		Enabled:     true,
		CRSDefault:  4326,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded LayerResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.CRSDefault != 4326 {
		t.Errorf("expected crs_default 4326, got %d", decoded.CRSDefault)
	}
}

func TestAPIKeyResponseJSON(t *testing.T) {
	wsID := "ws-123"
	resp := APIKeyResponse{
		ID:          "key-123",
		KeyPrefix:   "abc",
		WorkspaceID: &wsID,
		RoleID:      "viewer",
		Name:        "Test Key",
		Revoked:     false,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded APIKeyResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if *decoded.WorkspaceID != wsID {
		t.Errorf("expected workspace_id %q, got %q", wsID, *decoded.WorkspaceID)
	}
}

func TestCreateWorkspaceRequestJSON(t *testing.T) {
	req := CreateWorkspaceRequest{
		Name:        "new-workspace",
		Description: "A new workspace",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded CreateWorkspaceRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Name != "new-workspace" {
		t.Errorf("expected name 'new-workspace', got %q", decoded.Name)
	}
}

func TestDiscoveredLayerResponseJSON(t *testing.T) {
	resp := DiscoveredLayerResponse{
		Name:           "cities",
		Schema:         "public",
		Title:          "World Cities",
		GeometryColumn: "geom",
		GeometryType:   "Point",
		SRID:           4326,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded DiscoveredLayerResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.GeometryType != "Point" {
		t.Errorf("expected geometry_type 'Point', got %q", decoded.GeometryType)
	}
}

// ============================================================================
// Settings Handler Tests
// ============================================================================

func TestGetWMSSettings_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/settings/wms", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getWMSSettings(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result WMSSettingsResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestUpdateWMSSettings_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"enabled":true,"max_width":2048,"max_height":2048,"title":"WMS Service"}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/settings/wms", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateWMSSettings(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestUpdateWMSSettings_InvalidJSON(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{invalid}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/settings/wms", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateWMSSettings(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGetWFSSettings_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/settings/wfs", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getWFSSettings(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestUpdateWFSSettings_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"enabled":true,"max_features":5000}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/settings/wfs", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateWFSSettings(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestUpdateWFSSettings_InvalidJSON(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{invalid}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/settings/wfs", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateWFSSettings(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGetOGCAPISettings_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/settings/ogcapi", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getOGCAPISettings(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestUpdateOGCAPISettings_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"enabled":true,"title":"OGC API Features"}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/settings/ogcapi", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateOGCAPISettings(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestUpdateOGCAPISettings_InvalidJSON(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{invalid}`)
	r := httptest.NewRequest("PUT", "/workspaces/ws-1/settings/ogcapi", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.updateOGCAPISettings(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// ============================================================================
// Roles Handler Tests
// ============================================================================

func TestListRoles_Success(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/roles", nil)
	w := httptest.NewRecorder()

	h.listRoles(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string][]RoleResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return empty array (not nil)
	if result["roles"] == nil {
		t.Error("expected roles array, got nil")
	}
}

func TestDeleteRole_NotFound(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/roles/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"roleId": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.deleteRole(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// ============================================================================
// Claim Mappings Handler Tests
// ============================================================================

func TestListClaimMappings_Success(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/claim-mappings", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.listClaimMappings(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string][]ClaimMappingResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestGetClaimMapping_NotFound(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/claim-mappings/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "mappingId": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getClaimMapping(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDeleteClaimMapping_Success(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.claimMappings["mapping-1"] = &store.ClaimRoleMapping{ID: "mapping-1", WorkspaceID: "ws-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/workspaces/ws-1/claim-mappings/mapping-1", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "mappingId": "mapping-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.deleteClaimMapping(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestDeleteClaimMapping_RejectsCrossWorkspaceID(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.claimMappings["mapping-other"] = &store.ClaimRoleMapping{ID: "mapping-other", WorkspaceID: "ws-2"}
	h := newTestHandlerWithEnforcer(t, mockSt)
	r := httptest.NewRequest("DELETE", "/workspaces/ws-1/claim-mappings/mapping-other", nil)
	r = r.WithContext(withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "mappingId": "mapping-other"}))
	w := httptest.NewRecorder()

	h.deleteClaimMapping(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if _, exists := mockSt.claimMappings["mapping-other"]; !exists {
		t.Fatal("cross-workspace mapping was deleted")
	}
}

func TestListGlobalClaimMappings_Success(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/claim-mappings", nil)
	w := httptest.NewRecorder()

	h.listGlobalClaimMappings(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestDeleteGlobalClaimMapping_Success(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("DELETE", "/claim-mappings/mapping-1", nil)
	ctx := withChiContext(r.Context(), map[string]string{"mappingId": "mapping-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.deleteGlobalClaimMapping(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

// ============================================================================
// Settings Response Types Tests
// ============================================================================

func TestWMSSettingsResponseJSON(t *testing.T) {
	resp := WMSSettingsResponse{
		Enabled:      true,
		MaxWidth:     4096,
		MaxHeight:    4096,
		Title:        "WMS Service",
		DefaultStyle: "default",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded WMSSettingsResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.MaxWidth != 4096 {
		t.Errorf("expected max_width 4096, got %d", decoded.MaxWidth)
	}
}

func TestWFSSettingsResponseJSON(t *testing.T) {
	resp := WFSSettingsResponse{
		Enabled:     true,
		MaxFeatures: 10000,
		Title:       "WFS Service",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded WFSSettingsResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.MaxFeatures != 10000 {
		t.Errorf("expected max_features 10000, got %d", decoded.MaxFeatures)
	}
}

func TestOGCAPISettingsResponseJSON(t *testing.T) {
	resp := OGCAPISettingsResponse{
		Enabled:  true,
		Title:    "OGC API Features",
		Abstract: "A geospatial API",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded OGCAPISettingsResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Title != "OGC API Features" {
		t.Errorf("expected title 'OGC API Features', got %q", decoded.Title)
	}
}

func TestRoleResponseJSON(t *testing.T) {
	resp := RoleResponse{
		ID:          "admin",
		Name:        "Administrator",
		Description: "Full access",
		IsSystem:    true,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded RoleResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if !decoded.IsSystem {
		t.Error("expected is_system to be true")
	}
}

func TestClaimMappingResponseJSON(t *testing.T) {
	resp := ClaimMappingResponse{
		ID:          "mapping-1",
		WorkspaceID: "ws-1",
		ClaimName:   "groups",
		ClaimValue:  "admins",
		RoleID:      "admin",
		Priority:    100,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ClaimMappingResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ClaimName != "groups" {
		t.Errorf("expected claim_name 'groups', got %q", decoded.ClaimName)
	}
}

func TestCreateRoleRequestJSON(t *testing.T) {
	req := CreateRoleRequest{
		ID:          "custom-role",
		Name:        "Custom Role",
		Description: "A custom role",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded CreateRoleRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != "custom-role" {
		t.Errorf("expected id 'custom-role', got %q", decoded.ID)
	}
}

func TestCreateClaimMappingRequestJSON(t *testing.T) {
	req := CreateClaimMappingRequest{
		ClaimName:  "groups",
		ClaimValue: "developers",
		RoleID:     "editor",
		Priority:   50,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded CreateClaimMappingRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Priority != 50 {
		t.Errorf("expected priority 50, got %d", decoded.Priority)
	}
}

// ============================================================================
// Additional Handler Tests for Coverage
// ============================================================================

func TestCreateRole_Success(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"id":"custom-role","name":"Custom Role","description":"A custom role"}`)
	r := httptest.NewRequest("POST", "/roles", body)
	w := httptest.NewRecorder()

	h.createRole(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var result RoleResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.ID != "custom-role" {
		t.Errorf("expected id 'custom-role', got %q", result.ID)
	}
}

func TestCreateRole_MissingID(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"Custom Role"}`)
	r := httptest.NewRequest("POST", "/roles", body)
	w := httptest.NewRecorder()

	h.createRole(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateRole_InvalidJSON(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{invalid}`)
	r := httptest.NewRequest("POST", "/roles", body)
	w := httptest.NewRecorder()

	h.createRole(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateClaimMapping_Success(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"claim_name":"groups","claim_value":"admins","role_id":"admin","priority":100}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/claim-mappings", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createClaimMapping(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
}

func TestCreateClaimMapping_MissingClaimName(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"claim_value":"admins","role_id":"admin"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/claim-mappings", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createClaimMapping(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateClaimMapping_MissingClaimValue(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"claim_name":"groups","role_id":"admin"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/claim-mappings", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createClaimMapping(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateClaimMapping_MissingRoleID(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"claim_name":"groups","claim_value":"admins"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/claim-mappings", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createClaimMapping(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateClaimMapping_InvalidJSON(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{invalid}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/claim-mappings", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createClaimMapping(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateGlobalClaimMapping_Success(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"claim_name":"groups","claim_value":"super-admins","role_id":"super_admin","priority":200}`)
	r := httptest.NewRequest("POST", "/claim-mappings", body)
	w := httptest.NewRecorder()

	h.createGlobalClaimMapping(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
}

func TestCreateGlobalClaimMapping_MissingFields(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	tests := []struct {
		name string
		body string
	}{
		{"missing claim_name", `{"claim_value":"admins","role_id":"admin"}`},
		{"missing claim_value", `{"claim_name":"groups","role_id":"admin"}`},
		{"missing role_id", `{"claim_name":"groups","claim_value":"admins"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewBufferString(tt.body)
			r := httptest.NewRequest("POST", "/claim-mappings", body)
			w := httptest.NewRecorder()

			h.createGlobalClaimMapping(w, r)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d", w.Code)
			}
		})
	}
}

func TestCreateGlobalClaimMapping_InvalidJSON(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{invalid}`)
	r := httptest.NewRequest("POST", "/claim-mappings", body)
	w := httptest.NewRecorder()

	h.createGlobalClaimMapping(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGetAPIKey_NotFound(t *testing.T) {
	mockSt := newMockStore()
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/apikeys/nonexistent", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "keyId": "nonexistent"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getAPIKey(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGetAPIKey_Success(t *testing.T) {
	mockSt := newMockStore()
	wsID := "ws-1"
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.apiKeys["key-1"] = &store.APIKey{
		ID:          "key-1",
		WorkspaceID: &wsID,
		Name:        "Test Key",
		KeyPrefix:   "abc",
		RoleID:      "viewer",
	}
	h := newTestHandlerWithEnforcer(t, mockSt)

	r := httptest.NewRequest("GET", "/workspaces/ws-1/apikeys/key-1", nil)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "keyId": "key-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.getAPIKey(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result APIKeyResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.ID != "key-1" {
		t.Errorf("expected id 'key-1', got %q", result.ID)
	}
}

func TestCreateAPIKey_MissingName(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"role_id":"viewer"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/apikeys", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createAPIKey(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateAPIKey_MissingRoleID(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"Test Key"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/apikeys", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createAPIKey(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateAPIKey_InvalidJSON(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{invalid}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/apikeys", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createAPIKey(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateAPIKey_InvalidRoleID(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	// No roles in mock store, so any role_id should be invalid
	body := bytes.NewBufferString(`{"name":"Test Key","role_id":"invalid-role"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/apikeys", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createAPIKey(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateAPIKey_Success(t *testing.T) {
	mockSt := newMockStore()
	// Create workspace first
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	// Add a valid role
	mockSt.roles["viewer"] = &store.Role{ID: "viewer", Name: "Viewer"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"Test Key","role_id":"viewer"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/apikeys", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createAPIKey(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
}

func TestCreateAPIKey_SuperAdminRoleDeniedForNonSuperAdmin(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.roles["super_admin"] = &store.Role{ID: "super_admin", Name: "super_admin"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"Escalate","role_id":"super_admin"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/apikeys", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	// Caller is a workspace admin, not a global super admin.
	ctx = withIdentity(ctx, &identity.Identity{Subject: "ws-admin", Roles: map[string]string{"ws-1": "admin"}})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createAPIKey(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestCreateAPIKey_SuperAdminRoleAllowedForSuperAdmin(t *testing.T) {
	mockSt := newMockStore()
	mockSt.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
	mockSt.roles["super_admin"] = &store.Role{ID: "super_admin", Name: "super_admin"}
	h := newTestHandlerWithEnforcer(t, mockSt)

	body := bytes.NewBufferString(`{"name":"Admin Key","role_id":"super_admin"}`)
	r := httptest.NewRequest("POST", "/workspaces/ws-1/apikeys", body)
	ctx := withChiContext(r.Context(), map[string]string{"workspace": "ws-1"})
	ctx = withIdentity(ctx, &identity.Identity{Subject: "root", Roles: map[string]string{"*": "super_admin"}})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	h.createAPIKey(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
}
