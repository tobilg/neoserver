package mgmt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/store"
)

// resolveWorkspaceIDForSettings resolves a workspace identifier (name or ID) to actual workspace ID.
func (h *handler) resolveWorkspaceIDForSettings(ctx context.Context, identifier string) (string, error) {
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

// WMSSettingsResponse is the API response for WMS settings.
type WMSSettingsResponse struct {
	Enabled                bool     `json:"enabled"`
	Public                 bool     `json:"public"`
	MaxWidth               int      `json:"max_width,omitempty"`
	MaxHeight              int      `json:"max_height,omitempty"`
	Title                  string   `json:"title,omitempty"`
	Abstract               string   `json:"abstract,omitempty"`
	DefaultStyle           string   `json:"default_style,omitempty"`
	MaxPixels              int      `json:"max_pixels,omitempty"`
	MaxRenderFeatures      int      `json:"max_render_features,omitempty"`
	MaxRenderVertices      int      `json:"max_render_vertices,omitempty"`
	SimplifyEnabled        *bool    `json:"simplify_enabled,omitempty"`
	SimplifyPixelTolerance float64  `json:"simplify_pixel_tolerance,omitempty"`
	Extensions             []string `json:"extensions,omitempty"`
}

// WFSSettingsResponse is the API response for WFS settings.
type WFSSettingsResponse struct {
	Enabled        bool   `json:"enabled"`
	Public         bool   `json:"public"`
	MaxFeatures    int    `json:"max_features,omitempty"`
	Title          string `json:"title,omitempty"`
	Abstract       string `json:"abstract,omitempty"`
	DefaultCount   int    `json:"default_count,omitempty"`
	MaxOffset      int    `json:"max_offset,omitempty"`
	CountTimeoutMS int    `json:"count_timeout_ms,omitempty"`
}

// OGCAPISettingsResponse is the API response for OGC API settings.
type OGCAPISettingsResponse struct {
	Enabled      bool   `json:"enabled"`
	Public       bool   `json:"public"`
	Title        string `json:"title,omitempty"`
	Abstract     string `json:"abstract,omitempty"`
	LimitDefault int    `json:"limit_default,omitempty"`
	LimitMax     int    `json:"limit_max,omitempty"`
	MaxOffset    int    `json:"max_offset,omitempty"`
}

type WCSSettingsResponse struct {
	Enabled              bool     `json:"enabled"`
	Public               bool     `json:"public"`
	Title                string   `json:"title,omitempty"`
	Abstract             string   `json:"abstract,omitempty"`
	MaxCells             int64    `json:"max_cells,omitempty"`
	MaxOutputBytes       int64    `json:"max_output_bytes,omitempty"`
	ProcessingTimeoutMS  int      `json:"processing_timeout_ms,omitempty"`
	Extensions           []string `json:"extensions,omitempty"`
	AllowedSubsettingCRS []string `json:"allowed_subsetting_crs,omitempty"`
	AllowedOutputCRS     []string `json:"allowed_output_crs,omitempty"`
	InterpolationMethods []string `json:"interpolation_methods,omitempty"`
	OutputFormats        []string `json:"output_formats,omitempty"`
	MaxDimensions        int      `json:"max_dimensions,omitempty"`
	MaxAxisValues        int64    `json:"max_axis_values,omitempty"`
	MaxSourceGranules    int      `json:"max_source_granules,omitempty"`
	MaxTemporaryBytes    int64    `json:"max_temporary_bytes,omitempty"`
}

var validWCSExtensions = map[string]bool{
	"xml-post": true, "range-subsetting": true, "scaling": true,
	"crs": true, "interpolation": true, "multidimensional": true,
}

var validWCSFormats = map[string]bool{
	"image/tiff": true, "application/gml+xml": true, "multipart/related": true,
	"application/netcdf": true, "image/jp2": true,
}

func validateCanonicalWCSList(name string, values []string, allowed map[string]bool) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value != strings.ToLower(strings.TrimSpace(value)) || !allowed[value] || seen[value] {
			return fmt.Errorf("%s contains invalid, non-canonical, or duplicate value %q", name, value)
		}
		seen[value] = true
	}
	return nil
}

