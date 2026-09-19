package store

import (
	"context"
	"fmt"
)

type WorkspaceCounts struct {
	Services  int64 `json:"services"`
	Layers    int64 `json:"layers"`
	Coverages int64 `json:"coverages"`
	Styles    int64 `json:"styles"`
	APIKeys   int64 `json:"api_keys"`
	Imports   int64 `json:"imports"`
}

type WorkspaceCountStore interface {
	ListWorkspaceCounts(context.Context) (map[string]WorkspaceCounts, error)
}

func (s *DuckDBStore) ListWorkspaceCounts(ctx context.Context) (map[string]WorkspaceCounts, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.id,
		(SELECT count(*) FROM services s WHERE s.workspace_id=w.id),
		(SELECT count(*) FROM layers l JOIN services s ON s.id=l.service_id WHERE s.workspace_id=w.id),
		(SELECT count(*) FROM coverages c WHERE c.workspace_id=w.id),
		(SELECT count(*) FROM styles st WHERE st.workspace_id=w.id),
		(SELECT count(*) FROM api_keys a WHERE a.workspace_id=w.id),
		(SELECT count(*) FROM import_jobs i WHERE i.workspace_id=w.id)
		FROM workspaces w ORDER BY w.name`)
	if err != nil {
		return nil, fmt.Errorf("list workspace counts: %w", err)
	}
	defer rows.Close()
	result := make(map[string]WorkspaceCounts)
	for rows.Next() {
		var id string
		var counts WorkspaceCounts
		if err = rows.Scan(&id, &counts.Services, &counts.Layers, &counts.Coverages, &counts.Styles, &counts.APIKeys, &counts.Imports); err != nil {
			return nil, err
		}
		result[id] = counts
	}
	return result, rows.Err()
}
