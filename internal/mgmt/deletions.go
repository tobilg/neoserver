package mgmt

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
)

func (h *handler) listDeletionOperations(w http.ResponseWriter, r *http.Request) {
	catalog, ok := h.store.(store.CatalogDeletionPageStore)
	if !ok {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "deletion history is unavailable")
		return
	}
	query := store.DeletionQuery{WorkspaceID: r.URL.Query().Get("workspace"), Status: r.URL.Query().Get("status"), Cursor: r.URL.Query().Get("cursor")}
	if r.URL.Query().Has("limit") {
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit < 1 || limit > 1000 {
			writeError(w, http.StatusBadRequest, "Bad Request", "limit must be 1–1000")
			return
		}
		query.Limit = limit
	}
	if r.URL.Query().Has("offset") {
		writeError(w, http.StatusBadRequest, "Bad Request", "use cursor rather than offset")
		return
	}
	principal, _ := identity.FromContext(r.Context())
	if query.WorkspaceID != "" && !principal.IsAdmin(query.WorkspaceID) {
		writeError(w, http.StatusForbidden, "Forbidden", "workspace administrator access is required")
		return
	}
	if !principal.IsSuperAdmin() {
		if principal.IsAdmin("*") {
			for workspaceID := range principal.Roles {
				if workspaceID != "*" && !principal.IsAdmin(workspaceID) {
					query.ExcludedWorkspaceIDs = append(query.ExcludedWorkspaceIDs, workspaceID)
				}
			}
		} else {
			query.WorkspaceIDs = []string{}
			if principal != nil {
				for workspaceID := range principal.Roles {
					if workspaceID != "*" && principal.IsAdmin(workspaceID) {
						query.WorkspaceIDs = append(query.WorkspaceIDs, workspaceID)
					}
				}
			}
		}
	}
	page, err := catalog.ListCatalogDeletionPage(r.Context(), query)
	if errors.Is(err, store.ErrInvalidDeletionQuery) {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid deletion history filter or cursor; restart pagination after changing filters")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list deletion operations")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

type deletionConflictResponse struct {
	Code            int                `json:"code"`
	Message         string             `json:"message"`
	RecurseRequired bool               `json:"recurse_required"`
	Plan            store.DeletionPlan `json:"plan"`
}

func recursiveDeleteRequested(r *http.Request) (bool, error) {
	value := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("recurse")))
	switch value {
	case "", "false", "0":
		return false, nil
	case "true", "1":
		return true, nil
	default:
		return false, errors.New("recurse must be true or false")
	}
}

func (h *handler) writeDeletionConflict(w http.ResponseWriter, err error) bool {
	if errors.Is(err, store.ErrDatasetMapInUse) {
		writeError(w, http.StatusConflict, "Conflict", err.Error())
		return true
	}
	var conflict *store.DeletionConflictError
	if !errors.As(err, &conflict) {
		return false
	}
	writeJSON(w, http.StatusConflict, deletionConflictResponse{Code: http.StatusConflict,
		Message: "resource has dependent catalog or operational state", RecurseRequired: true, Plan: conflict.Plan})
	return true
}

func (h *handler) writeDeletionAccepted(w http.ResponseWriter, r *http.Request, operation *store.DeletionOperation) {
	location := strings.TrimRight(h.cfg.Server.BasePath, "/") + "/api/v1/deletions/" + operation.ID
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusAccepted, operation)
}

func (h *handler) getWorkspaceDeletionPlan(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "catalog lifecycle is unavailable")
		return
	}
	workspaceID, err := h.resolveWorkspaceIDForWorkspaces(r.Context(), chi.URLParam(r, "workspace"))
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}
	plan, err := h.lifecycle.PlanWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to inspect workspace dependencies")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (h *handler) getServiceDeletionPlan(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "catalog lifecycle is unavailable")
		return
	}
	workspaceID, err := h.resolveWorkspaceID(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	serviceID, err := h.resolveServiceID(r.Context(), workspaceID, chi.URLParam(r, "service"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "service not found")
		return
	}
	plan, err := h.lifecycle.PlanService(r.Context(), workspaceID, serviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to inspect service dependencies")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (h *handler) getDeletionOperation(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "catalog lifecycle is unavailable")
		return
	}
	operation, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "operation"))
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "Not Found", "deletion operation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to load deletion operation")
		return
	}
	if id, ok := identity.FromContext(r.Context()); !ok || (!id.IsSuperAdmin() && !id.IsAdmin(operation.WorkspaceID)) {
		writeError(w, http.StatusForbidden, "Forbidden", "workspace administrator access is required")
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

func (h *handler) retryDeletionOperation(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "catalog lifecycle is unavailable")
		return
	}
	operation, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "operation"))
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "Not Found", "deletion operation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to retry deletion operation")
		return
	}
	if id, ok := identity.FromContext(r.Context()); !ok || (!id.IsSuperAdmin() && !id.IsAdmin(operation.WorkspaceID)) {
		writeError(w, http.StatusForbidden, "Forbidden", "workspace administrator access is required")
		return
	}
	operation, err = h.lifecycle.Retry(r.Context(), operation.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to retry deletion operation")
		return
	}
	h.writeDeletionAccepted(w, r, operation)
}

func (h *handler) getCatalogIntegrity(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "catalog lifecycle is unavailable")
		return
	}
	report, err := h.lifecycle.AuditIntegrity(r.Context())
	if err != nil {
		h.logger.Error("failed to audit catalog integrity", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to audit catalog integrity")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *handler) repairCatalogIntegrity(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "catalog lifecycle is unavailable")
		return
	}
	if !strings.EqualFold(r.URL.Query().Get("confirm"), "true") {
		writeError(w, http.StatusBadRequest, "Bad Request", "confirm=true is required")
		return
	}
	report, err := h.lifecycle.RepairIntegrity(r.Context())
	if err != nil {
		h.logger.Error("failed to repair catalog integrity", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to repair catalog integrity")
		return
	}
	writeJSON(w, http.StatusOK, report)
}
