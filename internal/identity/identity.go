// Package identity provides authentication and identity extraction for neoserver.
package identity

import (
	"context"
	"time"
)

// Identity represents an authenticated user with their workspace roles.
type Identity struct {
	// Subject is the user identifier (from sub claim, API key owner, etc.)
	Subject string

	// Email is the user's email address if available.
	Email string

	// DisplayName is a human-friendly name from the authenticating credential.
	DisplayName string

	// AuthMethod indicates how the user authenticated.
	AuthMethod AuthMethod

	// Roles maps workspace ID to role ID.
	// "*" key indicates global roles (e.g., super_admin).
	Roles map[string]string

	// Claims contains the raw claims from JWT/OIDC tokens.
	Claims map[string]interface{}

	// APIKeyID is set when authenticated via API key.
	APIKeyID string

	// SessionID is set only for server-side browser sessions.
	SessionID            string
	SessionExpiresAt     time.Time
	SessionIdleExpiresAt time.Time
	SessionTokenHash     string
	SessionPreviousToken bool
	SessionRotationUntil *time.Time
}

// AuthMethod indicates the authentication method used.
type AuthMethod string

const (
	AuthMethodNone      AuthMethod = "none"
	AuthMethodAPIKey    AuthMethod = "apikey"
	AuthMethodBasic     AuthMethod = "basic"
	AuthMethodJWT       AuthMethod = "jwt"
	AuthMethodOIDC      AuthMethod = "oidc"
	AuthMethodAnonymous AuthMethod = "anonymous"
)

// HasRole checks if the identity has a specific role for a workspace.
func (id *Identity) HasRole(workspaceID, role string) bool {
	if id == nil {
		return false
	}

	resolved := id.GetWorkspaceRole(workspaceID)
	return resolved != "" && (resolved == role || roleIncludes(resolved, role))
}

// HasWorkspaceAccess checks if the identity has any access to a workspace.
func (id *Identity) HasWorkspaceAccess(workspaceID string) bool {
	if id == nil {
		return false
	}

	return id.GetWorkspaceRole(workspaceID) != ""
}

// IsSuperAdmin checks if the identity has global super_admin role.
func (id *Identity) IsSuperAdmin() bool {
	if id == nil {
		return false
	}
	return id.Roles["*"] == "super_admin"
}

// IsAdmin checks if the identity has admin role for a workspace.
func (id *Identity) IsAdmin(workspaceID string) bool {
	return id.HasRole(workspaceID, "admin")
}

// CanRead checks if the identity can read from a workspace (viewer or higher).
func (id *Identity) CanRead(workspaceID string) bool {
	return id.HasRole(workspaceID, "viewer")
}

// CanWrite checks if the identity can write to a workspace (editor or higher).
func (id *Identity) CanWrite(workspaceID string) bool {
	return id.HasRole(workspaceID, "editor")
}

// GetWorkspaceRole returns the role for a specific workspace.
func (id *Identity) GetWorkspaceRole(workspaceID string) string {
	if id == nil {
		return ""
	}

	// Check global role first
	if globalRole, ok := id.Roles["*"]; ok && globalRole == "super_admin" {
		return "super_admin"
	}

	// A workspace assignment is an explicit override; otherwise global roles
	// inherit into the workspace. Only a global super_admin bypasses it.
	if role, ok := id.Roles[workspaceID]; ok {
		return role
	}
	return id.Roles["*"]
}

// roleIncludes checks if a role includes another role's permissions.
// Role hierarchy: super_admin > admin > editor > viewer
func roleIncludes(role, target string) bool {
	hierarchy := map[string]int{
		"super_admin": 4,
		"admin":       3,
		"editor":      2,
		"viewer":      1,
	}

	roleLevel := hierarchy[role]
	targetLevel := hierarchy[target]

	return roleLevel > 0 && targetLevel > 0 && roleLevel >= targetLevel
}

// Context key for identity
type contextKey int

const identityKey contextKey = iota

// WithIdentity adds an identity to the context.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	RecordAuditPrincipal(ctx, id)
	return context.WithValue(ctx, identityKey, id)
}

// FromContext returns the identity from the context.
func FromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(identityKey).(*Identity)
	return id, ok
}

// MustFromContext returns the identity from the context or panics.
func MustFromContext(ctx context.Context) *Identity {
	id, ok := FromContext(ctx)
	if !ok {
		panic("identity not found in context")
	}
	return id
}

// Anonymous returns an anonymous identity with no permissions.
func Anonymous() *Identity {
	return &Identity{
		Subject:    "anonymous",
		AuthMethod: AuthMethodAnonymous,
		Roles:      make(map[string]string),
	}
}
