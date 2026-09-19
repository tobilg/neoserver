package store

import (
	"context"
	"encoding/json"
)

// WFSVersionChange preserves mutation order, including history trimming and
// eviction. Zero flags mean upsert Record; deletion flags use its feature key.
type WFSVersionChange struct {
	Record     WFSFeatureVersionRecord
	DeleteAll  bool
	MinVersion int
}

func (s *DuckDBStore) ApplyWFSVersionChanges(ctx context.Context, changes []WFSVersionChange) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var pending []WFSFeatureVersionRecord
	type key struct {
		workspace, layer, feature string
		version                   int
	}
	indices := make(map[key]int)
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		// One bound JSON vector avoids preparing thousands of scalar parameter
		// bindings through cgo, particularly expensive under the race detector.
		payload, err := json.Marshal(pending)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO wfs_feature_versions (workspace_id,layer_id,feature_id,version,previous_rid,state,modified_by,created_at)
			SELECT value->>'WorkspaceID', value->>'LayerID', value->>'FeatureID', CAST(value->>'Version' AS INTEGER),
			value->>'PreviousRid', value->>'State', value->>'ModifiedBy', CAST(value->>'CreatedAt' AS TIMESTAMP)
			FROM json_each(?)`, string(payload))
		pending = nil
		clear(indices)
		return err
	}
	for _, change := range changes {
		rec := change.Record
		rec.CreatedAt = rec.CreatedAt.UTC()
		if change.DeleteAll || change.MinVersion > 0 {
			if err := flush(); err != nil {
				return err
			}
			query := `DELETE FROM wfs_feature_versions WHERE workspace_id=? AND layer_id=? AND feature_id=?`
			args := []any{rec.WorkspaceID, rec.LayerID, rec.FeatureID}
			if !change.DeleteAll {
				query += " AND version < ?"
				args = append(args, change.MinVersion)
			}
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return err
			}
			continue
		}
		id := key{rec.WorkspaceID, rec.LayerID, rec.FeatureID, rec.Version}
		if index, ok := indices[id]; ok {
			pending[index] = rec
		} else {
			indices[id] = len(pending)
			pending = append(pending, rec)
		}
		if len(pending) >= 2048 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return tx.Commit()
}
