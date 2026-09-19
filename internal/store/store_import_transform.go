package store

import (
	"context"
	"fmt"
	"time"
)

// CompleteImportTransform atomically selects a fully closed candidate and its
// encryption key. A failed revision never replaces the last successful asset.
func (s *DuckDBStore) CompleteImportTransform(ctx context.Context, asset ManagedAsset) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE import_jobs SET status='ready_to_publish',phase='preview',managed_path=?,completed_at=NULL,error_message='',updated_at=?
		WHERE id=? AND workspace_id=? AND status IN ('running','queued') AND NOT cancel_requested`, asset.Path, now, asset.ImportID, asset.WorkspaceID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("import is no longer accepting a transformed revision")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO managed_assets(import_id,workspace_id,service_id,path,encryption_key,created_at)
		VALUES(?,?,NULL,?,?,?) ON CONFLICT(import_id) DO UPDATE SET path=excluded.path,encryption_key=excluded.encryption_key`, asset.ImportID, asset.WorkspaceID, asset.Path, asset.EncryptionKey, now)
	if err != nil {
		return err
	}
	job, err := scanImportJob(tx.QueryRowContext(ctx, importSelect+` WHERE id=?`, asset.ImportID))
	if err != nil {
		return err
	}
	if err = insertImportEvent(ctx, tx, job, nil); err != nil {
		return err
	}
	return tx.Commit()
}
