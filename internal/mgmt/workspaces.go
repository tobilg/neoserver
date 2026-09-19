package mgmt

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
)

// resolveWorkspaceIDForWorkspaces resolves a workspace identifier (name or ID) to actual workspace ID.
func (h *handler) resolveWorkspaceIDForWorkspaces(ctx context.Context, identifier string) (string, error) {
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

// WorkspaceResponse is the API response for a workspace.
type WorkspaceResponse struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
	Counts      store.WorkspaceCounts `json:"counts"`
}

// CreateWorkspaceRequest is the request to create a workspace.
type CreateWorkspaceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// UpdateWorkspaceRequest is the request to update a workspace.
type UpdateWorkspaceRequest struct {
	Name        string  `json:"name,omitempty"`
	Description *string `json:"description,omitempty"` // omitted/null leaves unchanged; empty clears
}

// listWorkspaces handles GET /workspaces
func (h *handler) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, ok := identity.FromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized", "authentication required")
		return
	}
	if !id.IsSuperAdmin() {
		writeError(w, http.StatusForbidden, "Forbidden", "super administrator required")
		return
	}

	workspaces, err := h.store.ListWorkspaces(ctx)
	if err != nil {
		h.logger.Error("failed to list workspaces", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list workspaces")
		return
	}

	counts := map[string]store.WorkspaceCounts{}
	if countStore, ok := h.store.(store.WorkspaceCountStore); ok {
		counts, _ = countStore.ListWorkspaceCounts(ctx)
	}
	var result []WorkspaceResponse
	for _, ws := range workspaces {
		result = append(result, WorkspaceResponse{
			ID: ws.ID, Name: ws.Name, Description: ws.Description,
			CreatedAt: ws.CreatedAt, UpdatedAt: ws.UpdatedAt, Counts: counts[ws.ID],
		})
	}

	if result == nil {
		result = []WorkspaceResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"workspaces": result})
}

// createWorkspace handles POST /workspaces
func (h *handler) createWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateWorkspaceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	if !validWorkspaceName(req.Name) {
		writeError(w, http.StatusBadRequest, "Bad Request", workspaceNameRule)
		return
	}

	input := store.CreateWorkspaceInput{
		Name:        req.Name,
		Description: req.Description,
	}

	// Use registry.CreateWorkspace to update both store AND runtime registry
	ws, err := h.registry.CreateWorkspace(ctx, input)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, http.StatusConflict, "Conflict", "a workspace with this name already exists")
			return
		}
		h.logger.Error("failed to create workspace", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to create workspace")
		return
	}

	response := WorkspaceResponse{ID: ws.ID, Name: ws.Name, Description: ws.Description}
	if stored, err := h.store.GetWorkspace(ctx, ws.ID); err == nil {
		response.CreatedAt, response.UpdatedAt = stored.CreatedAt, stored.UpdatedAt
	} else {
		h.logger.Warn("failed to read created workspace timestamps", "workspace", ws.ID, "error", err)
	}
	writeJSON(w, http.StatusCreated, response)
}

// getWorkspace handles GET /workspaces/{workspace}
func (h *handler) getWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForWorkspaces(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	ws, err := h.store.GetWorkspace(ctx, workspaceID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to get workspace", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get workspace")
		return
	}

	writeJSON(w, http.StatusOK, WorkspaceResponse{
		ID:          ws.ID,
		Name:        ws.Name,
		Description: ws.Description,
		CreatedAt:   ws.CreatedAt,
		UpdatedAt:   ws.UpdatedAt,
	})
}

// updateWorkspace handles PUT /workspaces/{workspace}
func (h *handler) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForWorkspaces(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req UpdateWorkspaceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	input := store.UpdateWorkspaceInput{}
	if req.Name != "" {
		if !validWorkspaceName(req.Name) {
			writeError(w, http.StatusBadRequest, "Bad Request", workspaceNameRule)
			return
		}
		input.Name = &req.Name
	}
	input.Description = req.Description

	// Use registry.UpdateWorkspace to update both store AND runtime registry
	ws, err := h.registry.UpdateWorkspace(ctx, workspaceID, input)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, http.StatusConflict, "Conflict", "a workspace with this name already exists")
			return
		}
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to update workspace", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update workspace")
		return
	}

	stored, err := h.store.GetWorkspace(ctx, ws.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to read updated workspace")
		return
	}
	writeJSON(w, http.StatusOK, WorkspaceResponse{
		ID:          ws.ID,
		Name:        ws.Name,
		Description: ws.Description,
		CreatedAt:   stored.CreatedAt,
		UpdatedAt:   stored.UpdatedAt,
	})
}

// deleteWorkspace handles DELETE /workspaces/{workspace}
func (h *handler) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForWorkspaces(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	recursive, err := recursiveDeleteRequested(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if h.lifecycle != nil {
		operation, err := h.lifecycle.DeleteWorkspace(ctx, workspaceID, recursive)
		if err != nil {
			if h.writeDeletionConflict(w, err) {
				return
			}
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
				return
			}
			h.logger.Error("failed to delete workspace", "workspace", workspaceID, "error", err)
			writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete workspace")
			return
		}
		if operation != nil {
			h.writeDeletionAccepted(w, r, operation)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := h.store.DeleteWorkspace(ctx, workspaceID); err != nil {
		if h.writeDeletionConflict(w, err) {
			return
		}
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to delete workspace", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete workspace")
		return
	}

	// Unregister from runtime registry
	h.registry.Remove(workspaceID)

	// Remove RBAC policies
	if err := h.enforcer.RemoveAllWorkspacePolicies(workspaceID); err != nil {
		h.logger.Warn("failed to remove workspace policies", "workspace", workspaceID, "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// workspaceNameRule explains validWorkspaceName; names are public URL segments.
const workspaceNameRule = "workspace name must start with a letter or digit and contain only letters, digits, '.', '_' or '-' (at most 64 characters)"

var workspaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validWorkspaceName(name string) bool { return workspaceNamePattern.MatchString(name) }