func validateWCSSettings(req WCSSettingsResponse, ceiling conf.WCS) error {
	if req.MaxCells < 0 || req.MaxOutputBytes < 0 || req.ProcessingTimeoutMS < 0 || req.MaxDimensions < 0 ||
		req.MaxAxisValues < 0 || req.MaxSourceGranules < 0 || req.MaxTemporaryBytes < 0 {
		return fmt.Errorf("WCS limits cannot be negative")
	}
	if (req.MaxCells > 0 && req.MaxCells > ceiling.MaxCells) ||
		(req.MaxOutputBytes > 0 && req.MaxOutputBytes > ceiling.MaxOutputBytes) ||
		(req.ProcessingTimeoutMS > 0 && req.ProcessingTimeoutMS > ceiling.ProcessingTimeoutMS) ||
		(req.MaxDimensions > 0 && req.MaxDimensions > ceiling.MaxDimensions) ||
		(req.MaxAxisValues > 0 && req.MaxAxisValues > ceiling.MaxAxisValues) ||
		(req.MaxSourceGranules > 0 && req.MaxSourceGranules > ceiling.MaxSourceGranules) ||
		(req.MaxTemporaryBytes > 0 && req.MaxTemporaryBytes > ceiling.MaxTemporaryBytes) {
		return fmt.Errorf("workspace WCS limits cannot exceed server limits")
	}
	if err := validateCanonicalWCSList("extensions", req.Extensions, validWCSExtensions); err != nil {
		return err
	}
	if err := validateCanonicalWCSList("output_formats", req.OutputFormats, validWCSFormats); err != nil {
		return err
	}
	if err := validateCanonicalWCSList("interpolation_methods", req.InterpolationMethods, map[string]bool{"nearest-neighbor": true, "linear": true}); err != nil {
		return err
	}
	for _, values := range [][]string{req.AllowedSubsettingCRS, req.AllowedOutputCRS} {
		seen := map[string]bool{}
		for _, value := range values {
			if strings.TrimSpace(value) == "" || seen[value] {
				return fmt.Errorf("CRS allowlists cannot contain empty or duplicate values")
			}
			seen[value] = true
		}
	}
	return nil
}

func (h *handler) getWCSSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	coverageStore, ok := h.store.(store.CoverageStore)
	if !ok {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "WCS persistence unavailable")
		return
	}
	settings, err := coverageStore.GetWCSSettings(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get WCS settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}
func (h *handler) updateWCSSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	var req WCSSettingsResponse
	if readJSON(r, &req) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if err := validateWCSSettings(req, h.cfg.WCS); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	settings := store.WCSSettings{
		Enabled: req.Enabled, Public: req.Public, Title: req.Title, Abstract: req.Abstract,
		MaxCells: req.MaxCells, MaxOutputBytes: req.MaxOutputBytes, ProcessingTimeoutMS: req.ProcessingTimeoutMS,
		Extensions: req.Extensions, AllowedSubsettingCRS: req.AllowedSubsettingCRS, AllowedOutputCRS: req.AllowedOutputCRS,
		InterpolationMethods: req.InterpolationMethods, OutputFormats: req.OutputFormats,
		MaxDimensions: req.MaxDimensions, MaxAxisValues: req.MaxAxisValues, MaxSourceGranules: req.MaxSourceGranules,
		MaxTemporaryBytes: req.MaxTemporaryBytes,
	}
	if err := h.registry.UpdateWCSSettings(r.Context(), workspaceID, settings); err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update WCS settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// OGCTilesAPIVectorSettingsResponse is the API response for vector tile settings.
type OGCTilesAPIVectorSettingsResponse struct {
	Enabled bool     `json:"enabled"`
	Formats []string `json:"formats,omitempty"`
}

