package store

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrInvalidDeletionQuery = errors.New("invalid deletion history query or cursor")

// DeletionQuery applies authorization BEFORE limiting. A nil WorkspaceIDs slice
// permits all workspaces; a non-nil empty slice permits none. Only trusted server
// code may populate WorkspaceIDs and ExcludedWorkspaceIDs.
type DeletionQuery struct {
	WorkspaceIDs, ExcludedWorkspaceIDs []string
	WorkspaceID, Status, Cursor        string
	Limit                              int
}

type DeletionPage struct {
	Deletions  []*DeletionOperation `json:"deletions"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type deletionCursor struct {
	Created time.Time `json:"created"`
	ID      string    `json:"id"`
	Scope   string    `json:"scope"`
}

func (s *DuckDBStore) ListCatalogDeletionPage(ctx context.Context, q DeletionQuery) (*DeletionPage, error) {
	if q.Limit == 0 {
		q.Limit = 200
	}
	if q.Limit < 1 || q.Limit > 1000 || len(q.Cursor) > 2048 {
		return nil, ErrInvalidDeletionQuery
	}
	switch q.Status {
	case "", "pending", "running", "failed", "completed", "actionable":
	default:
		return nil, ErrInvalidDeletionQuery
	}
	// Bind continuation tokens to the authorized scope and filters. A cursor is
	// only a position, never an authorization grant (including after role changes).
	scopeQuery := q
	scopeQuery.Limit, scopeQuery.Cursor = 0, ""
	if q.WorkspaceIDs != nil {
		scopeQuery.WorkspaceIDs = append([]string{}, q.WorkspaceIDs...)
		sort.Strings(scopeQuery.WorkspaceIDs)
	}
	scopeQuery.ExcludedWorkspaceIDs = append([]string(nil), q.ExcludedWorkspaceIDs...)
	sort.Strings(scopeQuery.ExcludedWorkspaceIDs)
	encodedScope, _ := json.Marshal(scopeQuery)
	scope := fmt.Sprintf("%x", sha256.Sum256(encodedScope))
	where, args := []string{"TRUE"}, []any{}
	addIDs := func(ids []string, exclude bool) {
		if len(ids) == 0 {
			if !exclude {
				where = append(where, "FALSE")
			}
			return
		}
		placeholders := make([]string, len(ids))
		for i, id := range ids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		op := " IN "
		if exclude {
			op = " NOT IN "
		}
		where = append(where, "workspace_id"+op+"("+strings.Join(placeholders, ",")+")")
	}
	if q.WorkspaceIDs != nil {
		addIDs(q.WorkspaceIDs, false)
	}
	if len(q.ExcludedWorkspaceIDs) > 0 {
		addIDs(q.ExcludedWorkspaceIDs, true)
	}
	if q.WorkspaceID != "" {
		where = append(where, "workspace_id=?")
		args = append(args, q.WorkspaceID)
	}
	if q.Status == "actionable" {
		where = append(where, "status IN ('pending','running','failed')")
	} else if q.Status != "" {
		where = append(where, "status=?")
		args = append(args, q.Status)
	}
	if q.Cursor != "" {
		var cursor deletionCursor
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.ID == "" || cursor.Created.IsZero() || cursor.Scope != scope {
			return nil, ErrInvalidDeletionQuery
		}
		where = append(where, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, cursor.Created, cursor.Created, cursor.ID)
	}
	args = append(args, q.Limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope_kind,workspace_id,target_id,target_name,status,phase,
		plan_json,last_error,attempt_count,created_at,updated_at,completed_at FROM catalog_deletions
		WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &DeletionPage{Deletions: make([]*DeletionOperation, 0)}
	for rows.Next() {
		op, err := scanDeletionOperation(rows)
		if err != nil {
			return nil, err
		}
		page.Deletions = append(page.Deletions, op)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Deletions) > q.Limit {
		page.Deletions = page.Deletions[:q.Limit]
		last := page.Deletions[len(page.Deletions)-1]
		raw, _ := json.Marshal(deletionCursor{Created: last.CreatedAt, ID: last.ID, Scope: scope})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
