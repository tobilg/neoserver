// Package rbac provides role-based access control using Casbin.
package rbac

import (
	"fmt"
	"strings"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

// RBAC model for workspace and layer access control.
// Request: sub (subject/role), ws (workspace), layer, act (action)
// Policy: sub (role), ws (workspace), layer, act (action)
const modelConf = `
[request_definition]
r = sub, ws, layer, act

[policy_definition]
p = sub, ws, layer, act

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && (p.ws == "*" || p.ws == r.ws) && (p.layer == "*" || p.layer == r.layer) && (p.act == "*" || p.act == r.act)
`

// Actions for RBAC checks.
const (
	ActionRead   = "read"
	ActionWrite  = "write"
	ActionDelete = "delete"
	ActionManage = "manage"
)

// Roles defines the built-in role names.
const (
	RoleSuperAdmin = "super_admin"
	RoleAdmin      = "admin"
	RoleEditor     = "editor"
	RoleViewer     = "viewer"
)

// Enforcer wraps the Casbin enforcer with convenience methods.
type Enforcer struct {
	mu       sync.RWMutex
	loadErr  error
	enforcer *casbin.Enforcer
}

// NewEnforcer creates a new RBAC enforcer with the given adapter.
func NewEnforcer(adapter Adapter) (*Enforcer, error) {
	m, err := model.NewModelFromString(modelConf)
	if err != nil {
		return nil, fmt.Errorf("failed to create model: %w", err)
	}

	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create enforcer: %w", err)
	}

	// Load policies from the adapter
	if err := e.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("failed to load policies: %w", err)
	}

	// Persist complete snapshots transactionally; never expose a partial
	// mutation to concurrent authorization requests.
	e.EnableAutoSave(false)
	return &Enforcer{enforcer: e}, nil
}

// NewEnforcerWithDefaults creates an enforcer and adds default policies.
func NewEnforcerWithDefaults(adapter Adapter) (*Enforcer, error) {
	e, err := NewEnforcer(adapter)
	if err != nil {
		return nil, err
	}

	// Add default policies for built-in roles
	if err := e.AddDefaultPolicies(); err != nil {
		return nil, fmt.Errorf("failed to add default policies: %w", err)
	}

	return e, nil
}

// AddDefaultPolicies adds the default role policies.
func (e *Enforcer) AddDefaultPolicies() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadErr != nil {
		return e.loadErr
	}

	// super_admin has global access to everything
	if _, err := e.enforcer.AddPolicy(RoleSuperAdmin, "*", "*", "*"); err != nil {
		return err
	}

	// admin has full access within their workspace
	if _, err := e.enforcer.AddPolicy(RoleAdmin, "*", "*", ActionManage); err != nil {
		return err
	}
	if _, err := e.enforcer.AddPolicy(RoleAdmin, "*", "*", ActionRead); err != nil {
		return err
	}
	if _, err := e.enforcer.AddPolicy(RoleAdmin, "*", "*", ActionWrite); err != nil {
		return err
	}
	if _, err := e.enforcer.AddPolicy(RoleAdmin, "*", "*", ActionDelete); err != nil {
		return err
	}

	// editor can read and write
	if _, err := e.enforcer.AddPolicy(RoleEditor, "*", "*", ActionRead); err != nil {
		return err
	}
	if _, err := e.enforcer.AddPolicy(RoleEditor, "*", "*", ActionWrite); err != nil {
		return err
	}

	// viewer can only read
	if _, err := e.enforcer.AddPolicy(RoleViewer, "*", "*", ActionRead); err != nil {
		return err
	}

	return e.persist()
}

// CanAccess checks if a role can perform an action on a workspace/layer.
func (e *Enforcer) CanAccess(role, workspace, layer, action string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.loadErr != nil {
		return false, e.loadErr
	}

	return e.enforcer.Enforce(role, workspace, layer, action)
}

