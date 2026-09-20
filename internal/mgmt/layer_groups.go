package mgmt

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/store"
)

type layerGroupRequest struct {
	PublicID            string                    `json:"public_id"`
	Title               string                    `json:"title,omitempty"`
	Description         *string                   `json:"description,omitempty"`
	Enabled             *bool                     `json:"enabled,omitempty"`
	Public              *bool                     `json:"public,omitempty"`
	AllowedRoles        *[]string                 `json:"allowed_roles,omitempty"`
	Members             *[]store.LayerGroupMember `json:"members,omitempty"`
	DefaultStyle        *string                   `json:"default_style,omitempty"`
	Styles              *[]string                 `json:"styles,omitempty"`
	NativeExtent        **store.SpatialExtent     `json:"native_extent,omitempty"`
	TileCacheQuotaBytes *int64                    `json:"tile_cache_quota_bytes,omitempty"`
}

func (h *handler) listLayerGroups(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := h.resolveWorkspaceIDForLayers(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	groupStore, ok := h.store.(store.LayerGroupStore)
	if !ok {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "layer groups are unavailable")
		return
	}
	groups, err := groupStore.ListLayerGroups(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list layer groups")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"layer_groups": groups})
}

func (h *handler) createLayerGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := h.resolveWorkspaceIDForLayers(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	var request layerGroupRequest
	if readJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if request.PublicID == "" || request.Members == nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "public_id and members are required")
		return
	}
	enabled, public := true, false
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	if request.Public != nil {
		public = *request.Public
	}
	roles, styles := []string(nil), []string(nil)
	if request.AllowedRoles != nil {
		roles = *request.AllowedRoles
	}
	if request.Styles != nil {
		styles = *request.Styles
	}
	defaultStyle := ""
	if request.DefaultStyle != nil {
		defaultStyle = *request.DefaultStyle
	}
	if err := h.validateGroupStyleBindings(r.Context(), workspaceID, defaultStyle, styles); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	description := ""
	if request.Description != nil {
		description = *request.Description
	}
	created, err := h.registry.CreateLayerGroup(r.Context(), workspaceID, store.CreateLayerGroupInput{
		PublicID: request.PublicID, Title: request.Title, Description: description, Enabled: enabled,
		Public: public, AllowedRoles: roles, Members: *request.Members, DefaultStyle: defaultStyle, Styles: styles,
		NativeExtent: derefExtent(request.NativeExtent), TileCacheQuotaBytes: derefInt64(request.TileCacheQuotaBytes),
	}, h.cfg.WMS.MaxGroupDepth)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, 409, "Conflict", "public_id already exists in this workspace")
			return
		}
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	h.writeStoredLayerGroup(w, r, created.ID, http.StatusCreated)
}

func (h *handler) getLayerGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID, group, ok := h.resolveLayerGroup(w, r)
	_ = workspaceID
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func (h *handler) updateLayerGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID, group, ok := h.resolveLayerGroup(w, r)
	if !ok {
		return
	}
	var request layerGroupRequest
	if readJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	input := store.UpdateLayerGroupInput{Enabled: request.Enabled, Public: request.Public, AllowedRoles: request.AllowedRoles,
		Members: request.Members, DefaultStyle: request.DefaultStyle, Styles: request.Styles, NativeExtent: request.NativeExtent,
		TileCacheQuotaBytes: request.TileCacheQuotaBytes}
	if request.PublicID != "" {
		input.PublicID = &request.PublicID
	}
	if request.Title != "" {
		input.Title = &request.Title
	}
	input.Description = request.Description
	defaultStyle, styles := group.DefaultStyle, group.Styles
	if request.DefaultStyle != nil {
		defaultStyle = *request.DefaultStyle
	}
	if request.Styles != nil {
		styles = *request.Styles
	}
	if err := h.validateGroupStyleBindings(r.Context(), workspaceID, defaultStyle, styles); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	updated, err := h.registry.UpdateLayerGroup(r.Context(), workspaceID, group.ID, input, h.cfg.WMS.MaxGroupDepth)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, 409, "Conflict", "public_id already exists in this workspace")
			return
		}
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	h.writeStoredLayerGroup(w, r, updated.ID, http.StatusOK)
}

func (h *handler) deleteLayerGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID, group, ok := h.resolveLayerGroup(w, r)
	if !ok {
		return
	}
	if err := h.registry.DeleteLayerGroup(r.Context(), workspaceID, group.ID); err != nil {
		writeError(w, http.StatusConflict, "Conflict", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) resolveLayerGroup(w http.ResponseWriter, r *http.Request) (string, *store.LayerGroup, bool) {
	workspaceID, err := h.resolveWorkspaceIDForLayers(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return "", nil, false
	}
	groupStore, ok := h.store.(store.LayerGroupStore)
	if !ok {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "layer groups are unavailable")
		return "", nil, false
	}
	identifier := chi.URLParam(r, "group")
	group, err := groupStore.GetLayerGroup(r.Context(), identifier)
	if err != nil {
		group, err = groupStore.GetLayerGroupByPublicID(r.Context(), workspaceID, identifier)
	}
	if err != nil || group.WorkspaceID != workspaceID {
		writeError(w, http.StatusNotFound, "Not Found", "layer group not found")
		return "", nil, false
	}
	return workspaceID, group, true
}

func derefExtent(value **store.SpatialExtent) *store.SpatialExtent {
	if value == nil {
		return nil
	}
	return *value
}
func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// Management responses use the same persisted schema as GET and list; runtime
// LayerGroup snapshots have neither JSON tags nor catalog ownership/timestamps.
func (h *handler) writeStoredLayerGroup(w http.ResponseWriter, r *http.Request, id string, status int) {
	group, err := h.store.(store.LayerGroupStore).GetLayerGroup(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to load saved layer group")
		return
	}
	writeJSON(w, status, group)
}