// OGCTilesAPIMapSettingsResponse is the API response for map tile settings.
type OGCTilesAPIMapSettingsResponse struct {
	Enabled bool     `json:"enabled"`
	Formats []string `json:"formats,omitempty"`
}

// OGCTilesAPIInnerSettingsResponse is the API response for inner tile settings.
type OGCTilesAPIInnerSettingsResponse struct {
	DatasetMapLayerGroupID    *string                           `json:"dataset_map_layer_group_id,omitempty"`
	TileMatrixSets            []string                          `json:"tile_matrix_sets,omitempty"`
	VectorTiles               OGCTilesAPIVectorSettingsResponse `json:"vector_tiles"`
	MapTiles                  OGCTilesAPIMapSettingsResponse    `json:"map_tiles"`
	CacheEnabled              bool                              `json:"cache_enabled"`
	MaxFeatures               int                               `json:"max_features,omitempty"`
	MaxVertices               int                               `json:"max_vertices,omitempty"`
	MaxTileBytes              int                               `json:"max_tile_bytes,omitempty"`
	PersistentCacheQuotaBytes int64                             `json:"persistent_cache_quota_bytes,omitempty"`
}

// OGCTilesAPISettingsResponse is the API response for OGC Tiles API settings.
type OGCTilesAPISettingsResponse struct {
	Enabled  bool                             `json:"enabled"`
	Public   bool                             `json:"public"`
	Title    string                           `json:"title,omitempty"`
	Abstract string                           `json:"abstract,omitempty"`
	Versions []string                         `json:"versions,omitempty"`
	Settings OGCTilesAPIInnerSettingsResponse `json:"settings"`
}

type WMTSSettingsResponse struct {
	Enabled                 bool   `json:"enabled"`
	Public                  bool   `json:"public"`
	Title                   string `json:"title,omitempty"`
	Abstract                string `json:"abstract,omitempty"`
	FeatureInfoEnabled      bool   `json:"feature_info_enabled"`
	VectorTilesEnabled      bool   `json:"vector_tiles_enabled,omitempty"`
	TileMatrixLimitsEnabled bool   `json:"tile_matrix_limits_enabled"`
	ProviderName            string `json:"provider_name,omitempty"`
	ProviderSite            string `json:"provider_site,omitempty"`
	ContactName             string `json:"contact_name,omitempty"`
	ContactPosition         string `json:"contact_position,omitempty"`
	ContactEmail            string `json:"contact_email,omitempty"`
}

func (h *handler) getWMTSSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	wmtsStore, ok := h.store.(store.WMTSStore)
	if !ok {
		writeError(w, http.StatusNotImplemented, "Not Implemented", "WMTS persistence unavailable")
		return
	}
	settings, err := wmtsStore.GetWMTSSettings(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get WMTS settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *handler) updateWMTSSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := h.resolveWorkspaceIDForSettings(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	var request WMTSSettingsResponse
	if readJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if request.ProviderSite != "" {
		providerSite, err := url.ParseRequestURI(request.ProviderSite)
		if err != nil || providerSite.Host == "" || (providerSite.Scheme != "http" && providerSite.Scheme != "https") {
			writeError(w, http.StatusBadRequest, "Bad Request", "provider_site must be an absolute HTTP(S) URL")
			return
		}
	}
	providerName := strings.TrimSpace(request.ProviderName)
	if providerName == "" {
		providerName = store.DefaultWMTSSettings().ProviderName
	}
	settings := store.WMTSSettings{
		Enabled: request.Enabled, Public: request.Public, Title: request.Title, Abstract: request.Abstract,
		FeatureInfoEnabled: request.FeatureInfoEnabled, VectorTilesEnabled: request.VectorTilesEnabled, TileMatrixLimitsEnabled: request.TileMatrixLimitsEnabled,
		ProviderName: providerName, ProviderSite: request.ProviderSite, ContactName: request.ContactName,
		ContactPosition: request.ContactPosition, ContactEmail: request.ContactEmail,
	}
	if err := h.registry.UpdateWMTSSettings(r.Context(), workspaceID, settings); err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update WMTS settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// getWMSSettings handles GET /workspaces/{workspace}/settings/wms
func (h *handler) getWMSSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForSettings(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	settings, err := h.store.GetWMSSettings(ctx, workspaceID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to get WMS settings", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get WMS settings")
		return
	}

	writeJSON(w, http.StatusOK, WMSSettingsResponse{
		Enabled:      settings.Enabled,
		Public:       settings.Public,
		MaxWidth:     settings.MaxWidth,
		MaxHeight:    settings.MaxHeight,
		Title:        settings.Title,
		Abstract:     settings.Abstract,
		DefaultStyle: settings.DefaultStyle,
		MaxPixels:    settings.MaxPixels, MaxRenderFeatures: settings.MaxRenderFeatures, MaxRenderVertices: settings.MaxRenderVertices,
		SimplifyEnabled: settings.SimplifyEnabled, SimplifyPixelTolerance: settings.SimplifyPixelTolerance,
		Extensions: settings.Extensions,
	})
}

