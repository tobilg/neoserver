package rbac

import (
	"testing"
)

func TestNewEnforcerWithDefaults(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, err := NewEnforcerWithDefaults(adapter)
	if err != nil {
		t.Fatalf("NewEnforcerWithDefaults failed: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil enforcer")
	}
}

func TestSuperAdminAccess(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	// super_admin should have access to everything
	tests := []struct {
		workspace string
		layer     string
		action    string
		expected  bool
	}{
		{"ws1", "layer1", ActionRead, true},
		{"ws1", "layer1", ActionWrite, true},
		{"ws1", "layer1", ActionDelete, true},
		{"ws1", "layer1", ActionManage, true},
		{"ws2", "layer2", ActionRead, true},
		{"*", "*", "*", true},
	}

	for _, tc := range tests {
		ok, err := e.CanAccess(RoleSuperAdmin, tc.workspace, tc.layer, tc.action)
		if err != nil {
			t.Fatalf("CanAccess error: %v", err)
		}
		if ok != tc.expected {
			t.Errorf("super_admin CanAccess(%q, %q, %q) = %v, want %v",
				tc.workspace, tc.layer, tc.action, ok, tc.expected)
		}
	}
}

func TestAdminAccess(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	tests := []struct {
		workspace string
		layer     string
		action    string
		expected  bool
	}{
		{"ws1", "layer1", ActionRead, true},
		{"ws1", "layer1", ActionWrite, true},
		{"ws1", "layer1", ActionDelete, true},
		{"ws1", "layer1", ActionManage, true},
	}

	for _, tc := range tests {
		ok, err := e.CanAccess(RoleAdmin, tc.workspace, tc.layer, tc.action)
		if err != nil {
			t.Fatalf("CanAccess error: %v", err)
		}
		if ok != tc.expected {
			t.Errorf("admin CanAccess(%q, %q, %q) = %v, want %v",
				tc.workspace, tc.layer, tc.action, ok, tc.expected)
		}
	}
}

func TestEditorAccess(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	tests := []struct {
		workspace string
		layer     string
		action    string
		expected  bool
	}{
		{"ws1", "layer1", ActionRead, true},
		{"ws1", "layer1", ActionWrite, true},
		{"ws1", "layer1", ActionDelete, false},
		{"ws1", "layer1", ActionManage, false},
	}

	for _, tc := range tests {
		ok, err := e.CanAccess(RoleEditor, tc.workspace, tc.layer, tc.action)
		if err != nil {
			t.Fatalf("CanAccess error: %v", err)
		}
		if ok != tc.expected {
			t.Errorf("editor CanAccess(%q, %q, %q) = %v, want %v",
				tc.workspace, tc.layer, tc.action, ok, tc.expected)
		}
	}
}

func TestViewerAccess(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	tests := []struct {
		workspace string
		layer     string
		action    string
		expected  bool
	}{
		{"ws1", "layer1", ActionRead, true},
		{"ws1", "layer1", ActionWrite, false},
		{"ws1", "layer1", ActionDelete, false},
		{"ws1", "layer1", ActionManage, false},
	}

	for _, tc := range tests {
		ok, err := e.CanAccess(RoleViewer, tc.workspace, tc.layer, tc.action)
		if err != nil {
			t.Fatalf("CanAccess error: %v", err)
		}
		if ok != tc.expected {
			t.Errorf("viewer CanAccess(%q, %q, %q) = %v, want %v",
				tc.workspace, tc.layer, tc.action, ok, tc.expected)
		}
	}
}

func TestCanAccessWorkspace(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	tests := []struct {
		role      string
		workspace string
		expected  bool
	}{
		{RoleSuperAdmin, "any-ws", true},
		{RoleAdmin, "any-ws", true},
		{RoleEditor, "any-ws", true},
		{RoleViewer, "any-ws", true},
		{"unknown_role", "any-ws", false},
	}

	for _, tc := range tests {
		ok, err := e.CanAccessWorkspace(tc.role, tc.workspace)
		if err != nil {
			t.Fatalf("CanAccessWorkspace error: %v", err)
		}
		if ok != tc.expected {
			t.Errorf("%s CanAccessWorkspace(%q) = %v, want %v",
				tc.role, tc.workspace, ok, tc.expected)
		}
	}
}

