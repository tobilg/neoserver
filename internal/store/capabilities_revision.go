package store

import (
	"context"
	"database/sql"
)

// CapabilitiesRevisionStore exposes the durable workspace metadata sequence.
type CapabilitiesRevisionStore interface {
	GetCapabilitiesRevision(context.Context, string) (int64, error)
}

func (s *DuckDBStore) GetCapabilitiesRevision(ctx context.Context, workspaceID string) (int64, error) {
	var revision int64
	err := s.db.QueryRowContext(ctx, "SELECT capabilities_revision FROM workspaces WHERE id = ?", workspaceID).Scan(&revision)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return revision, err
}

// execCapabilitiesMutation changes catalog metadata and its sequence in the
// same transaction. Resolve ownership before deletion as well as insertion.
// The selector is an internal SQL statement, never client input.
func (s *DuckDBStore) execCapabilitiesMutation(ctx context.Context, selector string, owner any, query string, args ...any) (sql.Result, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var workspaceID string
	if selector != "" {
		err = tx.QueryRowContext(ctx, selector, owner).Scan(&workspaceID)
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if selector == "" {
		_, err = tx.ExecContext(ctx, "UPDATE workspaces SET capabilities_revision = capabilities_revision + 1")
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE workspaces SET capabilities_revision = capabilities_revision + 1 WHERE id = ?", workspaceID)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
