package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

type CatalogOrphan struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Reason      string `json:"reason"`
}

type CatalogOwnership struct {
	Workspaces map[string]bool   `json:"-"`
	Services   map[string]string `json:"-"` // service ID -> workspace ID
	Resources  map[string]string `json:"-"` // layer/coverage/group ID -> workspace ID
}

func (s *DuckDBStore) AuditCatalogOrphans(ctx context.Context) ([]CatalogOrphan, error) {
	queries := []struct {
		kind, reason, query string
	}{
		{"dataset_map", "workspace map group does not exist in this workspace", `SELECT id,name,id FROM workspaces WHERE ` + invalidDatasetMapPredicate},
		{"rbac_role", "role does not exist", `SELECT CAST(id AS VARCHAR),coalesce(v0,''),coalesce(v1,'') FROM casbin_rules WHERE v0 NOT IN (SELECT id FROM roles)`},
		{"api_key_role", "role does not exist", `SELECT id,name,coalesce(workspace_id,'') FROM api_keys WHERE role_id NOT IN (SELECT id FROM roles)`},
		{"claim_mapping_role", "role does not exist", `SELECT id,claim_name,workspace_id FROM claim_role_mappings WHERE role_id NOT IN (SELECT id FROM roles)`},
		{"service", "workspace does not exist", `SELECT s.id,s.name,s.workspace_id FROM services s LEFT JOIN workspaces w ON w.id=s.workspace_id WHERE w.id IS NULL`},
		{"layer", "service does not exist", `SELECT l.id,l.public_id,'' FROM layers l LEFT JOIN services s ON s.id=l.service_id WHERE s.id IS NULL`},
		{"coverage", "workspace or service does not exist", `SELECT c.id,c.public_id,c.workspace_id FROM coverages c LEFT JOIN workspaces w ON w.id=c.workspace_id LEFT JOIN services s ON s.id=c.service_id WHERE w.id IS NULL OR s.id IS NULL OR s.workspace_id<>c.workspace_id`},
		{"style", "workspace does not exist", `SELECT x.id,x.name,x.workspace_id FROM styles x LEFT JOIN workspaces w ON w.id=x.workspace_id WHERE w.id IS NULL`},
		{"style_asset", "workspace does not exist", `SELECT x.id,x.name,x.workspace_id FROM style_assets x LEFT JOIN workspaces w ON w.id=x.workspace_id WHERE w.id IS NULL`},
		{"layer_group", "workspace does not exist", `SELECT x.id,x.public_id,x.workspace_id FROM layer_groups x LEFT JOIN workspaces w ON w.id=x.workspace_id WHERE w.id IS NULL`},
		{"api_key", "workspace does not exist", `SELECT x.id,x.name,x.workspace_id FROM api_keys x LEFT JOIN workspaces w ON w.id=x.workspace_id WHERE x.workspace_id IS NOT NULL AND w.id IS NULL`},
		{"stored_query", "workspace does not exist", `SELECT x.id,x.query_id,x.workspace_id FROM wfs_stored_queries x LEFT JOIN workspaces w ON w.id=x.workspace_id WHERE w.id IS NULL`},
		{"claim_mapping", "workspace does not exist", `SELECT x.id,x.claim_name || '=' || x.claim_value,x.workspace_id FROM claim_role_mappings x LEFT JOIN workspaces w ON w.id=x.workspace_id WHERE w.id IS NULL`},
		{"rbac_policy", "workspace does not exist", `SELECT CAST(x.id AS VARCHAR),x.ptype || ':' || coalesce(x.v0,'') || ':' || coalesce(x.v2,''),x.v1 FROM casbin_rules x LEFT JOIN workspaces w ON w.id=x.v1 WHERE x.v1 NOT IN ('','*') AND w.id IS NULL`},
	}
	var result []CatalogOrphan
	for _, item := range queries {
		rows, err := s.db.QueryContext(ctx, item.query)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var orphan CatalogOrphan
			if err := rows.Scan(&orphan.ID, &orphan.Name, &orphan.WorkspaceID); err != nil {
				rows.Close()
				return nil, err
			}
			orphan.Kind, orphan.Reason = item.kind, item.reason
			result = append(result, orphan)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (s *DuckDBStore) RepairCatalogOrphans(ctx context.Context) (int64, error) {
	s.datasetMapMu.Lock()
	defer s.datasetMapMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	statements := []string{
		`UPDATE workspaces SET ogc_tiles_api_settings=json_merge_patch(ogc_tiles_api_settings, '{"settings":{"dataset_map_layer_group_id":""}}'), capabilities_revision=capabilities_revision+1, tile_revision=tile_revision+1, updated_at=current_timestamp WHERE ` + invalidDatasetMapPredicate,
		`DELETE FROM casbin_rules WHERE v0 NOT IN (SELECT id FROM roles)`,
		`DELETE FROM api_keys WHERE role_id NOT IN (SELECT id FROM roles)`,
		`DELETE FROM claim_role_mappings WHERE role_id NOT IN (SELECT id FROM roles)`,
		`DELETE FROM layers WHERE service_id NOT IN (SELECT id FROM services) OR service_id IN (SELECT s.id FROM services s WHERE s.workspace_id NOT IN (SELECT id FROM workspaces))`,
		`DELETE FROM coverages WHERE workspace_id NOT IN (SELECT id FROM workspaces) OR service_id NOT IN (SELECT id FROM services) OR service_id IN (SELECT s.id FROM services s WHERE s.workspace_id<>coverages.workspace_id)`,
		`DELETE FROM services WHERE workspace_id NOT IN (SELECT id FROM workspaces)`,
		`DELETE FROM styles WHERE workspace_id NOT IN (SELECT id FROM workspaces)`,
		`DELETE FROM style_assets WHERE workspace_id NOT IN (SELECT id FROM workspaces)`,
		`DELETE FROM layer_groups WHERE workspace_id NOT IN (SELECT id FROM workspaces)`,
		`DELETE FROM api_keys WHERE workspace_id IS NOT NULL AND workspace_id NOT IN (SELECT id FROM workspaces)`,
		`DELETE FROM wfs_stored_queries WHERE workspace_id NOT IN (SELECT id FROM workspaces)`,
		`DELETE FROM claim_role_mappings WHERE workspace_id NOT IN (SELECT id FROM workspaces)`,
		`DELETE FROM casbin_rules WHERE v1 NOT IN ('','*') AND v1 NOT IN (SELECT id FROM workspaces)`,
	}
	var removed int64
	for _, statement := range statements {
		result, err := tx.ExecContext(ctx, statement)
		if err != nil {
			return 0, err
		}
		rows, _ := result.RowsAffected()
		removed += rows
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return removed, nil
}

func (s *DuckDBStore) CatalogOwnership(ctx context.Context) (*CatalogOwnership, error) {
	result := &CatalogOwnership{Workspaces: make(map[string]bool), Services: make(map[string]string), Resources: make(map[string]string)}
	if err := collectOwnership(ctx, s.db, `SELECT id,id FROM workspaces`, func(id, _ string) { result.Workspaces[id] = true }); err != nil {
		return nil, err
	}
	if err := collectOwnership(ctx, s.db, `SELECT s.id,s.workspace_id FROM services s JOIN workspaces w ON w.id=s.workspace_id`, func(id, workspaceID string) { result.Services[id] = workspaceID }); err != nil {
		return nil, err
	}
	queries := []string{
		`SELECT l.id,s.workspace_id FROM layers l JOIN services s ON s.id=l.service_id JOIN workspaces w ON w.id=s.workspace_id`,
		`SELECT c.id,c.workspace_id FROM coverages c JOIN services s ON s.id=c.service_id AND s.workspace_id=c.workspace_id JOIN workspaces w ON w.id=c.workspace_id`,
		`SELECT g.id,g.workspace_id FROM layer_groups g JOIN workspaces w ON w.id=g.workspace_id`,
	}
	for _, query := range queries {
		if err := collectOwnership(ctx, s.db, query, func(id, workspaceID string) { result.Resources[id] = workspaceID }); err != nil {
			return nil, err
		}
	}
	return result, nil
}

type ownershipQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func collectOwnership(ctx context.Context, queryer ownershipQuerier, query string, collect func(string, string)) error {
	rows, err := queryer.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, owner string
		if err := rows.Scan(&id, &owner); err != nil {
			return fmt.Errorf("scan catalog ownership: %w", err)
		}
		collect(id, owner)
	}
	return rows.Err()
}
