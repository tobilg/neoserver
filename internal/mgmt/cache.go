package mgmt

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
)

// getCacheStats returns cache statistics.
func (h *handler) getCacheStats(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false,
			"message": "Caching is not enabled",
		})
		return
	}

	metrics := h.cache.GetMetrics()
	response := map[string]any{
		"enabled":                 true,
		"profile":                 metrics.Profile,
		"capabilities":            metrics.Capabilities,
		"collections":             metrics.Collections,
		"features":                metrics.Features,
		"tiles":                   metrics.Tiles,
		"counts":                  metrics.Counts,
		"total_size_bytes":        metrics.TotalSizeBytes,
		"total_max_size_bytes":    metrics.TotalMaxSizeBytes,
		"total_invalidation_keys": metrics.TotalInvalidationKeys,
	}
	if h.tileCache != nil {
		if persistent, err := h.tileCache.Stats(r.Context(), "", "", 0, 0); err == nil {
			response["persistent_tiles"] = persistent
		}
	} else {
		response["persistent_tiles"] = map[string]any{"enabled": false}
	}
	writeJSON(w, http.StatusOK, response)
}

// clearAllCaches clears all caches.
func (h *handler) clearAllCaches(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		writeError(w, http.StatusBadRequest, "Caching not enabled", "")
		return
	}

	h.cache.ClearAll()
	h.logger.Info("all caches cleared by admin request")

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "All caches cleared successfully",
	})
}

// clearCacheByType clears a specific cache type.
func (h *handler) clearCacheByType(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		writeError(w, http.StatusBadRequest, "Caching not enabled", "")
		return
	}

	cacheType := chi.URLParam(r, "cacheType")

	var ct cache.CacheType
	switch cacheType {
	case "capabilities":
		ct = cache.CacheTypeCapabilities
	case "collections":
		ct = cache.CacheTypeCollections
	case "features":
		ct = cache.CacheTypeFeatures
	case "tiles":
		ct = cache.CacheTypeTiles
	case "counts":
		ct = cache.CacheTypeCounts
	default:
		writeError(w, http.StatusBadRequest, "Invalid cache type", "Valid types: capabilities, collections, features, tiles, counts")
		return
	}

	h.cache.ClearByType(ct)
	h.logger.Info("cache cleared by admin request", "type", cacheType)

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "Cache cleared successfully",
		"type":    cacheType,
	})
}

// clearWorkspaceCache clears all caches for a specific workspace.
func (h *handler) clearWorkspaceCache(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		writeError(w, http.StatusBadRequest, "Caching not enabled", "")
		return
	}

	workspaceID := chi.URLParam(r, "workspace")
	ws, ok := h.registry.GetByID(workspaceID)
	if !ok {
		// Try by name
		ws, ok = h.registry.Get(workspaceID)
		if !ok {
			writeError(w, http.StatusNotFound, "Workspace not found", "")
			return
		}
	}

	h.cache.InvalidateWorkspace(ws.ID)
	h.logger.Info("workspace cache cleared by admin request", "workspace", ws.Name)

	writeJSON(w, http.StatusOK, map[string]any{
		"message":   "Workspace cache cleared successfully",
		"workspace": ws.Name,
	})
}
