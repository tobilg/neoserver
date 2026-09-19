package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/store"
)

// ============================================================================
// Identity Tests
// ============================================================================

func TestIdentity_HasRole(t *testing.T) {
	tests := []struct {
		name        string
		identity    *Identity
		workspaceID string
		role        string
		want        bool
	}{
		{
			name:        "nil identity",
			identity:    nil,
			workspaceID: "ws-1",
			role:        "viewer",
			want:        false,
		},
		{
			name: "global super_admin has all roles",
			identity: &Identity{
				Roles: map[string]string{"*": "super_admin"},
			},
			workspaceID: "ws-1",
			role:        "admin",
			want:        true,
		},
		{
			name: "global super_admin matches super_admin role check",
			identity: &Identity{
				Roles: map[string]string{"*": "super_admin"},
			},
			workspaceID: "ws-1",
			role:        "super_admin",
			want:        true,
		},
		{
			name: "exact workspace role match",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "editor"},
			},
			workspaceID: "ws-1",
			role:        "editor",
			want:        true,
		},
		{
			name: "admin includes viewer (role hierarchy)",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "admin"},
			},
			workspaceID: "ws-1",
			role:        "viewer",
			want:        true,
		},
		{
			name: "editor includes viewer",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "editor"},
			},
			workspaceID: "ws-1",
			role:        "viewer",
			want:        true,
		},
		{
			name: "viewer does not include editor",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "viewer"},
			},
			workspaceID: "ws-1",
			role:        "editor",
			want:        false,
		},
		{
			name: "no role for workspace",
			identity: &Identity{
				Roles: map[string]string{"ws-2": "admin"},
			},
			workspaceID: "ws-1",
			role:        "viewer",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.HasRole(tt.workspaceID, tt.role)
			if got != tt.want {
				t.Errorf("HasRole() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_HasWorkspaceAccess(t *testing.T) {
	tests := []struct {
		name        string
		identity    *Identity
		workspaceID string
		want        bool
	}{
		{
			name:        "nil identity",
			identity:    nil,
			workspaceID: "ws-1",
			want:        false,
		},
		{
			name: "super_admin has access to all",
			identity: &Identity{
				Roles: map[string]string{"*": "super_admin"},
			},
			workspaceID: "ws-1",
			want:        true,
		},
		{
			name: "has role for workspace",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "viewer"},
			},
			workspaceID: "ws-1",
			want:        true,
		},
		{
			name: "no role for workspace",
			identity: &Identity{
				Roles: map[string]string{"ws-2": "viewer"},
			},
			workspaceID: "ws-1",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.HasWorkspaceAccess(tt.workspaceID)
			if got != tt.want {
				t.Errorf("HasWorkspaceAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_IsSuperAdmin(t *testing.T) {
	tests := []struct {
		name     string
		identity *Identity
		want     bool
	}{
		{
			name:     "nil identity",
			identity: nil,
			want:     false,
		},
		{
			name: "is super_admin",
			identity: &Identity{
				Roles: map[string]string{"*": "super_admin"},
			},
			want: true,
		},
		{
			name: "is not super_admin",
			identity: &Identity{
				Roles: map[string]string{"*": "admin"},
			},
			want: false,
		},
		{
			name: "workspace admin is not super_admin",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "admin"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.IsSuperAdmin()
			if got != tt.want {
				t.Errorf("IsSuperAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_IsAdmin(t *testing.T) {
	tests := []struct {
		name        string
		identity    *Identity
		workspaceID string
		want        bool
	}{
		{
			name: "is workspace admin",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "admin"},
			},
			workspaceID: "ws-1",
			want:        true,
		},
		{
			name: "super_admin is admin for any workspace",
			identity: &Identity{
				Roles: map[string]string{"*": "super_admin"},
			},
			workspaceID: "ws-1",
			want:        true,
		},
		{
			name: "editor is not admin",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "editor"},
			},
			workspaceID: "ws-1",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.IsAdmin(tt.workspaceID)
			if got != tt.want {
				t.Errorf("IsAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_CanRead(t *testing.T) {
	tests := []struct {
		name        string
		identity    *Identity
		workspaceID string
		want        bool
	}{
		{
			name: "viewer can read",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "viewer"},
			},
			workspaceID: "ws-1",
			want:        true,
		},
		{
			name: "editor can read",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "editor"},
			},
			workspaceID: "ws-1",
			want:        true,
		},
		{
			name: "admin can read",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "admin"},
			},
			workspaceID: "ws-1",
			want:        true,
		},
		{
			name: "no access cannot read",
			identity: &Identity{
				Roles: map[string]string{},
			},
			workspaceID: "ws-1",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.CanRead(tt.workspaceID)
			if got != tt.want {
				t.Errorf("CanRead() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_CanWrite(t *testing.T) {
	tests := []struct {
		name        string
		identity    *Identity
		workspaceID string
		want        bool
	}{
		{
			name: "viewer cannot write",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "viewer"},
			},
			workspaceID: "ws-1",
			want:        false,
		},
		{
			name: "editor can write",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "editor"},
			},
			workspaceID: "ws-1",
			want:        true,
		},
		{
			name: "admin can write",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "admin"},
			},
			workspaceID: "ws-1",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.CanWrite(tt.workspaceID)
			if got != tt.want {
				t.Errorf("CanWrite() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_GetWorkspaceRole(t *testing.T) {
	tests := []struct {
		name        string
		identity    *Identity
		workspaceID string
		want        string
	}{
		{
			name:        "nil identity",
			identity:    nil,
			workspaceID: "ws-1",
			want:        "",
		},
		{
			name: "super_admin returns super_admin",
			identity: &Identity{
				Roles: map[string]string{"*": "super_admin"},
			},
			workspaceID: "ws-1",
			want:        "super_admin",
		},
		{
			name: "workspace role",
			identity: &Identity{
				Roles: map[string]string{"ws-1": "editor"},
			},
			workspaceID: "ws-1",
			want:        "editor",
		},
		{
			name: "no role for workspace",
			identity: &Identity{
				Roles: map[string]string{"ws-2": "editor"},
			},
			workspaceID: "ws-1",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.GetWorkspaceRole(tt.workspaceID)
			if got != tt.want {
				t.Errorf("GetWorkspaceRole() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoleIncludes(t *testing.T) {
	tests := []struct {
		role   string
		target string
		want   bool
	}{
		{"super_admin", "admin", true},
		{"super_admin", "editor", true},
		{"super_admin", "viewer", true},
		{"admin", "editor", true},
		{"admin", "viewer", true},
		{"editor", "viewer", true},
		{"viewer", "viewer", true},
		{"viewer", "editor", false},
		{"viewer", "admin", false},
		{"editor", "admin", false},
		{"unknown", "viewer", false},
	}

	for _, tt := range tests {
		t.Run(tt.role+"_includes_"+tt.target, func(t *testing.T) {
			got := roleIncludes(tt.role, tt.target)
			if got != tt.want {
				t.Errorf("roleIncludes(%s, %s) = %v, want %v", tt.role, tt.target, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Context Tests
// ============================================================================

func TestWithIdentity_FromContext(t *testing.T) {
	id := &Identity{
		Subject:    "user@example.com",
		AuthMethod: AuthMethodAPIKey,
		Roles:      map[string]string{"ws-1": "admin"},
	}

	ctx := WithIdentity(context.Background(), id)

	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("expected identity in context")
	}

	if got.Subject != "user@example.com" {
		t.Errorf("expected subject 'user@example.com', got %s", got.Subject)
	}
}

func TestFromContext_NotPresent(t *testing.T) {
	ctx := context.Background()

	_, ok := FromContext(ctx)
	if ok {
		t.Error("expected no identity in context")
	}
}

func TestMustFromContext(t *testing.T) {
	id := &Identity{Subject: "test"}
	ctx := WithIdentity(context.Background(), id)

	got := MustFromContext(ctx)
	if got.Subject != "test" {
		t.Errorf("expected subject 'test', got %s", got.Subject)
	}
}

func TestMustFromContext_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when identity not in context")
		}
	}()

	MustFromContext(context.Background())
}

func TestAnonymous(t *testing.T) {
	id := Anonymous()

	if id.Subject != "anonymous" {
		t.Errorf("expected subject 'anonymous', got %s", id.Subject)
	}
	if id.AuthMethod != AuthMethodAnonymous {
		t.Errorf("expected auth method anonymous, got %s", id.AuthMethod)
	}
	if id.Roles == nil {
		t.Error("expected non-nil roles map")
	}
}

// ============================================================================
// AuthError Tests
// ============================================================================

func TestAuthError_Error(t *testing.T) {
	err := &AuthError{Message: "invalid credentials"}
	if err.Error() != "invalid credentials" {
		t.Errorf("expected 'invalid credentials', got %s", err.Error())
	}
}

// ============================================================================
// Mock Store for API Key Tests
// ============================================================================

type mockStore struct {
	apiKeys map[string]*store.APIKey
	lookups int
}

func newMockStore() *mockStore {
	return &mockStore{
		apiKeys: make(map[string]*store.APIKey),
	}
}

func (m *mockStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (*store.APIKey, error) {
	m.lookups++
	key, ok := m.apiKeys[keyHash]
	if !ok {
		return nil, store.ErrNotFound
	}
	return key, nil
}

func TestAPIKeyValidator_RevocationIsImmediate(t *testing.T) {
	ms := newMockStore()
	const rawKey = "nsk_revoked"
	ms.addTestKey(rawKey, &store.APIKey{ID: "key-revoked", OwnerName: "User", RoleID: "viewer"})
	validator := NewAPIKeyValidator(ms)
	if _, err := validator.Validate(context.Background(), rawKey); err != nil {
		t.Fatalf("initial validation failed: %v", err)
	}
	hash := sha256.Sum256([]byte(rawKey))
	delete(ms.apiKeys, hex.EncodeToString(hash[:]))
	if _, err := validator.Validate(context.Background(), rawKey); err == nil {
		t.Fatal("revoked key remained valid")
	}
	if ms.lookups != 2 {
		t.Fatalf("expected a store lookup per validation, got %d", ms.lookups)
	}
}

func TestMiddleware_InvalidCredentialRejectedEvenWhenAuthOptional(t *testing.T) {
	ms := newMockStore()
	mw := Middleware(MiddlewareConfig{Store: ms, RequireAuth: false})

	nextCalled := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	// A presented-but-invalid API key must be rejected with 401 even though
	// RequireAuth is false, rather than silently downgrading to anonymous.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "nsk_does_not_exist")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid credential: got status %d, want 401", rec.Code)
	}
	if nextCalled {
		t.Fatal("handler should not run for an invalid credential")
	}

	// No credential at all must pass through as anonymous.
	nextCalled = false
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK || !nextCalled {
		t.Fatalf("no credential: got status %d, nextCalled=%v, want 200/true", rec2.Code, nextCalled)
	}
}

// Add a test key to the mock store
func (m *mockStore) addTestKey(rawKey string, apiKey *store.APIKey) {
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])
	apiKey.KeyHash = keyHash
	m.apiKeys[keyHash] = apiKey
}

// Stub implementations for unused methods
func (m *mockStore) CreateWorkspace(ctx context.Context, input store.CreateWorkspaceInput) (*store.Workspace, error) {
	return nil, nil
}
func (m *mockStore) GetWorkspace(ctx context.Context, id string) (*store.Workspace, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) GetWorkspaceByName(ctx context.Context, name string) (*store.Workspace, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListWorkspaces(ctx context.Context) ([]*store.Workspace, error) { return nil, nil }
func (m *mockStore) UpdateWorkspace(ctx context.Context, id string, input store.UpdateWorkspaceInput) (*store.Workspace, error) {
	return nil, nil
}
func (m *mockStore) DeleteWorkspace(ctx context.Context, id string) error { return nil }
func (m *mockStore) CreateService(ctx context.Context, input store.CreateServiceInput) (*store.Service, error) {
	return nil, nil
}
func (m *mockStore) GetService(ctx context.Context, id string) (*store.Service, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListServices(ctx context.Context, workspaceID string) ([]*store.Service, error) {
	return nil, nil
}
func (m *mockStore) UpdateService(ctx context.Context, id string, input store.UpdateServiceInput) (*store.Service, error) {
	return nil, nil
}
func (m *mockStore) DeleteService(ctx context.Context, id string) error { return nil }
func (m *mockStore) CreateLayer(ctx context.Context, input store.CreateLayerInput) (*store.Layer, error) {
	return nil, nil
}
func (m *mockStore) GetLayer(ctx context.Context, id string) (*store.Layer, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) GetLayerByPublicID(ctx context.Context, serviceID, publicID string) (*store.Layer, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListLayers(ctx context.Context, serviceID string) ([]*store.Layer, error) {
	return nil, nil
}
func (m *mockStore) UpdateLayer(ctx context.Context, id string, input store.UpdateLayerInput) (*store.Layer, error) {
	return nil, nil
}
func (m *mockStore) DeleteLayer(ctx context.Context, id string) error { return nil }
func (m *mockStore) CreateStyle(ctx context.Context, input store.CreateStyleInput) (*store.Style, error) {
	return nil, nil
}
func (m *mockStore) GetStyle(ctx context.Context, id string) (*store.Style, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) GetStyleByName(ctx context.Context, workspaceID, name string) (*store.Style, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListStyles(ctx context.Context, workspaceID string) ([]*store.Style, error) {
	return nil, nil
}
func (m *mockStore) UpdateStyle(ctx context.Context, id string, input store.UpdateStyleInput) (*store.Style, error) {
	return nil, nil
}
func (m *mockStore) DeleteStyle(ctx context.Context, id string) error { return nil }
func (m *mockStore) CreateWFSStoredQuery(ctx context.Context, input store.CreateWFSStoredQueryInput) (*store.WFSStoredQuery, error) {
	return nil, nil
}
func (m *mockStore) GetWFSStoredQuery(ctx context.Context, workspaceID, queryID string) (*store.WFSStoredQuery, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListWFSStoredQueries(ctx context.Context, workspaceID string) ([]*store.WFSStoredQuery, error) {
	return nil, nil
}
func (m *mockStore) DeleteWFSStoredQuery(ctx context.Context, workspaceID, queryID string) error {
	return nil
}
func (m *mockStore) GetWMSSettings(ctx context.Context, workspaceID string) (*store.WMSSettings, error) {
	return nil, nil
}
func (m *mockStore) UpdateWMSSettings(ctx context.Context, workspaceID string, settings store.WMSSettings) error {
	return nil
}
func (m *mockStore) GetWFSSettings(ctx context.Context, workspaceID string) (*store.WFSSettings, error) {
	return nil, nil
}
func (m *mockStore) UpdateWFSSettings(ctx context.Context, workspaceID string, settings store.WFSSettings) error {
	return nil
}
func (m *mockStore) GetOGCAPISettings(ctx context.Context, workspaceID string) (*store.OGCAPISettings, error) {
	return nil, nil
}
func (m *mockStore) UpdateOGCAPISettings(ctx context.Context, workspaceID string, settings store.OGCAPISettings) error {
	return nil
}
func (m *mockStore) GetOGCTilesAPISettings(ctx context.Context, workspaceID string) (*store.OGCTilesAPISettings, error) {
	return nil, nil
}
func (m *mockStore) UpdateOGCTilesAPISettings(ctx context.Context, workspaceID string, settings store.OGCTilesAPISettings) error {
	return nil
}
func (m *mockStore) CreateAPIKey(ctx context.Context, input store.CreateAPIKeyInput) (*store.CreateAPIKeyOutput, error) {
	return nil, nil
}
func (m *mockStore) ListAPIKeys(ctx context.Context, workspaceID *string) ([]*store.APIKey, error) {
	return nil, nil
}
func (m *mockStore) RevokeAPIKey(ctx context.Context, id string) error { return nil }
func (m *mockStore) DeleteAPIKey(ctx context.Context, id string) error { return nil }
func (m *mockStore) CreateRole(ctx context.Context, input store.CreateRoleInput) (*store.Role, error) {
	return nil, nil
}
func (m *mockStore) GetRole(ctx context.Context, id string) (*store.Role, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListRoles(ctx context.Context) ([]*store.Role, error) { return nil, nil }
func (m *mockStore) DeleteRole(ctx context.Context, id string) error      { return nil }
func (m *mockStore) CreateClaimMapping(ctx context.Context, input store.CreateClaimMappingInput) (*store.ClaimRoleMapping, error) {
	return nil, nil
}
func (m *mockStore) GetClaimMapping(ctx context.Context, id string) (*store.ClaimRoleMapping, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListClaimMappings(ctx context.Context, workspaceID string) ([]*store.ClaimRoleMapping, error) {
	return nil, nil
}
func (m *mockStore) DeleteClaimMapping(ctx context.Context, id string) error { return nil }
func (m *mockStore) ResolveClaimsToRoles(ctx context.Context, claims map[string][]string) (map[string]string, error) {
	return nil, nil
}
func (m *mockStore) GetActiveSigningKey(ctx context.Context) (*store.SigningKey, error) {
	return nil, nil
}
func (m *mockStore) RotateSigningKey(ctx context.Context) (*store.SigningKey, error) { return nil, nil }
func (m *mockStore) LoadCasbinPolicies(ctx context.Context) ([]*store.CasbinRule, error) {
	return nil, nil
}
func (m *mockStore) SaveCasbinPolicy(ctx context.Context, rule *store.CasbinRule) error   { return nil }
func (m *mockStore) RemoveCasbinPolicy(ctx context.Context, rule *store.CasbinRule) error { return nil }
func (m *mockStore) Close() error                                                         { return nil }

// ============================================================================
// APIKeyValidator Tests
// ============================================================================

func TestAPIKeyValidator_ValidateRequest_NoKey(t *testing.T) {
	ms := newMockStore()
	v := NewAPIKeyValidator(ms)

	r := httptest.NewRequest("GET", "/", nil)
	id, err := v.ValidateRequest(context.Background(), r)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if id != nil {
		t.Error("expected nil identity when no key provided")
	}
}

func TestAPIKeyValidator_ValidateRequest_XAPIKeyHeader(t *testing.T) {
	ms := newMockStore()
	wsID := "ws-1"
	ms.addTestKey("nsk_testkey123", &store.APIKey{
		ID:          "key-1",
		OwnerName:   "Test User",
		OwnerEmail:  "test@example.com",
		WorkspaceID: &wsID,
		RoleID:      "editor",
		CreatedAt:   time.Now(),
	})

	v := NewAPIKeyValidator(ms)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-API-Key", "nsk_testkey123")

	id, err := v.ValidateRequest(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id == nil {
		t.Fatal("expected identity")
	}
	if id.Subject != "Test User" {
		t.Errorf("expected subject 'Test User', got %s", id.Subject)
	}
	if id.Roles["ws-1"] != "editor" {
		t.Errorf("expected role 'editor' for ws-1, got %s", id.Roles["ws-1"])
	}
}

func TestAPIKeyValidator_ValidateRequest_BearerHeader(t *testing.T) {
	ms := newMockStore()
	ms.addTestKey("nsk_bearer123", &store.APIKey{
		ID:        "key-2",
		OwnerName: "Bearer User",
		RoleID:    "super_admin",
		CreatedAt: time.Now(),
	})

	v := NewAPIKeyValidator(ms)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer nsk_bearer123")

	id, err := v.ValidateRequest(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id == nil {
		t.Fatal("expected identity")
	}
	// Global key (no workspace) should have role in "*"
	if id.Roles["*"] != "super_admin" {
		t.Errorf("expected global super_admin role, got %v", id.Roles)
	}
}

func TestAPIKeyValidator_ValidateRequest_QueryParam(t *testing.T) {
	ms := newMockStore()
	wsID := "ws-2"
	ms.addTestKey("nsk_queryparam", &store.APIKey{
		ID:          "key-3",
		OwnerName:   "Query User",
		WorkspaceID: &wsID,
		RoleID:      "viewer",
		CreatedAt:   time.Now(),
	})

	v := NewAPIKeyValidator(ms)
	v.allowQueryParam = true // query-param auth is opt-in

	r := httptest.NewRequest("GET", "/?apikey=nsk_queryparam", nil)

	id, err := v.ValidateRequest(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id == nil {
		t.Fatal("expected identity")
	}
	if id.Roles["ws-2"] != "viewer" {
		t.Errorf("expected role 'viewer' for ws-2, got %s", id.Roles["ws-2"])
	}
}

func TestAPIKeyValidator_ValidateRequest_InvalidFormat(t *testing.T) {
	ms := newMockStore()
	v := NewAPIKeyValidator(ms)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-API-Key", "invalid_key_format")

	id, err := v.ValidateRequest(context.Background(), r)

	if err == nil {
		t.Error("expected error for invalid key format")
	}
	if id != nil {
		t.Error("expected nil identity for invalid key")
	}
}

func TestAPIKeyValidator_ValidateRequest_InvalidKey(t *testing.T) {
	ms := newMockStore()
	v := NewAPIKeyValidator(ms)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-API-Key", "nsk_nonexistent")

	id, err := v.ValidateRequest(context.Background(), r)

	if err == nil {
		t.Error("expected error for invalid key")
	}
	if id != nil {
		t.Error("expected nil identity for invalid key")
	}
}

// ============================================================================
// Middleware Tests
// ============================================================================

func TestRequireIdentity_WithIdentity(t *testing.T) {
	handler := RequireIdentity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	ctx := WithIdentity(r.Context(), &Identity{Subject: "test"})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRequireIdentity_WithoutIdentity(t *testing.T) {
	handler := RequireIdentity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestRequireRole_HasRole(t *testing.T) {
	middleware := RequireRole("ws-1", "editor")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	id := &Identity{Roles: map[string]string{"ws-1": "editor"}}
	r = r.WithContext(WithIdentity(r.Context(), id))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRequireRole_NoIdentity(t *testing.T) {
	middleware := RequireRole("ws-1", "editor")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestRequireRole_InsufficientRole(t *testing.T) {
	middleware := RequireRole("ws-1", "admin")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	id := &Identity{Roles: map[string]string{"ws-1": "viewer"}}
	r = r.WithContext(WithIdentity(r.Context(), id))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestRequireSuperAdmin_IsSuperAdmin(t *testing.T) {
	handler := RequireSuperAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	id := &Identity{Roles: map[string]string{"*": "super_admin"}}
	r = r.WithContext(WithIdentity(r.Context(), id))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRequireSuperAdmin_NotSuperAdmin(t *testing.T) {
	handler := RequireSuperAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	id := &Identity{Roles: map[string]string{"ws-1": "admin"}}
	r = r.WithContext(WithIdentity(r.Context(), id))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestRequireWorkspaceAccess_SuperAdmin(t *testing.T) {
	middleware := RequireWorkspaceAccess("workspace")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/workspaces/ws-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspace", "ws-1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	id := &Identity{Roles: map[string]string{"*": "super_admin"}}
	r = r.WithContext(WithIdentity(r.Context(), id))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRequireWorkspaceAccess_HasAccess(t *testing.T) {
	middleware := RequireWorkspaceAccess("workspace")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/workspaces/ws-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspace", "ws-1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	id := &Identity{Roles: map[string]string{"ws-1": "viewer"}}
	r = r.WithContext(WithIdentity(r.Context(), id))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRequireWorkspaceAccess_NoAccess(t *testing.T) {
	middleware := RequireWorkspaceAccess("workspace")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/workspaces/ws-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspace", "ws-1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	id := &Identity{Roles: map[string]string{"ws-2": "viewer"}}
	r = r.WithContext(WithIdentity(r.Context(), id))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestRequireWorkspaceAccess_NoWorkspaceParam(t *testing.T) {
	middleware := RequireWorkspaceAccess("workspace")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/workspaces/", nil)
	rctx := chi.NewRouteContext()
	// No workspace param added
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	id := &Identity{Roles: map[string]string{"ws-1": "viewer"}}
	r = r.WithContext(WithIdentity(r.Context(), id))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// ============================================================================
// JWT Tests
// ============================================================================

func TestParseJWT(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		// Create a simple test token (header.payload.signature)
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"neoserver","sub":"test","iat":1234567890,"exp":1234567890}`))
		signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature-32-bytes-longggg"))

		token := header + "." + payload + "." + signature
		result, err := parseJWT(token)
		if err != nil {
			t.Fatalf("parseJWT() error = %v", err)
		}

		if result.Header.Alg != "ES256" {
			t.Errorf("Header.Alg = %s, want ES256", result.Header.Alg)
		}
		if result.Payload.Iss != "neoserver" {
			t.Errorf("Payload.Iss = %s, want neoserver", result.Payload.Iss)
		}
		if result.Payload.Sub != "test" {
			t.Errorf("Payload.Sub = %s, want test", result.Payload.Sub)
		}
	})

	t.Run("invalid format - wrong number of parts", func(t *testing.T) {
		_, err := parseJWT("only.two")
		if err == nil {
			t.Error("expected error for invalid format")
		}
	})

	t.Run("invalid header base64", func(t *testing.T) {
		_, err := parseJWT("!!!invalid-base64!!!.payload.signature")
		if err == nil {
			t.Error("expected error for invalid header base64")
		}
	})

	t.Run("invalid payload base64", func(t *testing.T) {
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256"}`))
		_, err := parseJWT(header + ".!!!invalid-base64!!!.signature")
		if err == nil {
			t.Error("expected error for invalid payload base64")
		}
	})

	t.Run("invalid header JSON", func(t *testing.T) {
		header := base64.RawURLEncoding.EncodeToString([]byte(`not-json`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"test"}`))
		_, err := parseJWT(header + "." + payload + ".sig")
		if err == nil {
			t.Error("expected error for invalid header JSON")
		}
	})

	t.Run("invalid payload JSON", func(t *testing.T) {
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(`not-json`))
		_, err := parseJWT(header + "." + payload + ".sig")
		if err == nil {
			t.Error("expected error for invalid payload JSON")
		}
	})
}

func TestJWTValidator_ValidateRequest(t *testing.T) {
	ms := newMockStore()
	v := NewJWTValidator(ms)

	t.Run("no authorization header", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		id, err := v.ValidateRequest(context.Background(), r)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if id != nil {
			t.Error("expected nil identity")
		}
	})

	t.Run("non-bearer authorization", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		id, err := v.ValidateRequest(context.Background(), r)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if id != nil {
			t.Error("expected nil identity")
		}
	})

	t.Run("empty bearer token", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer ")
		id, err := v.ValidateRequest(context.Background(), r)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if id != nil {
			t.Error("expected nil identity")
		}
	})

	t.Run("API key token (nsk_ prefix)", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer nsk_testkey")
		id, err := v.ValidateRequest(context.Background(), r)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if id != nil {
			t.Error("expected nil identity for API key")
		}
	})

	t.Run("invalid token format", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer invalid-token")
		id, err := v.ValidateRequest(context.Background(), r)
		if err == nil {
			t.Error("expected error for invalid token")
		}
		if id != nil {
			t.Error("expected nil identity")
		}
	})
}

// ============================================================================
// OIDC Helper Function Tests
// ============================================================================

func TestDecodeJWTPayload(t *testing.T) {
	t.Run("valid payload", func(t *testing.T) {
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"test","email":"test@example.com"}`))
		result, err := decodeJWTPayload(payload)
		if err != nil {
			t.Fatalf("decodeJWTPayload() error = %v", err)
		}
		if result["sub"] != "test" {
			t.Errorf("sub = %v, want test", result["sub"])
		}
		if result["email"] != "test@example.com" {
			t.Errorf("email = %v, want test@example.com", result["email"])
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		_, err := decodeJWTPayload("!!!invalid!!!")
		if err == nil {
			t.Error("expected error for invalid base64")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		payload := base64.RawURLEncoding.EncodeToString([]byte(`not-json`))
		_, err := decodeJWTPayload(payload)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestExtractStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
	}{
		{
			name:     "nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "string",
			input:    "single",
			expected: []string{"single"},
		},
		{
			name:     "string slice",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "interface slice",
			input:    []interface{}{"x", "y", "z"},
			expected: []string{"x", "y", "z"},
		},
		{
			name:     "interface slice with non-strings",
			input:    []interface{}{"a", 123, "b"},
			expected: []string{"a", "b"},
		},
		{
			name:     "unsupported type",
			input:    123,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractStringSlice(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if len(result) != len(tt.expected) {
				t.Errorf("length = %d, want %d", len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("result[%d] = %s, want %s", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestExtractClaimValues(t *testing.T) {
	claims := map[string]interface{}{
		"groups": []interface{}{"group1", "group2"},
		"roles":  "single_role",
		"nested": map[string]interface{}{
			"values": []interface{}{"nested1", "nested2"},
		},
	}

	t.Run("simple path", func(t *testing.T) {
		result := extractClaimValues(claims, "groups")
		if len(result) != 2 {
			t.Errorf("expected 2 values, got %d", len(result))
		}
	})

	t.Run("string value", func(t *testing.T) {
		result := extractClaimValues(claims, "roles")
		if len(result) != 1 || result[0] != "single_role" {
			t.Errorf("expected [single_role], got %v", result)
		}
	})

	t.Run("nested path", func(t *testing.T) {
		result := extractClaimValues(claims, "nested.values")
		if len(result) != 2 {
			t.Errorf("expected 2 values, got %d", len(result))
		}
	})

	t.Run("non-existent path", func(t *testing.T) {
		result := extractClaimValues(claims, "does.not.exist")
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("non-object nested path", func(t *testing.T) {
		result := extractClaimValues(claims, "groups.values")
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})
}

func TestExtractMappableClaims(t *testing.T) {
	claims := map[string]interface{}{
		"groups":       []interface{}{"admin", "users"},
		"roles":        "manager",
		"realm_access": map[string]interface{}{"roles": []interface{}{"realm_role"}},
	}

	result := extractMappableClaims(claims)

	if len(result["groups"]) != 2 {
		t.Errorf("groups = %v, want 2 values", result["groups"])
	}
	if len(result["roles"]) != 1 || result["roles"][0] != "manager" {
		t.Errorf("roles = %v, want [manager]", result["roles"])
	}
	if len(result["realm_access.roles"]) != 1 {
		t.Errorf("realm_access.roles = %v, want 1 value", result["realm_access.roles"])
	}
}

// ============================================================================
// Middleware Tests
// ============================================================================

func TestMiddleware(t *testing.T) {
	ms := newMockStore()

	t.Run("no auth required - no identity", func(t *testing.T) {
		middleware := Middleware(MiddlewareConfig{
			Store:       ms,
			RequireAuth: false,
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok := FromContext(r.Context())
			if ok {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNoContent) // No identity is fine
			}
		}))

		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", w.Code)
		}
	})

	t.Run("auth required - no identity", func(t *testing.T) {
		middleware := Middleware(MiddlewareConfig{
			Store:       ms,
			RequireAuth: true,
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})

	t.Run("auth required - valid API key", func(t *testing.T) {
		wsID := "ws-1"
		ms.addTestKey("nsk_validkey", &store.APIKey{
			ID:          "key-1",
			OwnerName:   "Test User",
			WorkspaceID: &wsID,
			RoleID:      "editor",
			CreatedAt:   time.Now(),
		})

		middleware := Middleware(MiddlewareConfig{
			Store:       ms,
			RequireAuth: true,
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := FromContext(r.Context())
			if !ok {
				t.Error("expected identity in context")
			}
			if id.Subject != "Test User" {
				t.Errorf("expected subject 'Test User', got %s", id.Subject)
			}
			w.WriteHeader(http.StatusOK)
		}))

		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-API-Key", "nsk_validkey")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("auth required - invalid API key", func(t *testing.T) {
		middleware := Middleware(MiddlewareConfig{
			Store:       ms,
			RequireAuth: true,
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-API-Key", "nsk_invalidkey")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})
}

func TestAPIKeyValidator_ApiKeyHeader(t *testing.T) {
	ms := newMockStore()
	ms.addTestKey("nsk_apikeystyle", &store.APIKey{
		ID:        "key-1",
		OwnerName: "API Key User",
		RoleID:    "admin",
		CreatedAt: time.Now(),
	})

	v := NewAPIKeyValidator(ms)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "ApiKey nsk_apikeystyle")

	id, err := v.ValidateRequest(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == nil {
		t.Fatal("expected identity")
	}
	if id.Subject != "API Key User" {
		t.Errorf("expected subject 'API Key User', got %s", id.Subject)
	}
}

func TestRequireSuperAdmin_NoIdentity(t *testing.T) {
	handler := RequireSuperAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestRequireWorkspaceAccess_NoIdentity(t *testing.T) {
	middleware := RequireWorkspaceAccess("workspace")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/workspaces/ws-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspace", "ws-1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}
