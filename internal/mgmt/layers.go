package mgmt

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// resolveWorkspaceIDForLayers resolves a workspace identifier (name or ID) to actual workspace ID.
func (h *handler) resolveWorkspaceIDForLayers(ctx context.Context, identifier string) (string, error) {
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

// resolveServiceIDForLayers resolves a service identifier (name or ID) to actual service ID.
func (h *handler) resolveServiceIDForLayers(ctx context.Context, workspaceID, identifier string) (string, error) {
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

// DimensionResponse is the API response for a layer dimension.
type DimensionResponse struct {
	Name           string `json:"name"`
	Units          string `json:"units"`
	SourceAxis     string `json:"source_axis,omitempty"`
	SourceProperty string `json:"source_property,omitempty"`
	EndProperty    string `json:"end_property,omitempty"`
	Default        string `json:"default,omitempty"`
	MultipleValues bool   `json:"multiple_values,omitempty"`
	NearestValue   bool   `json:"nearest_value,omitempty"`
	Current        bool   `json:"current,omitempty"`
	Extent         string `json:"extent"`
}

// LayerResponse is the API response for a layer.
type LayerResponse struct {
	ID                  string                          `json:"id"`
	ServiceID           string                          `json:"service_id"`
	SourceLayer         string                          `json:"source_layer"`
	PublicID            string                          `json:"public_id"`
	Title               string                          `json:"title,omitempty"`
	Description         string                          `json:"description,omitempty"`
	Enabled             bool                            `json:"enabled"`
	CRSDefault          int                             `json:"crs_default,omitempty"`
	Dimensions          []*DimensionResponse            `json:"dimensions,omitempty"`
	IsSQLView           bool                            `json:"is_sql_view,omitempty"`
	SQLViewConfig       *SQLViewConfigResponse          `json:"sql_view_config,omitempty"`
	Public              bool                            `json:"public"`
	AllowedRoles        []string                        `json:"allowed_roles,omitempty"`
	DefaultStyle        string                          `json:"default_style,omitempty"`
	Styles              []string                        `json:"styles,omitempty"`
	NativeExtent        *store.SpatialExtent            `json:"native_extent,omitempty"`
	TileCacheQuotaBytes int64                           `json:"tile_cache_quota_bytes,omitempty"`
	TileCacheParameters *store.TileCacheParameterPolicy `json:"tile_cache_parameters,omitempty"`
	TileCacheGeneration int64                           `json:"tile_cache_generation"`
	CreatedAt           time.Time                       `json:"created_at"`
	UpdatedAt           time.Time                       `json:"updated_at"`
}

// SQLViewConfigResponse is the API response for SQL View configuration.
type SQLViewConfigResponse struct {
	SQL            string                     `json:"sql"`
	GeometryColumn string                     `json:"geometry_column"`
	GeometryType   string                     `json:"geometry_type,omitempty"`
	SRID           int                        `json:"srid,omitempty"`
	IDColumn       string                     `json:"id_column,omitempty"`
	Properties     []*SQLViewPropertyResponse `json:"properties,omitempty"`
	ReadOnly       bool                       `json:"read_only"`
}

// SQLViewPropertyResponse is the API response for a SQL View property.
type SQLViewPropertyResponse struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// DimensionInput is the request input for a layer dimension.
type DimensionInput struct {
	Name           string `json:"name"`
	Units          string `json:"units"`
	SourceAxis     string `json:"source_axis,omitempty"`
	SourceProperty string `json:"source_property,omitempty"`
	EndProperty    string `json:"end_property,omitempty"`
	Default        string `json:"default,omitempty"`
	MultipleValues bool   `json:"multiple_values,omitempty"`
	NearestValue   bool   `json:"nearest_value,omitempty"`
	Current        bool   `json:"current,omitempty"`
	Extent         string `json:"extent"`
}

// CreateLayerRequest is the request to create a layer.
type CreateLayerRequest struct {
	SourceLayer string              `json:"source_layer"`
	PublicID    string              `json:"public_id,omitempty"`
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Enabled     *bool               `json:"enabled,omitempty"`
	CRSDefault  int                 `json:"crs_default,omitempty"`
	Dimensions  []*DimensionInput   `json:"dimensions,omitempty"`
	SQLView     *SQLViewConfigInput `json:"sql_view,omitempty"` // If provided, creates a SQL View layer
	// Public marks the layer readable by anyone who can reach the service.
	Public bool `json:"public,omitempty"`
	// AllowedRoles restricts read access to these workspace roles (empty = any
	// principal with workspace access).
	AllowedRoles        []string                        `json:"allowed_roles,omitempty"`
	DefaultStyle        string                          `json:"default_style,omitempty"`
	Styles              []string                        `json:"styles,omitempty"`
	TileCacheQuotaBytes int64                           `json:"tile_cache_quota_bytes,omitempty"`
	TileCacheParameters *store.TileCacheParameterPolicy `json:"tile_cache_parameters,omitempty"`
}

// SQLViewConfigInput is the request input for SQL View configuration.
type SQLViewConfigInput struct {
	SQL            string `json:"sql"`
	GeometryColumn string `json:"geometry_column"`
	GeometryType   string `json:"geometry_type,omitempty"`
	SRID           int    `json:"srid,omitempty"`
	IDColumn       string `json:"id_column,omitempty"`
}

// ValidateSQLRequest is the request to validate a SQL query.
type ValidateSQLRequest struct {
	SQL string `json:"sql"`
}

// ValidateSQLResponse is the response from SQL validation.
type ValidateSQLResponse struct {
	Valid      bool                      `json:"valid"`
	Error      string                    `json:"error,omitempty"`
	Discovered *SQLViewDiscoveryResponse `json:"discovered,omitempty"`
}

// SQLViewDiscoveryResponse contains auto-discovered metadata from a SQL query.
type SQLViewDiscoveryResponse struct {
	Columns           []*SQLViewColumnResponse `json:"columns"`
	GeometryColumn    string                   `json:"geometry_column"`
	GeometryType      string                   `json:"geometry_type"`
	SRID              int                      `json:"srid"`
	SuggestedIDColumn string                   `json:"suggested_id_column"`
}

// SQLViewColumnResponse describes a discovered column.
type SQLViewColumnResponse struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// UpdateLayerRequest is the request to update a layer.
type UpdateLayerRequest struct {
	PublicID            string                          `json:"public_id,omitempty"`
	Title               *string                         `json:"title,omitempty"`
	Description         *string                         `json:"description,omitempty"`
	Enabled             *bool                           `json:"enabled,omitempty"`
	CRSDefault          *int                            `json:"crs_default,omitempty"`
	Dimensions          []*DimensionInput               `json:"dimensions,omitempty"`
	Public              *bool                           `json:"public,omitempty"`
	AllowedRoles        []string                        `json:"allowed_roles,omitempty"`
	DefaultStyle        *string                         `json:"default_style,omitempty"`
	Styles              []string                        `json:"styles,omitempty"`
	TileCacheQuotaBytes *int64                          `json:"tile_cache_quota_bytes,omitempty"`
	TileCacheParameters *store.TileCacheParameterPolicy `json:"tile_cache_parameters,omitempty"`
}

// listLayers handles GET /workspaces/{workspace}/services/{service}/layers
func (h *handler) listLayers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceIdentifier := chi.URLParam(r, "service")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForLayers(ctx, workspaceIdentifier)
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
	serviceID, err := h.resolveServiceIDForLayers(ctx, workspaceID, serviceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		h.logger.Error("failed to resolve service", "service", serviceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve service")
		return
	}

	layers, err := h.store.ListLayers(ctx, serviceID)
	if err != nil {
		h.logger.Error("failed to list layers", "service", serviceID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list layers")
		return
	}

	var result []LayerResponse
	for _, l := range layers {
		// Convert dimensions for response
		var dims []*DimensionResponse
		for _, dim := range l.Dimensions {
			dims = append(dims, &DimensionResponse{
				Name:           dim.Name,
				Units:          dim.Units,
				SourceAxis:     dim.SourceAxis,
				SourceProperty: dim.SourceProperty,
				EndProperty:    dim.EndProperty,
				Default:        dim.Default,
				MultipleValues: dim.MultipleValues,
				NearestValue:   dim.NearestValue,
				Current:        dim.Current,
				Extent:         dim.Extent,
			})
		}
		// Convert SQL view config for response
		var sqlViewConfig *SQLViewConfigResponse
		if l.SQLViewConfig != nil {
			sqlViewConfig = convertSQLViewConfigToResponse(l.SQLViewConfig)
		}
		result = append(result, LayerResponse{
			ID:                  l.ID,
			ServiceID:           l.ServiceID,
			SourceLayer:         l.SourceLayer,
			PublicID:            l.PublicID,
			Title:               l.Title,
			Description:         l.Description,
			Enabled:             l.Enabled,
			CRSDefault:          l.CRSDefault,
			Dimensions:          dims,
			IsSQLView:           l.IsSQLView,
			SQLViewConfig:       sqlViewConfig,
			Public:              l.Public,
			AllowedRoles:        l.AllowedRoles,
			DefaultStyle:        l.DefaultStyle,
			Styles:              l.Styles,
			NativeExtent:        l.NativeExtent,
			TileCacheQuotaBytes: l.TileCacheQuotaBytes,
			TileCacheParameters: l.TileCacheParameters,
			TileCacheGeneration: l.TileCacheGeneration,
			CreatedAt:           l.CreatedAt,
			UpdatedAt:           l.UpdatedAt,
		})
	}

	if result == nil {
		result = []LayerResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": workspaceID,
		"service_id":   serviceID,
		"layers":       result,
	})
}

// createLayer handles POST /workspaces/{workspace}/services/{service}/layers
func (h *handler) createLayer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceIdentifier := chi.URLParam(r, "service")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForLayers(ctx, workspaceIdentifier)
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
	serviceID, err := h.resolveServiceIDForLayers(ctx, workspaceID, serviceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		h.logger.Error("failed to resolve service", "service", serviceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve service")
		return
	}

	var req CreateLayerRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if err := h.validateStyleBindings(ctx, workspaceID, req.DefaultStyle, req.Styles, false); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if err := h.validateResourceCacheQuota(ctx, workspaceID, req.TileCacheQuotaBytes); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if err := h.validateTileCacheParameters(req.TileCacheParameters); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	// Determine if this is a SQL View layer
	isSQLView := req.SQLView != nil
	var sqlViewProperties []*store.SQLViewProperty

	// For SQL View layers, source_layer is not required (set to a placeholder)
	// For regular layers, source_layer is required
	if !isSQLView && req.SourceLayer == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "source_layer is required for non-SQL-view layers")
		return
	}

	// Validate SQL view configuration
	if isSQLView {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if req.SQLView.SQL == "" {
			writeError(w, http.StatusBadRequest, "Bad Request", "sql_view.sql is required")
			return
		}
		if req.SQLView.GeometryColumn == "" {
			writeError(w, http.StatusBadRequest, "Bad Request", "sql_view.geometry_column is required")
			return
		}
		ws, release, ok := h.registry.AcquireByID(workspaceID)
		defer release()
		if !ok {
			writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
			return
		}
		svc := ws.ResolveService(serviceID)
		if svc == nil || svc.DataSource == nil {
			writeError(w, http.StatusServiceUnavailable, "Service Unavailable", "data source not available")
			return
		}
		sqlViewDS, ok := svc.DataSource.(datasource.SQLViewDataSource)
		if !ok {
			writeError(w, http.StatusBadRequest, "Bad Request", "data source does not support SQL views")
			return
		}
		if err := sqlViewDS.ValidateSQLView(ctx, req.SQLView.SQL); err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "invalid SQL view")
			return
		}
		discovery, err := sqlViewDS.DiscoverSQLViewColumns(ctx, req.SQLView.SQL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "could not discover SQL view columns")
			return
		}
		if discovery.GeometryColumn != req.SQLView.GeometryColumn {
			writeError(w, http.StatusBadRequest, "Bad Request", "geometry_column does not match the discovered geometry column")
			return
		}
		if req.SQLView.IDColumn == "" {
			req.SQLView.IDColumn = discovery.SuggestedIDColumn
		}
		if req.SQLView.IDColumn == "" {
			writeError(w, http.StatusBadRequest, "Bad Request", "sql_view.id_column is required; select a unique, non-null column returned by the view")
			return
		}
		idFound := false
		for _, col := range discovery.Columns {
			sqlViewProperties = append(sqlViewProperties, &store.SQLViewProperty{Name: col.Name, Type: string(col.JSONType)})
			if col.Name == req.SQLView.IDColumn {
				idFound = true
			}
		}
		if !idFound {
			writeError(w, http.StatusBadRequest, "Bad Request", "id_column was not returned by the SQL view")
			return
		}
		identityValidator, ok := svc.DataSource.(datasource.SQLViewIdentityValidator)
		if !ok {
			writeError(w, http.StatusBadRequest, "Bad Request", "data source cannot validate SQL-view feature identity")
			return
		}
		if err := identityValidator.ValidateSQLViewIdentity(ctx, &datasource.SQLViewConfig{SQL: req.SQLView.SQL, IDColumn: req.SQLView.IDColumn}); err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "sql_view.id_column must be unique and non-null; validation could not establish stable feature IDs")
			return
		}
		// Physical source_layer is never authoritative for a SQL publication.
		req.SourceLayer = "_sql_view_"
	}

	// Default public_id to source_layer if not provided
	if req.PublicID == "" {
		req.PublicID = req.SourceLayer
	}
	// Default title to public_id
	if req.Title == "" {
		req.Title = req.PublicID
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	crsDefault := 4326
	if req.CRSDefault != 0 {
		crsDefault = req.CRSDefault
	}

	// Convert dimensions to store format
	var dimensions []*store.Dimension
	for _, dim := range req.Dimensions {
		if dim == nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "dimensions must not contain null")
			return
		}
		dimensions = append(dimensions, &store.Dimension{
			Name:           dim.Name,
			Units:          dim.Units,
			SourceAxis:     dim.SourceAxis,
			SourceProperty: dim.SourceProperty,
			EndProperty:    dim.EndProperty,
			Default:        dim.Default,
			MultipleValues: dim.MultipleValues,
			NearestValue:   dim.NearestValue,
			Current:        dim.Current,
			Extent:         dim.Extent,
		})
	}

	var nativeExtent *store.SpatialExtent
	if !isSQLView && enabled {
		if _, code, err := h.validateLayerSource(ctx, workspaceID, serviceID, req.SourceLayer); err != nil {
			writeError(w, code, "Cannot publish source layer", err.Error())
			return
		}
	}
	if !isSQLView {
		if runtimeWorkspace, release, ok := h.registry.AcquireByID(workspaceID); ok {
			defer release()
			if service := runtimeWorkspace.ResolveService(serviceID); service != nil && service.DataSource != nil {
				if info, infoErr := service.DataSource.GetLayerInfo(ctx, req.SourceLayer); infoErr == nil {
					extent := info.Extent
					if extent == nil {
						if provider, supported := service.DataSource.(datasource.LayerExtentDataSource); supported {
							extent, _ = provider.GetLayerExtent(ctx, req.SourceLayer)
						}
					}
					if extent != nil {
						nativeExtent = &store.SpatialExtent{MinX: extent.MinX, MinY: extent.MinY, MaxX: extent.MaxX, MaxY: extent.MaxY, SRID: extent.SRID}
					}
				}
			}
		}
	}

	input := store.CreateLayerInput{
		ServiceID:           serviceID,
		SourceLayer:         req.SourceLayer,
		PublicID:            req.PublicID,
		Title:               req.Title,
		Description:         req.Description,
		Enabled:             enabled,
		CRSDefault:          crsDefault,
		Dimensions:          dimensions,
		IsSQLView:           isSQLView,
		Public:              req.Public,
		AllowedRoles:        req.AllowedRoles,
		DefaultStyle:        req.DefaultStyle,
		Styles:              req.Styles,
		NativeExtent:        nativeExtent,
		TileCacheQuotaBytes: req.TileCacheQuotaBytes,
		TileCacheParameters: req.TileCacheParameters,
	}

	// Convert SQL view configuration to store format
	if isSQLView {
		input.SQLViewConfig = &store.SQLViewConfig{
			SQL:            req.SQLView.SQL,
			GeometryColumn: req.SQLView.GeometryColumn,
			GeometryType:   req.SQLView.GeometryType,
			SRID:           req.SQLView.SRID,
			IDColumn:       req.SQLView.IDColumn,
			Properties:     sqlViewProperties,
			ReadOnly:       true, // Always read-only for SQL views
		}
		// Override CRS default with SQL view SRID if specified
		if req.SQLView.SRID != 0 && req.CRSDefault == 0 {
			input.CRSDefault = req.SQLView.SRID
		}
	}

	// Use registry.CreateLayer to update both store AND runtime registry
	layer, err := h.registry.CreateLayer(ctx, workspaceID, serviceID, input)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, http.StatusConflict, "Conflict", "public_id is already published in this workspace: "+req.PublicID)
			return
		}
		h.logger.Error("failed to create layer", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to create layer")
		return
	}

	// Convert dimensions for response
	var respDimensions []*DimensionResponse
	for _, dim := range layer.Dimensions {
		respDimensions = append(respDimensions, &DimensionResponse{
			Name:           dim.Name,
			Units:          dim.Units,
			SourceAxis:     dim.SourceAxis,
			SourceProperty: dim.SourceProperty,
			EndProperty:    dim.EndProperty,
			Default:        dim.Default,
			MultipleValues: dim.MultipleValues,
			NearestValue:   dim.NearestValue,
			Current:        dim.Current,
			Extent:         dim.Extent,
		})
	}

	// Convert SQL view config for response
	var sqlViewConfigResp *SQLViewConfigResponse
	if layer.SQLViewConfig != nil {
		sqlViewConfigResp = &SQLViewConfigResponse{
			SQL:            layer.SQLViewConfig.SQL,
			GeometryColumn: layer.SQLViewConfig.GeometryColumn,
			GeometryType:   layer.SQLViewConfig.GeometryType,
			SRID:           layer.SQLViewConfig.SRID,
			IDColumn:       layer.SQLViewConfig.IDColumn,
			ReadOnly:       layer.SQLViewConfig.ReadOnly,
		}
	}

	writeJSON(w, http.StatusCreated, LayerResponse{
		ID:                  layer.ID,
		ServiceID:           serviceID,
		SourceLayer:         layer.SourceLayer,
		PublicID:            layer.PublicID,
		Title:               layer.Title,
		Description:         layer.Description,
		Enabled:             layer.Enabled,
		CRSDefault:          layer.CRSDefault,
		Dimensions:          respDimensions,
		IsSQLView:           layer.IsSQLView,
		SQLViewConfig:       sqlViewConfigResp,
		Public:              layer.Public,
		AllowedRoles:        layer.AllowedRoles,
		DefaultStyle:        layer.DefaultStyle,
		Styles:              layer.Styles,
		NativeExtent:        layer.NativeExtent,
		TileCacheQuotaBytes: layer.TileCacheQuotaBytes,
		TileCacheParameters: layer.TileCacheParameters,
		TileCacheGeneration: layer.TileCacheGeneration,
	})
}

