package mgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/tiles"
)

func (h *handler) workspaceSummary(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	counts := store.WorkspaceCounts{}
	if catalog, ok := h.store.(store.WorkspaceCountStore); ok {
		all, countErr := catalog.ListWorkspaceCounts(r.Context())
		if countErr == nil {
			counts = all[workspaceID]
		}
	}
	protocols := map[string]bool{}
	if value, getErr := h.store.GetWMSSettings(r.Context(), workspaceID); getErr == nil {
		protocols["wms"] = value.Enabled
	}
	if value, getErr := h.store.GetWFSSettings(r.Context(), workspaceID); getErr == nil {
		protocols["wfs"] = value.Enabled
	}
	if value, getErr := h.store.GetOGCAPISettings(r.Context(), workspaceID); getErr == nil {
		protocols["ogcapi"] = value.Enabled
	}
	if value, getErr := h.store.GetOGCTilesAPISettings(r.Context(), workspaceID); getErr == nil {
		protocols["ogc_tiles"] = value.Enabled
	}
	if catalog, ok := h.store.(store.CoverageStore); ok {
		if value, getErr := catalog.GetWCSSettings(r.Context(), workspaceID); getErr == nil {
			protocols["wcs"] = value.Enabled
		}
	}
	if catalog, ok := h.store.(store.WMTSStore); ok {
		if value, getErr := catalog.GetWMTSSettings(r.Context(), workspaceID); getErr == nil {
			protocols["wmts"] = value.Enabled
		}
	}
	activeImports := 0
	if imports, ok := h.store.(store.ImportStore); ok {
		if jobs, listErr := imports.ListImportJobs(r.Context(), workspaceID, 1000); listErr == nil {
			for _, job := range jobs {
				if job.CompletedAt == nil && job.Status != store.ImportCancelled && job.Status != store.ImportFailed {
					activeImports++
				}
			}
		}
	}
	activeTileJobs := 0
	if h.tileJobs != nil {
		if jobs, listErr := h.tileJobs.List(r.Context(), workspaceID, 1000); listErr == nil {
			for _, job := range jobs {
				if job.Status == tilecache.JobQueued || job.Status == tilecache.JobRunning || job.Status == tilecache.JobCancelling {
					activeTileJobs++
				}
			}
		}
	}
	response := map[string]any{"workspace_id": workspaceID, "counts": counts, "protocols": protocols,
		"active_jobs": map[string]int{"imports": activeImports, "tile_cache": activeTileJobs}}
	if h.cache != nil {
		response["cache"] = h.cache.GetMetrics()
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) listWorkspaceRoles(w http.ResponseWriter, r *http.Request) {
	principal, _ := identity.FromContext(r.Context())
	roles, err := h.store.ListRoles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list roles")
		return
	}
	result := make([]RoleResponse, 0, len(roles))
	for _, role := range roles {
		if role.ID == "super_admin" && !principal.IsSuperAdmin() {
			continue
		}
		result = append(result, RoleResponse{ID: role.ID, Name: role.Name, Description: role.Description,
			IsSystem: role.IsSystem, CreatedAt: role.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": result})
}

func (h *handler) listWorkspaceTileMatrixSets(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	settings, err := h.store.GetOGCTilesAPISettings(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to load tile settings")
		return
	}
	definitions := make([]*tiles.TileMatrixSetDefinition, 0, len(settings.Settings.TileMatrixSets))
	for _, id := range settings.Settings.TileMatrixSets {
		if definition, getErr := tiles.GetTileMatrixSetDefinition(id); getErr == nil {
			definitions = append(definitions, definition)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tile_matrix_sets": definitions})
}

func (h *handler) testNewServiceConnection(w http.ResponseWriter, r *http.Request) {
	var request CreateServiceRequest
	if readJSON(r, &request) != nil || request.Type == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "type and connection_info are required")
		return
	}
	if !validManagedBinding(nil, request.ConnectionInfo) {
		writeError(w, http.StatusBadRequest, "Bad Request", "managed import bindings are server-owned")
		return
	}
	h.runConnectionTest(w, r, &store.Service{ID: "connection-test", Name: request.Name,
		Type: store.ServiceType(request.Type), ConnectionInfo: request.ConnectionInfo, Enabled: true})
}

func (h *handler) testExistingServiceConnection(w http.ResponseWriter, r *http.Request) {
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
	existing, err := h.store.GetService(r.Context(), serviceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "service not found")
		return
	}
	var request UpdateServiceRequest
	if readJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid connection test request")
		return
	}
	if request.ConnectionInfo != nil {
		merged := mergeConnectionSecrets(existing.ConnectionInfo, request.ConnectionInfo)
		if !validManagedBinding(existing.ConnectionInfo, merged) {
			writeError(w, http.StatusBadRequest, "Bad Request", "managed import bindings cannot be changed")
			return
		}
		existing.ConnectionInfo = merged
	}
	h.runConnectionTest(w, r, existing)
}

func (h *handler) runConnectionTest(w http.ResponseWriter, r *http.Request, service *store.Service) {
	started := time.Now()
	source, err := datasource.Prepare(r.Context(), func() (datasource.DataSource, error) { return datasource.CreateFromService(service) })
	if err == nil {
		defer source.Close()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		err = source.Health(ctx)
	}
	duration := float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "duration_ms": duration, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duration_ms": duration})
}

func sanitizeConnectionInfo(raw json.RawMessage) (json.RawMessage, []string) {
	if len(raw) == 0 {
		return raw, nil
	}
	var values map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return nil, nil
	}
	configured := make([]string, 0)
	for _, key := range []string{"password"} {
		if value, ok := values[key]; ok {
			if text, stringValue := value.(string); stringValue && text != "" {
				configured = append(configured, key)
			}
			delete(values, key)
		}
	}
	encoded, _ := json.Marshal(values)
	return encoded, configured
}

func mergeConnectionSecrets(existing, update json.RawMessage) json.RawMessage {
	var oldValues, newValues map[string]any
	if json.Unmarshal(existing, &oldValues) != nil || json.Unmarshal(update, &newValues) != nil {
		return update
	}
	for _, key := range []string{"password"} {
		value, present := newValues[key]
		if !present || value == "" {
			if old, exists := oldValues[key]; exists {
				newValues[key] = old
			}
		}
	}
	encoded, _ := json.Marshal(newValues)
	return encoded
}
