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

// Service operations

func (s *DuckDBStore) CreateService(ctx context.Context, input CreateServiceInput) (*Service, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	// Convert json.RawMessage to string for DuckDB driver compatibility
	// Use empty JSON object if nil/empty (DuckDB doesn't accept empty string for JSON)
	connInfoStr := string(input.ConnectionInfo)
	if connInfoStr == "" {
		connInfoStr = "{}"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO services (id, workspace_id, name, type, connection_info, cache_settings, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, input.WorkspaceID, input.Name, string(input.Type), connInfoStr, nullableJSON(input.CacheSettings), input.Enabled, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	return &Service{
		ID:             id,
		WorkspaceID:    input.WorkspaceID,
		Name:           input.Name,
		Type:           input.Type,
		ConnectionInfo: input.ConnectionInfo,
		CacheSettings:  input.CacheSettings,
		Enabled:        input.Enabled,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (s *DuckDBStore) GetService(ctx context.Context, id string) (*Service, error) {
	var svc Service
	var svcType string
	var connInfo, cacheSettings any // DuckDB returns JSON as map[string]interface{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, name, type, connection_info, cache_settings, enabled, created_at, updated_at
		FROM services svc WHERE id = ?
		AND NOT EXISTS (SELECT 1 FROM catalog_deletions d WHERE d.scope_kind='service'
			AND d.target_id=svc.id AND d.status IN ('pending','running','failed'))
		AND NOT EXISTS (SELECT 1 FROM catalog_deletions d WHERE d.scope_kind='workspace'
			AND d.target_id=svc.workspace_id AND d.status IN ('pending','running','failed'))
	`, id).Scan(&svc.ID, &svc.WorkspaceID, &svc.Name, &svcType, &connInfo, &cacheSettings, &svc.Enabled, &svc.CreatedAt, &svc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err := decodeJSONValue(cacheSettings, &svc.CacheSettings); err != nil {
		return nil, fmt.Errorf("failed to decode cache settings: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}
	svc.Type = ServiceType(svcType)
	// Convert JSON value back to []byte
	if connInfo != nil {
		jsonBytes, err := json.Marshal(connInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal connection info: %w", err)
		}
		svc.ConnectionInfo = jsonBytes
	}
	return &svc, nil
}

func (s *DuckDBStore) ListServices(ctx context.Context, workspaceID string) ([]*Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, name, type, connection_info, cache_settings, enabled, created_at, updated_at
		FROM services svc WHERE workspace_id = ?
		AND NOT EXISTS (SELECT 1 FROM catalog_deletions d WHERE d.scope_kind='service'
			AND d.target_id=svc.id AND d.status IN ('pending','running','failed'))
		AND NOT EXISTS (SELECT 1 FROM catalog_deletions d WHERE d.scope_kind='workspace'
			AND d.target_id=svc.workspace_id AND d.status IN ('pending','running','failed'))
		ORDER BY name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}
	defer rows.Close()

	var services []*Service
	for rows.Next() {
		var svc Service
		var svcType string
		var connInfo, cacheSettings any
		if err := rows.Scan(&svc.ID, &svc.WorkspaceID, &svc.Name, &svcType, &connInfo, &cacheSettings, &svc.Enabled, &svc.CreatedAt, &svc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan service: %w", err)
		}
		if err := decodeJSONValue(cacheSettings, &svc.CacheSettings); err != nil {
			return nil, err
		}
		svc.Type = ServiceType(svcType)
		// Convert JSON value back to []byte
		if connInfo != nil {
			jsonBytes, err := json.Marshal(connInfo)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal connection info: %w", err)
			}
			svc.ConnectionInfo = jsonBytes
		}
		services = append(services, &svc)
	}
	return services, rows.Err()
}

func (s *DuckDBStore) UpdateService(ctx context.Context, id string, input UpdateServiceInput) (*Service, error) {
	svc, err := s.GetService(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		svc.Name = *input.Name
	}
	if input.ConnectionInfo != nil {
		svc.ConnectionInfo = *input.ConnectionInfo
	}
	if input.Enabled != nil {
		svc.Enabled = *input.Enabled
	}
	if input.CacheSettings != nil {
		svc.CacheSettings = input.CacheSettings
	}
	svc.UpdatedAt = time.Now().UTC()

	// Convert json.RawMessage to string for DuckDB driver compatibility
	// Use empty JSON object if nil/empty (DuckDB doesn't accept empty string for JSON)
	connInfoStr := string(svc.ConnectionInfo)
	if connInfoStr == "" {
		connInfoStr = "{}"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin service update: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE services SET name = ?, connection_info = ?, cache_settings = ?, enabled = ?, updated_at = ? WHERE id = ?
	`, svc.Name, connInfoStr, nullableJSON(svc.CacheSettings), svc.Enabled, svc.UpdatedAt, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "Duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to update service: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, ErrNotFound
	}
	// A service update can redirect a publication to different source bytes.
	// Bump the workspace render revision conservatively for every update so no
	// persistent tile produced through the previous service state is reused.
	if _, err := tx.ExecContext(ctx, "UPDATE workspaces SET tile_revision = tile_revision + 1 WHERE id = ?", svc.WorkspaceID); err != nil {
		return nil, fmt.Errorf("bump workspace tile revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit service update: %w", err)
	}

	return svc, nil
}

func nullableJSON(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(b)
}

func decodeJSONValue(raw any, dst any) error {
	if raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func (s *DuckDBStore) DeleteService(ctx context.Context, id string) error {
	service, err := s.GetService(ctx, id)
	if err != nil {
		return err
	}
	plan, err := s.PlanServiceDeletion(ctx, service.WorkspaceID, id)
	if err != nil {
		return err
	}
	if plan.HasDependencies() {
		return &DeletionConflictError{Plan: *plan}
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM services WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
