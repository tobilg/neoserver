package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Layer operations

func (s *DuckDBStore) CreateLayer(ctx context.Context, input CreateLayerInput) (*Layer, error) {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	if err := s.checkRoleAssignments(ctx, input.AllowedRoles); err != nil {
		return nil, err
	}
	id := uuid.New().String()
	now := time.Now().UTC()
	crsDefault := input.CRSDefault
	if crsDefault == 0 {
		crsDefault = 4326
	}

	// Serialize dimensions to JSON
	dimensionsJSON := "[]"
	if len(input.Dimensions) > 0 {
		b, err := json.Marshal(input.Dimensions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal dimensions: %w", err)
		}
		dimensionsJSON = string(b)
	}

	// Serialize SQL view config to JSON
	var sqlViewConfigJSON *string
	if input.SQLViewConfig != nil {
		// Ensure ReadOnly is always true for SQL views
		input.SQLViewConfig.ReadOnly = true
		b, err := json.Marshal(input.SQLViewConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal sql_view_config: %w", err)
		}
		s := string(b)
		sqlViewConfigJSON = &s
	}

	allowedRolesJSON := marshalAllowedRoles(input.AllowedRoles)
	stylesJSON := marshalJSON(input.Styles, "[]")
	var nativeExtentJSON any
	var tileCacheParametersJSON any
	if input.NativeExtent != nil {
		nativeExtentJSON = marshalJSON(input.NativeExtent, "null")
	}
	if input.TileCacheParameters != nil {
		tileCacheParametersJSON = marshalJSON(input.TileCacheParameters, "null")
	}

	_, err := s.execCapabilitiesMutation(ctx, "SELECT workspace_id FROM services WHERE id = ?", input.ServiceID, `
		INSERT INTO layers (id, service_id, source_layer, public_id, title, description, enabled, crs_default, dimensions, is_sql_view, sql_view_config, public, allowed_roles, default_style, styles, native_extent, tile_cache_quota_bytes, tile_cache_parameters, tile_cache_generation, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, id, input.ServiceID, input.SourceLayer, input.PublicID, input.Title, input.Description, input.Enabled, crsDefault, dimensionsJSON, input.IsSQLView, sqlViewConfigJSON, input.Public, allowedRolesJSON, input.DefaultStyle, stylesJSON, nativeExtentJSON, input.TileCacheQuotaBytes, tileCacheParametersJSON, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to create layer: %w", err)
	}

	return &Layer{
		ID:                  id,
		ServiceID:           input.ServiceID,
		SourceLayer:         input.SourceLayer,
		PublicID:            input.PublicID,
		Title:               input.Title,
		Description:         input.Description,
		Enabled:             input.Enabled,
		CRSDefault:          crsDefault,
		Dimensions:          input.Dimensions,
		IsSQLView:           input.IsSQLView,
		SQLViewConfig:       input.SQLViewConfig,
		Public:              input.Public,
		AllowedRoles:        input.AllowedRoles,
		DefaultStyle:        input.DefaultStyle,
		Styles:              input.Styles,
		NativeExtent:        input.NativeExtent,
		TileCacheQuotaBytes: input.TileCacheQuotaBytes,
		TileCacheParameters: input.TileCacheParameters,
		TileCacheGeneration: 1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}, nil
}

// marshalAllowedRoles serializes the allowed-roles list to a JSON array string
// for storage (empty/nil becomes "[]").
func marshalAllowedRoles(roles []string) string {
	if len(roles) == 0 {
		return "[]"
	}
	b, err := json.Marshal(roles)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// parseAllowedRoles decodes the allowed_roles JSON column (DuckDB returns JSON
// as a variety of Go types) into a string slice.
func parseAllowedRoles(v any) []string {
	if v == nil {
		return nil
	}
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var roles []string
	if json.Unmarshal(jsonBytes, &roles) != nil {
		return nil
	}
	return roles
}

func (s *DuckDBStore) GetLayer(ctx context.Context, id string) (*Layer, error) {
	var layer Layer
	var dimensionsJSON any    // DuckDB returns JSON as various types
	var sqlViewConfigJSON any // DuckDB returns JSON as various types
	var allowedRolesJSON any  // DuckDB returns JSON as various types
	var stylesJSON any
	var nativeExtentJSON any
	var tileCacheParametersJSON any
	err := s.db.QueryRowContext(ctx, `
		SELECT id, service_id, source_layer, public_id, title, description, enabled, crs_default, dimensions, is_sql_view, sql_view_config, public, allowed_roles, default_style, styles, native_extent, tile_cache_quota_bytes, tile_cache_parameters, tile_cache_generation, created_at, updated_at
		FROM layers WHERE id = ?
	`, id).Scan(&layer.ID, &layer.ServiceID, &layer.SourceLayer, &layer.PublicID, &layer.Title, &layer.Description, &layer.Enabled, &layer.CRSDefault, &dimensionsJSON, &layer.IsSQLView, &sqlViewConfigJSON, &layer.Public, &allowedRolesJSON, &layer.DefaultStyle, &stylesJSON, &nativeExtentJSON, &layer.TileCacheQuotaBytes, &tileCacheParametersJSON, &layer.TileCacheGeneration, &layer.CreatedAt, &layer.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get layer: %w", err)
	}
	// Parse dimensions JSON
	if dimensionsJSON != nil {
		jsonBytes, err := json.Marshal(dimensionsJSON)
		if err == nil {
			json.Unmarshal(jsonBytes, &layer.Dimensions)
		}
	}
	// Parse SQL view config JSON
	if sqlViewConfigJSON != nil {
		jsonBytes, err := json.Marshal(sqlViewConfigJSON)
		if err == nil {
			var cfg SQLViewConfig
			if json.Unmarshal(jsonBytes, &cfg) == nil {
				layer.SQLViewConfig = &cfg
			}
		}
	}
	layer.AllowedRoles = parseAllowedRoles(allowedRolesJSON)
	decodeJSONColumn(stylesJSON, &layer.Styles)
	decodeJSONColumn(nativeExtentJSON, &layer.NativeExtent)
	decodeJSONColumn(tileCacheParametersJSON, &layer.TileCacheParameters)
	return &layer, nil
}

func (s *DuckDBStore) GetLayerByPublicID(ctx context.Context, serviceID, publicID string) (*Layer, error) {
	var layer Layer
	var dimensionsJSON any    // DuckDB returns JSON as various types
	var sqlViewConfigJSON any // DuckDB returns JSON as various types
	var allowedRolesJSON any  // DuckDB returns JSON as various types
	var stylesJSON any
	var nativeExtentJSON any
	var tileCacheParametersJSON any
	err := s.db.QueryRowContext(ctx, `
		SELECT id, service_id, source_layer, public_id, title, description, enabled, crs_default, dimensions, is_sql_view, sql_view_config, public, allowed_roles, default_style, styles, native_extent, tile_cache_quota_bytes, tile_cache_parameters, tile_cache_generation, created_at, updated_at
		FROM layers WHERE service_id = ? AND public_id = ?
	`, serviceID, publicID).Scan(&layer.ID, &layer.ServiceID, &layer.SourceLayer, &layer.PublicID, &layer.Title, &layer.Description, &layer.Enabled, &layer.CRSDefault, &dimensionsJSON, &layer.IsSQLView, &sqlViewConfigJSON, &layer.Public, &allowedRolesJSON, &layer.DefaultStyle, &stylesJSON, &nativeExtentJSON, &layer.TileCacheQuotaBytes, &tileCacheParametersJSON, &layer.TileCacheGeneration, &layer.CreatedAt, &layer.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get layer: %w", err)
	}
	// Parse dimensions JSON
	if dimensionsJSON != nil {
		jsonBytes, err := json.Marshal(dimensionsJSON)
		if err == nil {
			json.Unmarshal(jsonBytes, &layer.Dimensions)
		}
	}
	// Parse SQL view config JSON
	if sqlViewConfigJSON != nil {
		jsonBytes, err := json.Marshal(sqlViewConfigJSON)
		if err == nil {
			var cfg SQLViewConfig
			if json.Unmarshal(jsonBytes, &cfg) == nil {
				layer.SQLViewConfig = &cfg
			}
		}
	}
	layer.AllowedRoles = parseAllowedRoles(allowedRolesJSON)
	decodeJSONColumn(stylesJSON, &layer.Styles)
	decodeJSONColumn(nativeExtentJSON, &layer.NativeExtent)
	decodeJSONColumn(tileCacheParametersJSON, &layer.TileCacheParameters)
	return &layer, nil
}

func (s *DuckDBStore) ListLayers(ctx context.Context, serviceID string) ([]*Layer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, service_id, source_layer, public_id, title, description, enabled, crs_default, dimensions, is_sql_view, sql_view_config, public, allowed_roles, default_style, styles, native_extent, tile_cache_quota_bytes, tile_cache_parameters, tile_cache_generation, created_at, updated_at
		FROM layers WHERE service_id = ? ORDER BY public_id
	`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list layers: %w", err)
	}
	defer rows.Close()

	var layers []*Layer
	for rows.Next() {
		var layer Layer
		var dimensionsJSON any    // DuckDB returns JSON as various types
		var sqlViewConfigJSON any // DuckDB returns JSON as various types
		var allowedRolesJSON any  // DuckDB returns JSON as various types
		var stylesJSON any
		var nativeExtentJSON any
		var tileCacheParametersJSON any
		if err := rows.Scan(&layer.ID, &layer.ServiceID, &layer.SourceLayer, &layer.PublicID, &layer.Title, &layer.Description, &layer.Enabled, &layer.CRSDefault, &dimensionsJSON, &layer.IsSQLView, &sqlViewConfigJSON, &layer.Public, &allowedRolesJSON, &layer.DefaultStyle, &stylesJSON, &nativeExtentJSON, &layer.TileCacheQuotaBytes, &tileCacheParametersJSON, &layer.TileCacheGeneration, &layer.CreatedAt, &layer.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan layer: %w", err)
		}
		// Parse dimensions JSON
		if dimensionsJSON != nil {
			jsonBytes, err := json.Marshal(dimensionsJSON)
			if err == nil {
				json.Unmarshal(jsonBytes, &layer.Dimensions)
			}
		}
		// Parse SQL view config JSON
		if sqlViewConfigJSON != nil {
			jsonBytes, err := json.Marshal(sqlViewConfigJSON)
			if err == nil {
				var cfg SQLViewConfig
				if json.Unmarshal(jsonBytes, &cfg) == nil {
					layer.SQLViewConfig = &cfg
				}
			}
		}
		layer.AllowedRoles = parseAllowedRoles(allowedRolesJSON)
		decodeJSONColumn(stylesJSON, &layer.Styles)
		decodeJSONColumn(nativeExtentJSON, &layer.NativeExtent)
		decodeJSONColumn(tileCacheParametersJSON, &layer.TileCacheParameters)
		layers = append(layers, &layer)
	}
	return layers, rows.Err()
}

