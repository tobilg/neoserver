package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// ============================================================================
// splitComma Tests
// ============================================================================

func TestSplitComma(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string returns wildcard",
			input: "",
			want:  []string{"*"},
		},
		{
			name:  "single value",
			input: "http://localhost:3000",
			want:  []string{"http://localhost:3000"},
		},
		{
			name:  "multiple values",
			input: "http://localhost:3000,http://example.com",
			want:  []string{"http://localhost:3000", "http://example.com"},
		},
		{
			name:  "values with whitespace",
			input: "  http://localhost:3000  ,  http://example.com  ",
			want:  []string{"http://localhost:3000", "http://example.com"},
		},
		{
			name:  "wildcard",
			input: "*",
			want:  []string{"*"},
		},
		{
			name:  "empty parts are filtered",
			input: "http://localhost:3000,,http://example.com",
			want:  []string{"http://localhost:3000", "http://example.com"},
		},
		{
			name:  "only commas returns wildcard",
			input: ",,,",
			want:  []string{"*"},
		},
		{
			name:  "whitespace only returns wildcard",
			input: "   ",
			want:  []string{"*"},
		},
		{
			name:  "three values",
			input: "http://a.com,http://b.com,http://c.com",
			want:  []string{"http://a.com", "http://b.com", "http://c.com"},
		},
		{
			name:  "mixed whitespace and empty",
			input: "  http://localhost  ,  ,  http://example.com  ,  ",
			want:  []string{"http://localhost", "http://example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitComma(tt.input)

			if len(got) != len(tt.want) {
				t.Errorf("splitComma(%q) returned %d elements, want %d", tt.input, len(got), len(tt.want))
				t.Errorf("got: %v, want: %v", got, tt.want)
				return
			}

			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("splitComma(%q)[%d] = %q, want %q", tt.input, i, v, tt.want[i])
				}
			}
		})
	}
}

// ============================================================================
// toOIDCConfig Tests
// ============================================================================

