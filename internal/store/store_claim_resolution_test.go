package store

import (
	"context"
	"reflect"
	"testing"
)

func TestResolveClaimsToRolesDeterministicPrecedence(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	priorityWS, _ := catalog.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "priority"})
	strengthWS, _ := catalog.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "strength"})
	customWS, _ := catalog.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "custom"})
	if _, err := catalog.CreateRole(ctx, CreateRoleInput{ID: "alpha", Name: "Alpha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.CreateRole(ctx, CreateRoleInput{ID: "zeta", Name: "Zeta"}); err != nil {
		t.Fatal(err)
	}

	mappings := []CreateClaimMappingInput{
		{WorkspaceID: priorityWS.ID, ClaimName: "groups", ClaimValue: "members", RoleID: "viewer", Priority: 5},
		{WorkspaceID: priorityWS.ID, ClaimName: "roles", ClaimValue: "operators", RoleID: "admin", Priority: 10},
		{WorkspaceID: strengthWS.ID, ClaimName: "groups", ClaimValue: "editors", RoleID: "editor", Priority: 10},
		{WorkspaceID: strengthWS.ID, ClaimName: "roles", ClaimValue: "admins", RoleID: "admin", Priority: 10},
		{WorkspaceID: customWS.ID, ClaimName: "groups", ClaimValue: "zeta-group", RoleID: "zeta", Priority: 10},
		{WorkspaceID: customWS.ID, ClaimName: "roles", ClaimValue: "alpha-group", RoleID: "alpha", Priority: 10},
		{WorkspaceID: "*", ClaimName: "groups", ClaimValue: "global-admins", RoleID: "admin", Priority: 1},
	}
	for _, mapping := range mappings {
		if _, err := catalog.CreateClaimMapping(ctx, mapping); err != nil {
			t.Fatal(err)
		}
	}

	claims := map[string][]string{
		"roles":  {"alpha-group", "admins", "operators"},
		"groups": {"global-admins", "zeta-group", "editors", "members"},
	}
	want := map[string]string{
		priorityWS.ID: "admin",
		strengthWS.ID: "admin",
		customWS.ID:   "alpha",
		"*":           "admin",
	}
	for run := 0; run < 100; run++ {
		got, err := catalog.ResolveClaimsToRoles(ctx, claims)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: got %v, want %v", run, got, want)
		}
	}
}