// getLayer handles GET /workspaces/{workspace}/services/{service}/layers/{layer}
func (h *handler) getLayer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceIdentifier := chi.URLParam(r, "service")
	layerID := chi.URLParam(r, "layer")
	workspaceID, err := h.resolveWorkspaceIDForLayers(ctx, workspaceIdentifier)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found")
		return
	}
	serviceID, err := h.resolveServiceIDForLayers(ctx, workspaceID, serviceIdentifier)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "service not found")
		return
	}

	layer, err := h.store.GetLayer(ctx, layerID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "layer not found")
			return
		}
		h.logger.Error("failed to get layer", "layer", layerID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to get layer")
		return
	}
	if layer.ServiceID != serviceID {
		writeError(w, http.StatusNotFound, "Not Found", "layer not found")
		return
	}

	// Convert dimensions for response
	var getDimensions []*DimensionResponse
	for _, dim := range layer.Dimensions {
		getDimensions = append(getDimensions, &DimensionResponse{
			Name:           dim.Name,
			Units:          dim.Units,
			SourceAxis:     dim.SourceAxis,
			SourceProperty: dim.SourceProperty,
			EndProperty:    dim.EndProperty,
			Default:        dim.Default,
			MultipleValues: dim.MultipleValues,
			NearestValue:   dim.NearestValue,
			Current:        dim.Current,
			Extent:         dim.Extent,
		})
	}

	// Convert SQL view config for response
	var sqlViewConfig *SQLViewConfigResponse
	if layer.SQLViewConfig != nil {
		sqlViewConfig = convertSQLViewConfigToResponse(layer.SQLViewConfig)
	}

	writeJSON(w, http.StatusOK, LayerResponse{
		ID:                  layer.ID,
		ServiceID:           layer.ServiceID,
		SourceLayer:         layer.SourceLayer,
		PublicID:            layer.PublicID,
		Title:               layer.Title,
		Description:         layer.Description,
		Enabled:             layer.Enabled,
		CRSDefault:          layer.CRSDefault,
		Dimensions:          getDimensions,
		IsSQLView:           layer.IsSQLView,
		SQLViewConfig:       sqlViewConfig,
		Public:              layer.Public,
		AllowedRoles:        layer.AllowedRoles,
		DefaultStyle:        layer.DefaultStyle,
		Styles:              layer.Styles,
		NativeExtent:        layer.NativeExtent,
		TileCacheQuotaBytes: layer.TileCacheQuotaBytes,
		TileCacheParameters: layer.TileCacheParameters,
		TileCacheGeneration: layer.TileCacheGeneration,
		CreatedAt:           layer.CreatedAt,
		UpdatedAt:           layer.UpdatedAt,
	})
}

