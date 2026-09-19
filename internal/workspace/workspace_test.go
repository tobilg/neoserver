package workspace

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/store"
)

// mockStore is a minimal mock implementation of store.Store for testing
type mockStore struct {
	workspaces map[string]*store.Workspace
	services   map[string]*store.Service
	layers     map[string]*store.Layer
	styles     map[string]*store.Style
	mu         sync.RWMutex
}

func newMockStore() *mockStore {
	return &mockStore{
		workspaces: make(map[string]*store.Workspace),
		services:   make(map[string]*store.Service),
		layers:     make(map[string]*store.Layer),
		styles:     make(map[string]*store.Style),
	}
}

// Workspace operations
func (m *mockStore) CreateWorkspace(ctx context.Context, input store.CreateWorkspaceInput) (*store.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.RLock()
	defer m.mu.RUnlock()
	ws, ok := m.workspaces[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return ws, nil
}

func (m *mockStore) GetWorkspaceByName(ctx context.Context, name string) (*store.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ws := range m.workspaces {
		if ws.Name == name {
			return ws, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) ListWorkspaces(ctx context.Context) ([]*store.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*store.Workspace
	for _, ws := range m.workspaces {
		result = append(result, ws)
	}
	return result, nil
}

func (m *mockStore) UpdateWorkspace(ctx context.Context, id string, input store.UpdateWorkspaceInput) (*store.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.workspaces, id)
	return nil
}

// Service operations
func (m *mockStore) CreateService(ctx context.Context, input store.CreateServiceInput) (*store.Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.RLock()
	defer m.mu.RUnlock()
	svc, ok := m.services[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return svc, nil
}

func (m *mockStore) ListServices(ctx context.Context, workspaceID string) ([]*store.Service, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*store.Service
	for _, svc := range m.services {
		if svc.WorkspaceID == workspaceID {
			result = append(result, svc)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateService(ctx context.Context, id string, input store.UpdateServiceInput) (*store.Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.services[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.services, id)
	return nil
}

// Layer operations
func (m *mockStore) CreateLayer(ctx context.Context, input store.CreateLayerInput) (*store.Layer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	layer := &store.Layer{
		ID:          "layer-" + input.PublicID,
		ServiceID:   input.ServiceID,
		PublicID:    input.PublicID,
		SourceLayer: input.SourceLayer,
		Title:       input.Title,
		Description: input.Description,
		Enabled:     input.Enabled,
		CRSDefault:  input.CRSDefault,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.layers[layer.ID] = layer
	return layer, nil
}

func (m *mockStore) GetLayer(ctx context.Context, id string) (*store.Layer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	layer, ok := m.layers[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return layer, nil
}

func (m *mockStore) GetLayerByPublicID(ctx context.Context, serviceID, publicID string) (*store.Layer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, layer := range m.layers {
		if layer.ServiceID == serviceID && layer.PublicID == publicID {
			return layer, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) ListLayers(ctx context.Context, serviceID string) ([]*store.Layer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*store.Layer
	for _, layer := range m.layers {
		if layer.ServiceID == serviceID {
			result = append(result, layer)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateLayer(ctx context.Context, id string, input store.UpdateLayerInput) (*store.Layer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	if input.PublicID != nil {
		layer.PublicID = *input.PublicID
	}
	layer.UpdatedAt = time.Now()
	return layer, nil
}

func (m *mockStore) DeleteLayer(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.layers[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.layers, id)
	return nil
}

// Style operations
func (m *mockStore) CreateStyle(ctx context.Context, input store.CreateStyleInput) (*store.Style, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	style := &store.Style{
		ID:          "style-" + input.Name,
		WorkspaceID: input.WorkspaceID,
		Name:        input.Name,
		Title:       input.Title,
		Description: input.Description,
		SLDBody:     input.SLDBody,
		Format:      input.Format,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.styles[style.ID] = style
	return style, nil
}

func (m *mockStore) GetStyle(ctx context.Context, id string) (*store.Style, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	style, ok := m.styles[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return style, nil
}

func (m *mockStore) GetStyleByName(ctx context.Context, workspaceID, name string) (*store.Style, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, style := range m.styles {
		if style.WorkspaceID == workspaceID && style.Name == name {
			return style, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) ListStyles(ctx context.Context, workspaceID string) ([]*store.Style, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*store.Style
	for _, style := range m.styles {
		if style.WorkspaceID == workspaceID {
			result = append(result, style)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateStyle(ctx context.Context, id string, input store.UpdateStyleInput) (*store.Style, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	style, ok := m.styles[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	if input.Name != nil {
		style.Name = *input.Name
	}
	if input.Title != nil {
		style.Title = *input.Title
	}
	if input.SLDBody != nil {
		style.SLDBody = *input.SLDBody
	}
	style.UpdatedAt = time.Now()
	return style, nil
}

func (m *mockStore) DeleteStyle(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.styles[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.styles, id)
	return nil
}

// WFS Stored Query operations (stub implementations)
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

// Settings operations (stub implementations)
func (m *mockStore) GetWMSSettings(ctx context.Context, workspaceID string) (*store.WMSSettings, error) {
	return &store.WMSSettings{Enabled: true}, nil
}
func (m *mockStore) UpdateWMSSettings(ctx context.Context, workspaceID string, settings store.WMSSettings) error {
	return nil
}
func (m *mockStore) GetWFSSettings(ctx context.Context, workspaceID string) (*store.WFSSettings, error) {
	return &store.WFSSettings{Enabled: true}, nil
}
func (m *mockStore) UpdateWFSSettings(ctx context.Context, workspaceID string, settings store.WFSSettings) error {
	return nil
}
func (m *mockStore) GetOGCAPISettings(ctx context.Context, workspaceID string) (*store.OGCAPISettings, error) {
	return &store.OGCAPISettings{Enabled: true}, nil
}
func (m *mockStore) UpdateOGCAPISettings(ctx context.Context, workspaceID string, settings store.OGCAPISettings) error {
	return nil
}
func (m *mockStore) GetOGCTilesAPISettings(ctx context.Context, workspaceID string) (*store.OGCTilesAPISettings, error) {
	return &store.OGCTilesAPISettings{Enabled: true}, nil
}
func (m *mockStore) UpdateOGCTilesAPISettings(ctx context.Context, workspaceID string, settings store.OGCTilesAPISettings) error {
	return nil
}

// Stub implementations for unused methods
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
// Workspace Tests
// ============================================================================

func TestWorkspace_AddService(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
	}

	svc := &Service{
		ID:      "svc-1",
		Name:    "test-service",
		Enabled: true,
	}

	ws.AddService(svc)

	if ws.Services == nil {
		t.Fatal("expected Services map to be initialized")
	}

	if got := ws.GetService("svc-1"); got != svc {
		t.Errorf("expected service svc-1, got %v", got)
	}
}

func TestWorkspace_GetService(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Services: map[string]*Service{
			"svc-1": {ID: "svc-1", Name: "service1"},
		},
	}

	// Test existing service
	svc := ws.GetService("svc-1")
	if svc == nil || svc.Name != "service1" {
		t.Errorf("expected service1, got %v", svc)
	}

	// Test non-existing service
	svc = ws.GetService("svc-nonexistent")
	if svc != nil {
		t.Errorf("expected nil, got %v", svc)
	}
}

func TestWorkspace_GetServiceByName(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Services: map[string]*Service{
			"svc-1": {ID: "svc-1", Name: "service1"},
			"svc-2": {ID: "svc-2", Name: "service2"},
		},
	}

	// Test existing service by name
	svc := ws.GetServiceByName("service1")
	if svc == nil || svc.ID != "svc-1" {
		t.Errorf("expected svc-1, got %v", svc)
	}

	// Test non-existing service
	svc = ws.GetServiceByName("nonexistent")
	if svc != nil {
		t.Errorf("expected nil, got %v", svc)
	}
}

func TestWorkspace_RemoveService(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Services: map[string]*Service{
			"svc-1": {ID: "svc-1", Name: "service1"},
		},
	}

	ws.RemoveService("svc-1")

	if got := ws.GetService("svc-1"); got != nil {
		t.Errorf("expected nil after removal, got %v", got)
	}

	// Should not panic on non-existent service
	ws.RemoveService("nonexistent")
}

func TestWorkspace_GetAllLayers(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Services: map[string]*Service{
			"svc-1": {
				ID:      "svc-1",
				Enabled: true,
				Layers: map[string]*Layer{
					"layer1": {PublicID: "layer1", Enabled: true},
					"layer2": {PublicID: "layer2", Enabled: false}, // disabled
				},
			},
			"svc-2": {
				ID:      "svc-2",
				Enabled: false, // disabled service
				Layers: map[string]*Layer{
					"layer3": {PublicID: "layer3", Enabled: true},
				},
			},
			"svc-3": {
				ID:      "svc-3",
				Enabled: true,
				Layers: map[string]*Layer{
					"layer4": {PublicID: "layer4", Enabled: true},
				},
			},
		},
	}

	layers := ws.GetAllLayers()

	// Should only return layer1 and layer4 (enabled layers from enabled services)
	if len(layers) != 2 {
		t.Errorf("expected 2 layers, got %d", len(layers))
	}

	layerIDs := make(map[string]bool)
	for _, l := range layers {
		layerIDs[l.PublicID] = true
	}

	if !layerIDs["layer1"] || !layerIDs["layer4"] {
		t.Errorf("expected layer1 and layer4, got %v", layerIDs)
	}
}

func TestWorkspace_GetLayer(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Services: map[string]*Service{
			"svc-1": {
				ID:      "svc-1",
				Name:    "service1",
				Enabled: true,
				Layers: map[string]*Layer{
					"layer1": {PublicID: "layer1", Enabled: true},
					"layer2": {PublicID: "layer2", Enabled: false},
				},
			},
		},
	}

	// Test finding an enabled layer
	layer, svc := ws.GetLayer("layer1")
	if layer == nil || layer.PublicID != "layer1" {
		t.Errorf("expected layer1, got %v", layer)
	}
	if svc == nil || svc.ID != "svc-1" {
		t.Errorf("expected svc-1, got %v", svc)
	}

	// Test disabled layer - should not be found
	layer, svc = ws.GetLayer("layer2")
	if layer != nil {
		t.Errorf("expected nil for disabled layer, got %v", layer)
	}

	// Test non-existent layer
	layer, svc = ws.GetLayer("nonexistent")
	if layer != nil || svc != nil {
		t.Errorf("expected nil, nil for non-existent layer")
	}
}

func TestWorkspace_AddStyle(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
	}

	style := &Style{
		ID:   "style-1",
		Name: "default",
	}

	ws.AddStyle(style)

	if ws.Styles == nil {
		t.Fatal("expected Styles map to be initialized")
	}

	if got := ws.GetStyle("default"); got != style {
		t.Errorf("expected style 'default', got %v", got)
	}
}

func TestWorkspace_GetStyle(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Styles: map[string]*Style{
			"default": {ID: "style-1", Name: "default"},
		},
	}

	// Test existing style
	style := ws.GetStyle("default")
	if style == nil || style.ID != "style-1" {
		t.Errorf("expected style-1, got %v", style)
	}

	// Test non-existing style
	style = ws.GetStyle("nonexistent")
	if style != nil {
		t.Errorf("expected nil, got %v", style)
	}
}

func TestWorkspace_RemoveStyle(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Styles: map[string]*Style{
			"default": {ID: "style-1", Name: "default"},
		},
	}

	ws.RemoveStyle("default")

	if got := ws.GetStyle("default"); got != nil {
		t.Errorf("expected nil after removal, got %v", got)
	}
}

func TestWorkspace_GetAllStyles(t *testing.T) {
	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Styles: map[string]*Style{
			"style1": {ID: "s1", Name: "style1"},
			"style2": {ID: "s2", Name: "style2"},
		},
	}

	styles := ws.GetAllStyles()
	if len(styles) != 2 {
		t.Errorf("expected 2 styles, got %d", len(styles))
	}
}

// ============================================================================
// Service Tests
// ============================================================================

func TestService_AddLayer(t *testing.T) {
	svc := &Service{
		ID:   "svc-1",
		Name: "test-service",
	}

	layer := &Layer{
		ID:       "layer-1",
		PublicID: "cities",
	}

	svc.AddLayer(layer)

	if svc.Layers == nil {
		t.Fatal("expected Layers map to be initialized")
	}

	if got := svc.Layers["cities"]; got != layer {
		t.Errorf("expected layer 'cities', got %v", got)
	}
}

func TestService_RemoveLayer(t *testing.T) {
	svc := &Service{
		ID:   "svc-1",
		Name: "test-service",
		Layers: map[string]*Layer{
			"cities": {ID: "layer-1", PublicID: "cities"},
		},
	}

	svc.RemoveLayer("cities")

	if got := svc.Layers["cities"]; got != nil {
		t.Errorf("expected nil after removal, got %v", got)
	}
}

// ============================================================================
// Registry Tests
// ============================================================================

func TestNewRegistry(t *testing.T) {
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	if reg == nil {
		t.Fatal("expected non-nil registry")
	}

	if reg.store != ms {
		t.Error("expected store to be set")
	}

	if reg.workspaces == nil || reg.workspacesByID == nil {
		t.Error("expected maps to be initialized")
	}
}

func TestRegistry_CreateWorkspace(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, err := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{
		Name:        "test-ws",
		Description: "Test workspace",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ws.Name != "test-ws" {
		t.Errorf("expected name 'test-ws', got %s", ws.Name)
	}

	// Should be accessible by name
	got, ok := reg.Get("test-ws")
	if !ok || got.ID != ws.ID || got == ws {
		t.Error("workspace should be accessible by name")
	}

	// Should be accessible by ID
	got, ok = reg.GetByID(ws.ID)
	if !ok || got.ID != ws.ID || got == ws {
		t.Error("workspace should be accessible by ID")
	}
}

func TestRegistry_UpdateWorkspace(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create workspace first
	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{
		Name:        "original",
		Description: "Original description",
	})

	// Update the name
	newName := "updated"
	updated, err := reg.UpdateWorkspace(ctx, ws.ID, store.UpdateWorkspaceInput{
		Name: &newName,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if updated.Name != "updated" {
		t.Errorf("expected name 'updated', got %s", updated.Name)
	}

	// Old name should not work
	_, ok := reg.Get("original")
	if ok {
		t.Error("old name should not work after rename")
	}

	// New name should work
	_, ok = reg.Get("updated")
	if !ok {
		t.Error("new name should work after rename")
	}
}

func TestRegistry_DeleteWorkspace(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create workspace
	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{
		Name: "to-delete",
	})

	// Delete it
	err := reg.DeleteWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should no longer be accessible
	_, ok := reg.Get("to-delete")
	if ok {
		t.Error("workspace should not be accessible after deletion")
	}

	_, ok = reg.GetByID(ws.ID)
	if ok {
		t.Error("workspace should not be accessible by ID after deletion")
	}
}

func TestRegistry_List(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create workspaces
	reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "ws1"})
	reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "ws2"})

	list := reg.List()
	if len(list) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(list))
	}
}

func TestRegistry_CreateService(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create workspace first
	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	// Create service
	svc, err := reg.CreateService(ctx, store.CreateServiceInput{
		WorkspaceID: ws.ID,
		Name:        "postgis-svc",
		Type:        store.ServiceTypePostGIS,
		Enabled:     true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if svc.Name != "postgis-svc" {
		t.Errorf("expected name 'postgis-svc', got %s", svc.Name)
	}

	// Service should be in workspace
	ws, _ = reg.Get("test-ws")
	if ws.GetService(svc.ID) == nil {
		t.Error("service should be in workspace")
	}
}

func TestRegistry_DeleteService(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create workspace and service
	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})
	svc, _ := reg.CreateService(ctx, store.CreateServiceInput{
		WorkspaceID: ws.ID,
		Name:        "to-delete",
		Enabled:     true,
	})

	// Delete service
	err := reg.DeleteService(ctx, ws.ID, svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Service should no longer be in workspace
	ws, _ = reg.Get("test-ws")
	if ws.GetService(svc.ID) != nil {
		t.Error("service should not be in workspace after deletion")
	}
}

func TestRegistry_CreateLayer(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create workspace and service
	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})
	svc, _ := reg.CreateService(ctx, store.CreateServiceInput{
		WorkspaceID: ws.ID,
		Name:        "postgis-svc",
		Enabled:     true,
	})

	// Create layer
	layer, err := reg.CreateLayer(ctx, ws.ID, svc.ID, store.CreateLayerInput{
		ServiceID:   svc.ID,
		SourceLayer: "public.cities",
		PublicID:    "cities",
		Title:       "Cities Layer",
		Enabled:     true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if layer.PublicID != "cities" {
		t.Errorf("expected public ID 'cities', got %s", layer.PublicID)
	}

	// Layer should be in service
	ws, _ = reg.Get("test-ws")
	s := ws.GetService(svc.ID)
	if s.Layers["cities"] == nil {
		t.Error("layer should be in service")
	}
}

func TestRegistry_Close(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create some workspaces
	reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "ws1"})
	reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "ws2"})

	err := reg.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have no workspaces after close
	if len(reg.List()) != 0 {
		t.Error("expected no workspaces after close")
	}
}

func TestRegistry_Remove(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	// Remove without touching store
	reg.Remove(ws.ID)

	_, ok := reg.Get("test-ws")
	if ok {
		t.Error("workspace should be removed from registry")
	}

	// Workspace should still exist in mock store
	if _, exists := ms.workspaces[ws.ID]; !exists {
		t.Error("workspace should still exist in store")
	}
}

// ============================================================================
// Concurrent Access Tests
// ============================================================================

func TestWorkspace_ConcurrentAccess(t *testing.T) {
	ws := &Workspace{
		ID:       "ws-1",
		Name:     "test",
		Services: make(map[string]*Service),
		Styles:   make(map[string]*Style),
	}

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent service additions
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			svc := &Service{
				ID:      "svc-" + string(rune('A'+i%26)),
				Name:    "service",
				Enabled: true,
				Layers:  make(map[string]*Layer),
			}
			ws.AddService(svc)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws.GetAllLayers()
		}()
	}

	// Concurrent style operations
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			style := &Style{ID: "s", Name: "style"}
			ws.AddStyle(style)
			ws.GetStyle("style")
			ws.GetAllStyles()
		}(i)
	}

	wg.Wait()
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	var wg sync.WaitGroup
	iterations := 50

	// Concurrent workspace creation and listing
	for i := 0; i < iterations; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{
				Name: "ws-" + string(rune('A'+i%26)) + "-" + string(rune('0'+i%10)),
			})
		}(i)

		go func() {
			defer wg.Done()
			reg.List()
		}()
	}

	wg.Wait()
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestDerefSettings(t *testing.T) {
	// Test nil WMS settings
	wms := derefWMSSettings(nil)
	if wms.Enabled {
		t.Error("expected disabled WMS settings from nil")
	}

	// Test non-nil WMS settings
	wmsInput := &store.WMSSettings{Enabled: true, MaxWidth: 4096}
	wms = derefWMSSettings(wmsInput)
	if !wms.Enabled || wms.MaxWidth != 4096 {
		t.Error("expected settings to be copied")
	}

	// Test nil WFS settings
	wfs := derefWFSSettings(nil)
	if wfs.Enabled {
		t.Error("expected disabled WFS settings from nil")
	}

	// Test nil OGC API settings (defaults to enabled)
	ogc := derefOGCAPISettings(nil)
	if !ogc.Enabled {
		t.Error("expected enabled OGC API settings from nil")
	}
}