// updateWMSSettings handles PUT /workspaces/{workspace}/settings/wms
func (h *handler) updateWMSSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForSettings(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req WMSSettingsResponse
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if exceeds(req.MaxWidth, h.cfg.WMS.MaxWidth) || exceeds(req.MaxHeight, h.cfg.WMS.MaxHeight) ||
		exceeds(req.MaxPixels, h.cfg.WMS.MaxPixels) || exceeds(req.MaxRenderFeatures, h.cfg.WMS.MaxRenderFeatures) ||
		exceeds(req.MaxRenderVertices, h.cfg.WMS.MaxRenderVertices) {
		writeError(w, http.StatusBadRequest, "Bad Request", "workspace WMS limits cannot exceed server limits")
		return
	}
	seenExtensions := map[string]bool{}
	for _, extension := range req.Extensions {
		if !store.ValidWMSExtension(extension) || !slices.Contains(h.cfg.WMS.Extensions, extension) || seenExtensions[extension] {
			writeError(w, http.StatusBadRequest, "Bad Request", "workspace WMS extensions must be unique and enabled by the server")
			return
		}
		seenExtensions[extension] = true
	}

	settings := store.WMSSettings{
		Enabled:      req.Enabled,
		Public:       req.Public,
		MaxWidth:     req.MaxWidth,
		MaxHeight:    req.MaxHeight,
		Title:        req.Title,
		Abstract:     req.Abstract,
		DefaultStyle: req.DefaultStyle,
		MaxPixels:    req.MaxPixels, MaxRenderFeatures: req.MaxRenderFeatures, MaxRenderVertices: req.MaxRenderVertices,
		SimplifyEnabled: req.SimplifyEnabled, SimplifyPixelTolerance: req.SimplifyPixelTolerance,
		Extensions: req.Extensions,
	}

	// Use registry.UpdateWMSSettings to update both store AND runtime registry
	if err := h.registry.UpdateWMSSettings(ctx, workspaceID, settings); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to update WMS settings", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update WMS settings")
		return
	}

	writeJSON(w, http.StatusOK, WMSSettingsResponse{
		Enabled:      settings.Enabled,
		Public:       settings.Public,
		MaxWidth:     settings.MaxWidth,
		MaxHeight:    settings.MaxHeight,
		Title:        settings.Title,
		Abstract:     settings.Abstract,
		DefaultStyle: settings.DefaultStyle,
		MaxPixels:    settings.MaxPixels, MaxRenderFeatures: settings.MaxRenderFeatures, MaxRenderVertices: settings.MaxRenderVertices,
		SimplifyEnabled: settings.SimplifyEnabled, SimplifyPixelTolerance: settings.SimplifyPixelTolerance,
		Extensions: settings.Extensions,
	})
}