func TestCanManageWorkspace(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	tests := []struct {
		role      string
		workspace string
		expected  bool
	}{
		{RoleSuperAdmin, "any-ws", true},
		{RoleAdmin, "any-ws", true},
		{RoleEditor, "any-ws", false},
		{RoleViewer, "any-ws", false},
	}

	for _, tc := range tests {
		ok, err := e.CanManageWorkspace(tc.role, tc.workspace)
		if err != nil {
			t.Fatalf("CanManageWorkspace error: %v", err)
		}
		if ok != tc.expected {
			t.Errorf("%s CanManageWorkspace(%q) = %v, want %v",
				tc.role, tc.workspace, ok, tc.expected)
		}
	}
}

func TestAddAndRemoveWorkspacePolicy(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	customRole := "custom_role"
	workspace := "test-ws"

	// Initially no access
	ok, _ := e.CanAccessWorkspace(customRole, workspace)
	if ok {
		t.Fatal("expected no access initially")
	}

	// Add policy
	if err := e.AddWorkspacePolicy(customRole, workspace, ActionRead); err != nil {
		t.Fatalf("AddWorkspacePolicy failed: %v", err)
	}

	// Now should have access
	ok, _ = e.CanAccessWorkspace(customRole, workspace)
	if !ok {
		t.Fatal("expected access after adding policy")
	}

	// Remove policy
	if err := e.RemoveWorkspacePolicy(customRole, workspace, ActionRead); err != nil {
		t.Fatalf("RemoveWorkspacePolicy failed: %v", err)
	}

	// No access again
	ok, _ = e.CanAccessWorkspace(customRole, workspace)
	if ok {
		t.Fatal("expected no access after removing policy")
	}
}

func TestAddAndRemoveLayerPolicy(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	customRole := "custom_role"
	workspace := "test-ws"
	layer := "secret-layer"

	// Initially no access
	ok, _ := e.CanReadLayer(customRole, workspace, layer)
	if ok {
		t.Fatal("expected no access initially")
	}

	// Add layer-specific policy
	if err := e.AddLayerPolicy(customRole, workspace, layer, ActionRead); err != nil {
		t.Fatalf("AddLayerPolicy failed: %v", err)
	}

	// Now should have access to that specific layer
	ok, _ = e.CanReadLayer(customRole, workspace, layer)
	if !ok {
		t.Fatal("expected access after adding policy")
	}

	// But not to other layers
	ok, _ = e.CanReadLayer(customRole, workspace, "other-layer")
	if ok {
		t.Fatal("expected no access to other layers")
	}

	// Remove policy
	if err := e.RemoveLayerPolicy(customRole, workspace, layer, ActionRead); err != nil {
		t.Fatalf("RemoveLayerPolicy failed: %v", err)
	}

	ok, _ = e.CanReadLayer(customRole, workspace, layer)
	if ok {
		t.Fatal("expected no access after removing policy")
	}
}

func TestGetPolicies(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	// Get all policies
	policies, err := e.GetAllPolicies()
	if err != nil {
		t.Fatalf("GetAllPolicies failed: %v", err)
	}
	if len(policies) == 0 {
		t.Fatal("expected default policies")
	}

	// Get policies for super_admin
	superAdminPolicies, err := e.GetPoliciesForRole(RoleSuperAdmin)
	if err != nil {
		t.Fatalf("GetPoliciesForRole failed: %v", err)
	}
	if len(superAdminPolicies) == 0 {
		t.Fatal("expected super_admin policies")
	}

	// Add workspace-specific policy
	e.AddWorkspacePolicy("test_role", "test-ws", ActionRead)

	// Get policies for workspace
	wsPolicies, err := e.GetPoliciesForWorkspace("test-ws")
	if err != nil {
		t.Fatalf("GetPoliciesForWorkspace failed: %v", err)
	}
	if len(wsPolicies) == 0 {
		t.Fatal("expected workspace policies")
	}
}