// updateLayer handles PUT /workspaces/{workspace}/services/{service}/layers/{layer}
func (h *handler) updateLayer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceIdentifier := chi.URLParam(r, "service")
	layerID := chi.URLParam(r, "layer")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForLayers(ctx, workspaceIdentifier)
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
	serviceID, err := h.resolveServiceIDForLayers(ctx, workspaceID, serviceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		h.logger.Error("failed to resolve service", "service", serviceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve service")
		return
	}
	existingLayer, err := h.store.GetLayer(ctx, layerID)
	if err != nil || existingLayer.ServiceID != serviceID {
		writeError(w, http.StatusNotFound, "Not Found", "layer not found")
		return
	}

	var req UpdateLayerRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	defaultStyle := existingLayer.DefaultStyle
	if req.DefaultStyle != nil {
		defaultStyle = *req.DefaultStyle
	}
	styles := existingLayer.Styles
	if req.Styles != nil {
		styles = req.Styles
	}
	if err := h.validateStyleBindings(ctx, workspaceID, defaultStyle, styles, false); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if req.TileCacheQuotaBytes != nil {
		if err := h.validateResourceCacheQuota(ctx, workspaceID, *req.TileCacheQuotaBytes); err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
			return
		}
	}
	if err := h.validateTileCacheParameters(req.TileCacheParameters); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	input := store.UpdateLayerInput{}
	if req.PublicID != "" {
		input.PublicID = &req.PublicID
	}
	input.Title = req.Title
	input.Description = req.Description
	if req.Enabled != nil {
		if *req.Enabled {
			existing, err := h.store.GetLayer(ctx, layerID)
			if err == nil && !existing.IsSQLView {
				if _, code, err := h.validateLayerSource(ctx, workspaceID, serviceID, existing.SourceLayer); err != nil {
					writeError(w, code, "Cannot enable source layer", err.Error())
					return
				}
			}
		}
		input.Enabled = req.Enabled
	}
	if req.CRSDefault != nil {
		input.CRSDefault = req.CRSDefault
	}
	// Convert dimensions to store format
	if req.Dimensions != nil {
		dimensions := make([]*store.Dimension, 0, len(req.Dimensions))
		for _, dim := range req.Dimensions {
			if dim == nil {
				writeError(w, http.StatusBadRequest, "Bad Request", "dimensions must not contain null")
				return
			}
			dimensions = append(dimensions, &store.Dimension{
				Name:           dim.Name,
				Units:          dim.Units,
				SourceAxis:     dim.SourceAxis,
				SourceProperty: dim.SourceProperty,
				EndProperty:    dim.EndProperty,
				Default:        dim.Default,
				MultipleValues: dim.MultipleValues,
				NearestValue:   dim.NearestValue,
				Current:        dim.Current,
				Extent:         dim.Extent,
			})
		}
		input.Dimensions = dimensions
	}
	if req.Public != nil {
		input.Public = req.Public
	}
	if req.AllowedRoles != nil {
		input.AllowedRoles = req.AllowedRoles
	}
	if req.DefaultStyle != nil {
		input.DefaultStyle = req.DefaultStyle
	}
	if req.Styles != nil {
		input.Styles = req.Styles
	}
	if req.TileCacheQuotaBytes != nil {
		input.TileCacheQuotaBytes = req.TileCacheQuotaBytes
	}
	if req.TileCacheParameters != nil {
		input.TileCacheParameters = req.TileCacheParameters
	}

	// Use registry.UpdateLayer to update both store AND runtime registry
	layer, err := h.registry.UpdateLayer(ctx, workspaceID, serviceID, layerID, input)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			writeError(w, http.StatusConflict, "Conflict", "public_id is already published in this workspace: "+req.PublicID)
			return
		}
		if errors.Is(err, workspace.ErrResourceReferenced) {
			writeError(w, http.StatusConflict, "Conflict", err.Error())
			return
		}
		if err == store.ErrNotFound || err == workspace.ErrLayerNotFound || err == workspace.ErrServiceNotFound || err == workspace.ErrWorkspaceNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "layer not found")
			return
		}
		h.logger.Error("failed to update layer", "layer", layerID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update layer")
		return
	}

	// Convert dimensions for response
	var updateRespDimensions []*DimensionResponse
	for _, dim := range layer.Dimensions {
		updateRespDimensions = append(updateRespDimensions, &DimensionResponse{
			Name:           dim.Name,
			Units:          dim.Units,
			SourceAxis:     dim.SourceAxis,
			SourceProperty: dim.SourceProperty,
			EndProperty:    dim.EndProperty,
			Default:        dim.Default,
			MultipleValues: dim.MultipleValues,
			NearestValue:   dim.NearestValue,
			Current:        dim.Current,
			Extent:         dim.Extent,
		})
	}

	// Convert SQL view config for response
	var sqlViewConfig *SQLViewConfigResponse
	if layer.SQLViewConfig != nil {
		sqlViewConfig = &SQLViewConfigResponse{
			SQL:            layer.SQLViewConfig.SQL,
			GeometryColumn: layer.SQLViewConfig.GeometryColumn,
			GeometryType:   layer.SQLViewConfig.GeometryType,
			SRID:           layer.SQLViewConfig.SRID,
			IDColumn:       layer.SQLViewConfig.IDColumn,
			ReadOnly:       layer.SQLViewConfig.ReadOnly,
		}
	}

	writeJSON(w, http.StatusOK, LayerResponse{
		ID:                  layer.ID,
		ServiceID:           serviceID,
		SourceLayer:         layer.SourceLayer,
		PublicID:            layer.PublicID,
		Title:               layer.Title,
		Description:         layer.Description,
		Enabled:             layer.Enabled,
		CRSDefault:          layer.CRSDefault,
		Dimensions:          updateRespDimensions,
		IsSQLView:           layer.IsSQLView,
		SQLViewConfig:       sqlViewConfig,
		Public:              layer.Public,
		AllowedRoles:        layer.AllowedRoles,
		DefaultStyle:        layer.DefaultStyle,
		Styles:              layer.Styles,
		NativeExtent:        layer.NativeExtent,
		TileCacheQuotaBytes: layer.TileCacheQuotaBytes,
		TileCacheParameters: layer.TileCacheParameters,
		TileCacheGeneration: layer.TileCacheGeneration,
	})
}

