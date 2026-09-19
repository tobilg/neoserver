package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// createTestStore creates a temporary store for testing.
func createTestStore(t *testing.T) (*DuckDBStore, func()) {
	t.Helper()

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	store, _, err := Init(Config{Path: dbPath})
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to init store: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestWorkspaceCRUD(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create
	ws, err := store.CreateWorkspace(ctx, CreateWorkspaceInput{
		Name:        "test-workspace",
		Description: "Test workspace description",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}
	if ws.ID == "" {
		t.Fatal("expected workspace ID")
	}
	if ws.Name != "test-workspace" {
		t.Fatalf("expected name 'test-workspace', got %q", ws.Name)
	}

	// Get
	got, err := store.GetWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWorkspace failed: %v", err)
	}
	if got.Name != ws.Name {
		t.Fatalf("expected name %q, got %q", ws.Name, got.Name)
	}

	// GetByName
	gotByName, err := store.GetWorkspaceByName(ctx, "test-workspace")
	if err != nil {
		t.Fatalf("GetWorkspaceByName failed: %v", err)
	}
	if gotByName.ID != ws.ID {
		t.Fatalf("expected ID %q, got %q", ws.ID, gotByName.ID)
	}

	// List
	workspaces, err := store.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(workspaces))
	}

	// Update
	newName := "updated-workspace"
	updated, err := store.UpdateWorkspace(ctx, ws.ID, UpdateWorkspaceInput{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("UpdateWorkspace failed: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("expected name %q, got %q", newName, updated.Name)
	}

	// Delete
	if err := store.DeleteWorkspace(ctx, ws.ID); err != nil {
		t.Fatalf("DeleteWorkspace failed: %v", err)
	}

	// Verify deleted
	_, err = store.GetWorkspace(ctx, ws.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWorkspaceDuplicateName(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.CreateWorkspace(ctx, CreateWorkspaceInput{
		Name: "dup-workspace",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}

	// Try to create duplicate
	_, err = store.CreateWorkspace(ctx, CreateWorkspaceInput{
		Name: "dup-workspace",
	})
	if err != ErrDuplicateKey {
		t.Fatalf("expected ErrDuplicateKey, got %v", err)
	}
}

func TestServiceCRUD(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create workspace first
	ws, err := store.CreateWorkspace(ctx, CreateWorkspaceInput{
		Name: "service-test",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}

	connInfo := json.RawMessage(`{"host": "localhost", "port": 5432}`)

	// Create service
	svc, err := store.CreateService(ctx, CreateServiceInput{
		WorkspaceID:    ws.ID,
		Name:           "test-service",
		Type:           ServiceTypePostGIS,
		ConnectionInfo: connInfo,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("CreateService failed: %v", err)
	}
	if svc.ID == "" {
		t.Fatal("expected service ID")
	}
	if svc.Type != ServiceTypePostGIS {
		t.Fatalf("expected type %q, got %q", ServiceTypePostGIS, svc.Type)
	}

	// Get
	got, err := store.GetService(ctx, svc.ID)
	if err != nil {
		t.Fatalf("GetService failed: %v", err)
	}
	if got.Name != svc.Name {
		t.Fatalf("expected name %q, got %q", svc.Name, got.Name)
	}

	// List
	services, err := store.ListServices(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListServices failed: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}

	// Update
	newName := "updated-service"
	enabled := false
	updated, err := store.UpdateService(ctx, svc.ID, UpdateServiceInput{
		Name:    &newName,
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("UpdateService failed: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("expected name %q, got %q", newName, updated.Name)
	}
	if updated.Enabled != false {
		t.Fatal("expected enabled=false")
	}

	// Delete
	if err := store.DeleteService(ctx, svc.ID); err != nil {
		t.Fatalf("DeleteService failed: %v", err)
	}

	_, err = store.GetService(ctx, svc.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLayerCRUD(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create workspace and service
	ws, err := store.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "layer-test"})
	if err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}
	svc, err := store.CreateService(ctx, CreateServiceInput{
		WorkspaceID: ws.ID,
		Name:        "layer-service",
		Type:        ServiceTypeDuckDB,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreateService failed: %v", err)
	}

	// Create layer
	layer, err := store.CreateLayer(ctx, CreateLayerInput{
		ServiceID:   svc.ID,
		SourceLayer: "public.buildings",
		PublicID:    "buildings",
		Title:       "Buildings",
		Description: "Building footprints",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}
	if layer.CRSDefault != 4326 {
		t.Fatalf("expected default CRS 4326, got %d", layer.CRSDefault)
	}

	// Get
	got, err := store.GetLayer(ctx, layer.ID)
	if err != nil {
		t.Fatalf("GetLayer failed: %v", err)
	}
	if got.PublicID != "buildings" {
		t.Fatalf("expected public_id 'buildings', got %q", got.PublicID)
	}

	// GetByPublicID
	gotByPublic, err := store.GetLayerByPublicID(ctx, svc.ID, "buildings")
	if err != nil {
		t.Fatalf("GetLayerByPublicID failed: %v", err)
	}
	if gotByPublic.ID != layer.ID {
		t.Fatalf("expected ID %q, got %q", layer.ID, gotByPublic.ID)
	}

	// List
	layers, err := store.ListLayers(ctx, svc.ID)
	if err != nil {
		t.Fatalf("ListLayers failed: %v", err)
	}
	if len(layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(layers))
	}

	// Update
	newTitle := "Updated Buildings"
	newCRS := 3857
	updated, err := store.UpdateLayer(ctx, layer.ID, UpdateLayerInput{
		Title:      &newTitle,
		CRSDefault: &newCRS,
	})
	if err != nil {
		t.Fatalf("UpdateLayer failed: %v", err)
	}
	if updated.Title != newTitle {
		t.Fatalf("expected title %q, got %q", newTitle, updated.Title)
	}
	if updated.CRSDefault != newCRS {
		t.Fatalf("expected CRS %d, got %d", newCRS, updated.CRSDefault)
	}

	// Delete
	if err := store.DeleteLayer(ctx, layer.ID); err != nil {
		t.Fatalf("DeleteLayer failed: %v", err)
	}

	_, err = store.GetLayer(ctx, layer.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLayerAccessControlRoundTrip(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	ws, err := store.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "acl-test"})
	if err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}
	svc, err := store.CreateService(ctx, CreateServiceInput{
		WorkspaceID: ws.ID, Name: "acl-service", Type: ServiceTypeDuckDB, Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateService failed: %v", err)
	}

	// Create a restricted layer.
	layer, err := store.CreateLayer(ctx, CreateLayerInput{
		ServiceID:    svc.ID,
		SourceLayer:  "public.secret",
		PublicID:     "secret",
		Enabled:      true,
		Public:       false,
		AllowedRoles: []string{"admin", "editor"},
	})
	if err != nil {
		t.Fatalf("CreateLayer failed: %v", err)
	}

	got, err := store.GetLayer(ctx, layer.ID)
	if err != nil {
		t.Fatalf("GetLayer failed: %v", err)
	}
	if got.Public {
		t.Fatal("expected Public=false")
	}
	if len(got.AllowedRoles) != 2 || got.AllowedRoles[0] != "admin" || got.AllowedRoles[1] != "editor" {
		t.Fatalf("expected allowed_roles [admin editor], got %v", got.AllowedRoles)
	}

	// Update to public with no role restriction.
	pub := true
	updated, err := store.UpdateLayer(ctx, layer.ID, UpdateLayerInput{
		Public:       &pub,
		AllowedRoles: []string{},
	})
	if err != nil {
		t.Fatalf("UpdateLayer failed: %v", err)
	}
	if !updated.Public {
		t.Fatal("expected Public=true after update")
	}

	reloaded, err := store.GetLayer(ctx, layer.ID)
	if err != nil {
		t.Fatalf("GetLayer failed: %v", err)
	}
	if !reloaded.Public {
		t.Fatal("expected persisted Public=true")
	}
	if len(reloaded.AllowedRoles) != 0 {
		t.Fatalf("expected empty allowed_roles, got %v", reloaded.AllowedRoles)
	}
}

func TestRoleCRUD(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// List should include default system roles
	roles, err := store.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles failed: %v", err)
	}
	if len(roles) < 4 {
		t.Fatalf("expected at least 4 default roles, got %d", len(roles))
	}

	// Verify super_admin exists
	superAdmin, err := store.GetRole(ctx, "super_admin")
	if err != nil {
		t.Fatalf("GetRole(super_admin) failed: %v", err)
	}
	if !superAdmin.IsSystem {
		t.Fatal("expected super_admin to be a system role")
	}

	// Create custom role
	role, err := store.CreateRole(ctx, CreateRoleInput{
		ID:          "custom_role",
		Name:        "Custom Role",
		Description: "A custom role",
	})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}
	if role.IsSystem {
		t.Fatal("expected custom role to not be a system role")
	}

	// Cannot delete system role
	err = store.DeleteRole(ctx, "super_admin")
	if err == nil || err.Error() != "cannot delete system role" {
		t.Fatalf("expected 'cannot delete system role' error, got %v", err)
	}

	// Can delete custom role
	if err := store.DeleteRole(ctx, "custom_role"); err != nil {
		t.Fatalf("DeleteRole failed: %v", err)
	}
}

func TestAPIKeyCRUD(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create workspace
	ws, _ := store.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "apikey-test"})

	// Create API key
	expiresAt := time.Now().Add(24 * time.Hour)
	output, err := store.CreateAPIKey(ctx, CreateAPIKeyInput{
		OwnerName:   "Test User",
		OwnerEmail:  "test@example.com",
		WorkspaceID: &ws.ID,
		RoleID:      "viewer",
		Name:        "test-key",
		ExpiresAt:   &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	if output.Key == "" {
		t.Fatal("expected key in output")
	}
	if output.Key[:4] != "nsk_" {
		t.Fatalf("expected key prefix 'nsk_', got %q", output.Key[:4])
	}

	// Verify key by hash
	gotByHash, err := store.GetAPIKeyByHash(ctx, output.APIKey.KeyHash)
	if err != nil {
		t.Fatalf("GetAPIKeyByHash failed: %v", err)
	}
	if gotByHash.ID != output.APIKey.ID {
		t.Fatalf("expected ID %q, got %q", output.APIKey.ID, gotByHash.ID)
	}

	// List
	keys, err := store.ListAPIKeys(ctx, &ws.ID)
	if err != nil {
		t.Fatalf("ListAPIKeys failed: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 API key, got %d", len(keys))
	}

	// Revoke
	if err := store.RevokeAPIKey(ctx, output.APIKey.ID); err != nil {
		t.Fatalf("RevokeAPIKey failed: %v", err)
	}

	// Revoked key should not be found
	_, err = store.GetAPIKeyByHash(ctx, output.APIKey.KeyHash)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for revoked key, got %v", err)
	}
}

func TestClaimMappingCRUD(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create workspace
	ws, _ := store.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "claim-test"})

	// Create claim mapping
	mapping, err := store.CreateClaimMapping(ctx, CreateClaimMappingInput{
		WorkspaceID: ws.ID,
		ClaimName:   "groups",
		ClaimValue:  "admins",
		RoleID:      "admin",
		Priority:    10,
	})
	if err != nil {
		t.Fatalf("CreateClaimMapping failed: %v", err)
	}
	if mapping.ID == "" {
		t.Fatal("expected mapping ID")
	}

	// Get
	got, err := store.GetClaimMapping(ctx, mapping.ID)
	if err != nil {
		t.Fatalf("GetClaimMapping failed: %v", err)
	}
	if got.ClaimValue != "admins" {
		t.Fatalf("expected claim_value 'admins', got %q", got.ClaimValue)
	}

	// List
	mappings, err := store.ListClaimMappings(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListClaimMappings failed: %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(mappings))
	}

	// Delete
	if err := store.DeleteClaimMapping(ctx, mapping.ID); err != nil {
		t.Fatalf("DeleteClaimMapping failed: %v", err)
	}

	_, err = store.GetClaimMapping(ctx, mapping.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestResolveClaimsToRoles(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create workspaces
	ws1, _ := store.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "ws1"})
	ws2, _ := store.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "ws2"})

	// Create claim mappings
	store.CreateClaimMapping(ctx, CreateClaimMappingInput{
		WorkspaceID: ws1.ID,
		ClaimName:   "groups",
		ClaimValue:  "ws1-admins",
		RoleID:      "admin",
		Priority:    10,
	})
	store.CreateClaimMapping(ctx, CreateClaimMappingInput{
		WorkspaceID: ws1.ID,
		ClaimName:   "groups",
		ClaimValue:  "ws1-viewers",
		RoleID:      "viewer",
		Priority:    5,
	})
	store.CreateClaimMapping(ctx, CreateClaimMappingInput{
		WorkspaceID: ws2.ID,
		ClaimName:   "groups",
		ClaimValue:  "ws2-editors",
		RoleID:      "editor",
		Priority:    10,
	})

	// Resolve claims
	claims := map[string][]string{
		"groups": {"ws1-admins", "ws2-editors"},
	}

	roles, err := store.ResolveClaimsToRoles(ctx, claims)
	if err != nil {
		t.Fatalf("ResolveClaimsToRoles failed: %v", err)
	}

	if roles[ws1.ID] != "admin" {
		t.Fatalf("expected ws1 role 'admin', got %q", roles[ws1.ID])
	}
	if roles[ws2.ID] != "editor" {
		t.Fatalf("expected ws2 role 'editor', got %q", roles[ws2.ID])
	}
}

