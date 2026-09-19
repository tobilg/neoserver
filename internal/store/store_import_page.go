package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

type ImportListOptions struct {
	Limit                  int
	Cursor, Status, Search string
}
type ImportJobPage struct {
	Imports    []*ImportJob `json:"imports"`
	NextCursor string       `json:"next_cursor,omitempty"`
}
type importCursor struct {
	CreatedAt time.Time
	ID        string
}

var ErrInvalidImportList = errors.New("invalid import list cursor, status or limit")

// Empty workspace is reserved for internal recovery across all catalog owners.
// The HTTP handler always supplies its authenticated workspace ID.
func (s *DuckDBStore) ListImportJobsPage(ctx context.Context, workspaceID string, options ImportListOptions) (*ImportJobPage, error) {
	limit := options.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalidImportList
	}
	query, args := importSelect+` WHERE true`, []any{}
	if workspaceID != "" {
		query += ` AND workspace_id=?`
		args = append(args, workspaceID)
	}
	switch options.Status {
	case "":
	case "actionable":
		query += ` AND status NOT IN ('published','cancelled','rolled_back')`
	case string(ImportQueued), string(ImportRunning), string(ImportAwaitingPlan), string(ImportReadyToPublish), string(ImportPublishing), string(ImportPublished), string(ImportCancelling), string(ImportCancelled), string(ImportFailed), string(ImportRollingBack), string(ImportRolledBack):
		query += ` AND status=?`
		args = append(args, options.Status)
	default:
		return nil, ErrInvalidImportList
	}
	if options.Search != "" {
		query += ` AND contains(lower(name),lower(?))`
		args = append(args, options.Search)
	}
	if options.Cursor != "" {
		var cursor importCursor
		raw, err := base64.RawURLEncoding.DecodeString(options.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.CreatedAt.IsZero() || cursor.ID == "" {
			return nil, ErrInvalidImportList
		}
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &ImportJobPage{Imports: []*ImportJob{}}
	for rows.Next() {
		job, err := scanImportJob(rows)
		if err != nil {
			return nil, err
		}
		page.Imports = append(page.Imports, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Imports) > limit {
		page.Imports = page.Imports[:limit]
		last := page.Imports[limit-1]
		raw, _ := json.Marshal(importCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