// getWFSSettings handles GET /workspaces/{workspace}/settings/wfs
func (h *handler) getWFSSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForSettings(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	settings, err := h.store.GetWFSSettings(ctx, workspaceID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to get WFS settings", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get WFS settings")
		return
	}

	writeJSON(w, http.StatusOK, WFSSettingsResponse{
		Enabled:      settings.Enabled,
		Public:       settings.Public,
		MaxFeatures:  settings.MaxFeatures,
		Title:        settings.Title,
		Abstract:     settings.Abstract,
		DefaultCount: settings.DefaultCount, MaxOffset: settings.MaxOffset, CountTimeoutMS: settings.CountTimeoutMS,
	})
}

// updateWFSSettings handles PUT /workspaces/{workspace}/settings/wfs
func (h *handler) updateWFSSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForSettings(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req WFSSettingsResponse
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if exceeds(req.MaxFeatures, h.cfg.WFS.MaxFeatures) || exceeds(req.DefaultCount, h.cfg.WFS.DefaultCount) || exceeds(req.MaxOffset, h.cfg.WFS.MaxOffset) {
		writeError(w, http.StatusBadRequest, "Bad Request", "workspace WFS limits cannot exceed server limits")
		return
	}

	settings := store.WFSSettings{
		Enabled:      req.Enabled,
		Public:       req.Public,
		MaxFeatures:  req.MaxFeatures,
		Title:        req.Title,
		Abstract:     req.Abstract,
		DefaultCount: req.DefaultCount, MaxOffset: req.MaxOffset, CountTimeoutMS: req.CountTimeoutMS,
	}

	// Use registry.UpdateWFSSettings to update both store AND runtime registry
	if err := h.registry.UpdateWFSSettings(ctx, workspaceID, settings); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to update WFS settings", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update WFS settings")
		return
	}

	writeJSON(w, http.StatusOK, WFSSettingsResponse{
		Enabled:      settings.Enabled,
		Public:       settings.Public,
		MaxFeatures:  settings.MaxFeatures,
		Title:        settings.Title,
		Abstract:     settings.Abstract,
		DefaultCount: settings.DefaultCount, MaxOffset: settings.MaxOffset, CountTimeoutMS: settings.CountTimeoutMS,
	})
}

// getOGCAPISettings handles GET /workspaces/{workspace}/settings/ogcapi
func (h *handler) getOGCAPISettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForSettings(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	settings, err := h.store.GetOGCAPISettings(ctx, workspaceID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to get OGC API settings", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get OGC API settings")
		return
	}

	writeJSON(w, http.StatusOK, OGCAPISettingsResponse{
		Enabled:      settings.Enabled,
		Public:       settings.Public,
		Title:        settings.Title,
		Abstract:     settings.Abstract,
		LimitDefault: settings.LimitDefault, LimitMax: settings.LimitMax, MaxOffset: settings.MaxOffset,
	})
}

// updateOGCAPISettings handles PUT /workspaces/{workspace}/settings/ogcapi
func (h *handler) updateOGCAPISettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForSettings(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req OGCAPISettingsResponse
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if exceeds(req.LimitDefault, h.cfg.Paging.LimitDefault) || exceeds(req.LimitMax, h.cfg.Paging.LimitMax) || exceeds(req.MaxOffset, h.cfg.Paging.MaxOffset) {
		writeError(w, http.StatusBadRequest, "Bad Request", "workspace OGC API limits cannot exceed server limits")
		return
	}

	settings := store.OGCAPISettings{
		Enabled:      req.Enabled,
		Public:       req.Public,
		Title:        req.Title,
		Abstract:     req.Abstract,
		LimitDefault: req.LimitDefault, LimitMax: req.LimitMax, MaxOffset: req.MaxOffset,
	}

	// Use registry.UpdateOGCAPISettings to update both store AND runtime registry
	if err := h.registry.UpdateOGCAPISettings(ctx, workspaceID, settings); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to update OGC API settings", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update OGC API settings")
		return
	}

	writeJSON(w, http.StatusOK, OGCAPISettingsResponse{
		Enabled:      settings.Enabled,
		Public:       settings.Public,
		Title:        settings.Title,
		Abstract:     settings.Abstract,
		LimitDefault: settings.LimitDefault, LimitMax: settings.LimitMax, MaxOffset: settings.MaxOffset,
	})
}

