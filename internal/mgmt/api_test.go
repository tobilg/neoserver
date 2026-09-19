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
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// mockStore is a minimal mock implementation of store.Store for testing
type mockStore struct {
	workspaces    map[string]*store.Workspace
	services      map[string]*store.Service
	layers        map[string]*store.Layer
	apiKeys       map[string]*store.APIKey
	roles         map[string]*store.Role
	claimMappings map[string]*store.ClaimRoleMapping
	wmtsSettings  map[string]store.WMTSSettings
}

func newMockStore() *mockStore {
	return &mockStore{
		workspaces:    make(map[string]*store.Workspace),
		services:      make(map[string]*store.Service),
		layers:        make(map[string]*store.Layer),
		apiKeys:       make(map[string]*store.APIKey),
		roles:         make(map[string]*store.Role),
		claimMappings: make(map[string]*store.ClaimRoleMapping),
		wmtsSettings:  make(map[string]store.WMTSSettings),
	}
}

// Workspace operations
func (m *mockStore) CreateWorkspace(ctx context.Context, input store.CreateWorkspaceInput) (*store.Workspace, error) {
	ws := &store.Workspace{
		ID:          "ws-" + input.Name,
		Name:        input.Name,
		Description: input.Description,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.workspaces[ws.ID] = ws
	return ws, nil
}

func (m *mockStore) GetWorkspace(ctx context.Context, id string) (*store.Workspace, error) {
	ws, ok := m.workspaces[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return ws, nil
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
	ws, ok := m.workspaces[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	if input.Name != nil {
		ws.Name = *input.Name
	}
	if input.Description != nil {
		ws.Description = *input.Description
	}
	ws.UpdatedAt = time.Now()
	return ws, nil
}

func (m *mockStore) DeleteWorkspace(ctx context.Context, id string) error {
	if _, ok := m.workspaces[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.workspaces, id)
	return nil
}

// Service operations
func (m *mockStore) CreateService(ctx context.Context, input store.CreateServiceInput) (*store.Service, error) {
	svc := &store.Service{
		ID:             "svc-" + input.Name,
		WorkspaceID:    input.WorkspaceID,
		Name:           input.Name,
		Type:           input.Type,
		ConnectionInfo: input.ConnectionInfo,
		Enabled:        input.Enabled,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	m.services[svc.ID] = svc
	return svc, nil
}

func (m *mockStore) GetService(ctx context.Context, id string) (*store.Service, error) {
	svc, ok := m.services[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return svc, nil
}

func (m *mockStore) ListServices(ctx context.Context, workspaceID string) ([]*store.Service, error) {
	var result []*store.Service
	for _, svc := range m.services {
		if svc.WorkspaceID == workspaceID {
			result = append(result, svc)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateService(ctx context.Context, id string, input store.UpdateServiceInput) (*store.Service, error) {
	svc, ok := m.services[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	if input.Name != nil {
		svc.Name = *input.Name
	}
	if input.Enabled != nil {
		svc.Enabled = *input.Enabled
	}
	svc.UpdatedAt = time.Now()
	return svc, nil
}

func (m *mockStore) DeleteService(ctx context.Context, id string) error {
	if _, ok := m.services[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.services, id)
	return nil
}

// Layer operations
func (m *mockStore) CreateLayer(ctx context.Context, input store.CreateLayerInput) (*store.Layer, error) {
	layer := &store.Layer{
		ID:            "layer-" + input.PublicID,
		ServiceID:     input.ServiceID,
		PublicID:      input.PublicID,
		SourceLayer:   input.SourceLayer,
		IsSQLView:     input.IsSQLView,
		SQLViewConfig: input.SQLViewConfig,
		Title:         input.Title,
		Description:   input.Description,
		Enabled:       input.Enabled,
		CRSDefault:    input.CRSDefault,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	m.layers[layer.ID] = layer
	return layer, nil
}

func (m *mockStore) GetLayer(ctx context.Context, id string) (*store.Layer, error) {
	layer, ok := m.layers[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return layer, nil
}

func (m *mockStore) GetLayerByPublicID(ctx context.Context, serviceID, publicID string) (*store.Layer, error) {
	for _, layer := range m.layers {
		if layer.ServiceID == serviceID && layer.PublicID == publicID {
			return layer, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) ListLayers(ctx context.Context, serviceID string) ([]*store.Layer, error) {
	var result []*store.Layer
	for _, layer := range m.layers {
		if layer.ServiceID == serviceID {
			result = append(result, layer)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateLayer(ctx context.Context, id string, input store.UpdateLayerInput) (*store.Layer, error) {
	layer, ok := m.layers[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	if input.Title != nil {
		layer.Title = *input.Title
	}
	if input.Enabled != nil {
		layer.Enabled = *input.Enabled
	}
	layer.UpdatedAt = time.Now()
	return layer, nil
}

func (m *mockStore) DeleteLayer(ctx context.Context, id string) error {
	if _, ok := m.layers[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.layers, id)
	return nil
}

// API Key operations
func (m *mockStore) CreateAPIKey(ctx context.Context, input store.CreateAPIKeyInput) (*store.CreateAPIKeyOutput, error) {
	key := &store.APIKey{
		ID:          "key-test",
		WorkspaceID: input.WorkspaceID,
		Name:        input.Name,
		KeyHash:     "hash123",
		KeyPrefix:   "test",
		OwnerName:   input.OwnerName,
		OwnerEmail:  input.OwnerEmail,
		RoleID:      input.RoleID,
		CreatedAt:   time.Now(),
	}
	m.apiKeys[key.ID] = key
	return &store.CreateAPIKeyOutput{
		APIKey: *key,
		Key:    "test-api-key-123",
	}, nil
}

func (m *mockStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (*store.APIKey, error) {
	for _, key := range m.apiKeys {
		if key.KeyHash == keyHash {
			return key, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) ListAPIKeys(ctx context.Context, workspaceID *string) ([]*store.APIKey, error) {
	var result []*store.APIKey
	for _, key := range m.apiKeys {
		if workspaceID == nil || (key.WorkspaceID != nil && *key.WorkspaceID == *workspaceID) {
			result = append(result, key)
		}
	}
	return result, nil
}

func (m *mockStore) RevokeAPIKey(ctx context.Context, id string) error {
	if _, ok := m.apiKeys[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.apiKeys, id)
	return nil
}

func (m *mockStore) DeleteAPIKey(ctx context.Context, id string) error {
	key, ok := m.apiKeys[id]
	if !ok {
		return store.ErrNotFound
	}
	if !key.Revoked {
		return store.ErrAPIKeyNotRevoked
	}
	delete(m.apiKeys, id)
	return nil
}

// Stub implementations for unused methods
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
	return &store.WMSSettings{}, nil
}
func (m *mockStore) UpdateWMSSettings(ctx context.Context, workspaceID string, settings store.WMSSettings) error {
	return nil
}
func (m *mockStore) GetWFSSettings(ctx context.Context, workspaceID string) (*store.WFSSettings, error) {
	return &store.WFSSettings{}, nil
}
func (m *mockStore) UpdateWFSSettings(ctx context.Context, workspaceID string, settings store.WFSSettings) error {
	return nil
}
func (m *mockStore) GetOGCAPISettings(ctx context.Context, workspaceID string) (*store.OGCAPISettings, error) {
	return &store.OGCAPISettings{}, nil
}
func (m *mockStore) UpdateOGCAPISettings(ctx context.Context, workspaceID string, settings store.OGCAPISettings) error {
	return nil
}
func (m *mockStore) GetOGCTilesAPISettings(ctx context.Context, workspaceID string) (*store.OGCTilesAPISettings, error) {
	return &store.OGCTilesAPISettings{}, nil
}
func (m *mockStore) UpdateOGCTilesAPISettings(ctx context.Context, workspaceID string, settings store.OGCTilesAPISettings) error {
	return nil
}
func (m *mockStore) CreateRole(ctx context.Context, input store.CreateRoleInput) (*store.Role, error) {
	role := &store.Role{
		ID:          input.ID,
		Name:        input.Name,
		Description: input.Description,
		IsSystem:    false,
		CreatedAt:   time.Now(),
	}
	m.roles[role.ID] = role
	return role, nil
}
func (m *mockStore) GetRole(ctx context.Context, id string) (*store.Role, error) {
	role, ok := m.roles[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return role, nil
}
func (m *mockStore) ListRoles(ctx context.Context) ([]*store.Role, error) {
	var result []*store.Role
	for _, role := range m.roles {
		result = append(result, role)
	}
	return result, nil
}
func (m *mockStore) DeleteRole(ctx context.Context, id string) error {
	delete(m.roles, id)
	return nil
}
func (m *mockStore) CreateClaimMapping(ctx context.Context, input store.CreateClaimMappingInput) (*store.ClaimRoleMapping, error) {
	mapping := &store.ClaimRoleMapping{
		ID:          "mapping-" + input.ClaimName,
		WorkspaceID: input.WorkspaceID,
		ClaimName:   input.ClaimName,
		ClaimValue:  input.ClaimValue,
		RoleID:      input.RoleID,
		Priority:    input.Priority,
		CreatedAt:   time.Now(),
	}
	m.claimMappings[mapping.ID] = mapping
	return mapping, nil
}
func (m *mockStore) GetClaimMapping(ctx context.Context, id string) (*store.ClaimRoleMapping, error) {
	mapping, ok := m.claimMappings[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return mapping, nil
}
func (m *mockStore) ListClaimMappings(ctx context.Context, workspaceID string) ([]*store.ClaimRoleMapping, error) {
	var result []*store.ClaimRoleMapping
	for _, m := range m.claimMappings {
		if m.WorkspaceID == workspaceID {
			result = append(result, m)
		}
	}
	return result, nil
}
func (m *mockStore) DeleteClaimMapping(ctx context.Context, id string) error {
	delete(m.claimMappings, id)
	return nil
}
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

func (m *mockStore) GetWMTSSettings(_ context.Context, workspaceID string) (*store.WMTSSettings, error) {
	settings, ok := m.wmtsSettings[workspaceID]
	if !ok {
		defaults := store.DefaultWMTSSettings()
		return &defaults, nil
	}
	return &settings, nil
}

func (m *mockStore) UpdateWMTSSettings(_ context.Context, workspaceID string, settings store.WMTSSettings) error {
	m.wmtsSettings[workspaceID] = settings
	return nil
}

func (m *mockStore) GetTileRevision(context.Context, string) (int64, error) { return 1, nil }

// testConfig returns a minimal config for testing
func testConfig() conf.Config {
	return conf.Config{
		Server: conf.Server{
			HttpPort: 9000,
		},
		Paging: conf.Paging{
			LimitDefault: 100,
			LimitMax:     10000,
		},
		WMS: conf.WMS{
			Enabled:      true,
			MaxWidth:     4096,
			MaxHeight:    4096,
			DefaultStyle: "default",
			Styles: map[string]conf.WMSStyle{
				"default": {
					FillColor:   "#3388ff",
					FillOpacity: 0.5,
					StrokeColor: "#3388ff",
					StrokeWidth: 2.0,
					PointRadius: 5.0,
				},
			},
		},
		WFS: conf.WFS{
			Enabled:     true,
			MaxFeatures: 10000,
		},
	}
}

// Test helper functions
func newTestHandler(t *testing.T, mockStore *mockStore) *handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testConfig()
	registry := workspace.NewRegistry(mockStore, nil)

	cacheManager, _ := cache.NewManager(cache.DefaultConfig())

	return &handler{
		cfg:      cfg,
		store:    mockStore,
		registry: registry,
		logger:   logger,
		cache:    cacheManager,
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"message": "hello"}
	writeJSON(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["message"] != "hello" {
		t.Errorf("expected message=hello, got %s", result["message"])
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "Bad Request", "invalid input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var result errorResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Code != 400 {
		t.Errorf("expected code=400, got %d", result.Code)
	}
	if result.Message != "Bad Request" {
		t.Errorf("expected message='Bad Request', got %s", result.Message)
	}
	if result.Detail != "invalid input" {
		t.Errorf("expected detail='invalid input', got %s", result.Detail)
	}
}

func TestReadJSON(t *testing.T) {
	body := bytes.NewBufferString(`{"name":"test","value":123}`)
	r := httptest.NewRequest("POST", "/", body)

	var data struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	if err := readJSON(r, &data); err != nil {
		t.Fatalf("failed to read JSON: %v", err)
	}

	if data.Name != "test" {
		t.Errorf("expected name=test, got %s", data.Name)
	}
	if data.Value != 123 {
		t.Errorf("expected value=123, got %d", data.Value)
	}
}

func TestReadJSON_Invalid(t *testing.T) {
	body := bytes.NewBufferString(`{invalid json}`)
	r := httptest.NewRequest("POST", "/", body)

	var data struct{}
	if err := readJSON(r, &data); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
