package rbac

import (
	"github.com/tobilg/neoserver/internal/httputil"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/workspace"
)

// MiddlewareConfig configures the RBAC middleware.
type MiddlewareConfig struct {
	Enforcer *Enforcer
	Logger   *slog.Logger
}

// RequireWorkspaceRead creates middleware that requires read access to the workspace.
func RequireWorkspaceRead(enforcer *Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := identity.FromContext(r.Context())
			if !ok || id == nil {
				httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
				return
			}

			ws, ok := workspace.FromContext(r.Context())
			if !ok || ws == nil {
				httputil.HTTPError(w, r, "workspace not found", http.StatusNotFound)
				return
			}

			// Get the role for this workspace
			role := id.GetWorkspaceRole(ws.ID)
			if role == "" {
				httputil.HTTPError(w, r, "forbidden: no access to workspace", http.StatusForbidden)
				return
			}

			// Check if the role has read access
			allowed, err := enforcer.CanAccessWorkspace(role, ws.ID)
			if err != nil {
				httputil.HTTPError(w, r, "internal error checking permissions", http.StatusInternalServerError)
				return
			}

			if !allowed {
				httputil.HTTPError(w, r, "forbidden: insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireLayerRead creates middleware that requires read access to a layer.
func RequireLayerRead(enforcer *Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := identity.FromContext(r.Context())
			if !ok || id == nil {
				httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
				return
			}

			ws, ok := workspace.FromContext(r.Context())
			if !ok || ws == nil {
				httputil.HTTPError(w, r, "workspace not found", http.StatusNotFound)
				return
			}

			layer, ok := workspace.LayerFromContext(r.Context())
			if !ok || layer == nil {
				// No layer in context, fall back to workspace check
				next.ServeHTTP(w, r)
				return
			}

			// Get the role for this workspace
			role := id.GetWorkspaceRole(ws.ID)
			if role == "" {
				httputil.HTTPError(w, r, "forbidden: no access to workspace", http.StatusForbidden)
				return
			}

			// Check if the role has read access to the layer
			allowed, err := enforcer.CanReadLayer(role, ws.ID, layer.ID)
			if err != nil {
				httputil.HTTPError(w, r, "internal error checking permissions", http.StatusInternalServerError)
				return
			}

			if !allowed {
				httputil.HTTPError(w, r, "forbidden: no access to layer", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireWorkspaceAdmin creates middleware that requires admin access to the workspace.
func RequireWorkspaceAdmin(enforcer *Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := identity.FromContext(r.Context())
			if !ok || id == nil {
				httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
				return
			}

			// Super admin has access to everything
			if id.IsSuperAdmin() {
				next.ServeHTTP(w, r)
				return
			}

			// Get workspace from context or URL param
			var workspaceID string
			if ws, ok := workspace.FromContext(r.Context()); ok && ws != nil {
				workspaceID = ws.ID
			} else {
				workspaceID = chi.URLParam(r, "workspace")
				if workspaceID == "" {
					workspaceID = chi.URLParam(r, "ws")
				}
			}

			if workspaceID == "" {
				httputil.HTTPError(w, r, "workspace not specified", http.StatusBadRequest)
				return
			}

			// Get the role for this workspace
			role := id.GetWorkspaceRole(workspaceID)
			if role == "" {
				httputil.HTTPError(w, r, "forbidden: no access to workspace", http.StatusForbidden)
				return
			}

			// Check if the role can manage the workspace
			allowed, err := enforcer.CanManageWorkspace(role, workspaceID)
			if err != nil {
				httputil.HTTPError(w, r, "internal error checking permissions", http.StatusInternalServerError)
				return
			}

			if !allowed {
				httputil.HTTPError(w, r, "forbidden: admin access required", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireSuperAdmin creates middleware that requires super admin access.
func RequireSuperAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := identity.FromContext(r.Context())
			if !ok || id == nil {
				httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
				return
			}

			if !id.IsSuperAdmin() {
				httputil.HTTPError(w, r, "forbidden: super admin required", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAction creates middleware that requires a specific action permission.
func RequireAction(enforcer *Enforcer, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := identity.FromContext(r.Context())
			if !ok || id == nil {
				httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
				return
			}

			// Super admin bypasses all checks
			if id.IsSuperAdmin() {
				next.ServeHTTP(w, r)
				return
			}

			ws, ok := workspace.FromContext(r.Context())
			if !ok || ws == nil {
				httputil.HTTPError(w, r, "workspace not found", http.StatusNotFound)
				return
			}

			// Get the role for this workspace
			role := id.GetWorkspaceRole(ws.ID)
			if role == "" {
				httputil.HTTPError(w, r, "forbidden: no access to workspace", http.StatusForbidden)
				return
			}

			// Determine layer ID if present
			layerID := "*"
			if layer, ok := workspace.LayerFromContext(r.Context()); ok && layer != nil {
				layerID = layer.ID
			}

			// Check permission
			allowed, err := enforcer.CanAccess(role, ws.ID, layerID, action)
			if err != nil {
				httputil.HTTPError(w, r, "internal error checking permissions", http.StatusInternalServerError)
				return
			}

			if !allowed {
				httputil.HTTPError(w, r, "forbidden: insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// OptionalAuth creates middleware that extracts identity but doesn't require it.
// Useful for endpoints that may be public or authenticated.
func OptionalAuth(enforcer *Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Just pass through - identity extraction happens earlier in the chain
			next.ServeHTTP(w, r)
		})
	}
}