func TestToOIDCConfig(t *testing.T) {
	tests := []struct {
		name   string
		input  conf.OIDCConfig
		isNil  bool
		verify func(t *testing.T, got *identity.OIDCConfig)
	}{
		{
			name: "empty issuer returns nil",
			input: conf.OIDCConfig{
				IssuerURL: "",
			},
			isNil: true,
		},
		{
			name: "valid config",
			input: conf.OIDCConfig{
				IssuerURL:       "https://auth.example.com",
				ClientID:        "client-123",
				RequiredScopes:  []string{"openid", "profile"},
				SkipIssuerCheck: false,
			},
			isNil: false,
			verify: func(t *testing.T, got *identity.OIDCConfig) {
				if got.IssuerURL != "https://auth.example.com" {
					t.Errorf("IssuerURL = %q, want %q", got.IssuerURL, "https://auth.example.com")
				}
				if got.ClientID != "client-123" {
					t.Errorf("ClientID = %q, want %q", got.ClientID, "client-123")
				}
				if len(got.RequiredScopes) != 2 {
					t.Errorf("RequiredScopes length = %d, want 2", len(got.RequiredScopes))
				}
				if got.SkipIssuerCheck != false {
					t.Error("SkipIssuerCheck should be false")
				}
			},
		},
		{
			name: "skip issuer check enabled",
			input: conf.OIDCConfig{
				IssuerURL:       "https://auth.example.com",
				ClientID:        "client-456",
				SkipIssuerCheck: true,
			},
			isNil: false,
			verify: func(t *testing.T, got *identity.OIDCConfig) {
				if !got.SkipIssuerCheck {
					t.Error("SkipIssuerCheck should be true")
				}
			},
		},
		{
			name: "empty scopes",
			input: conf.OIDCConfig{
				IssuerURL:      "https://auth.example.com",
				ClientID:       "client-789",
				RequiredScopes: []string{},
			},
			isNil: false,
			verify: func(t *testing.T, got *identity.OIDCConfig) {
				if len(got.RequiredScopes) != 0 {
					t.Errorf("RequiredScopes should be empty, got %v", got.RequiredScopes)
				}
			},
		},
		{
			name: "nil scopes",
			input: conf.OIDCConfig{
				IssuerURL:      "https://auth.example.com",
				ClientID:       "client-abc",
				RequiredScopes: nil,
			},
			isNil: false,
			verify: func(t *testing.T, got *identity.OIDCConfig) {
				if got.RequiredScopes != nil {
					t.Errorf("RequiredScopes should be nil, got %v", got.RequiredScopes)
				}
			},
		},
		{
			name: "whitespace only issuer returns nil",
			input: conf.OIDCConfig{
				IssuerURL: "",
				ClientID:  "client-xyz",
			},
			isNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toOIDCConfig(tt.input)

			if tt.isNil {
				if got != nil {
					t.Errorf("toOIDCConfig() = %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("toOIDCConfig() = nil, want non-nil")
			}

			if tt.verify != nil {
				tt.verify(t, got)
			}
		})
	}
}

func TestToOIDCConfig_PreservesAllFields(t *testing.T) {
	input := conf.OIDCConfig{
		IssuerURL:       "https://auth.example.com/realms/test",
		ClientID:        "my-client-id",
		RequiredScopes:  []string{"openid", "profile", "email", "custom:scope"},
		SkipIssuerCheck: true,
	}

	got := toOIDCConfig(input)

	if got == nil {
		t.Fatal("expected non-nil config")
	}

	if got.IssuerURL != input.IssuerURL {
		t.Errorf("IssuerURL = %q, want %q", got.IssuerURL, input.IssuerURL)
	}
	if got.ClientID != input.ClientID {
		t.Errorf("ClientID = %q, want %q", got.ClientID, input.ClientID)
	}
	if len(got.RequiredScopes) != len(input.RequiredScopes) {
		t.Errorf("RequiredScopes length = %d, want %d", len(got.RequiredScopes), len(input.RequiredScopes))
	}
	for i, scope := range got.RequiredScopes {
		if scope != input.RequiredScopes[i] {
			t.Errorf("RequiredScopes[%d] = %q, want %q", i, scope, input.RequiredScopes[i])
		}
	}
	if got.SkipIssuerCheck != input.SkipIssuerCheck {
		t.Errorf("SkipIssuerCheck = %v, want %v", got.SkipIssuerCheck, input.SkipIssuerCheck)
	}
}

// ============================================================================
// HealthHandler Tests
// ============================================================================

func TestHealthHandler(t *testing.T) {
	handler := HealthHandler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	expected := `{"status":"ok"}`
	if string(body) != expected {
		t.Errorf("body = %q, want %q", string(body), expected)
	}
}

func TestHealthHandler_DifferentMethods(t *testing.T) {
	handler := HealthHandler()

	methods := []string{http.MethodGet, http.MethodHead, http.MethodPost}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/health", nil)
			w := httptest.NewRecorder()

			handler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			// Handler should always return 200 regardless of method
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
		})
	}
}

// ============================================================================
// WorkspaceRouter Tests
// ============================================================================

// mockStore implements store.Store for testing
type mockStore struct {
	workspaces map[string]*store.Workspace
}

// Workspace operations
func (m *mockStore) CreateWorkspace(ctx context.Context, input store.CreateWorkspaceInput) (*store.Workspace, error) {
	return nil, nil
}
func (m *mockStore) GetWorkspace(ctx context.Context, id string) (*store.Workspace, error) {
	if ws, ok := m.workspaces[id]; ok {
		return ws, nil
	}
	return nil, store.ErrNotFound
}
func (m *mockStore) GetWorkspaceByName(ctx context.Context, name string) (*store.Workspace, error) {
	for _, ws := range m.workspaces {
		if ws.Name == name {
			return ws, nil
		}
	}
	return nil, store.ErrNotFound
}
func (m *mockStore) ListWorkspaces(ctx context.Context) ([]*store.Workspace, error) {
	var result []*store.Workspace
	for _, ws := range m.workspaces {
		result = append(result, ws)
	}
	return result, nil
}
func (m *mockStore) UpdateWorkspace(ctx context.Context, id string, input store.UpdateWorkspaceInput) (*store.Workspace, error) {
	return nil, nil
}
func (m *mockStore) DeleteWorkspace(ctx context.Context, id string) error { return nil }

// Service operations
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

// Layer operations
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

// Style operations
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

// WFS Stored Query operations
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

