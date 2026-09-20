package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func (s *DuckDBStore) UpsertStyleAsset(ctx context.Context, input UpsertStyleAssetInput) (*StyleAsset, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	asset, err := scanStyleAsset(tx.QueryRowContext(ctx, `INSERT INTO style_assets (id,workspace_id,name,content_type,size_bytes,sha256,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT (workspace_id,name) DO UPDATE SET content_type=excluded.content_type,size_bytes=excluded.size_bytes,sha256=excluded.sha256,updated_at=excluded.updated_at
		RETURNING id,workspace_id,name,content_type,size_bytes,sha256,created_at,updated_at`, uuid.NewString(), input.WorkspaceID, input.Name, input.ContentType, input.SizeBytes, input.SHA256, now, now))
	if err != nil {
		return nil, err
	}
	// The revision and asset commit together, invalidating persistent render
	// identities even after a crash/restart between commit and runtime refresh.
	if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET capabilities_revision = capabilities_revision + 1, tile_revision=tile_revision+1 WHERE id=?`, input.WorkspaceID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return asset, nil
}

func scanStyleAsset(row interface{ Scan(...any) error }) (*StyleAsset, error) {
	var asset StyleAsset
	if err := row.Scan(&asset.ID, &asset.WorkspaceID, &asset.Name, &asset.ContentType, &asset.SizeBytes, &asset.SHA256, &asset.CreatedAt, &asset.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan style asset: %w", err)
	}
	return &asset, nil
}

func (s *DuckDBStore) GetStyleAsset(ctx context.Context, workspaceID, name string) (*StyleAsset, error) {
	return scanStyleAsset(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,content_type,size_bytes,sha256,created_at,updated_at FROM style_assets WHERE workspace_id=? AND name=?`, workspaceID, name))
}

func (s *DuckDBStore) ListStyleAssets(ctx context.Context, workspaceID string) ([]*StyleAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,name,content_type,size_bytes,sha256,created_at,updated_at FROM style_assets WHERE workspace_id=? ORDER BY name`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list style assets: %w", err)
	}
	defer rows.Close()
	result := make([]*StyleAsset, 0)
	for rows.Next() {
		asset, scanErr := scanStyleAsset(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, asset)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) DeleteStyleAsset(ctx context.Context, workspaceID, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM style_assets WHERE workspace_id=? AND name=?`, workspaceID, name)
	if err != nil {
		return fmt.Errorf("failed to delete style asset: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET capabilities_revision = capabilities_revision + 1, tile_revision=tile_revision+1 WHERE id=?`, workspaceID); err != nil {
		return err
	}
	return tx.Commit()
}
