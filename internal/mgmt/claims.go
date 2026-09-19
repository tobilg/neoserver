package mgmt

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/store"
)

// ClaimMappingResponse is the API response for a claim mapping.
type ClaimMappingResponse struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	ClaimName   string    `json:"claim_name"`
	ClaimValue  string    `json:"claim_value"`
	RoleID      string    `json:"role_id"`
	Priority    int       `json:"priority"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateClaimMappingRequest is the request to create a claim mapping.
type CreateClaimMappingRequest struct {
	ClaimName  string `json:"claim_name"`
	ClaimValue string `json:"claim_value"`
	RoleID     string `json:"role_id"`
	Priority   int    `json:"priority,omitempty"`
}

// listClaimMappings handles GET /workspaces/{workspace}/claim-mappings
func (h *handler) listClaimMappings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := chi.URLParam(r, "workspace")
	if resolved, resolveErr := h.resolveWorkspaceID(ctx, workspaceID); resolveErr == nil {
		workspaceID = resolved
	}

	mappings, err := h.store.ListClaimMappings(ctx, workspaceID)
	if err != nil {
		h.logger.Error("failed to list claim mappings", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list claim mappings")
		return
	}

	var result []ClaimMappingResponse
	for _, m := range mappings {
		result = append(result, ClaimMappingResponse{
			ID:          m.ID,
			WorkspaceID: m.WorkspaceID,
			ClaimName:   m.ClaimName,
			ClaimValue:  m.ClaimValue,
			RoleID:      m.RoleID,
			Priority:    m.Priority,
			CreatedAt:   m.CreatedAt,
		})
	}

	if result == nil {
		result = []ClaimMappingResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"claim_mappings": result})
}

// createClaimMapping handles POST /workspaces/{workspace}/claim-mappings
func (h *handler) createClaimMapping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := chi.URLParam(r, "workspace")
	if resolved, resolveErr := h.resolveWorkspaceID(ctx, workspaceID); resolveErr == nil {
		workspaceID = resolved
	}

	var req CreateClaimMappingRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	if req.ClaimName == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "claim_name is required")
		return
	}
	if req.ClaimValue == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "claim_value is required")
		return
	}
	if req.RoleID == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", h.roleIDRequiredMessage(r.Context()))
		return
	}

	input := store.CreateClaimMappingInput{
		WorkspaceID: workspaceID,
		ClaimName:   req.ClaimName,
		ClaimValue:  req.ClaimValue,
		RoleID:      req.RoleID,
		Priority:    req.Priority,
	}

	mapping, err := h.store.CreateClaimMapping(ctx, input)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, http.StatusConflict, "Conflict", "claim mapping already exists")
			return
		}
		h.logger.Error("failed to create claim mapping", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to create claim mapping")
		return
	}

	writeJSON(w, http.StatusCreated, ClaimMappingResponse{
		ID:          mapping.ID,
		WorkspaceID: mapping.WorkspaceID,
		ClaimName:   mapping.ClaimName,
		ClaimValue:  mapping.ClaimValue,
		RoleID:      mapping.RoleID,
		Priority:    mapping.Priority,
		CreatedAt:   mapping.CreatedAt,
	})
}

// getClaimMapping handles GET /workspaces/{workspace}/claim-mappings/{mappingId}
func (h *handler) getClaimMapping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	mappingID := chi.URLParam(r, "mappingId")
	workspaceID, err := h.resolveWorkspaceID(ctx, workspaceIdentifier)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}

	mapping, err := h.store.GetClaimMapping(ctx, mappingID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "claim mapping not found")
			return
		}
		h.logger.Error("failed to get claim mapping", "mapping", mappingID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get claim mapping")
		return
	}
	if mapping.WorkspaceID != workspaceID {
		writeError(w, http.StatusNotFound, "Not Found", "claim mapping not found")
		return
	}

	writeJSON(w, http.StatusOK, ClaimMappingResponse{
		ID:          mapping.ID,
		WorkspaceID: mapping.WorkspaceID,
		ClaimName:   mapping.ClaimName,
		ClaimValue:  mapping.ClaimValue,
		RoleID:      mapping.RoleID,
		Priority:    mapping.Priority,
		CreatedAt:   mapping.CreatedAt,
	})
}

// deleteClaimMapping handles DELETE /workspaces/{workspace}/claim-mappings/{mappingId}
func (h *handler) deleteClaimMapping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	mappingID := chi.URLParam(r, "mappingId")
	workspaceID, err := h.resolveWorkspaceID(ctx, workspaceIdentifier)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	mapping, err := h.store.GetClaimMapping(ctx, mappingID)
	if err != nil || mapping.WorkspaceID != workspaceID {
		writeError(w, http.StatusNotFound, "Not Found", "claim mapping not found")
		return
	}

	if err := h.store.DeleteClaimMapping(ctx, mappingID); err != nil {
		h.logger.Error("failed to delete claim mapping", "mapping", mappingID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete claim mapping")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// listGlobalClaimMappings handles GET /claim-mappings (global mappings)
func (h *handler) listGlobalClaimMappings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Global mappings use "*" as workspace ID
	mappings, err := h.store.ListClaimMappings(ctx, "*")
	if err != nil {
		h.logger.Error("failed to list global claim mappings", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list claim mappings")
		return
	}

	var result []ClaimMappingResponse
	for _, m := range mappings {
		result = append(result, ClaimMappingResponse{
			ID:          m.ID,
			WorkspaceID: m.WorkspaceID,
			ClaimName:   m.ClaimName,
			ClaimValue:  m.ClaimValue,
			RoleID:      m.RoleID,
			Priority:    m.Priority,
			CreatedAt:   m.CreatedAt,
		})
	}

	if result == nil {
		result = []ClaimMappingResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"claim_mappings": result})
}

// createGlobalClaimMapping handles POST /claim-mappings (global mappings)
func (h *handler) createGlobalClaimMapping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateClaimMappingRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	if req.ClaimName == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "claim_name is required")
		return
	}
	if req.ClaimValue == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "claim_value is required")
		return
	}
	if req.RoleID == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", h.roleIDRequiredMessage(r.Context()))
		return
	}

	// Global mappings use "*" as workspace ID
	input := store.CreateClaimMappingInput{
		WorkspaceID: "*",
		ClaimName:   req.ClaimName,
		ClaimValue:  req.ClaimValue,
		RoleID:      req.RoleID,
		Priority:    req.Priority,
	}

	mapping, err := h.store.CreateClaimMapping(ctx, input)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, http.StatusConflict, "Conflict", "claim mapping already exists")
			return
		}
		h.logger.Error("failed to create global claim mapping", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to create claim mapping")
		return
	}

	writeJSON(w, http.StatusCreated, ClaimMappingResponse{
		ID:          mapping.ID,
		WorkspaceID: mapping.WorkspaceID,
		ClaimName:   mapping.ClaimName,
		ClaimValue:  mapping.ClaimValue,
		RoleID:      mapping.RoleID,
		Priority:    mapping.Priority,
		CreatedAt:   mapping.CreatedAt,
	})
}

// deleteGlobalClaimMapping handles DELETE /claim-mappings/{mappingId}
func (h *handler) deleteGlobalClaimMapping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mappingID := chi.URLParam(r, "mappingId")

	if err := h.store.DeleteClaimMapping(ctx, mappingID); err != nil {
		h.logger.Error("failed to delete global claim mapping", "mapping", mappingID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete claim mapping")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