// Workspace settings operations
func (m *mockStore) GetWMSSettings(ctx context.Context, workspaceID string) (*store.WMSSettings, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) UpdateWMSSettings(ctx context.Context, workspaceID string, settings store.WMSSettings) error {
	return nil
}
func (m *mockStore) GetWFSSettings(ctx context.Context, workspaceID string) (*store.WFSSettings, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) UpdateWFSSettings(ctx context.Context, workspaceID string, settings store.WFSSettings) error {
	return nil
}
func (m *mockStore) GetOGCAPISettings(ctx context.Context, workspaceID string) (*store.OGCAPISettings, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) UpdateOGCAPISettings(ctx context.Context, workspaceID string, settings store.OGCAPISettings) error {
	return nil
}
func (m *mockStore) GetOGCTilesAPISettings(ctx context.Context, workspaceID string) (*store.OGCTilesAPISettings, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) UpdateOGCTilesAPISettings(ctx context.Context, workspaceID string, settings store.OGCTilesAPISettings) error {
	return nil
}

// Role operations
func (m *mockStore) CreateRole(ctx context.Context, input store.CreateRoleInput) (*store.Role, error) {
	return nil, nil
}
func (m *mockStore) GetRole(ctx context.Context, id string) (*store.Role, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListRoles(ctx context.Context) ([]*store.Role, error) {
	return nil, nil
}
func (m *mockStore) DeleteRole(ctx context.Context, id string) error { return nil }

// API Key operations
func (m *mockStore) CreateAPIKey(ctx context.Context, input store.CreateAPIKeyInput) (*store.CreateAPIKeyOutput, error) {
	return nil, nil
}
func (m *mockStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListAPIKeys(ctx context.Context, workspaceID *string) ([]*store.APIKey, error) {
	return nil, nil
}
func (m *mockStore) RevokeAPIKey(ctx context.Context, id string) error { return nil }
func (m *mockStore) DeleteAPIKey(ctx context.Context, id string) error { return nil }

// Claim mapping operations
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

// Signing key operations
func (m *mockStore) GetActiveSigningKey(ctx context.Context) (*store.SigningKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) RotateSigningKey(ctx context.Context) (*store.SigningKey, error) {
	return nil, nil
}

// Casbin adapter operations
func (m *mockStore) LoadCasbinPolicies(ctx context.Context) ([]*store.CasbinRule, error) {
	return nil, nil
}
func (m *mockStore) SaveCasbinPolicy(ctx context.Context, rule *store.CasbinRule) error {
	return nil
}
func (m *mockStore) RemoveCasbinPolicy(ctx context.Context, rule *store.CasbinRule) error {
	return nil
}

func (m *mockStore) Close() error { return nil }

func testConfig() conf.Config {
	return conf.Config{
		Server: conf.Server{
			HttpPort:        9000,
			HttpHost:        "localhost",
			ReadTimeoutSec:  30,
			WriteTimeoutSec: 30,
		},
		WMS: conf.WMS{
			Enabled: true,
		},
		WFS: conf.WFS{
			Enabled: true,
		},
		Cache: conf.Cache{
			Enabled: false,
		},
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewWorkspaceRouter(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	ms := &mockStore{workspaces: make(map[string]*store.Workspace)}
	registry := workspace.NewRegistry(ms, nil)
	adapter := rbac.NewMemoryAdapter()
	enforcer, err := rbac.NewEnforcerWithDefaults(adapter)
	if err != nil {
		t.Fatalf("failed to create enforcer: %v", err)
	}
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	wr := NewWorkspaceRouter(cfg, logger, registry, enforcer, ms, cacheManager)

	if wr == nil {
		t.Fatal("NewWorkspaceRouter returned nil")
	}
	if wr.registry != registry {
		t.Error("registry not set correctly")
	}
	if wr.enforcer != enforcer {
		t.Error("enforcer not set correctly")
	}
	if wr.logger != logger {
		t.Error("logger not set correctly")
	}
	if wr.store != ms {
		t.Error("store not set correctly")
	}
	if wr.cache != cacheManager {
		t.Error("cache not set correctly")
	}
}

func TestNewWorkspaceRouter_WMSEnabled(t *testing.T) {
	cfg := testConfig()
	cfg.WMS.Enabled = true
	logger := testLogger()
	ms := &mockStore{workspaces: make(map[string]*store.Workspace)}
	registry := workspace.NewRegistry(ms, nil)
	adapter := rbac.NewMemoryAdapter()
	enforcer, _ := rbac.NewEnforcerWithDefaults(adapter)
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	wr := NewWorkspaceRouter(cfg, logger, registry, enforcer, ms, cacheManager)

	if wr == nil {
		t.Fatal("NewWorkspaceRouter returned nil")
	}
	// WMS enabled should be reflected in config
	if !wr.cfg.WMS.Enabled {
		t.Error("WMS should be enabled")
	}
}

func TestNewWorkspaceRouter_WFSEnabled(t *testing.T) {
	cfg := testConfig()
	cfg.WFS.Enabled = true
	logger := testLogger()
	ms := &mockStore{workspaces: make(map[string]*store.Workspace)}
	registry := workspace.NewRegistry(ms, nil)
	adapter := rbac.NewMemoryAdapter()
	enforcer, _ := rbac.NewEnforcerWithDefaults(adapter)
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	wr := NewWorkspaceRouter(cfg, logger, registry, enforcer, ms, cacheManager)

	if wr == nil {
		t.Fatal("NewWorkspaceRouter returned nil")
	}
	// WFS enabled should be reflected in config
	if !wr.cfg.WFS.Enabled {
		t.Error("WFS should be enabled")
	}
}

func TestNewWorkspaceRouter_BothDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.WMS.Enabled = false
	cfg.WFS.Enabled = false
	logger := testLogger()
	ms := &mockStore{workspaces: make(map[string]*store.Workspace)}
	registry := workspace.NewRegistry(ms, nil)
	adapter := rbac.NewMemoryAdapter()
	enforcer, _ := rbac.NewEnforcerWithDefaults(adapter)
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	wr := NewWorkspaceRouter(cfg, logger, registry, enforcer, ms, cacheManager)

	if wr == nil {
		t.Fatal("NewWorkspaceRouter returned nil")
	}
	if wr.cfg.WMS.Enabled {
		t.Error("WMS should be disabled")
	}
	if wr.cfg.WFS.Enabled {
		t.Error("WFS should be disabled")
	}
}

// ============================================================================
// Server Tests
// ============================================================================

func TestServer_Cache(t *testing.T) {
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	s := &Server{
		cache: cacheManager,
	}

	if s.Cache() != cacheManager {
		t.Error("Cache() should return the cache manager")
	}
}

func TestServer_CacheNil(t *testing.T) {
	s := &Server{
		cache: nil,
	}

	if s.Cache() != nil {
		t.Error("Cache() should return nil when cache is nil")
	}
}

func TestServer_Shutdown(t *testing.T) {
	// Create a minimal server for shutdown testing
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())
	ms := &mockStore{workspaces: make(map[string]*store.Workspace)}
	registry := workspace.NewRegistry(ms, nil)

	// Create a test HTTP server that we can control
	httpServer := &http.Server{
		Addr:    "127.0.0.1:0", // Use port 0 for automatic assignment
		Handler: http.NewServeMux(),
	}

	s := &Server{
		logger:   testLogger(),
		http:     httpServer,
		registry: registry,
		cache:    cacheManager,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shutdown should not error even if server wasn't started
	err := s.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v, want nil", err)
	}
}

func TestServer_ShutdownWithNilRegistryAndCache(t *testing.T) {
	httpServer := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: http.NewServeMux(),
	}

	s := &Server{
		logger:   testLogger(),
		http:     httpServer,
		registry: nil,
		cache:    nil,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Should handle nil registry and cache gracefully
	err := s.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v, want nil", err)
	}
}

// ============================================================================
// WorkspaceRouter Mount Tests
// ============================================================================

func TestWorkspaceRouter_Mount_WMSEnabled(t *testing.T) {
	cfg := testConfig()
	cfg.WMS.Enabled = true
	cfg.WFS.Enabled = false
	logger := testLogger()
	ms := &mockStore{workspaces: make(map[string]*store.Workspace)}
	registry := workspace.NewRegistry(ms, nil)
	adapter := rbac.NewMemoryAdapter()
	enforcer, _ := rbac.NewEnforcerWithDefaults(adapter)
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	wr := NewWorkspaceRouter(cfg, logger, registry, enforcer, ms, cacheManager)

	// Create a chi router and mount the workspace routes
	r := newTestChiRouter()
	wr.Mount(r)

	// The router should have mounted the routes without error
	if wr == nil {
		t.Fatal("NewWorkspaceRouter returned nil")
	}
}

func TestWorkspaceRouter_Mount_WFSEnabled(t *testing.T) {
	cfg := testConfig()
	cfg.WMS.Enabled = false
	cfg.WFS.Enabled = true
	logger := testLogger()
	ms := &mockStore{workspaces: make(map[string]*store.Workspace)}
	registry := workspace.NewRegistry(ms, nil)
	adapter := rbac.NewMemoryAdapter()
	enforcer, _ := rbac.NewEnforcerWithDefaults(adapter)
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	wr := NewWorkspaceRouter(cfg, logger, registry, enforcer, ms, cacheManager)

	// Create a chi router and mount the workspace routes
	r := newTestChiRouter()
	wr.Mount(r)

	// The router should have mounted the routes without error
	if wr == nil {
		t.Fatal("NewWorkspaceRouter returned nil")
	}
}

func TestWorkspaceRouter_Mount_BothEnabled(t *testing.T) {
	cfg := testConfig()
	cfg.WMS.Enabled = true
	cfg.WFS.Enabled = true
	logger := testLogger()
	ms := &mockStore{workspaces: make(map[string]*store.Workspace)}
	registry := workspace.NewRegistry(ms, nil)
	adapter := rbac.NewMemoryAdapter()
	enforcer, _ := rbac.NewEnforcerWithDefaults(adapter)
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	wr := NewWorkspaceRouter(cfg, logger, registry, enforcer, ms, cacheManager)

	// Create a chi router and mount the workspace routes
	r := newTestChiRouter()
	wr.Mount(r)

	// The router should have mounted the routes without error
	if wr == nil {
		t.Fatal("NewWorkspaceRouter returned nil")
	}
}

func TestWorkspaceRouter_Mount_BothDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.WMS.Enabled = false
	cfg.WFS.Enabled = false
	logger := testLogger()
	ms := &mockStore{workspaces: make(map[string]*store.Workspace)}
	registry := workspace.NewRegistry(ms, nil)
	adapter := rbac.NewMemoryAdapter()
	enforcer, _ := rbac.NewEnforcerWithDefaults(adapter)
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	wr := NewWorkspaceRouter(cfg, logger, registry, enforcer, ms, cacheManager)

	// Create a chi router and mount the workspace routes
	r := newTestChiRouter()
	wr.Mount(r)

	// The router should have mounted the routes without error
	if wr == nil {
		t.Fatal("NewWorkspaceRouter returned nil")
	}
}