// ============================================================================
// Context Tests
// ============================================================================

func TestWithWorkspace_FromContext(t *testing.T) {
	ws := &Workspace{ID: "ws-1", Name: "test"}
	ctx := WithWorkspace(context.Background(), ws)

	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("expected workspace in context")
	}
	if got.ID != "ws-1" {
		t.Errorf("expected ID 'ws-1', got %s", got.ID)
	}
}

func TestFromContext_NotPresent(t *testing.T) {
	_, ok := FromContext(context.Background())
	if ok {
		t.Error("expected no workspace in empty context")
	}
}

func TestMustFromContext_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when workspace not in context")
		}
	}()
	MustFromContext(context.Background())
}

func TestMustFromContext_Success(t *testing.T) {
	ws := &Workspace{ID: "ws-1"}
	ctx := WithWorkspace(context.Background(), ws)
	got := MustFromContext(ctx)
	if got.ID != "ws-1" {
		t.Errorf("expected ID 'ws-1', got %s", got.ID)
	}
}

func TestWithService_ServiceFromContext(t *testing.T) {
	svc := &Service{ID: "svc-1", Name: "test-service"}
	ctx := WithService(context.Background(), svc)

	got, ok := ServiceFromContext(ctx)
	if !ok {
		t.Fatal("expected service in context")
	}
	if got.ID != "svc-1" {
		t.Errorf("expected ID 'svc-1', got %s", got.ID)
	}
}

