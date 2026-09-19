package mgmt

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/tilecache"
)

func (h *handler) createTileCacheJob(w http.ResponseWriter, r *http.Request) {
	if h.tileJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "Persistent cache unavailable", "enable PersistentCache to run tile cache jobs")
		return
	}
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	var request tilecache.JobRequest
	if readJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	createdBy := ""
	if principal, ok := identity.FromContext(r.Context()); ok && principal != nil {
		createdBy = principal.Subject
	}
	job, err := h.tileJobs.Create(r.Context(), workspaceID, createdBy, request)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Invalid tile cache job", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *handler) listTileCacheJobs(w http.ResponseWriter, r *http.Request) {
	if h.tileJobs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"jobs": []any{}})
		return
	}
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	jobs, err := h.tileJobs.List(r.Context(), workspaceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list tile cache jobs")
		return
	}
	if jobs == nil {
		jobs = []*tilecache.Job{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *handler) getTileCacheJob(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceTileCacheJob(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *handler) getTileCacheJobProgress(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceTileCacheJob(w, r)
	if !ok {
		return
	}
	if h.tileCache == nil {
		writeError(w, http.StatusServiceUnavailable, "Persistent cache unavailable", "")
		return
	}
	chunks, err := h.tileCache.JobStore().ListTileCacheJobChunks(r.Context(), job.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to load tile cache progress")
		return
	}
	type zoomProgress struct {
		Zoom      int   `json:"zoom"`
		Total     int64 `json:"total_tiles"`
		Processed int64 `json:"processed_tiles"`
		Failed    int64 `json:"failed_chunks"`
	}
	byZoom := make(map[int]*zoomProgress)
	for _, chunk := range chunks {
		value := byZoom[chunk.Zoom]
		if value == nil {
			value = &zoomProgress{Zoom: chunk.Zoom}
			byZoom[chunk.Zoom] = value
		}
		total := int64(chunk.MaxCol-chunk.MinCol+1) * int64(chunk.MaxRow-chunk.MinRow+1)
		value.Total += total
		processed := chunk.NextOffset
		if processed > total {
			processed = total
		}
		value.Processed += processed
		if chunk.Status == "failed" {
			value.Failed++
		}
	}
	zooms := make([]*zoomProgress, 0, len(byZoom))
	for _, value := range byZoom {
		zooms = append(zooms, value)
	}
	sort.Slice(zooms, func(i, j int) bool { return zooms[i].Zoom < zooms[j].Zoom })
	writeJSON(w, http.StatusOK, map[string]any{"job_id": job.ID, "zooms": zooms})
}

func (h *handler) cancelTileCacheJob(w http.ResponseWriter, r *http.Request) {
	job, ok := h.workspaceTileCacheJob(w, r)
	if !ok {
		return
	}
	if err := h.tileJobs.Cancel(r.Context(), job.ID); err != nil {
		writeError(w, http.StatusConflict, "Conflict", "tile cache job cannot be cancelled")
		return
	}
	updated, _ := h.tileJobs.Get(r.Context(), job.ID)
	writeJSON(w, http.StatusAccepted, updated)
}

func (h *handler) workspaceTileCacheJob(w http.ResponseWriter, r *http.Request) (*tilecache.Job, bool) {
	if h.tileJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "Persistent cache unavailable", "")
		return nil, false
	}
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return nil, false
	}
	job, err := h.tileJobs.Get(r.Context(), chi.URLParam(r, "job"))
	if err != nil || job.WorkspaceID != workspaceID {
		writeError(w, http.StatusNotFound, "Not Found", "tile cache job not found")
		return nil, false
	}
	return job, true
}

func (h *handler) getPersistentTileCacheStats(w http.ResponseWriter, r *http.Request) {
	if h.tileCache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	settings, err := h.store.GetOGCTilesAPISettings(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace cache quota")
		return
	}
	resourceID, resourceQuota := "", int64(0)
	if identifier := r.URL.Query().Get("resource"); identifier != "" {
		ws, ok := h.registry.GetByID(workspaceID)
		if !ok {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		resource := ws.GetResource(identifier)
		if resource == nil {
			writeError(w, http.StatusNotFound, "Not Found", "resource not found")
			return
		}
		resourceID, resourceQuota = resource.CacheIdentity()
		if resourceID == "" {
			writeError(w, http.StatusNotFound, "Not Found", "resource not found")
			return
		}
	}
	stats, err := h.tileCache.Stats(r.Context(), workspaceID, resourceID, settings.Settings.PersistentCacheQuotaBytes, resourceQuota)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to read persistent cache statistics")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