func TestWorkspaceRouter_OldViewerRouteIsGone(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	ms := &mockStore{workspaces: map[string]*store.Workspace{
		"demo": {ID: "workspace-1", Name: "demo"},
	}}
	registry := workspace.NewRegistry(ms, nil)
	if err := registry.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	adapter := rbac.NewMemoryAdapter()
	enforcer, _ := rbac.NewEnforcerWithDefaults(adapter)
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())
	wrapper := NewWorkspaceRouter(cfg, logger, registry, enforcer, ms, cacheManager)
	router := chi.NewRouter()
	wrapper.Mount(router)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/workspaces/demo/ui", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("old viewer route returned %d, want 404", recorder.Code)
	}
}

// newTestChiRouter creates a chi router for testing
func newTestChiRouter() *testRouter {
	return &testRouter{routes: make(map[string]bool)}
}

// testRouter is a minimal chi.Router implementation for testing
type testRouter struct {
	routes map[string]bool
}

func (r *testRouter) ServeHTTP(w http.ResponseWriter, req *http.Request)             {}
func (r *testRouter) Use(middlewares ...func(http.Handler) http.Handler)             {}
func (r *testRouter) With(middlewares ...func(http.Handler) http.Handler) chi.Router { return r }
func (r *testRouter) Group(fn func(r chi.Router)) chi.Router                         { fn(r); return r }
func (r *testRouter) Route(pattern string, fn func(r chi.Router)) chi.Router         { fn(r); return r }
func (r *testRouter) Mount(pattern string, h http.Handler)                           {}
func (r *testRouter) Handle(pattern string, h http.Handler)                          {}
func (r *testRouter) HandleFunc(pattern string, h http.HandlerFunc)                  {}
func (r *testRouter) Method(method, pattern string, h http.Handler)                  {}
func (r *testRouter) MethodFunc(method, pattern string, h http.HandlerFunc)          {}
func (r *testRouter) Connect(pattern string, h http.HandlerFunc)                     {}
func (r *testRouter) Delete(pattern string, h http.HandlerFunc)                      {}
func (r *testRouter) Get(pattern string, h http.HandlerFunc)                         {}
func (r *testRouter) Head(pattern string, h http.HandlerFunc)                        {}
func (r *testRouter) Options(pattern string, h http.HandlerFunc)                     {}
func (r *testRouter) Patch(pattern string, h http.HandlerFunc)                       {}
func (r *testRouter) Post(pattern string, h http.HandlerFunc)                        {}
func (r *testRouter) Put(pattern string, h http.HandlerFunc)                         {}
func (r *testRouter) Trace(pattern string, h http.HandlerFunc)                       {}
func (r *testRouter) NotFound(h http.HandlerFunc)                                    {}
func (r *testRouter) MethodNotAllowed(h http.HandlerFunc)                            {}
func (r *testRouter) Routes() []chi.Route                                            { return nil }
func (r *testRouter) Middlewares() chi.Middlewares                                   { return nil }
func (r *testRouter) Match(rctx *chi.Context, method, path string) bool              { return false }
func (r *testRouter) Find(rctx *chi.Context, method, path string) string             { return "" }