func TestParsePolicy(t *testing.T) {
	p := ParsePolicy([]string{"admin", "ws1", "layer1", "read"})
	if p.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", p.Role)
	}
	if p.Workspace != "ws1" {
		t.Errorf("expected workspace 'ws1', got %q", p.Workspace)
	}
	if p.Layer != "layer1" {
		t.Errorf("expected layer 'layer1', got %q", p.Layer)
	}
	if p.Action != "read" {
		t.Errorf("expected action 'read', got %q", p.Action)
	}
}

func TestPolicyString(t *testing.T) {
	p := Policy{
		Role:      "admin",
		Workspace: "ws1",
		Layer:     "layer1",
		Action:    "read",
	}
	s := p.String()
	if s != "admin can read ws1/layer1" {
		t.Errorf("unexpected string: %q", s)
	}
}

func TestPolicyIsGlobal(t *testing.T) {
	global := Policy{Workspace: "*"}
	if !global.IsGlobal() {
		t.Error("expected IsGlobal() to be true for workspace '*'")
	}

	local := Policy{Workspace: "ws1"}
	if local.IsGlobal() {
		t.Error("expected IsGlobal() to be false for workspace 'ws1'")
	}
}

func TestPolicyIsLayerSpecific(t *testing.T) {
	specific := Policy{Layer: "layer1"}
	if !specific.IsLayerSpecific() {
		t.Error("expected IsLayerSpecific() to be true")
	}

	wildcard := Policy{Layer: "*"}
	if wildcard.IsLayerSpecific() {
		t.Error("expected IsLayerSpecific() to be false for '*'")
	}

	empty := Policy{Layer: ""}
	if empty.IsLayerSpecific() {
		t.Error("expected IsLayerSpecific() to be false for empty")
	}
}

func TestPolicyMatches(t *testing.T) {
	p := Policy{
		Role:      "admin",
		Workspace: "ws1",
		Layer:     "*",
		Action:    "read",
	}

	tests := []struct {
		role      string
		workspace string
		layer     string
		action    string
		expected  bool
	}{
		{"admin", "ws1", "layer1", "read", true},
		{"admin", "ws1", "layer2", "read", true},
		{"admin", "ws2", "layer1", "read", false},  // wrong workspace
		{"viewer", "ws1", "layer1", "read", false}, // wrong role
		{"admin", "ws1", "layer1", "write", false}, // wrong action
	}

	for _, tc := range tests {
		if got := p.Matches(tc.role, tc.workspace, tc.layer, tc.action); got != tc.expected {
			t.Errorf("Matches(%q, %q, %q, %q) = %v, want %v",
				tc.role, tc.workspace, tc.layer, tc.action, got, tc.expected)
		}
	}
}

func TestPolicyMatchesGlobal(t *testing.T) {
	p := Policy{
		Role:      "super_admin",
		Workspace: "*",
		Layer:     "*",
		Action:    "*",
	}

	tests := []struct {
		role      string
		workspace string
		layer     string
		action    string
		expected  bool
	}{
		{"super_admin", "any-ws", "any-layer", "read", true},
		{"super_admin", "other-ws", "other-layer", "write", true},
		{"other_role", "any-ws", "any-layer", "read", false},
	}

	for _, tc := range tests {
		if got := p.Matches(tc.role, tc.workspace, tc.layer, tc.action); got != tc.expected {
			t.Errorf("Matches(%q, %q, %q, %q) = %v, want %v",
				tc.role, tc.workspace, tc.layer, tc.action, got, tc.expected)
		}
	}
}

func TestMemoryAdapter_LoadPolicy(t *testing.T) {
	adapter := NewMemoryAdapter()

	// Add some policies manually
	adapter.AddPolicy("p", "p", []string{"admin", "ws1", "*", "read"})
	adapter.AddPolicy("p", "p", []string{"viewer", "ws1", "*", "read"})

	// Create a new enforcer and load policies
	e, err := NewEnforcerWithDefaults(adapter)
	if err != nil {
		t.Fatalf("failed to create enforcer: %v", err)
	}

	// Verify policies loaded
	policies, _ := e.GetAllPolicies()
	if len(policies) == 0 {
		t.Error("expected policies to be loaded")
	}
}

