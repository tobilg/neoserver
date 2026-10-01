package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

type STACSettings struct {
	Enabled     bool   `json:"enabled"`
	Public      bool   `json:"public"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// Optional so alternative catalog implementations can opt into STAC.
type STACSettingsStore interface {
	GetSTACSettings(context.Context, string) (*STACSettings, error)
	UpdateSTACSettings(context.Context, string, STACSettings) error
}

func (s *DuckDBStore) GetSTACSettings(ctx context.Context, workspaceID string) (*STACSettings, error) {
	var raw sql.NullString
	err := s.read.QueryRowContext(ctx, "SELECT stac_settings::VARCHAR FROM workspaces WHERE id=?", workspaceID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var settings STACSettings
	if raw.Valid {
		err = json.Unmarshal([]byte(raw.String), &settings)
	}
	return &settings, err
}

func (s *DuckDBStore) UpdateSTACSettings(ctx context.Context, workspaceID string, settings STACSettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE workspaces SET stac_settings=?, capabilities_revision=capabilities_revision+1, updated_at=current_timestamp WHERE id=?", string(raw), workspaceID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}