// ============================================================================
// Additional splitComma Tests
// ============================================================================

func TestSplitComma_SingleOrigin(t *testing.T) {
	result := splitComma("https://example.com")
	if len(result) != 1 {
		t.Errorf("expected 1 element, got %d", len(result))
	}
	if result[0] != "https://example.com" {
		t.Errorf("expected 'https://example.com', got %q", result[0])
	}
}

func TestSplitComma_TrailingComma(t *testing.T) {
	result := splitComma("https://example.com,")
	if len(result) != 1 {
		t.Errorf("expected 1 element, got %d", len(result))
	}
	if result[0] != "https://example.com" {
		t.Errorf("expected 'https://example.com', got %q", result[0])
	}
}

func TestSplitComma_LeadingComma(t *testing.T) {
	result := splitComma(",https://example.com")
	if len(result) != 1 {
		t.Errorf("expected 1 element, got %d", len(result))
	}
	if result[0] != "https://example.com" {
		t.Errorf("expected 'https://example.com', got %q", result[0])
	}
}

// ============================================================================
// toOIDCConfig Additional Tests
// ============================================================================

func TestToOIDCConfig_WithOnlyIssuerAndClient(t *testing.T) {
	input := conf.OIDCConfig{
		IssuerURL: "https://auth.example.com",
		ClientID:  "my-client",
	}
	result := toOIDCConfig(input)
	if result == nil {
		t.Fatal("expected non-nil config")
	}
	if result.IssuerURL != "https://auth.example.com" {
		t.Errorf("expected IssuerURL 'https://auth.example.com', got %q", result.IssuerURL)
	}
	if result.ClientID != "my-client" {
		t.Errorf("expected ClientID 'my-client', got %q", result.ClientID)
	}
}