// CanAccessOperation resolves an operation grant from most-specific to broad:
// operation:<service>:<operation>, service:<service>, then the existing
// workspace/resource wildcard. This keeps one Casbin policy model for layers,
// services, and protocol operations.
func (e *Enforcer) CanAccessOperation(role, workspace, service, operation, action string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.loadErr != nil {
		return false, e.loadErr
	}

	resources := []string{"operation:" + strings.ToLower(service) + ":" + strings.ToUpper(operation), "service:" + strings.ToLower(service), "*"}
	for _, resource := range resources {
		allowed, err := e.enforcer.Enforce(role, workspace, resource, action)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

func (e *Enforcer) AddOperationPolicy(role, workspace, service, operation, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadErr != nil {
		return e.loadErr
	}

	resource := "service:" + strings.ToLower(service)
	if strings.TrimSpace(operation) != "" && operation != "*" {
		resource = "operation:" + strings.ToLower(service) + ":" + strings.ToUpper(operation)
	}
	_, err := e.enforcer.AddPolicy(role, workspace, resource, action)
	if err != nil {
		return err
	}
	return e.persist()
}

func (e *Enforcer) RemoveOperationPolicy(role, workspace, service, operation, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadErr != nil {
		return e.loadErr
	}

	resource := "service:" + strings.ToLower(service)
	if strings.TrimSpace(operation) != "" && operation != "*" {
		resource = "operation:" + strings.ToLower(service) + ":" + strings.ToUpper(operation)
	}
	_, err := e.enforcer.RemovePolicy(role, workspace, resource, action)
	if err != nil {
		return err
	}
	return e.persist()
}

// CanAccessWorkspace checks if a role can access a workspace (any layer, read action).
func (e *Enforcer) CanAccessWorkspace(role, workspace string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.loadErr != nil {
		return false, e.loadErr
	}

	return e.enforcer.Enforce(role, workspace, "*", ActionRead)
}

// CanReadLayer checks if a role can read a specific layer.
func (e *Enforcer) CanReadLayer(role, workspace, layer string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.loadErr != nil {
		return false, e.loadErr
	}

	return e.enforcer.Enforce(role, workspace, layer, ActionRead)
}

// CanWriteLayer checks if a role can write to a specific layer.
func (e *Enforcer) CanWriteLayer(role, workspace, layer string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.loadErr != nil {
		return false, e.loadErr
	}

	return e.enforcer.Enforce(role, workspace, layer, ActionWrite)
}

// CanManageWorkspace checks if a role can manage a workspace.
func (e *Enforcer) CanManageWorkspace(role, workspace string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.loadErr != nil {
		return false, e.loadErr
	}

	return e.enforcer.Enforce(role, workspace, "*", ActionManage)
}

// AddWorkspacePolicy adds a policy for a role in a workspace.
func (e *Enforcer) AddWorkspacePolicy(role, workspace, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadErr != nil {
		return e.loadErr
	}

	_, err := e.enforcer.AddPolicy(role, workspace, "*", action)
	if err != nil {
		return err
	}
	return e.persist()
}

// AddLayerPolicy adds a policy for a role on a specific layer.
func (e *Enforcer) AddLayerPolicy(role, workspace, layer, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadErr != nil {
		return e.loadErr
	}

	_, err := e.enforcer.AddPolicy(role, workspace, layer, action)
	if err != nil {
		return err
	}
	return e.persist()
}

// RemoveWorkspacePolicy removes a workspace policy.
func (e *Enforcer) RemoveWorkspacePolicy(role, workspace, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadErr != nil {
		return e.loadErr
	}

	_, err := e.enforcer.RemovePolicy(role, workspace, "*", action)
	if err != nil {
		return err
	}
	return e.persist()
}

// RemoveLayerPolicy removes a layer policy.
func (e *Enforcer) RemoveLayerPolicy(role, workspace, layer, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadErr != nil {
		return e.loadErr
	}

	_, err := e.enforcer.RemovePolicy(role, workspace, layer, action)
	if err != nil {
		return err
	}
	return e.persist()
}

// RemoveAllWorkspacePolicies removes all policies for a workspace.
func (e *Enforcer) RemoveAllWorkspacePolicies(workspace string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadErr != nil {
		return e.loadErr
	}

	policies, err := e.enforcer.GetFilteredPolicy(1, workspace)
	if err != nil {
		return err
	}
	for _, p := range policies {
		if _, err := e.enforcer.RemovePolicy(p); err != nil {
			return err
		}
	}
	return e.persist()
}

// RemoveResourcePolicies removes only policies targeting the supplied public
// resource names, preserving unrelated workspace grants during service
// deletion.
func (e *Enforcer) RemoveResourcePolicies(workspace string, resources []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loadErr != nil {
		return e.loadErr
	}

	wanted := make(map[string]bool, len(resources))
	for _, resource := range resources {
		wanted[resource] = true
	}
	policies, err := e.enforcer.GetFilteredPolicy(1, workspace)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		if len(policy) > 2 && wanted[policy[2]] {
			if _, err := e.enforcer.RemovePolicy(policy); err != nil {
				return err
			}
		}
	}
	return e.persist()
}

// GetPoliciesForWorkspace returns all policies for a workspace.
func (e *Enforcer) GetPoliciesForWorkspace(workspace string) ([][]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.loadErr != nil {
		return nil, e.loadErr
	}

	return e.enforcer.GetFilteredPolicy(1, workspace)
}

// GetPoliciesForRole returns all policies for a role.
func (e *Enforcer) GetPoliciesForRole(role string) ([][]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.loadErr != nil {
		return nil, e.loadErr
	}

	return e.enforcer.GetFilteredPolicy(0, role)
}

// Reload reloads policies from the adapter.
func (e *Enforcer) Reload() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadErr = e.enforcer.LoadPolicy()
	return e.loadErr
}