func TestMemoryAdapter_SavePolicy(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	// Add a custom policy
	e.AddWorkspacePolicy("test_role", "test-ws", ActionRead)

	// Save policies
	err := e.enforcer.SavePolicy()
	if err != nil {
		t.Fatalf("SavePolicy failed: %v", err)
	}

	// Create new enforcer with same adapter - policies should load
	e2, _ := NewEnforcerWithDefaults(adapter)
	policies, _ := e2.GetAllPolicies()
	if len(policies) == 0 {
		t.Error("expected saved policies to be reloaded")
	}
}

func TestMemoryAdapter_RemovePolicy(t *testing.T) {
	adapter := NewMemoryAdapter()

	// Add a policy
	adapter.AddPolicy("p", "p", []string{"test_role", "ws1", "*", "read"})

	// Remove the policy
	err := adapter.RemovePolicy("p", "p", []string{"test_role", "ws1", "*", "read"})
	if err != nil {
		t.Fatalf("RemovePolicy failed: %v", err)
	}
}

func TestMemoryAdapter_RemoveFilteredPolicy(t *testing.T) {
	adapter := NewMemoryAdapter()

	// Add multiple policies
	adapter.AddPolicy("p", "p", []string{"role1", "ws1", "*", "read"})
	adapter.AddPolicy("p", "p", []string{"role2", "ws1", "*", "read"})
	adapter.AddPolicy("p", "p", []string{"role1", "ws2", "*", "read"})

	// Remove all policies for ws1 (fieldIndex 1 = workspace)
	err := adapter.RemoveFilteredPolicy("p", "p", 1, "ws1")
	if err != nil {
		t.Fatalf("RemoveFilteredPolicy failed: %v", err)
	}
}

func TestCanReadLayer(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	tests := []struct {
		role      string
		workspace string
		layer     string
		expected  bool
	}{
		{RoleSuperAdmin, "ws1", "layer1", true},
		{RoleAdmin, "ws1", "layer1", true},
		{RoleEditor, "ws1", "layer1", true},
		{RoleViewer, "ws1", "layer1", true},
		{"unknown_role", "ws1", "layer1", false},
	}

	for _, tc := range tests {
		ok, err := e.CanReadLayer(tc.role, tc.workspace, tc.layer)
		if err != nil {
			t.Fatalf("CanReadLayer error: %v", err)
		}
		if ok != tc.expected {
			t.Errorf("%s CanReadLayer(%q, %q) = %v, want %v",
				tc.role, tc.workspace, tc.layer, ok, tc.expected)
		}
	}
}

func TestCanWriteLayer(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	tests := []struct {
		role      string
		workspace string
		layer     string
		expected  bool
	}{
		{RoleSuperAdmin, "ws1", "layer1", true},
		{RoleAdmin, "ws1", "layer1", true},
		{RoleEditor, "ws1", "layer1", true},
		{RoleViewer, "ws1", "layer1", false},
	}

	for _, tc := range tests {
		ok, err := e.CanWriteLayer(tc.role, tc.workspace, tc.layer)
		if err != nil {
			t.Fatalf("CanWriteLayer error: %v", err)
		}
		if ok != tc.expected {
			t.Errorf("%s CanWriteLayer(%q, %q) = %v, want %v",
				tc.role, tc.workspace, tc.layer, ok, tc.expected)
		}
	}
}

func TestCanAccessDelete(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	tests := []struct {
		role      string
		workspace string
		layer     string
		expected  bool
	}{
		{RoleSuperAdmin, "ws1", "layer1", true},
		{RoleAdmin, "ws1", "layer1", true},
		{RoleEditor, "ws1", "layer1", false},
		{RoleViewer, "ws1", "layer1", false},
	}

	for _, tc := range tests {
		ok, err := e.CanAccess(tc.role, tc.workspace, tc.layer, ActionDelete)
		if err != nil {
			t.Fatalf("CanAccess (delete) error: %v", err)
		}
		if ok != tc.expected {
			t.Errorf("%s CanAccess(%q, %q, delete) = %v, want %v",
				tc.role, tc.workspace, tc.layer, ok, tc.expected)
		}
	}
}

