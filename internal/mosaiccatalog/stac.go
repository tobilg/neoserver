package mosaiccatalog

import "context"

// ScanGranules keeps a single database snapshot while streaming a complete
// active generation. Harvests can publish new generations during the scan.
func (m *Manager) ScanGranules(ctx context.Context, workspaceID, serviceID string, visit func(*Granule) error) error {
	rows, err := m.catalog.db.QueryContext(ctx, `SELECT `+granuleColumns+` FROM mosaic_granules WHERE workspace_id=? AND service_id=? AND generation=(SELECT active_generation FROM mosaic_services WHERE workspace_id=? AND service_id=?) ORDER BY source_uri`, workspaceID, serviceID, workspaceID, serviceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		g, err := scanGranule(rows)
		if err != nil {
			return err
		}
		if err = visit(g); err != nil {
			return err
		}
	}
	return rows.Err()
}
func (m *Manager) Generation(ctx context.Context, workspaceID, serviceID string) (int64, error) {
	var generation int64
	err := m.catalog.db.QueryRowContext(ctx, "SELECT coalesce(max(active_generation),0) FROM mosaic_services WHERE workspace_id=? AND service_id=?", workspaceID, serviceID).Scan(&generation)
	return generation, err
}
