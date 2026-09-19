package mgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// resolveWorkspaceID resolves a workspace identifier (name or ID) to actual workspace ID.
func (h *handler) canonicalWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := h.resolveWorkspaceID(r.Context(), chi.URLParam(r, "workspace"))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, 404, "Not Found", "workspace not found")
			} else {
				writeError(w, 503, "Unavailable", "workspace lookup failed")
			}
			return
		}
		// Only attach canonical identity here, not a pinned registry snapshot:
		// management writes must remain free to replace runtime workspaces.
		ctx := workspace.WithWorkspace(r.Context(), &workspace.Workspace{ID: id})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *handler) resolveWorkspaceID(ctx context.Context, identifier string) (string, error) {
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

// resolveServiceID resolves a service identifier (name or ID) to actual service ID.
func (h *handler) resolveServiceID(ctx context.Context, workspaceID, identifier string) (string, error) {
	// First try to get by ID directly from store
	svc, err := h.store.GetService(ctx, identifier)
	if err == nil && svc.WorkspaceID == workspaceID {
		return svc.ID, nil
	}

	// If not found by ID, try by name - list services and find by name
	services, err := h.store.ListServices(ctx, workspaceID)
	if err != nil {
		return "", err
	}

	for _, s := range services {
		if s.Name == identifier || s.ID == identifier {
			return s.ID, nil
		}
	}

	return "", store.ErrNotFound
}

// ServiceResponse is the API response for a service.
type ServiceResponse struct {
	ID                string                      `json:"id"`
	WorkspaceID       string                      `json:"workspace_id"`
	Name              string                      `json:"name"`
	Type              string                      `json:"type"`
	ConnectionInfo    json.RawMessage             `json:"connection_info,omitempty"`
	Enabled           bool                        `json:"enabled"`
	CreatedAt         time.Time                   `json:"created_at"`
	UpdatedAt         time.Time                   `json:"updated_at"`
	CacheSettings     *store.ServiceCacheSettings `json:"cache_settings,omitempty"`
	ConfiguredSecrets []string                    `json:"configured_secrets,omitempty"`
}

// CreateServiceRequest is the request to create a service.
type CreateServiceRequest struct {
	Name           string                      `json:"name"`
	Type           string                      `json:"type"`
	ConnectionInfo json.RawMessage             `json:"connection_info"`
	Enabled        *bool                       `json:"enabled,omitempty"`
	CacheSettings  *store.ServiceCacheSettings `json:"cache_settings,omitempty"`
}

// UpdateServiceRequest is the request to update a service.
type UpdateServiceRequest struct {
	Name           string                      `json:"name,omitempty"`
	ConnectionInfo json.RawMessage             `json:"connection_info,omitempty"`
	Enabled        *bool                       `json:"enabled,omitempty"`
	CacheSettings  *store.ServiceCacheSettings `json:"cache_settings,omitempty"`
}

// DiscoveredLayerResponse is the response for a discovered layer.
type DiscoveredLayerResponse struct {
	Name           string `json:"name"`
	Schema         string `json:"schema,omitempty"`
	Title          string `json:"title,omitempty"`
	Description    string `json:"description,omitempty"`
	GeometryColumn string `json:"geometry_column,omitempty"`
	GeometryType   string `json:"geometry_type,omitempty"`
	SRID           int    `json:"srid,omitempty"`
}

// listServices handles GET /workspaces/{workspace}/services
func (h *handler) listServices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceID(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	services, err := h.store.ListServices(ctx, workspaceID)
	if err != nil {
		h.logger.Error("failed to list services", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list services")
		return
	}

	var result []ServiceResponse
	for _, svc := range services {
		result = append(result, ServiceResponse{
			ID:            svc.ID,
			WorkspaceID:   svc.WorkspaceID,
			Name:          svc.Name,
			Type:          string(svc.Type),
			Enabled:       svc.Enabled,
			CreatedAt:     svc.CreatedAt,
			UpdatedAt:     svc.UpdatedAt,
			CacheSettings: svc.CacheSettings,
		})
	}

	if result == nil {
		result = []ServiceResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"services": result})
}

// createService handles POST /workspaces/{workspace}/services
func (h *handler) createService(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceID(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req CreateServiceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "type is required")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	if !validManagedBinding(nil, req.ConnectionInfo) {
		writeError(w, http.StatusBadRequest, "Bad Request", "managed import bindings are server-owned; publish through the import workflow")
		return
	}
	if err := validateServiceCacheSettings(req.CacheSettings); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	input := store.CreateServiceInput{
		WorkspaceID:    workspaceID,
		Name:           req.Name,
		Type:           store.ServiceType(req.Type),
		ConnectionInfo: req.ConnectionInfo,
		CacheSettings:  req.CacheSettings,
		Enabled:        enabled,
	}

	// Use registry.CreateService to update both store AND runtime registry
	svc, err := h.registry.CreateService(ctx, input)
	if err != nil {
		if detail, ok := dataSourceErrorDetail(err); ok {
			writeError(w, http.StatusBadRequest, "Bad Request", detail)
			return
		}
		h.logger.Error("failed to create service", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to create service")
		return
	}

	writeJSON(w, http.StatusCreated, ServiceResponse{
		ID:            svc.ID,
		WorkspaceID:   workspaceID,
		Name:          svc.Name,
		Type:          string(svc.Type),
		Enabled:       svc.Enabled,
		CacheSettings: svc.CacheSettings,
	})
}

// getService handles GET /workspaces/{workspace}/services/{service}
func (h *handler) getService(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceIdentifier := chi.URLParam(r, "service")
	workspaceID, err := h.resolveWorkspaceID(ctx, workspaceIdentifier)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	serviceID, err := h.resolveServiceID(ctx, workspaceID, serviceIdentifier)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "service not found")
		return
	}

	svc, err := h.store.GetService(ctx, serviceID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		h.logger.Error("failed to get service", "service", serviceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get service")
		return
	}

	connectionInfo, configuredSecrets := sanitizeConnectionInfo(svc.ConnectionInfo)
	writeJSON(w, http.StatusOK, ServiceResponse{
		ID:                svc.ID,
		WorkspaceID:       svc.WorkspaceID,
		Name:              svc.Name,
		Type:              string(svc.Type),
		ConnectionInfo:    connectionInfo,
		ConfiguredSecrets: configuredSecrets,
		Enabled:           svc.Enabled,
		CreatedAt:         svc.CreatedAt,
		UpdatedAt:         svc.UpdatedAt,
		CacheSettings:     svc.CacheSettings,
	})
}

// updateService handles PUT /workspaces/{workspace}/services/{service}
func (h *handler) updateService(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceID := chi.URLParam(r, "service")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceID(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req UpdateServiceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if err := validateServiceCacheSettings(req.CacheSettings); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	input := store.UpdateServiceInput{}
	serviceID, err = h.resolveServiceID(ctx, workspaceID, serviceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "service not found")
		return
	}
	if req.Name != "" {
		input.Name = &req.Name
	}
	if req.ConnectionInfo != nil {
		existing, getErr := h.store.GetService(ctx, serviceID)
		if getErr != nil || existing.WorkspaceID != workspaceID {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		merged := mergeConnectionSecrets(existing.ConnectionInfo, req.ConnectionInfo)
		if !validManagedBinding(existing.ConnectionInfo, merged) {
			writeError(w, http.StatusBadRequest, "Bad Request", "managed import bindings cannot be changed")
			return
		}
		input.ConnectionInfo = &merged
	}
	if req.Enabled != nil {
		input.Enabled = req.Enabled
	}
	if req.CacheSettings != nil {
		input.CacheSettings = req.CacheSettings
	}

	// Use registry.UpdateService to update both store AND runtime registry
	svc, err := h.registry.UpdateService(ctx, workspaceID, serviceID, input)
	if err != nil {
		if err == store.ErrNotFound || err == workspace.ErrServiceNotFound || err == workspace.ErrWorkspaceNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		if detail, ok := dataSourceErrorDetail(err); ok {
			writeError(w, http.StatusBadRequest, "Bad Request", detail)
			return
		}
		h.logger.Error("failed to update service", "service", serviceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update service")
		return
	}

	writeJSON(w, http.StatusOK, ServiceResponse{
		ID:            svc.ID,
		WorkspaceID:   workspaceID,
		Name:          svc.Name,
		Type:          string(svc.Type),
		Enabled:       svc.Enabled,
		CacheSettings: svc.CacheSettings,
	})
}

func validateServiceCacheSettings(settings *store.ServiceCacheSettings) error {
	if settings == nil {
		return nil
	}
	if settings.FeaturesTTLSec != nil && *settings.FeaturesTTLSec < 0 {
		return fmt.Errorf("features_ttl_sec must be non-negative")
	}
	if settings.TilesTTLSec != nil && *settings.TilesTTLSec < 0 {
		return fmt.Errorf("tiles_ttl_sec must be non-negative")
	}
	return nil
}

// deleteService handles DELETE /workspaces/{workspace}/services/{service}
func (h *handler) deleteService(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceID := chi.URLParam(r, "service")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceID(ctx, workspaceIdentifier)
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
		actualServiceID, err := h.resolveServiceID(ctx, workspaceID, serviceID)
		if err != nil {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		operation, err := h.lifecycle.DeleteService(ctx, workspaceID, actualServiceID, recursive)
		if err != nil {
			if h.writeDeletionConflict(w, err) {
				return
			}
			if err == store.ErrNotFound || err == workspace.ErrServiceNotFound {
				writeError(w, http.StatusNotFound, "Not Found", "service not found")
				return
			}
			h.logger.Error("failed to delete service", "service", serviceID, "error", err)
			writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete service")
			return
		}
		if operation != nil {
			h.writeDeletionAccepted(w, r, operation)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Use registry.DeleteService to update both store AND runtime registry
	if err := h.registry.DeleteService(ctx, workspaceID, serviceID); err != nil {
		if h.writeDeletionConflict(w, err) {
			return
		}
		if errors.Is(err, workspace.ErrResourceReferenced) {
			writeError(w, http.StatusConflict, "Conflict", err.Error())
			return
		}
		if err == store.ErrNotFound || err == workspace.ErrServiceNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		h.logger.Error("failed to delete service", "service", serviceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete service")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// discoverLayers handles POST /workspaces/{workspace}/services/{service}/discover
func (h *handler) discoverLayers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceIdentifier := chi.URLParam(r, "service")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceID(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	// Resolve service name/ID to actual service ID
	serviceID, err := h.resolveServiceID(ctx, workspaceID, serviceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		h.logger.Error("failed to resolve service", "service", serviceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve service")
		return
	}

	// Use registry to discover layers
	discovered, err := h.registry.DiscoverLayers(ctx, workspaceID, serviceID)
	if err != nil {
		h.logger.Error("failed to discover layers", "service", serviceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to discover layers: "+err.Error())
		return
	}

	var result []DiscoveredLayerResponse
	for _, l := range discovered {
		result = append(result, DiscoveredLayerResponse{
			Name:           l.Name,
			Schema:         l.Schema,
			Title:          l.Title,
			Description:    l.Description,
			GeometryColumn: l.GeometryColumn,
			GeometryType:   l.GeometryType,
			SRID:           l.SRID,
		})
	}

	if result == nil {
		result = []DiscoveredLayerResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"layers": result})
}

// dataSourceErrorDetail turns a data source that could not be opened into a
// client-facing explanation instead of an internal error.
func dataSourceErrorDetail(err error) (string, bool) {
	var dsErr *workspace.DataSourceError
	if !errors.As(err, &dsErr) {
		return "", false
	}
	return "the data source could not be opened: " + dsErr.Err.Error(), true
}
