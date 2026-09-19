package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// WFSLockRecord is the persisted form of a WFS feature lock. FeatureIDs maps
// a layer/type name to the locked local feature IDs.
type WFSLockRecord struct {
	LockID      string
	WorkspaceID string
	OwnerID     string
	FeatureIDs  map[string][]string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// PutWFSLock inserts or replaces a persisted WFS lock.
func (s *DuckDBStore) PutWFSLock(ctx context.Context, lock WFSLockRecord) error {
	raw, err := json.Marshal(lock.FeatureIDs)
	if err != nil {
		return fmt.Errorf("marshal WFS lock feature ids: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO wfs_locks
		(lock_id, workspace_id, owner_id, feature_ids, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		lock.LockID, lock.WorkspaceID, lock.OwnerID, raw,
		lock.CreatedAt.UTC(), lock.ExpiresAt.UTC())
	if err != nil {
		return fmt.Errorf("put WFS lock: %w", err)
	}
	return nil
}

// DeleteWFSLock removes a persisted lock by ID. Missing rows are not an error.
func (s *DuckDBStore) DeleteWFSLock(ctx context.Context, lockID string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM wfs_locks WHERE lock_id = ?", lockID); err != nil {
		return fmt.Errorf("delete WFS lock: %w", err)
	}
	return nil
}

// DeleteExpiredWFSLocks removes all locks that expired at or before now.
func (s *DuckDBStore) DeleteExpiredWFSLocks(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM wfs_locks WHERE expires_at <= ?", now.UTC()); err != nil {
		return fmt.Errorf("delete expired WFS locks: %w", err)
	}
	return nil
}

// DeleteWorkspaceWFSLocks removes all locks belonging to a workspace.
func (s *DuckDBStore) DeleteWorkspaceWFSLocks(ctx context.Context, workspaceID string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM wfs_locks WHERE workspace_id = ?", workspaceID); err != nil {
		return fmt.Errorf("delete workspace WFS locks: %w", err)
	}
	return nil
}

// ListWFSLocks returns all persisted locks that have not yet expired.
func (s *DuckDBStore) ListWFSLocks(ctx context.Context) ([]WFSLockRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT lock_id, workspace_id, owner_id, feature_ids, created_at, expires_at
		FROM wfs_locks WHERE expires_at > ?`, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("list WFS locks: %w", err)
	}
	defer rows.Close()

	var locks []WFSLockRecord
	for rows.Next() {
		var (
			rec WFSLockRecord
			raw any
		)
		if err := rows.Scan(&rec.LockID, &rec.WorkspaceID, &rec.OwnerID, &raw, &rec.CreatedAt, &rec.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan WFS lock: %w", err)
		}
		rec.FeatureIDs = map[string][]string{}
		decodeJSONColumn(raw, &rec.FeatureIDs)
		locks = append(locks, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate WFS locks: %w", err)
	}
	return locks, nil
}
