package mgmt

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/protocolrequest"
	"github.com/tobilg/neoserver/internal/store"
)

type OperationPolicyRequest struct {
	Workspace string `json:"workspace"`
	Service   string `json:"service"`
	Operation string `json:"operation,omitempty"`
	Action    string `json:"action"`
}

// RoleResponse is the API response for a role.
type RoleResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateRoleRequest is the request to create a role.
type CreateRoleRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// listRoles handles GET /roles
func (h *handler) listRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	roles, err := h.store.ListRoles(ctx)
	if err != nil {
		h.logger.Error("failed to list roles", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list roles")
		return
	}

	var result []RoleResponse
	for _, role := range roles {
		result = append(result, RoleResponse{
			ID:          role.ID,
			Name:        role.Name,
			Description: role.Description,
			IsSystem:    role.IsSystem,
			CreatedAt:   role.CreatedAt,
		})
	}

	if result == nil {
		result = []RoleResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"roles": result})
}

// createRole handles POST /roles
func (h *handler) createRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateRoleRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "id is required")
		return
	}
	if req.Name == "" {
		req.Name = req.ID
	}

	input := store.CreateRoleInput{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
	}

	role, err := h.store.CreateRole(ctx, input)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, http.StatusConflict, "Role ID unavailable", "This ID already exists or is retired; choose a new ID")
			return
		}
		h.logger.Error("failed to create role", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to create role")
		return
	}

	writeJSON(w, http.StatusCreated, RoleResponse{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		IsSystem:    role.IsSystem,
		CreatedAt:   role.CreatedAt,
	})
}

// deleteRole handles DELETE /roles/{roleId}
func (h *handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roleID := chi.URLParam(r, "roleId")

	// Get role to check if it's a system role
	role, err := h.store.GetRole(ctx, roleID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "role not found")
			return
		}
		h.logger.Error("failed to get role", "role", roleID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get role")
		return
	}

	if role.IsSystem {
		writeError(w, http.StatusBadRequest, "Bad Request", "cannot delete system role")
		return
	}

	if err := h.enforcer.DeleteRole(func() error { return h.store.DeleteRole(ctx, roleID) }); err != nil {
		if errors.Is(err, store.ErrResourceNotEmpty) {
			writeError(w, http.StatusConflict, "Role is still assigned", err.Error())
			return
		}
		h.logger.Error("failed to delete role", "role", roleID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete role")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) roleDeletionPlan(w http.ResponseWriter, r *http.Request) {
	lookup, ok := h.store.(interface {
		RoleDependencies(context.Context, string) (map[string]int, error)
	})
	if !ok {
		writeError(w, 503, "Unavailable", "role dependency lookup unavailable")
		return
	}
	id := chi.URLParam(r, "roleId")
	role, err := h.store.GetRole(r.Context(), id)
	if err != nil {
		writeError(w, 404, "Not Found", "role not found")
		return
	}
	counts, err := lookup.RoleDependencies(r.Context(), id)
	if err != nil {
		writeError(w, 503, "Unavailable", "role dependency lookup failed")
		return
	}
	writeJSON(w, 200, map[string]any{"role_id": id, "is_system": role.IsSystem, "dependencies": counts})
}

func (h *handler) listRolePolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := h.enforcer.GetPoliciesForRole(chi.URLParam(r, "roleId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list role policies")
		return
	}
	if policies == nil {
		policies = [][]string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": policies})
}

func (h *handler) addRolePolicy(w http.ResponseWriter, r *http.Request) {
	h.changeRolePolicy(w, r, false)
}

func (h *handler) removeRolePolicy(w http.ResponseWriter, r *http.Request) {
	h.changeRolePolicy(w, r, true)
}

func (h *handler) changeRolePolicy(w http.ResponseWriter, r *http.Request, remove bool) {
	roleID := chi.URLParam(r, "roleId")
	if _, err := h.store.GetRole(r.Context(), roleID); err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "role not found")
		return
	}
	var request OperationPolicyRequest
	if readJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid policy")
		return
	}
	request.Workspace = strings.TrimSpace(request.Workspace)
	request.Service = strings.ToLower(strings.TrimSpace(request.Service))
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	services := []string{"ogcapi", "wms", "wfs", "wcs", "ogc-tiles", "wmts"}
	actions := []string{"read", "write", "delete", "manage"}
	if request.Workspace == "" || !slices.Contains(services, request.Service) || !slices.Contains(actions, request.Action) {
		writeError(w, http.StatusUnprocessableEntity, "Invalid policy", "workspace, supported service, and valid action are required")
		return
	}
	var err error
	if remove {
		err = h.enforcer.RemoveOperationPolicy(roleID, request.Workspace, request.Service, request.Operation, request.Action)
	} else {
		request.Operation = strings.ToUpper(strings.TrimSpace(request.Operation))
		if err := protocolrequest.ValidatePolicy(request.Service, request.Operation, request.Action); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "Invalid policy", err.Error())
			return
		}
		if request.Workspace != "*" {
			ws, lookupErr := h.store.GetWorkspace(r.Context(), request.Workspace)
			if errors.Is(lookupErr, store.ErrNotFound) {
				ws, lookupErr = h.store.GetWorkspaceByName(r.Context(), request.Workspace)
			}
			if errors.Is(lookupErr, store.ErrNotFound) {
				writeError(w, http.StatusUnprocessableEntity, "Invalid workspace", "Select an existing workspace by ID or name, or use * for all workspaces")
				return
			}
			if lookupErr != nil {
				writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
				return
			}
			request.Workspace = ws.ID
		}
		// Removal deliberately targets the exact stored scope, so legacy
		// unmatched name/ID policies remain removable after an upgrade.
		err = h.enforcer.AddOperationPolicy(roleID, request.Workspace, request.Service, request.Operation, request.Action)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to persist policy")
		return
	}
	if remove {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