func TestServiceFromContext_NotPresent(t *testing.T) {
	_, ok := ServiceFromContext(context.Background())
	if ok {
		t.Error("expected no service in empty context")
	}
}

func TestWithLayer_LayerFromContext(t *testing.T) {
	layer := &Layer{ID: "layer-1", PublicID: "cities"}
	ctx := WithLayer(context.Background(), layer)

	got, ok := LayerFromContext(ctx)
	if !ok {
		t.Fatal("expected layer in context")
	}
	if got.ID != "layer-1" {
		t.Errorf("expected ID 'layer-1', got %s", got.ID)
	}
}

func TestLayerFromContext_NotPresent(t *testing.T) {
	_, ok := LayerFromContext(context.Background())
	if ok {
		t.Error("expected no layer in empty context")
	}
}

// ============================================================================
// Middleware Tests
// ============================================================================

func TestMiddleware_NoWorkspaceName(t *testing.T) {
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	handler := Middleware(reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	// No chi URL params - workspace param will be empty
	rctx := chi.NewRouteContext()
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestMiddleware_WorkspaceNotFound(t *testing.T) {
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	handler := Middleware(reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/workspaces/nonexistent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", "nonexistent")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestMiddleware_WorkspaceFound(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create a workspace
	reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	handler := Middleware(reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, ok := FromContext(r.Context())
		if !ok {
			t.Error("expected workspace in context")
		}
		if ws.Name != "test-ws" {
			t.Errorf("expected name 'test-ws', got %s", ws.Name)
		}
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/workspaces/test-ws", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", "test-ws")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRequireWorkspace_NotInContext(t *testing.T) {
	handler := RequireWorkspace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestRequireWorkspace_InContext(t *testing.T) {
	handler := RequireWorkspace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ws := &Workspace{ID: "ws-1"}
	ctx := WithWorkspace(context.Background(), ws)

	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestLoadLayer_NoWorkspace(t *testing.T) {
	handler := LoadLayer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestLoadLayer_NoLayerParam(t *testing.T) {
	ws := &Workspace{
		ID:       "ws-1",
		Services: make(map[string]*Service),
	}
	ctx := WithWorkspace(context.Background(), ws)

	handler := LoadLayer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(ctx)
	rctx := chi.NewRouteContext()
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	// Re-add workspace to new context
	r = r.WithContext(WithWorkspace(r.Context(), ws))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	// Should pass through when no layer param
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestLoadLayer_LayerNotFound(t *testing.T) {
	ws := &Workspace{
		ID: "ws-1",
		Services: map[string]*Service{
			"svc-1": {
				ID:      "svc-1",
				Enabled: true,
				Layers:  make(map[string]*Layer),
			},
		},
	}
	ctx := WithWorkspace(context.Background(), ws)

	handler := LoadLayer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/layers/nonexistent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("layer", "nonexistent")
	r = r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestLoadLayer_LayerFound(t *testing.T) {
	ws := &Workspace{
		ID: "ws-1",
		Services: map[string]*Service{
			"svc-1": {
				ID:      "svc-1",
				Enabled: true,
				Layers: map[string]*Layer{
					"cities": {ID: "layer-1", PublicID: "cities", Enabled: true},
				},
			},
		},
	}
	ctx := WithWorkspace(context.Background(), ws)

	handler := LoadLayer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		layer, ok := LayerFromContext(r.Context())
		if !ok {
			t.Error("expected layer in context")
		}
		if layer.PublicID != "cities" {
			t.Errorf("expected public ID 'cities', got %s", layer.PublicID)
		}
		svc, ok := ServiceFromContext(r.Context())
		if !ok {
			t.Error("expected service in context")
		}
		if svc.ID != "svc-1" {
			t.Errorf("expected service ID 'svc-1', got %s", svc.ID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/layers/cities", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("layer", "cities")
	r = r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestLoadLayer_CollectionIdParam(t *testing.T) {
	ws := &Workspace{
		ID: "ws-1",
		Services: map[string]*Service{
			"svc-1": {
				ID:      "svc-1",
				Enabled: true,
				Layers: map[string]*Layer{
					"buildings": {ID: "layer-2", PublicID: "buildings", Enabled: true},
				},
			},
		},
	}
	ctx := WithWorkspace(context.Background(), ws)

	handler := LoadLayer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		layer, ok := LayerFromContext(r.Context())
		if !ok {
			t.Error("expected layer in context")
		}
		if layer.PublicID != "buildings" {
			t.Errorf("expected public ID 'buildings', got %s", layer.PublicID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/collections/buildings", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("collectionId", "buildings")
	r = r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// ============================================================================
// Registry Update/Delete Tests
// ============================================================================

func TestRegistry_UpdateService(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create workspace and service
	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})
	svc, _ := reg.CreateService(ctx, store.CreateServiceInput{
		WorkspaceID: ws.ID,
		Name:        "original-name",
		Type:        store.ServiceTypePostGIS,
		Enabled:     true,
	})

	// Update service
	newName := "updated-name"
	enabled := false
	updated, err := reg.UpdateService(ctx, ws.ID, svc.ID, store.UpdateServiceInput{
		Name:    &newName,
		Enabled: &enabled,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if updated.Name != "updated-name" {
		t.Errorf("expected name 'updated-name', got %s", updated.Name)
	}

	// Verify in workspace
	ws, _ = reg.Get("test-ws")
	s := ws.GetService(svc.ID)
	if s == nil {
		t.Fatal("service not found in workspace")
	}
	if s.Name != "updated-name" {
		t.Errorf("expected updated name in workspace, got %s", s.Name)
	}
}

func TestRegistry_UpdateService_NotFound(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	newName := "updated"
	_, err := reg.UpdateService(ctx, ws.ID, "nonexistent", store.UpdateServiceInput{
		Name: &newName,
	})

	if err == nil {
		t.Error("expected error for non-existent service")
	}
}

func TestRegistry_UpdateLayer_NotFound(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})
	svc, _ := reg.CreateService(ctx, store.CreateServiceInput{
		WorkspaceID: ws.ID,
		Name:        "postgis-svc",
		Enabled:     true,
	})

	newTitle := "Updated"
	_, err := reg.UpdateLayer(ctx, ws.ID, svc.ID, "nonexistent", store.UpdateLayerInput{
		Title: &newTitle,
	})

	if err == nil {
		t.Error("expected error for non-existent layer")
	}
}

func TestRegistry_DeleteLayer(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	// Create workspace, service, and layer
	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})
	svc, _ := reg.CreateService(ctx, store.CreateServiceInput{
		WorkspaceID: ws.ID,
		Name:        "postgis-svc",
		Enabled:     true,
	})
	layer, _ := reg.CreateLayer(ctx, ws.ID, svc.ID, store.CreateLayerInput{
		ServiceID:   svc.ID,
		SourceLayer: "public.cities",
		PublicID:    "cities",
		Title:       "Cities",
		Enabled:     true,
	})

	// Delete layer
	err := reg.DeleteLayer(ctx, ws.ID, svc.ID, layer.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify layer is removed from service
	ws, _ = reg.Get("test-ws")
	s := ws.GetService(svc.ID)
	if s.Layers["cities"] != nil {
		t.Error("layer should be removed from service")
	}
}

func TestRegistry_DeleteLayer_NotFound(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})
	svc, _ := reg.CreateService(ctx, store.CreateServiceInput{
		WorkspaceID: ws.ID,
		Name:        "postgis-svc",
		Enabled:     true,
	})

	err := reg.DeleteLayer(ctx, ws.ID, svc.ID, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent layer")
	}
}

func TestRegistry_LayerMutationsRejectCrossServiceID(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)
	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})
	first, _ := reg.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "first", Enabled: true})
	second, _ := reg.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "second", Enabled: true})
	layer, _ := reg.CreateLayer(ctx, ws.ID, second.ID, store.CreateLayerInput{
		ServiceID: second.ID, SourceLayer: "public.cities", PublicID: "cities", Title: "Original", Enabled: true,
	})

	changed := "Changed"
	if _, err := reg.UpdateLayer(ctx, ws.ID, first.ID, layer.ID, store.UpdateLayerInput{Title: &changed}); !errors.Is(err, ErrLayerNotFound) {
		t.Fatalf("cross-service update error = %v, want ErrLayerNotFound", err)
	}
	if got, _ := ms.GetLayer(ctx, layer.ID); got.Title != "Original" {
		t.Fatalf("cross-service update changed title to %q", got.Title)
	}
	if err := reg.DeleteLayer(ctx, ws.ID, first.ID, layer.ID); !errors.Is(err, ErrLayerNotFound) {
		t.Fatalf("cross-service delete error = %v, want ErrLayerNotFound", err)
	}
	if _, err := ms.GetLayer(ctx, layer.ID); err != nil {
		t.Fatal("cross-service delete removed the layer")
	}
}

// ============================================================================
// Style CRUD Tests
// ============================================================================

func TestRegistry_CreateStyle(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	style, err := reg.CreateStyle(ctx, ws.ID, store.CreateStyleInput{
		WorkspaceID: ws.ID,
		Name:        "default-style",
		Title:       "Default Style",
		SLDBody:     "<sld></sld>",
		Format:      "sld/1.0.0",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if style.Name != "default-style" {
		t.Errorf("expected name 'default-style', got %s", style.Name)
	}

	// Verify style is in workspace
	ws, _ = reg.Get("test-ws")
	if ws.GetStyle("default-style") == nil {
		t.Error("style should be in workspace")
	}
}

func TestRegistry_CreateStyle_Success(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	// Create a style
	style, err := reg.CreateStyle(ctx, ws.ID, store.CreateStyleInput{
		WorkspaceID: ws.ID,
		Name:        "another-style",
		Title:       "Another Style",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if style.Name != "another-style" {
		t.Errorf("expected name 'another-style', got %s", style.Name)
	}
}

func TestRegistry_UpdateStyle(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})
	style, _ := reg.CreateStyle(ctx, ws.ID, store.CreateStyleInput{
		WorkspaceID: ws.ID,
		Name:        "original-style",
		Title:       "Original Title",
		SLDBody:     "<sld>old</sld>",
	})

	newName := "updated-style"
	newTitle := "Updated Title"
	newSLD := "<sld>new</sld>"
	updated, err := reg.UpdateStyle(ctx, ws.ID, style.ID, store.UpdateStyleInput{
		Name:    &newName,
		Title:   &newTitle,
		SLDBody: &newSLD,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if updated.Name != "updated-style" {
		t.Errorf("expected name 'updated-style', got %s", updated.Name)
	}
	if updated.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %s", updated.Title)
	}
}

func TestRegistry_UpdateStyle_NotFound(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	newName := "updated"
	_, err := reg.UpdateStyle(ctx, ws.ID, "nonexistent", store.UpdateStyleInput{
		Name: &newName,
	})

	if err == nil {
		t.Error("expected error for non-existent style")
	}
}

func TestRegistry_DeleteStyle(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})
	style, _ := reg.CreateStyle(ctx, ws.ID, store.CreateStyleInput{
		WorkspaceID: ws.ID,
		Name:        "to-delete",
	})

	err := reg.DeleteStyle(ctx, ws.ID, style.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify style is removed
	ws, _ = reg.Get("test-ws")
	if ws.GetStyle("to-delete") != nil {
		t.Error("style should be removed from workspace")
	}
}

func TestRegistry_DeleteStyle_NotFound(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	err := reg.DeleteStyle(ctx, ws.ID, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent style")
	}
}

// ============================================================================
// Settings Tests
// ============================================================================

func TestRegistry_UpdateWMSSettings(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	settings := store.WMSSettings{
		Enabled:   true,
		MaxWidth:  4096,
		MaxHeight: 4096,
		Title:     "WMS Service",
	}

	err := reg.UpdateWMSSettings(ctx, ws.ID, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify in workspace
	ws, _ = reg.Get("test-ws")
	if ws.Settings == nil || ws.Settings.WMS.MaxWidth != 4096 {
		t.Errorf("expected MaxWidth 4096")
	}
}

func TestRegistry_UpdateWMSSettings_VerifySettings(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	settings := store.WMSSettings{
		Enabled:      true,
		MaxWidth:     8192,
		MaxHeight:    8192,
		Title:        "My WMS",
		Abstract:     "Test WMS service",
		DefaultStyle: "default",
	}

	err := reg.UpdateWMSSettings(ctx, ws.ID, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get the workspace again and verify
	ws, _ = reg.Get("test-ws")
	if ws.Settings == nil {
		t.Fatal("settings should not be nil")
	}
	if ws.Settings.WMS.Title != "My WMS" {
		t.Errorf("expected title 'My WMS', got %s", ws.Settings.WMS.Title)
	}
}

func TestRegistry_UpdateWFSSettings(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	settings := store.WFSSettings{
		Enabled:     true,
		MaxFeatures: 10000,
		Title:       "WFS Service",
	}

	err := reg.UpdateWFSSettings(ctx, ws.ID, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify in workspace
	ws, _ = reg.Get("test-ws")
	if ws.Settings == nil || ws.Settings.WFS.MaxFeatures != 10000 {
		t.Errorf("expected MaxFeatures 10000")
	}
}

func TestRegistry_UpdateWFSSettings_VerifySettings(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	settings := store.WFSSettings{
		Enabled:     true,
		MaxFeatures: 50000,
		Title:       "My WFS",
		Abstract:    "Test WFS service",
	}

	err := reg.UpdateWFSSettings(ctx, ws.ID, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get the workspace again and verify
	ws, _ = reg.Get("test-ws")
	if ws.Settings == nil {
		t.Fatal("settings should not be nil")
	}
	if ws.Settings.WFS.Title != "My WFS" {
		t.Errorf("expected title 'My WFS', got %s", ws.Settings.WFS.Title)
	}
}

func TestRegistry_UpdateOGCAPISettings(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	settings := store.OGCAPISettings{
		Enabled:  true,
		Title:    "OGC API Service",
		Abstract: "A test OGC API service",
	}

	err := reg.UpdateOGCAPISettings(ctx, ws.ID, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify in workspace
	ws, _ = reg.Get("test-ws")
	if ws.Settings == nil || ws.Settings.OGCAPI.Title != "OGC API Service" {
		t.Errorf("expected Title 'OGC API Service'")
	}
}

func TestRegistry_UpdateOGCAPISettings_VerifySettings(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	reg := NewRegistry(ms, nil)

	ws, _ := reg.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "test-ws"})

	settings := store.OGCAPISettings{
		Enabled:  true,
		Title:    "My OGC API",
		Abstract: "Test OGC API service",
	}

	err := reg.UpdateOGCAPISettings(ctx, ws.ID, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get the workspace again and verify
	ws, _ = reg.Get("test-ws")
	if ws.Settings == nil {
		t.Fatal("settings should not be nil")
	}
	if ws.Settings.OGCAPI.Abstract != "Test OGC API service" {
		t.Errorf("expected abstract 'Test OGC API service', got %s", ws.Settings.OGCAPI.Abstract)
	}
}