// deleteLayer handles DELETE /workspaces/{workspace}/services/{service}/layers/{layer}
func (h *handler) deleteLayer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceIdentifier := chi.URLParam(r, "service")
	layerID := chi.URLParam(r, "layer")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForLayers(ctx, workspaceIdentifier)
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
	serviceID, err := h.resolveServiceIDForLayers(ctx, workspaceID, serviceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		h.logger.Error("failed to resolve service", "service", serviceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve service")
		return
	}

	// Use registry.DeleteLayer to update both store AND runtime registry
	if err := h.registry.DeleteLayer(ctx, workspaceID, serviceID, layerID); err != nil {
		if errors.Is(err, workspace.ErrResourceReferenced) {
			writeError(w, http.StatusConflict, "Conflict", err.Error())
			return
		}
		if err == store.ErrNotFound || err == workspace.ErrLayerNotFound || err == workspace.ErrServiceNotFound || err == workspace.ErrWorkspaceNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "layer not found")
			return
		}
		h.logger.Error("failed to delete layer", "layer", layerID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete layer")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// validateSQL handles POST /workspaces/{workspace}/services/{service}/validate-sql
func (h *handler) validateSQL(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	workspaceIdentifier := chi.URLParam(r, "workspace")
	serviceIdentifier := chi.URLParam(r, "service")

	// Resolve workspace name/ID to actual workspace ID
	workspaceID, err := h.resolveWorkspaceIDForLayers(ctx, workspaceIdentifier)
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
	serviceID, err := h.resolveServiceIDForLayers(ctx, workspaceID, serviceIdentifier)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "Not Found", "service not found")
			return
		}
		h.logger.Error("failed to resolve service", "service", serviceIdentifier, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to resolve service")
		return
	}

	var req ValidateSQLRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}

	if req.SQL == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "sql is required")
		return
	}

	// Get the workspace and service to access the data source
	ws, release, ok := h.registry.AcquireByID(workspaceID)
	defer release()
	if !ok {
		writeError(w, http.StatusNotFound, "Not Found", "workspace not found in registry")
		return
	}

	svc := ws.ResolveService(serviceID)
	if svc == nil {
		writeError(w, http.StatusNotFound, "Not Found", "service not found in registry")
		return
	}

	if svc.DataSource == nil {
		writeError(w, http.StatusServiceUnavailable, "Service Unavailable", "data source not available")
		return
	}

	// Check if the data source supports SQL views
	sqlViewDS, ok := svc.DataSource.(datasource.SQLViewDataSource)
	if !ok {
		writeError(w, http.StatusBadRequest, "Bad Request", "data source does not support SQL views")
		return
	}

	// Validate the SQL
	if err := sqlViewDS.ValidateSQLView(ctx, req.SQL); err != nil {
		writeJSON(w, http.StatusOK, ValidateSQLResponse{
			Valid: false,
			Error: err.Error(),
		})
		return
	}

	// Discover columns
	discovery, err := sqlViewDS.DiscoverSQLViewColumns(ctx, req.SQL)
	if err != nil {
		writeJSON(w, http.StatusOK, ValidateSQLResponse{
			Valid: false,
			Error: err.Error(),
		})
		return
	}

	// Convert discovery to response format
	var columns []*SQLViewColumnResponse
	for _, col := range discovery.Columns {
		columns = append(columns, &SQLViewColumnResponse{
			Name: col.Name,
			Type: string(col.JSONType),
		})
	}

	writeJSON(w, http.StatusOK, ValidateSQLResponse{
		Valid: true,
		Discovered: &SQLViewDiscoveryResponse{
			Columns:           columns,
			GeometryColumn:    discovery.GeometryColumn,
			GeometryType:      discovery.GeometryType,
			SRID:              discovery.SRID,
			SuggestedIDColumn: discovery.SuggestedIDColumn,
		},
	})
}