func (s *DuckDBStore) UpdateLayer(ctx context.Context, id string, input UpdateLayerInput) (*Layer, error) {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	if err := s.checkRoleAssignments(ctx, input.AllowedRoles); err != nil {
		return nil, err
	}
	layer, err := s.GetLayer(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.PublicID != nil {
		layer.PublicID = *input.PublicID
	}
	if input.Title != nil {
		layer.Title = *input.Title
	}
	if input.Description != nil {
		layer.Description = *input.Description
	}
	if input.Enabled != nil {
		layer.Enabled = *input.Enabled
	}
	if input.CRSDefault != nil {
		layer.CRSDefault = *input.CRSDefault
	}
	if input.Dimensions != nil {
		layer.Dimensions = input.Dimensions
	}
	if input.SQLViewConfig != nil {
		// Ensure ReadOnly is always true for SQL views
		input.SQLViewConfig.ReadOnly = true
		layer.SQLViewConfig = input.SQLViewConfig
	}
	if input.Public != nil {
		layer.Public = *input.Public
	}
	if input.AllowedRoles != nil {
		layer.AllowedRoles = input.AllowedRoles
	}
	if input.DefaultStyle != nil {
		layer.DefaultStyle = *input.DefaultStyle
	}
	if input.Styles != nil {
		layer.Styles = input.Styles
	}
	if input.NativeExtent != nil {
		layer.NativeExtent = input.NativeExtent
	}
	if input.MarkExtentStale != nil && *input.MarkExtentStale && layer.NativeExtent != nil {
		layer.NativeExtent.Stale = true
	}
	if input.TileCacheQuotaBytes != nil {
		layer.TileCacheQuotaBytes = *input.TileCacheQuotaBytes
	}
	if input.TileCacheParameters != nil {
		layer.TileCacheParameters = input.TileCacheParameters
	}
	layer.TileCacheGeneration++
	layer.UpdatedAt = time.Now().UTC()

	// Serialize dimensions to JSON
	dimensionsJSON := "[]"
	if len(layer.Dimensions) > 0 {
		b, err := json.Marshal(layer.Dimensions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal dimensions: %w", err)
		}
		dimensionsJSON = string(b)
	}

	// Serialize SQL view config to JSON
	var sqlViewConfigJSON *string
	if layer.SQLViewConfig != nil {
		b, err := json.Marshal(layer.SQLViewConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal sql_view_config: %w", err)
		}
		s := string(b)
		sqlViewConfigJSON = &s
	}

	result, err := s.execCapabilitiesMutation(ctx, "SELECT workspace_id FROM services WHERE id = ?", layer.ServiceID, `
		UPDATE layers SET public_id = ?, title = ?, description = ?, enabled = ?, crs_default = ?, dimensions = ?, sql_view_config = ?, public = ?, allowed_roles = ?, default_style = ?, styles = ?, native_extent = ?, tile_cache_quota_bytes = ?, tile_cache_parameters = ?, tile_cache_generation = ?, updated_at = ? WHERE id = ?
	`, layer.PublicID, layer.Title, layer.Description, layer.Enabled, layer.CRSDefault, dimensionsJSON, sqlViewConfigJSON, layer.Public, marshalAllowedRoles(layer.AllowedRoles), layer.DefaultStyle, marshalJSON(layer.Styles, "[]"), nullableJSON(layer.NativeExtent), layer.TileCacheQuotaBytes, nullableJSON(layer.TileCacheParameters), layer.TileCacheGeneration, layer.UpdatedAt, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "Duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to update layer: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, ErrNotFound
	}

	return layer, nil
}

func (s *DuckDBStore) DeleteLayer(ctx context.Context, id string) error {
	result, err := s.execCapabilitiesMutation(ctx, "SELECT s.workspace_id FROM services s JOIN layers l ON l.service_id=s.id WHERE l.id = ?", id, "DELETE FROM layers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete layer: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