func exceeds(value, ceiling int) bool {
	return value > 0 && ceiling > 0 && value > ceiling
}

func (h *handler) validateResourceCacheQuota(ctx context.Context, workspaceID string, quota int64) error {
	if quota < 0 {
		return &validationError{"tile_cache_quota_bytes cannot be negative"}
	}
	if h.cfg.PersistentCache.MaxBytes > 0 && quota > h.cfg.PersistentCache.MaxBytes {
		return &validationError{"resource tile cache quota cannot exceed the global quota"}
	}
	settings, err := h.store.GetOGCTilesAPISettings(ctx, workspaceID)
	if err != nil {
		return err
	}
	if settings.Settings.PersistentCacheQuotaBytes > 0 && quota > settings.Settings.PersistentCacheQuotaBytes {
		return &validationError{"resource tile cache quota cannot exceed the workspace quota"}
	}
	return nil
}

// getOGCTilesAPISettings handles GET /workspaces/{workspace}/settings/ogc-tiles
func (h *handler) getOGCTilesAPISettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForSettings(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	settings, err := h.store.GetOGCTilesAPISettings(ctx, workspaceID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to get OGC Tiles API settings", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get OGC Tiles API settings")
		return
	}

	writeJSON(w, http.StatusOK, OGCTilesAPISettingsResponse{
		Enabled:  settings.Enabled,
		Public:   settings.Public,
		Title:    settings.Title,
		Abstract: settings.Abstract,
		Versions: settings.Versions,
		Settings: OGCTilesAPIInnerSettingsResponse{
			DatasetMapLayerGroupID: &settings.Settings.DatasetMapLayerGroupID,
			TileMatrixSets:         settings.Settings.TileMatrixSets,
			VectorTiles: OGCTilesAPIVectorSettingsResponse{
				Enabled: settings.Settings.VectorTiles.Enabled,
				Formats: settings.Settings.VectorTiles.Formats,
			},
			MapTiles: OGCTilesAPIMapSettingsResponse{
				Enabled: settings.Settings.MapTiles.Enabled,
				Formats: settings.Settings.MapTiles.Formats,
			},
			CacheEnabled: settings.Settings.CacheEnabled,
			MaxFeatures:  settings.Settings.MaxFeatures, MaxVertices: settings.Settings.MaxVertices, MaxTileBytes: settings.Settings.MaxTileBytes,
			PersistentCacheQuotaBytes: settings.Settings.PersistentCacheQuotaBytes,
		},
	})
}