// convertSQLViewConfigToResponse converts a store.SQLViewConfig to SQLViewConfigResponse.
func convertSQLViewConfigToResponse(cfg *store.SQLViewConfig) *SQLViewConfigResponse {
	if cfg == nil {
		return nil
	}
	resp := &SQLViewConfigResponse{
		SQL:            cfg.SQL,
		GeometryColumn: cfg.GeometryColumn,
		GeometryType:   cfg.GeometryType,
		SRID:           cfg.SRID,
		IDColumn:       cfg.IDColumn,
		ReadOnly:       cfg.ReadOnly,
	}
	if len(cfg.Properties) > 0 {
		resp.Properties = make([]*SQLViewPropertyResponse, len(cfg.Properties))
		for i, prop := range cfg.Properties {
			resp.Properties[i] = &SQLViewPropertyResponse{
				Name: prop.Name,
				Type: prop.Type,
			}
		}
	}
	return resp
}

func (h *handler) validateTileCacheParameters(policy *store.TileCacheParameterPolicy) error {
	if policy == nil {
		return nil
	}
	if policy.MetatileFactor < 0 || policy.GutterPixels < 0 {
		return errors.New("tile cache metatile factor and gutter cannot be negative")
	}
	if maximum := max(1, h.cfg.Tiles.MaxMetatileFactor); policy.MetatileFactor > maximum {
		return errors.New("tile cache metatile factor exceeds the server limit")
	}
	if maximum := max(0, h.cfg.Tiles.MaxGutterPixels); policy.GutterPixels > maximum {
		return errors.New("tile cache gutter exceeds the server limit")
	}
	combinations := 1
	maximum := h.cfg.Tiles.MaxParameterCombinations
	for _, values := range [][]string{policy.Styles, policy.Times, policy.Elevations} {
		seen := make(map[string]bool, len(values))
		for _, value := range values {
			if value == "" || seen[value] {
				return errors.New("tile cache parameter allowlists must contain unique, non-empty values")
			}
			seen[value] = true
		}
		factor := max(1, len(values))
		if maximum > 0 && combinations > maximum/factor {
			return errors.New("tile cache parameter combinations exceed the server limit")
		}
		combinations *= factor
	}
	if maximum > 0 && combinations > maximum {
		return errors.New("tile cache parameter combinations exceed the server limit")
	}
	return nil
}