// persist is called with the write lock held. Failed writes restore the
// durable snapshot; if reloading fails, authorization remains fail-closed.
func (e *Enforcer) persist() error {
	if err := e.enforcer.SavePolicy(); err != nil {
		e.loadErr = e.enforcer.LoadPolicy()
		return err
	}
	return nil
}

// DeleteRole coordinates durable role deletion and live policy replacement.
func (e *Enforcer) DeleteRole(remove func() error) error {
	return e.UpdatePolicyStore(remove)
}

// UpdatePolicyStore serializes an atomic catalog mutation and replacement of
// the live policy snapshot. Failed reloads leave all authorization fail-closed.
func (e *Enforcer) UpdatePolicyStore(remove func() error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := remove(); err != nil {
		return err
	}
	e.loadErr = e.enforcer.LoadPolicy()
	return e.loadErr
}

// GetAllPolicies returns all policies.
func (e *Enforcer) GetAllPolicies() ([][]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.loadErr != nil {
		return nil, e.loadErr
	}

	return e.enforcer.GetPolicy()
}

// Policy represents a single RBAC policy.
type Policy struct {
	Role      string
	Workspace string
	Layer     string
	Action    string
}

// ParsePolicy parses a policy from string slice.
func ParsePolicy(p []string) Policy {
	policy := Policy{}
	if len(p) > 0 {
		policy.Role = p[0]
	}
	if len(p) > 1 {
		policy.Workspace = p[1]
	}
	if len(p) > 2 {
		policy.Layer = p[2]
	}
	if len(p) > 3 {
		policy.Action = p[3]
	}
	return policy
}

// String returns the policy as a readable string.
func (p Policy) String() string {
	return fmt.Sprintf("%s can %s %s/%s", p.Role, p.Action, p.Workspace, p.Layer)
}

// IsGlobal returns true if the policy applies globally.
func (p Policy) IsGlobal() bool {
	return p.Workspace == "*"
}

// IsLayerSpecific returns true if the policy targets a specific layer.
func (p Policy) IsLayerSpecific() bool {
	return p.Layer != "*" && p.Layer != ""
}

// Matches checks if this policy matches the given criteria.
func (p Policy) Matches(role, workspace, layer, action string) bool {
	if p.Role != role {
		return false
	}
	if p.Workspace != "*" && p.Workspace != workspace {
		return false
	}
	if p.Layer != "*" && p.Layer != layer {
		return false
	}
	if p.Action != "*" && !strings.EqualFold(p.Action, action) {
		return false
	}
	return true
}
