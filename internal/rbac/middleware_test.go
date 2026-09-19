package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/workspace"
)

func newTestIdentity(superAdmin bool, workspaceRoles map[string]string) *identity.Identity {
	roles := make(map[string]string)
	if superAdmin {
		roles["*"] = "super_admin"
	}
	for ws, role := range workspaceRoles {
		roles[ws] = role
	}
	return &identity.Identity{
		Subject:    "test-user",
		AuthMethod: identity.AuthMethodAPIKey,
		Roles:      roles,
	}
}

func newTestWorkspace(id string) *workspace.Workspace {
	return &workspace.Workspace{
		ID:   id,
		Name: "Test Workspace",
	}
}

func newTestLayer(id string) *workspace.Layer {
	return &workspace.Layer{
		ID:    id,
		Title: "Test Layer",
	}
}

func TestRequireWorkspaceRead_NoIdentity(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireWorkspaceRead(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestRequireWorkspaceRead_NoWorkspace(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireWorkspaceRead(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Add identity to context but no workspace
	id := newTestIdentity(false, map[string]string{"ws1": RoleViewer})
	ctx := identity.WithIdentity(context.Background(), id)

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRequireWorkspaceRead_NoRole(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireWorkspaceRead(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Add identity with no role for the workspace
	id := newTestIdentity(false, map[string]string{})
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestRequireWorkspaceRead_Success(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireWorkspaceRead(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleViewer})
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireLayerRead_NoIdentity(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireLayerRead(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestRequireLayerRead_NoWorkspace(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireLayerRead(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleViewer})
	ctx := identity.WithIdentity(context.Background(), id)

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRequireLayerRead_NoLayerInContext(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireLayerRead(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleViewer})
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))
	// No layer in context

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should pass through when no layer in context
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d (no layer = pass through), got %d", http.StatusOK, w.Code)
	}
}

func TestRequireLayerRead_WithLayer_NoRole(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireLayerRead(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{}) // No role for ws1
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))
	ctx = workspace.WithLayer(ctx, newTestLayer("layer1"))

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestRequireLayerRead_WithLayer_Success(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireLayerRead(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleViewer})
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))
	ctx = workspace.WithLayer(ctx, newTestLayer("layer1"))

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireWorkspaceAdmin_NoIdentity(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireWorkspaceAdmin(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestRequireWorkspaceAdmin_SuperAdmin(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireWorkspaceAdmin(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(true, nil)
	ctx := identity.WithIdentity(context.Background(), id)

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireWorkspaceAdmin_NoWorkspace(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireWorkspaceAdmin(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleAdmin})
	ctx := identity.WithIdentity(context.Background(), id)

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRequireWorkspaceAdmin_WithWorkspaceContext(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireWorkspaceAdmin(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleAdmin})
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireWorkspaceAdmin_WithURLParam(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	// Create a chi router to handle URL params
	r := chi.NewRouter()
	r.With(RequireWorkspaceAdmin(e)).Get("/workspaces/{workspace}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	id := newTestIdentity(false, map[string]string{"ws1": RoleAdmin})
	ctx := identity.WithIdentity(context.Background(), id)

	req := httptest.NewRequest("GET", "/workspaces/ws1", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireWorkspaceAdmin_Forbidden(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireWorkspaceAdmin(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Editor role cannot manage workspace
	id := newTestIdentity(false, map[string]string{"ws1": RoleEditor})
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestRequireSuperAdmin_NoIdentity(t *testing.T) {
	handler := RequireSuperAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestRequireSuperAdmin_NotSuperAdmin(t *testing.T) {
	handler := RequireSuperAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleAdmin})
	ctx := identity.WithIdentity(context.Background(), id)

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestRequireSuperAdmin_Success(t *testing.T) {
	handler := RequireSuperAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(true, nil)
	ctx := identity.WithIdentity(context.Background(), id)

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireAction_NoIdentity(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireAction(e, ActionRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestRequireAction_SuperAdmin(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireAction(e, ActionManage)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(true, nil)
	ctx := identity.WithIdentity(context.Background(), id)

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireAction_NoWorkspace(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireAction(e, ActionRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleViewer})
	ctx := identity.WithIdentity(context.Background(), id)

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRequireAction_Success(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireAction(e, ActionRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleViewer})
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireAction_Forbidden(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireAction(e, ActionManage)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleViewer})
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestRequireAction_WithLayer(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := RequireAction(e, ActionRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	id := newTestIdentity(false, map[string]string{"ws1": RoleViewer})
	ctx := identity.WithIdentity(context.Background(), id)
	ctx = workspace.WithWorkspace(ctx, newTestWorkspace("ws1"))
	ctx = workspace.WithLayer(ctx, newTestLayer("layer1"))

	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestOptionalAuth(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	handler := OptionalAuth(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Should pass through without any auth
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}