func TestSigningKeyOperations(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Get active signing key (created during init)
	key, err := store.GetActiveSigningKey(ctx)
	if err != nil {
		t.Fatalf("GetActiveSigningKey failed: %v", err)
	}
	if key.Algorithm != "ES256" {
		t.Fatalf("expected algorithm ES256, got %q", key.Algorithm)
	}
	if !key.IsActive {
		t.Fatal("expected key to be active")
	}

	// Rotate key
	newKey, err := store.RotateSigningKey(ctx)
	if err != nil {
		t.Fatalf("RotateSigningKey failed: %v", err)
	}
	if newKey.ID == key.ID {
		t.Fatal("expected new key ID after rotation")
	}

	// Old key should be inactive, new key active
	activeKey, err := store.GetActiveSigningKey(ctx)
	if err != nil {
		t.Fatalf("GetActiveSigningKey after rotation failed: %v", err)
	}
	if activeKey.ID != newKey.ID {
		t.Fatalf("expected active key ID %q, got %q", newKey.ID, activeKey.ID)
	}
}

func TestTokenCreationAndValidation(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	signingKey, _ := store.GetActiveSigningKey(ctx)

	// Create token
	token, err := store.CreateToken(signingKey, "test-user", "admin", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Validate token
	payload, err := store.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if payload.Sub != "test-user" {
		t.Fatalf("expected subject 'test-user', got %q", payload.Sub)
	}
	if payload.Role != "admin" {
		t.Fatalf("expected role 'admin', got %q", payload.Role)
	}
	if payload.Iss != "neoserver" {
		t.Fatalf("expected issuer 'neoserver', got %q", payload.Iss)
	}
}

func TestCasbinPolicyOperations(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Load default policies
	policies, err := store.LoadCasbinPolicies(ctx)
	if err != nil {
		t.Fatalf("LoadCasbinPolicies failed: %v", err)
	}
	if len(policies) < 1 {
		t.Fatal("expected at least one default policy")
	}

	// Save new policy
	newRule := &CasbinRule{
		PType: "p",
		V0:    "editor",
		V1:    "test-ws",
		V2:    "*",
		V3:    "write",
	}
	if err := store.SaveCasbinPolicy(ctx, newRule); err != nil {
		t.Fatalf("SaveCasbinPolicy failed: %v", err)
	}

	// Verify policy was saved
	policiesAfter, _ := store.LoadCasbinPolicies(ctx)
	if len(policiesAfter) != len(policies)+1 {
		t.Fatalf("expected %d policies after save, got %d", len(policies)+1, len(policiesAfter))
	}

	// Remove policy
	if err := store.RemoveCasbinPolicy(ctx, newRule); err != nil {
		t.Fatalf("RemoveCasbinPolicy failed: %v", err)
	}

	// Verify removed
	policiesFinal, _ := store.LoadCasbinPolicies(ctx)
	if len(policiesFinal) != len(policies) {
		t.Fatalf("expected %d policies after remove, got %d", len(policies), len(policiesFinal))
	}
}
