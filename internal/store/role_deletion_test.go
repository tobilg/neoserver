package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRoleDeletionDependenciesAndPolicyRemoval(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	role, err := s.CreateRole(ctx, CreateRoleInput{ID: "custom", Name: "Custom"})
	if err != nil {
		t.Fatal(err)
	}
	rule := &CasbinRule{PType: "p", V0: role.ID, V1: "*", V2: "service:wms", V3: "read"}
	if err := s.SaveCasbinPolicy(ctx, rule); err != nil {
		t.Fatal(err)
	}
	key, err := s.CreateAPIKey(ctx, CreateAPIKeyInput{RoleID: role.ID, Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	deps, err := s.RoleDependencies(ctx, role.ID)
	if err != nil || deps["api_keys"] != 1 {
		t.Fatalf("dependencies=%v err=%v", deps, err)
	}
	if err := s.DeleteRole(ctx, role.ID); !errors.Is(err, ErrResourceNotEmpty) {
		t.Fatalf("assigned role deletion: %v", err)
	}
	if err := s.RevokeAPIKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	session, err := s.CreateBrowserSession(ctx, BrowserSession{TokenHash: "unique", CSRFHash: "csrf", Subject: "test", AuthMethod: "jwt", Roles: map[string]string{"*": role.ID}, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRole(ctx, role.ID); !errors.Is(err, ErrResourceNotEmpty) {
		t.Fatalf("session dependency ignored: %v", err)
	}
	if err := s.RevokeBrowserSession(ctx, session.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRole(ctx, role.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRole(ctx, role.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("role remains")
	}
	policies, err := s.LoadCasbinPolicies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		if policy.V0 == role.ID {
			t.Fatal("deleted role grant remains")
		}
	}
	if _, err := s.GetAPIKeyByID(ctx, key.ID); err == nil {
		t.Fatal("deleted role key authenticates")
	}
	if _, err := s.CreateRole(ctx, CreateRoleInput{ID: role.ID, Name: "Recreated"}); !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("retired role ID reused: %v", err)
	}
}

func TestPublicationRoleDependencies(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := s.CreateRole(ctx, CreateRoleInput{ID: "custom", Name: "Custom"}); err != nil {
		t.Fatal(err)
	}
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "publications"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := s.CreateService(ctx, CreateServiceInput{WorkspaceID: ws.ID, Name: "data", Type: ServiceTypePostGIS})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLayer(ctx, CreateLayerInput{ServiceID: svc.ID, PublicID: "features", SourceLayer: "features", AllowedRoles: []string{"custom"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCoverage(ctx, CreateCoverageInput{ServiceID: svc.ID, WorkspaceID: ws.ID, PublicID: "raster", SourceCoverage: "raster", AllowedRoles: []string{"custom"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLayerGroup(ctx, CreateLayerGroupInput{WorkspaceID: ws.ID, PublicID: "group", AllowedRoles: []string{"custom"}}); err != nil {
		t.Fatal(err)
	}
	deps, err := s.RoleDependencies(ctx, "custom")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"layers", "coverages", "layer_groups"} {
		if deps[kind] != 1 {
			t.Fatalf("missing %s dependency: %v", kind, deps)
		}
	}
	if err := s.DeleteRole(ctx, "custom"); !errors.Is(err, ErrResourceNotEmpty) {
		t.Fatalf("publication assignments ignored: %v", err)
	}
}
