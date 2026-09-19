package store

import (
	"context"
	"fmt"
	"time"
)

// WFSFeatureVersionRecord is persisted WFS feature version metadata. Only
// metadata is stored (no feature content); it exists so recorded version
// history survives a restart. Version navigation is not served from this
// table until content history is implemented.
type WFSFeatureVersionRecord struct {
	WorkspaceID string
	LayerID     string
	FeatureID   string
	Version     int
	PreviousRid string
	State       string
	ModifiedBy  string
	CreatedAt   time.Time
}

// PutWFSFeatureVersion inserts or replaces one feature version record.
func (s *DuckDBStore) PutWFSFeatureVersion(ctx context.Context, rec WFSFeatureVersionRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO wfs_feature_versions
		(workspace_id, layer_id, feature_id, version, previous_rid, state, modified_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.WorkspaceID, rec.LayerID, rec.FeatureID, rec.Version,
		rec.PreviousRid, rec.State, rec.ModifiedBy, rec.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("put WFS feature version: %w", err)
	}
	return nil
}

// ListWFSFeatureVersions returns all persisted feature version records.
func (s *DuckDBStore) ListWFSFeatureVersions(ctx context.Context) ([]WFSFeatureVersionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT workspace_id, layer_id, feature_id, version, previous_rid, state, modified_by, created_at
		FROM wfs_feature_versions ORDER BY workspace_id, layer_id, feature_id, version`)
	if err != nil {
		return nil, fmt.Errorf("list WFS feature versions: %w", err)
	}
	defer rows.Close()

	var recs []WFSFeatureVersionRecord
	for rows.Next() {
		var rec WFSFeatureVersionRecord
		if err := rows.Scan(&rec.WorkspaceID, &rec.LayerID, &rec.FeatureID, &rec.Version,
			&rec.PreviousRid, &rec.State, &rec.ModifiedBy, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan WFS feature version: %w", err)
		}
		recs = append(recs, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate WFS feature versions: %w", err)
	}
	return recs, nil
}

// DeleteWFSFeatureVersions removes all version records for one feature. It
// mirrors in-memory eviction of a feature's version history.
func (s *DuckDBStore) DeleteWFSFeatureVersions(ctx context.Context, workspaceID, layerID, featureID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wfs_feature_versions
		WHERE workspace_id = ? AND layer_id = ? AND feature_id = ?`,
		workspaceID, layerID, featureID)
	if err != nil {
		return fmt.Errorf("delete WFS feature versions: %w", err)
	}
	return nil
}

// DeleteWFSFeatureVersionsBelow removes a feature's version records older
// than minVersion. It mirrors in-memory trimming of a version history.
func (s *DuckDBStore) DeleteWFSFeatureVersionsBelow(ctx context.Context, workspaceID, layerID, featureID string, minVersion int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wfs_feature_versions
		WHERE workspace_id = ? AND layer_id = ? AND feature_id = ? AND version < ?`,
		workspaceID, layerID, featureID, minVersion)
	if err != nil {
		return fmt.Errorf("delete WFS feature versions below: %w", err)
	}
	return nil
}

// DeleteWorkspaceWFSFeatureVersions removes all version records for a workspace.
func (s *DuckDBStore) DeleteWorkspaceWFSFeatureVersions(ctx context.Context, workspaceID string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM wfs_feature_versions WHERE workspace_id = ?", workspaceID); err != nil {
		return fmt.Errorf("delete workspace WFS feature versions: %w", err)
	}
	return nil
}