// updateOGCTilesAPISettings handles PUT /workspaces/{workspace}/settings/ogc-tiles
func (h *handler) updateOGCTilesAPISettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForSettings(ctx, workspaceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to resolve workspace", "workspace", workspaceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve workspace")
		return
	}

	var req OGCTilesAPISettingsResponse
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if req.Settings.VectorTiles.Enabled && !slices.Equal(req.Settings.VectorTiles.Formats, []string{"application/vnd.mapbox-vector-tile"}) {
		writeError(w, http.StatusBadRequest, "Bad Request", "vector tile formats must be exactly application/vnd.mapbox-vector-tile")
		return
	}
	if req.Settings.MapTiles.Enabled {
		if len(req.Settings.MapTiles.Formats) == 0 {
			writeError(w, http.StatusBadRequest, "Bad Request", "at least one map tile format is required")
			return
		}
		for _, format := range req.Settings.MapTiles.Formats {
			if format != "image/png" && format != "image/jpeg" && format != "image/webp" {
				writeError(w, http.StatusBadRequest, "Bad Request", "unsupported map tile format")
				return
			}
		}
	}
	if exceeds(req.Settings.MaxFeatures, h.cfg.Tiles.MaxFeatures) || exceeds(req.Settings.MaxVertices, h.cfg.Tiles.MaxVertices) || exceeds(req.Settings.MaxTileBytes, h.cfg.Tiles.MaxTileBytes) {
		writeError(w, http.StatusBadRequest, "Bad Request", "workspace tile limits cannot exceed server limits")
		return
	}
	if req.Settings.PersistentCacheQuotaBytes < 0 || (h.cfg.PersistentCache.MaxBytes > 0 && req.Settings.PersistentCacheQuotaBytes > h.cfg.PersistentCache.MaxBytes) {
		writeError(w, http.StatusBadRequest, "Bad Request", "workspace persistent cache quota cannot exceed the global quota")
		return
	}

	if req.Settings.DatasetMapLayerGroupID == nil {
		previous, err := h.store.GetOGCTilesAPISettings(ctx, workspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Internal Error", "failed to load workspace map selection")
			return
		}
		req.Settings.DatasetMapLayerGroupID = &previous.Settings.DatasetMapLayerGroupID
	}
	settings := store.OGCTilesAPISettings{
		Enabled:  req.Enabled,
		Public:   req.Public,
		Title:    req.Title,
		Abstract: req.Abstract,
		Versions: req.Versions,
		Settings: store.OGCTilesAPIInnerSettings{
			DatasetMapLayerGroupID: *req.Settings.DatasetMapLayerGroupID,
			TileMatrixSets:         req.Settings.TileMatrixSets,
			VectorTiles: store.OGCTilesAPIVectorSettings{
				Enabled: req.Settings.VectorTiles.Enabled,
				Formats: req.Settings.VectorTiles.Formats,
			},
			MapTiles: store.OGCTilesAPIMapSettings{
				Enabled: req.Settings.MapTiles.Enabled,
				Formats: req.Settings.MapTiles.Formats,
			},
			CacheEnabled: req.Settings.CacheEnabled,
			MaxFeatures:  req.Settings.MaxFeatures, MaxVertices: req.Settings.MaxVertices, MaxTileBytes: req.Settings.MaxTileBytes,
			PersistentCacheQuotaBytes: req.Settings.PersistentCacheQuotaBytes,
		},
	}

	// Use registry.UpdateOGCTilesAPISettings to update both store AND runtime registry
	if err := h.registry.UpdateOGCTilesAPISettings(ctx, workspaceID, settings); err != nil {
		if errors.Is(err, store.ErrInvalidDatasetMap) {
			writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
			return
		}
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		h.logger.Error("failed to update OGC Tiles API settings", "workspace", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update OGC Tiles API settings")
		return
	}

	writeJSON(w, http.StatusOK, OGCTilesAPISettingsResponse{
		Enabled:  settings.Enabled,
		Public:   settings.Public,
		Title:    settings.Title,
		Abstract: settings.Abstract,
		Versions: settings.Versions,
		Settings: OGCTilesAPIInnerSettingsResponse{
			DatasetMapLayerGroupID: &settings.Settings.DatasetMapLayerGroupID,
			TileMatrixSets:         settings.Settings.TileMatrixSets,
			VectorTiles: OGCTilesAPIVectorSettingsResponse{
				Enabled: settings.Settings.VectorTiles.Enabled,
				Formats: settings.Settings.VectorTiles.Formats,
			},
			MapTiles: OGCTilesAPIMapSettingsResponse{
				Enabled: settings.Settings.MapTiles.Enabled,
				Formats: settings.Settings.MapTiles.Formats,
			},
			CacheEnabled: settings.Settings.CacheEnabled,
			MaxFeatures:  settings.Settings.MaxFeatures, MaxVertices: settings.Settings.MaxVertices, MaxTileBytes: settings.Settings.MaxTileBytes,
			PersistentCacheQuotaBytes: settings.Settings.PersistentCacheQuotaBytes,
		},
	})
}