func TestRoleConstants(t *testing.T) {
	if RoleSuperAdmin != "super_admin" {
		t.Errorf("RoleSuperAdmin = %q, want 'super_admin'", RoleSuperAdmin)
	}
	if RoleAdmin != "admin" {
		t.Errorf("RoleAdmin = %q, want 'admin'", RoleAdmin)
	}
	if RoleEditor != "editor" {
		t.Errorf("RoleEditor = %q, want 'editor'", RoleEditor)
	}
	if RoleViewer != "viewer" {
		t.Errorf("RoleViewer = %q, want 'viewer'", RoleViewer)
	}
}

func TestActionConstants(t *testing.T) {
	if ActionRead != "read" {
		t.Errorf("ActionRead = %q, want 'read'", ActionRead)
	}
	if ActionWrite != "write" {
		t.Errorf("ActionWrite = %q, want 'write'", ActionWrite)
	}
	if ActionDelete != "delete" {
		t.Errorf("ActionDelete = %q, want 'delete'", ActionDelete)
	}
	if ActionManage != "manage" {
		t.Errorf("ActionManage = %q, want 'manage'", ActionManage)
	}
}

func TestParsePolicyPartial(t *testing.T) {
	// Test with fewer elements
	p := ParsePolicy([]string{"admin", "ws1"})
	if p.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", p.Role)
	}
	if p.Workspace != "ws1" {
		t.Errorf("expected workspace 'ws1', got %q", p.Workspace)
	}
	if p.Layer != "" {
		t.Errorf("expected empty layer, got %q", p.Layer)
	}
	if p.Action != "" {
		t.Errorf("expected empty action, got %q", p.Action)
	}
}

func TestParsePolicyEmpty(t *testing.T) {
	p := ParsePolicy([]string{})
	if p.Role != "" {
		t.Errorf("expected empty role, got %q", p.Role)
	}
}

func TestNewEnforcer(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, err := NewEnforcer(adapter)
	if err != nil {
		t.Fatalf("NewEnforcer failed: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil enforcer")
	}
}

func TestEnforcer_AddPolicies(t *testing.T) {
	adapter := NewMemoryAdapter()
	e, _ := NewEnforcerWithDefaults(adapter)

	// Add multiple policies for a custom role
	role := "custom_role"
	workspace := "custom-ws"

	// Add workspace policy
	if err := e.AddWorkspacePolicy(role, workspace, ActionRead); err != nil {
		t.Fatalf("AddWorkspacePolicy failed: %v", err)
	}
	if err := e.AddWorkspacePolicy(role, workspace, ActionWrite); err != nil {
		t.Fatalf("AddWorkspacePolicy failed: %v", err)
	}

	// Check access
	ok, _ := e.CanAccess(role, workspace, "*", ActionRead)
	if !ok {
		t.Error("expected read access after adding policy")
	}
	ok, _ = e.CanAccess(role, workspace, "*", ActionWrite)
	if !ok {
		t.Error("expected write access after adding policy")
	}
}

func TestMemoryAdapter_MatchRule(t *testing.T) {
	adapter := NewMemoryAdapter()

	// Test matching rules
	if !adapter.matchRule([]string{"a", "b", "c"}, []string{"a", "b", "c"}) {
		t.Error("expected rules to match")
	}

	// Test non-matching (different length)
	if adapter.matchRule([]string{"a", "b"}, []string{"a", "b", "c"}) {
		t.Error("expected rules not to match (different length)")
	}

	// Test non-matching (different values)
	if adapter.matchRule([]string{"a", "b", "c"}, []string{"a", "x", "c"}) {
		t.Error("expected rules not to match (different values)")
	}
}
