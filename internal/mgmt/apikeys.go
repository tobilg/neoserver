package mgmt

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
)

// resolveWorkspaceIDForAPIKeys resolves a workspace identifier (name or ID) to actual workspace ID.
func (h *handler) resolveWorkspaceIDForAPIKeys(ctx context.Context, identifier string) (string, error) {
	// First try to get by ID
	ws, err := h.store.GetWorkspace(ctx, identifier)
	if err == nil {
		return ws.ID, nil
	}

	// If not found by ID, try by name
	if err == store.ErrNotFound {
		ws, err = h.store.GetWorkspaceByName(ctx, identifier)
		if err == nil {
			return ws.ID, nil
		}
	}

	return "", err
}

// APIKeyResponse is the API response for an API key (without the secret).
type APIKeyResponse struct {
	ID          string     `json:"id"`
	KeyPrefix   string     `json:"key_prefix"`
	WorkspaceID *string    `json:"workspace_id,omitempty"`
	RoleID      string     `json:"role_id"`
	Name        string     `json:"name"`
	OwnerName   string     `json:"owner_name,omitempty"`
	OwnerEmail  string     `json:"owner_email,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Revoked     bool       `json:"revoked"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CreateAPIKeyRequest is the request to create an API key.
type CreateAPIKeyRequest struct {
	Name       string     `json:"name"`
	RoleID     string     `json:"role_id"`
	OwnerName  string     `json:"owner_name,omitempty"`
	OwnerEmail string     `json:"owner_email,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// CreateAPIKeyResponse includes the full key (only returned on creation).
type CreateAPIKeyResponse struct {
	APIKeyResponse
	Key string `json:"key"` // Full API key - only returned on creation
}

// listAPIKeys handles GET /workspaces/{workspace}/apikeys
func (h *handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForAPIKeys(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	keys, err := h.store.ListAPIKeys(ctx, &workspaceID)
	if err != nil {
		h.logger.Error("failed to list API keys", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list API keys")
		return
	}

	var result []APIKeyResponse
	for _, k := range keys {
		result = append(result, APIKeyResponse{
			ID:          k.ID,
			KeyPrefix:   k.KeyPrefix,
			WorkspaceID: k.WorkspaceID,
			RoleID:      k.RoleID,
			Name:        k.Name,
			OwnerName:   k.OwnerName,
			OwnerEmail:  k.OwnerEmail,
			ExpiresAt:   k.ExpiresAt,
			Revoked:     k.Revoked,
			CreatedAt:   k.CreatedAt,
		})
	}

	if result == nil {
		result = []APIKeyResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"api_keys": result})
}

// createAPIKey handles POST /workspaces/{workspace}/apikeys
func (h *handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForAPIKeys(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req CreateAPIKeyRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	if req.RoleID == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", h.roleIDRequiredMessage(r.Context()))
		return
	}

	// Validate role exists
	roles, err := h.store.ListRoles(ctx)
	if err != nil {
		h.logger.Error("failed to list roles", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to validate role")
		return
	}
	roleValid := false
	for _, role := range roles {
		if role.ID == req.RoleID {
			roleValid = true
			break
		}
	}
	if !roleValid {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid role_id")
		return
	}

	// Least privilege: a caller may not grant a role more privileged than their own.
	// Only global super admins can mint super_admin keys.
	if req.RoleID == "super_admin" {
		caller, ok := identity.FromContext(ctx)
		if !ok || caller == nil || !caller.IsSuperAdmin() {
			writeError(w, http.StatusForbidden, "Forbidden", "only super admins can grant the super_admin role")
			return
		}
	}

	input := store.CreateAPIKeyInput{
		OwnerName:   req.OwnerName,
		OwnerEmail:  req.OwnerEmail,
		WorkspaceID: &workspaceID,
		RoleID:      req.RoleID,
		Name:        req.Name,
		ExpiresAt:   req.ExpiresAt,
	}

	output, err := h.store.CreateAPIKey(ctx, input)
	if err != nil {
		h.logger.Error("failed to create API key", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to create API key")
		return
	}

	writeJSON(w, http.StatusCreated, CreateAPIKeyResponse{
		APIKeyResponse: APIKeyResponse{
			ID:          output.ID,
			KeyPrefix:   output.KeyPrefix,
			WorkspaceID: output.WorkspaceID,
			RoleID:      output.RoleID,
			Name:        output.Name,
			OwnerName:   output.OwnerName,
			OwnerEmail:  output.OwnerEmail,
			ExpiresAt:   output.ExpiresAt,
			Revoked:     output.Revoked,
			CreatedAt:   output.CreatedAt,
		},
		Key: output.Key,
	})
}

// getAPIKey handles GET /workspaces/{workspace}/apikeys/{keyId}
// Note: This returns limited info since we only store the key hash
func (h *handler) getAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	keyID := chi.URLParam(r, "keyId")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForAPIKeys(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	// List all keys and find by ID
	keys, err := h.store.ListAPIKeys(ctx, &workspaceID)
	if err != nil {
		h.logger.Error("failed to list API keys", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get API key")
		return
	}

	for _, k := range keys {
		if k.ID == keyID {
			writeJSON(w, http.StatusOK, APIKeyResponse{
				ID:          k.ID,
				KeyPrefix:   k.KeyPrefix,
				WorkspaceID: k.WorkspaceID,
				RoleID:      k.RoleID,
				Name:        k.Name,
				OwnerName:   k.OwnerName,
				OwnerEmail:  k.OwnerEmail,
				ExpiresAt:   k.ExpiresAt,
				Revoked:     k.Revoked,
				CreatedAt:   k.CreatedAt,
			})
			return
		}
	}

	writeError(w, http.StatusNotFound, "Not Found", "API key not found")
}

// revokeAPIKey handles DELETE /workspaces/{workspace}/apikeys/{keyId}
func (h *handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	keyID := chi.URLParam(r, "keyId")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForAPIKeys(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	// Ensure the key belongs to this workspace before revoking. Without this check a
	// workspace admin could revoke keys in other workspaces (or global keys) by ID.
	keys, err := h.store.ListAPIKeys(ctx, &workspaceID)
	if err != nil {
		h.logger.Error("failed to list API keys", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to revoke API key")
		return
	}
	found := false
	for _, k := range keys {
		if k.ID == keyID {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "Not Found", "API key not found")
		return
	}

	if err := h.store.RevokeAPIKey(ctx, keyID); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "API key not found")
			return
		}
		h.logger.Error("failed to revoke API key", "key", keyID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to revoke API key")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// deleteAPIKey handles DELETE /workspaces/{workspace}/apikeys/{keyId}/permanent.
// Only revoked keys can be removed, so an active credential is never deleted
// in a single step.
func (h *handler) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID, err := h.resolveWorkspaceIDForAPIKeys(ctx, chi.URLParam(r, "workspace"))
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}
	keyID := chi.URLParam(r, "keyId")
	// Only keys of this workspace may be deleted through its route.
	keys, err := h.store.ListAPIKeys(ctx, &workspaceID)
	if err != nil {
		h.logger.Error("failed to list API keys", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete API key")
		return
	}
	found := false
	for _, k := range keys {
		if k.ID == keyID {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "Not Found", "API key not found")
		return
	}
	switch err := h.store.DeleteAPIKey(ctx, keyID); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not Found", "API key not found")
	case errors.Is(err, store.ErrAPIKeyNotRevoked):
		writeError(w, http.StatusConflict, "Conflict", "revoke the API key before deleting it")
	default:
		h.logger.Error("failed to delete API key", "key", keyID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete API key")
	}
}

// roleIDRequiredMessage names the accepted role IDs, so a client that sent
// "role" instead of "role_id" can correct the request.
func (h *handler) roleIDRequiredMessage(ctx context.Context) string {
	roles, err := h.store.ListRoles(ctx)
	if err != nil || len(roles) == 0 {
		return "role_id is required"
	}
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		ids = append(ids, role.ID)
	}
	sort.Strings(ids)
	return "role_id is required (one of: " + strings.Join(ids, ", ") + ")"
}
