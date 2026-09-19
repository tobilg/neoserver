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

// Style operations

func (s *DuckDBStore) CreateStyle(ctx context.Context, input CreateStyleInput) (*Style, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	format := input.Format
	if format == "" {
		format = "sld_1.1.0"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO styles (id, workspace_id, name, title, description, sld_body, format, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, input.WorkspaceID, input.Name, input.Title, input.Description, input.SLDBody, format, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to create style: %w", err)
	}

	return &Style{
		ID:          id,
		WorkspaceID: input.WorkspaceID,
		Name:        input.Name,
		Title:       input.Title,
		Description: input.Description,
		SLDBody:     input.SLDBody,
		Format:      format,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (s *DuckDBStore) GetStyle(ctx context.Context, id string) (*Style, error) {
	var style Style
	err := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, name, title, description, sld_body, format, created_at, updated_at
		FROM styles WHERE id = ?
	`, id).Scan(&style.ID, &style.WorkspaceID, &style.Name, &style.Title, &style.Description, &style.SLDBody, &style.Format, &style.CreatedAt, &style.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get style: %w", err)
	}
	return &style, nil
}

func (s *DuckDBStore) GetStyleByName(ctx context.Context, workspaceID, name string) (*Style, error) {
	var style Style
	err := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, name, title, description, sld_body, format, created_at, updated_at
		FROM styles WHERE workspace_id = ? AND name = ?
	`, workspaceID, name).Scan(&style.ID, &style.WorkspaceID, &style.Name, &style.Title, &style.Description, &style.SLDBody, &style.Format, &style.CreatedAt, &style.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get style: %w", err)
	}
	return &style, nil
}

func (s *DuckDBStore) ListStyles(ctx context.Context, workspaceID string) ([]*Style, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, name, title, description, sld_body, format, created_at, updated_at
		FROM styles WHERE workspace_id = ? ORDER BY name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list styles: %w", err)
	}
	defer rows.Close()

	var styles []*Style
	for rows.Next() {
		var style Style
		if err := rows.Scan(&style.ID, &style.WorkspaceID, &style.Name, &style.Title, &style.Description, &style.SLDBody, &style.Format, &style.CreatedAt, &style.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan style: %w", err)
		}
		styles = append(styles, &style)
	}
	return styles, rows.Err()
}

func (s *DuckDBStore) UpdateStyle(ctx context.Context, id string, input UpdateStyleInput) (*Style, error) {
	style, err := s.GetStyle(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		style.Name = *input.Name
	}
	if input.Title != nil {
		style.Title = *input.Title
	}
	if input.Description != nil {
		style.Description = *input.Description
	}
	if input.SLDBody != nil {
		style.SLDBody = *input.SLDBody
	}
	if input.Format != nil {
		style.Format = *input.Format
	}
	style.UpdatedAt = time.Now().UTC()

	result, err := s.db.ExecContext(ctx, `
		UPDATE styles SET name = ?, title = ?, description = ?, sld_body = ?, format = ?, updated_at = ? WHERE id = ?
	`, style.Name, style.Title, style.Description, style.SLDBody, style.Format, style.UpdatedAt, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "Duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to update style: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, ErrNotFound
	}

	return style, nil
}

func (s *DuckDBStore) DeleteStyle(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM styles WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete style: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// WFS Stored Query operations

func (s *DuckDBStore) CreateWFSStoredQuery(ctx context.Context, input CreateWFSStoredQueryInput) (*WFSStoredQuery, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	// Marshal parameters and return types to JSON
	paramsJSON, err := json.Marshal(input.Parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parameters: %w", err)
	}
	returnTypesJSON, err := json.Marshal(input.ReturnTypes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal return types: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO wfs_stored_queries (id, workspace_id, query_id, title, abstract, parameters, query_expression, language, return_types, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, input.WorkspaceID, input.QueryID, input.Title, input.Abstract, string(paramsJSON), input.QueryExpression, input.Language, string(returnTypesJSON), now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to create WFS stored query: %w", err)
	}

	return &WFSStoredQuery{
		ID:              id,
		WorkspaceID:     input.WorkspaceID,
		QueryID:         input.QueryID,
		Title:           input.Title,
		Abstract:        input.Abstract,
		Parameters:      input.Parameters,
		QueryExpression: input.QueryExpression,
		Language:        input.Language,
		ReturnTypes:     input.ReturnTypes,
		CreatedAt:       now,
	}, nil
}

func (s *DuckDBStore) GetWFSStoredQuery(ctx context.Context, workspaceID, queryID string) (*WFSStoredQuery, error) {
	var sq WFSStoredQuery
	var paramsJSON, returnTypesJSON any
	err := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, query_id, title, abstract, parameters, query_expression, language, return_types, created_at
		FROM wfs_stored_queries WHERE workspace_id = ? AND query_id = ?
	`, workspaceID, queryID).Scan(&sq.ID, &sq.WorkspaceID, &sq.QueryID, &sq.Title, &sq.Abstract, &paramsJSON, &sq.QueryExpression, &sq.Language, &returnTypesJSON, &sq.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get WFS stored query: %w", err)
	}

	// Convert JSON values back to structs
	if paramsJSON != nil {
		jsonBytes, _ := json.Marshal(paramsJSON)
		json.Unmarshal(jsonBytes, &sq.Parameters)
	}
	if returnTypesJSON != nil {
		jsonBytes, _ := json.Marshal(returnTypesJSON)
		json.Unmarshal(jsonBytes, &sq.ReturnTypes)
	}

	return &sq, nil
}

func (s *DuckDBStore) ListWFSStoredQueries(ctx context.Context, workspaceID string) ([]*WFSStoredQuery, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, query_id, title, abstract, parameters, query_expression, language, return_types, created_at
		FROM wfs_stored_queries WHERE workspace_id = ? ORDER BY query_id
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list WFS stored queries: %w", err)
	}
	defer rows.Close()

	var queries []*WFSStoredQuery
	for rows.Next() {
		var sq WFSStoredQuery
		var paramsJSON, returnTypesJSON any
		if err := rows.Scan(&sq.ID, &sq.WorkspaceID, &sq.QueryID, &sq.Title, &sq.Abstract, &paramsJSON, &sq.QueryExpression, &sq.Language, &returnTypesJSON, &sq.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan WFS stored query: %w", err)
		}

		// Convert JSON values back to structs
		if paramsJSON != nil {
			jsonBytes, _ := json.Marshal(paramsJSON)
			json.Unmarshal(jsonBytes, &sq.Parameters)
		}
		if returnTypesJSON != nil {
			jsonBytes, _ := json.Marshal(returnTypesJSON)
			json.Unmarshal(jsonBytes, &sq.ReturnTypes)
		}

		queries = append(queries, &sq)
	}
	return queries, rows.Err()
}

func (s *DuckDBStore) DeleteWFSStoredQuery(ctx context.Context, workspaceID, queryID string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM wfs_stored_queries WHERE workspace_id = ? AND query_id = ?", workspaceID, queryID)
	if err != nil {
		return fmt.Errorf("failed to delete WFS stored query: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// Workspace settings operations

func (s *DuckDBStore) GetWMSSettings(ctx context.Context, workspaceID string) (*WMSSettings, error) {
	var settingsJSON any // DuckDB returns JSON as map[string]interface{}
	err := s.db.QueryRowContext(ctx, `
		SELECT wms_settings FROM workspaces WHERE id = ?
	`, workspaceID).Scan(&settingsJSON)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get WMS settings: %w", err)
	}

	var settings WMSSettings
	if settingsJSON != nil {
		// Convert DuckDB JSON value back to []byte then unmarshal
		jsonBytes, err := json.Marshal(settingsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal WMS settings JSON: %w", err)
		}
		if err := json.Unmarshal(jsonBytes, &settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal WMS settings: %w", err)
		}
	}
	return &settings, nil
}

func (s *DuckDBStore) UpdateWMSSettings(ctx context.Context, workspaceID string, settings WMSSettings) error {
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal WMS settings: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE workspaces SET wms_settings = ?, updated_at = ? WHERE id = ?
	`, settingsJSON, time.Now().UTC(), workspaceID)
	if err != nil {
		return fmt.Errorf("failed to update WMS settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) GetWFSSettings(ctx context.Context, workspaceID string) (*WFSSettings, error) {
	var settingsJSON any // DuckDB returns JSON as map[string]interface{}
	err := s.db.QueryRowContext(ctx, `
		SELECT wfs_settings FROM workspaces WHERE id = ?
	`, workspaceID).Scan(&settingsJSON)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get WFS settings: %w", err)
	}

	var settings WFSSettings
	if settingsJSON != nil {
		// Convert DuckDB JSON value back to []byte then unmarshal
		jsonBytes, err := json.Marshal(settingsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal WFS settings JSON: %w", err)
		}
		if err := json.Unmarshal(jsonBytes, &settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal WFS settings: %w", err)
		}
	}
	return &settings, nil
}

func (s *DuckDBStore) UpdateWFSSettings(ctx context.Context, workspaceID string, settings WFSSettings) error {
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal WFS settings: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE workspaces SET wfs_settings = ?, updated_at = ? WHERE id = ?
	`, settingsJSON, time.Now().UTC(), workspaceID)
	if err != nil {
		return fmt.Errorf("failed to update WFS settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) GetOGCAPISettings(ctx context.Context, workspaceID string) (*OGCAPISettings, error) {
	var settingsJSON any // DuckDB returns JSON as map[string]interface{}
	err := s.db.QueryRowContext(ctx, `
		SELECT ogcapi_settings FROM workspaces WHERE id = ?
	`, workspaceID).Scan(&settingsJSON)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get OGC API settings: %w", err)
	}

	var settings OGCAPISettings
	if settingsJSON != nil {
		// Convert DuckDB JSON value back to []byte then unmarshal
		jsonBytes, err := json.Marshal(settingsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal OGC API settings JSON: %w", err)
		}
		if err := json.Unmarshal(jsonBytes, &settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal OGC API settings: %w", err)
		}
	}
	return &settings, nil
}

func (s *DuckDBStore) UpdateOGCAPISettings(ctx context.Context, workspaceID string, settings OGCAPISettings) error {
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal OGC API settings: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE workspaces SET ogcapi_settings = ?, updated_at = ? WHERE id = ?
	`, settingsJSON, time.Now().UTC(), workspaceID)
	if err != nil {
		return fmt.Errorf("failed to update OGC API settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) GetOGCTilesAPISettings(ctx context.Context, workspaceID string) (*OGCTilesAPISettings, error) {
	var settingsJSON any // DuckDB returns JSON as map[string]interface{}
	err := s.db.QueryRowContext(ctx, `
		SELECT ogc_tiles_api_settings FROM workspaces WHERE id = ?
	`, workspaceID).Scan(&settingsJSON)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get OGC Tiles API settings: %w", err)
	}

	// Start with defaults
	settings := DefaultOGCTilesAPISettings()
	if settingsJSON != nil {
		// Convert DuckDB JSON value back to []byte then unmarshal
		jsonBytes, err := json.Marshal(settingsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal OGC Tiles API settings JSON: %w", err)
		}
		if err := json.Unmarshal(jsonBytes, &settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal OGC Tiles API settings: %w", err)
		}
	}
	return &settings, nil
}

func (s *DuckDBStore) UpdateOGCTilesAPISettings(ctx context.Context, workspaceID string, settings OGCTilesAPISettings) error {
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal OGC Tiles API settings: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE workspaces SET ogc_tiles_api_settings = ?, tile_revision = tile_revision + 1, updated_at = ? WHERE id = ?
	`, settingsJSON, time.Now().UTC(), workspaceID)
	if err != nil {
		return fmt.Errorf("failed to update OGC Tiles API settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