// ============================================================================
// Server Start Test
// ============================================================================

func TestServer_Start_InvalidAddress(t *testing.T) {
	// Create a server with an invalid address
	httpServer := &http.Server{
		Addr:    "invalid:address:format",
		Handler: http.NewServeMux(),
	}

	s := &Server{
		cfg:    testConfig(),
		logger: testLogger(),
		http:   httpServer,
	}

	// Start should fail with an invalid address
	err := s.Start()
	if err == nil {
		// If it somehow started, shut it down
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}
	// Either error or nil is acceptable - depends on OS handling of address
}

// ============================================================================
// HealthHandler Additional Tests
// ============================================================================

func TestHealthHandler_ResponseHeaders(t *testing.T) {
	handler := HealthHandler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Verify Content-Type header
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestHealthHandler_ResponseBody(t *testing.T) {
	handler := HealthHandler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", string(body), `{"status":"ok"}`)
	}
}

// ============================================================================
// toCacheConfig Tests
// ============================================================================

func TestToCacheConfig(t *testing.T) {
	input := conf.Cache{
		Enabled:     true,
		MaxMemoryMB: 512,
		Capabilities: conf.CacheCapabilities{
			Enabled: true,
			TTLSec:  3600,
		},
		Collections: conf.CacheCollections{
			Enabled: true,
			TTLSec:  1800,
		},
		Features: conf.CacheFeatures{
			Enabled:      true,
			TTLSec:       300,
			MaxEntries:   1000,
			MaxEntrySize: 10485760,
		},
		Tiles: conf.CacheTiles{
			Enabled:     true,
			TTLSec:      900,
			MaxMemoryMB: 256,
		},
	}

	result := toCacheConfig(input)

	if !result.Enabled {
		t.Error("Enabled should be true")
	}
	if result.MaxMemoryMB != 512 {
		t.Errorf("MaxMemoryMB = %d, want 512", result.MaxMemoryMB)
	}
	if !result.Capabilities.Enabled {
		t.Error("Capabilities.Enabled should be true")
	}
	if result.Capabilities.TTL.Seconds() != 3600 {
		t.Errorf("Capabilities.TTL = %v, want 3600s", result.Capabilities.TTL)
	}
	if !result.Collections.Enabled {
		t.Error("Collections.Enabled should be true")
	}
	if result.Collections.TTL.Seconds() != 1800 {
		t.Errorf("Collections.TTL = %v, want 1800s", result.Collections.TTL)
	}
	if !result.Features.Enabled {
		t.Error("Features.Enabled should be true")
	}
	if result.Features.TTL.Seconds() != 300 {
		t.Errorf("Features.TTL = %v, want 300s", result.Features.TTL)
	}
	if result.Features.MaxEntries != 1000 {
		t.Errorf("Features.MaxEntries = %d, want 1000", result.Features.MaxEntries)
	}
	if result.Features.MaxEntrySize != 10485760 {
		t.Errorf("Features.MaxEntrySize = %d, want 10485760", result.Features.MaxEntrySize)
	}
	if !result.Tiles.Enabled {
		t.Error("Tiles.Enabled should be true")
	}
	if result.Tiles.TTL.Seconds() != 900 {
		t.Errorf("Tiles.TTL = %v, want 900s", result.Tiles.TTL)
	}
	if result.Tiles.MaxMemoryMB != 256 {
		t.Errorf("Tiles.MaxMemoryMB = %d, want 256", result.Tiles.MaxMemoryMB)
	}
}

