package store

import (
	"context"
	"database/sql"
)

// DataRevisionStore records a durable dirty barrier around external source
// transactions. A crash or failed completion keeps cached data unservable until
// recovery advances the revision. Source commits and catalog commits are separate.
type DataRevisionStore interface {
	GetDataRevision(context.Context, string) (int64, bool, error)
	SetDataWritePending(context.Context, string, bool) (int64, error)
	RecoverDataWrites(context.Context) error
}

const dataRevisionSchema = `CREATE TABLE IF NOT EXISTS workspace_data_revisions (
 workspace_id VARCHAR PRIMARY KEY, revision BIGINT NOT NULL, pending BOOLEAN NOT NULL
);`

func (s *DuckDBStore) GetDataRevision(ctx context.Context, workspaceID string) (int64, bool, error) {
	var revision int64
	var pending bool
	err := s.db.QueryRowContext(ctx, `SELECT revision,pending FROM workspace_data_revisions WHERE workspace_id=?`, workspaceID).Scan(&revision, &pending)
	if err == sql.ErrNoRows {
		return 1, false, nil
	}
	return revision, pending, err
}

func (s *DuckDBStore) SetDataWritePending(ctx context.Context, workspaceID string, pending bool) (int64, error) {
	var revision int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO workspace_data_revisions VALUES (?,2,?)
 ON CONFLICT(workspace_id) DO UPDATE SET revision=workspace_data_revisions.revision+1,pending=excluded.pending
 RETURNING revision`, workspaceID, pending).Scan(&revision)
	return revision, err
}

// Called only at server startup, after acquiring exclusive catalog ownership.
func (s *DuckDBStore) RecoverDataWrites(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workspace_data_revisions SET revision=revision+1,pending=false WHERE pending=true`)
	return err
}
