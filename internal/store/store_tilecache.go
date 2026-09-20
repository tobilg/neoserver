package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func (s *DuckDBStore) GetWMTSSettings(ctx context.Context, workspaceID string) (*WMTSSettings, error) {
	var raw any
	if err := s.db.QueryRowContext(ctx, "SELECT wmts_settings FROM workspaces WHERE id = ?", workspaceID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get WMTS settings: %w", err)
	}
	settings := DefaultWMTSSettings()
	decodeJSONColumn(raw, &settings)
	return &settings, nil
}

func (s *DuckDBStore) UpdateWMTSSettings(ctx context.Context, workspaceID string, settings WMTSSettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal WMTS settings: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE workspaces
		SET capabilities_revision = capabilities_revision + 1, wmts_settings = ?, updated_at = ? WHERE id = ?`,
		raw, time.Now().UTC(), workspaceID)
	if err != nil {
		return fmt.Errorf("update WMTS settings: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) GetTileRevision(ctx context.Context, workspaceID string) (int64, error) {
	var revision int64
	if err := s.db.QueryRowContext(ctx, "SELECT tile_revision FROM workspaces WHERE id = ?", workspaceID).Scan(&revision); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("get tile revision: %w", err)
	}
	return revision, nil
}