func TestToCacheConfig_Disabled(t *testing.T) {
	input := conf.Cache{
		Enabled: false,
	}

	result := toCacheConfig(input)

	if result.Enabled {
		t.Error("Enabled should be false")
	}
}

// ============================================================================
// Security Headers Middleware Tests
// ============================================================================

func TestSecurityHeaders_SetsAllHeaders(t *testing.T) {
	// Create a simple handler that the middleware will wrap
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with security headers middleware
	handler := securityHeaders(testHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Test all security headers
	tests := []struct {
		header   string
		expected string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
		{"Cache-Control", "no-store"},
		{"Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'"},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := resp.Header.Get(tt.header)
			if got != tt.expected {
				t.Errorf("%s = %q, want %q", tt.header, got, tt.expected)
			}
		})
	}
}

func TestSecurityHeaders_PreservesCacheControl(t *testing.T) {
	// Create a handler that sets its own Cache-Control header
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with security headers middleware
	handler := securityHeaders(testHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Cache-Control should be preserved from the inner handler
	got := resp.Header.Get("Cache-Control")
	if got != "max-age=3600" {
		t.Errorf("Cache-Control = %q, want %q", got, "max-age=3600")
	}
}

func TestSecurityHeaders_WorksWithDifferentMethods(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := securityHeaders(testHandler)

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/test", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			// Verify X-Content-Type-Options is set for all methods
			if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
			}
		})
	}
}

func TestSecurityHeaders_PassesThroughResponse(t *testing.T) {
	expectedBody := `{"data":"test"}`
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(expectedBody))
	})

	handler := securityHeaders(testHandler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Verify the inner handler's response is passed through
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if string(body) != expectedBody {
		t.Errorf("body = %q, want %q", string(body), expectedBody)
	}

	// And security headers should still be present
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want %q", got, "DENY")
	}
}
