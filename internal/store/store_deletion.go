package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type DeletionScope string

const (
	DeletionScopeWorkspace DeletionScope = "workspace"
	DeletionScopeService   DeletionScope = "service"
)

type DeletionStatus string

const (
	DeletionPending   DeletionStatus = "pending"
	DeletionRunning   DeletionStatus = "running"
	DeletionFailed    DeletionStatus = "failed"
	DeletionCompleted DeletionStatus = "completed"
)

type DeletionPhase string

const (
	DeletionPhasePlanned          DeletionPhase = "planned"
	DeletionPhaseTombstoned       DeletionPhase = "tombstoned"
	DeletionPhaseJobsQuiesced     DeletionPhase = "jobs_quiesced"
	DeletionPhaseAuxiliaryRemoved DeletionPhase = "auxiliary_state_removed"
	DeletionPhaseCatalogCommitted DeletionPhase = "catalog_committed"
	DeletionPhaseRuntimeCleared   DeletionPhase = "runtime_cleared"
	DeletionPhaseCompleted        DeletionPhase = "completed"
)

// DeletionRef is a non-sensitive reference included in dependency reports.
type DeletionRef struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
}

type DeletionAuxiliary struct {
	TileEntries    int64 `json:"tile_entries,omitempty"`
	TileBytes      int64 `json:"tile_bytes,omitempty"`
	TileJobs       int64 `json:"tile_jobs,omitempty"`
	MosaicGranules int64 `json:"mosaic_granules,omitempty"`
	MosaicJobs     int64 `json:"mosaic_jobs,omitempty"`
	MosaicServices int64 `json:"mosaic_services,omitempty"`
	ManagedAssets  int64 `json:"managed_assets,omitempty"`
}

// DeletionPlan is both the public dependency report and the durable execution
// snapshot. Internal ID slices are intentionally omitted from JSON because the
// corresponding dependency records already carry those identifiers.
type DeletionPlan struct {
	Blockers    []DeletionRef `json:"blockers,omitempty"`
	Scope       DeletionScope `json:"scope"`
	WorkspaceID string        `json:"workspace_id"`
	Target      DeletionRef   `json:"target"`

	Services      []DeletionRef     `json:"services,omitempty"`
	Layers        []DeletionRef     `json:"layers,omitempty"`
	Coverages     []DeletionRef     `json:"coverages,omitempty"`
	LayerGroups   []DeletionRef     `json:"layer_groups,omitempty"`
	Styles        []DeletionRef     `json:"styles,omitempty"`
	StyleAssets   []DeletionRef     `json:"style_assets,omitempty"`
	APIKeys       []DeletionRef     `json:"api_keys,omitempty"`
	StoredQueries []DeletionRef     `json:"stored_queries,omitempty"`
	ClaimMappings []DeletionRef     `json:"claim_mappings,omitempty"`
	Policies      []DeletionRef     `json:"policies,omitempty"`
	Auxiliary     DeletionAuxiliary `json:"auxiliary,omitempty"`
}

func (p DeletionPlan) HasDependencies() bool {
	return len(p.Blockers)+len(p.Services)+len(p.Layers)+len(p.Coverages)+len(p.LayerGroups)+len(p.Styles)+
		len(p.StyleAssets)+len(p.APIKeys)+len(p.StoredQueries)+len(p.ClaimMappings)+len(p.Policies) > 0 ||
		p.Auxiliary.TileEntries > 0 || p.Auxiliary.TileJobs > 0 || p.Auxiliary.MosaicGranules > 0 ||
		p.Auxiliary.MosaicJobs > 0 || p.Auxiliary.MosaicServices > 0 || p.Auxiliary.ManagedAssets > 0
}

func (p *DeletionPlan) sort() {
	collections := [][]DeletionRef{p.Blockers, p.Services, p.Layers, p.Coverages, p.LayerGroups, p.Styles,
		p.StyleAssets, p.APIKeys, p.StoredQueries, p.ClaimMappings, p.Policies}
	for _, refs := range collections {
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].Kind != refs[j].Kind {
				return refs[i].Kind < refs[j].Kind
			}
			if refs[i].Name != refs[j].Name {
				return refs[i].Name < refs[j].Name
			}
			return refs[i].ID < refs[j].ID
		})
	}
}

type DeletionOperation struct {
	ID           string         `json:"id"`
	Scope        DeletionScope  `json:"scope"`
	WorkspaceID  string         `json:"workspace_id"`
	TargetID     string         `json:"target_id"`
	TargetName   string         `json:"target_name"`
	Status       DeletionStatus `json:"status"`
	Phase        DeletionPhase  `json:"phase"`
	Plan         DeletionPlan   `json:"plan"`
	LastError    string         `json:"last_error,omitempty"`
	AttemptCount int            `json:"attempt_count"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
}

type DeletionConflictError struct{ Plan DeletionPlan }

func (e *DeletionConflictError) Error() string { return "resource has dependent catalog state" }
func (e *DeletionConflictError) Unwrap() error { return ErrResourceNotEmpty }

func (s *DuckDBStore) PlanWorkspaceDeletion(ctx context.Context, workspaceID string) (*DeletionPlan, error) {
	workspace, err := s.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	plan := &DeletionPlan{Scope: DeletionScopeWorkspace, WorkspaceID: workspaceID,
		Target: DeletionRef{ID: workspace.ID, Name: workspace.Name, Kind: string(DeletionScopeWorkspace)}}
	if plan.Services, err = s.deletionRefs(ctx, `SELECT id,name FROM services WHERE workspace_id=?`, "service", "owned by workspace", workspaceID); err != nil {
		return nil, err
	}
	if plan.Layers, err = s.deletionRefs(ctx, `SELECT l.id,l.public_id FROM layers l JOIN services s ON s.id=l.service_id WHERE s.workspace_id=?`, "layer", "owned by workspace service", workspaceID); err != nil {
		return nil, err
	}
	if plan.Coverages, err = s.deletionRefs(ctx, `SELECT id,public_id FROM coverages WHERE workspace_id=?`, "coverage", "owned by workspace", workspaceID); err != nil {
		return nil, err
	}
	if plan.LayerGroups, err = s.deletionRefs(ctx, `SELECT id,public_id FROM layer_groups WHERE workspace_id=?`, "layer_group", "owned by workspace", workspaceID); err != nil {
		return nil, err
	}
	if plan.Styles, err = s.deletionRefs(ctx, `SELECT id,name FROM styles WHERE workspace_id=?`, "style", "owned by workspace", workspaceID); err != nil {
		return nil, err
	}
	if plan.StyleAssets, err = s.deletionRefs(ctx, `SELECT id,name FROM style_assets WHERE workspace_id=?`, "style_asset", "owned by workspace", workspaceID); err != nil {
		return nil, err
	}
	if plan.APIKeys, err = s.deletionRefs(ctx, `SELECT id,name FROM api_keys WHERE workspace_id=?`, "api_key", "scoped to workspace", workspaceID); err != nil {
		return nil, err
	}
	if plan.StoredQueries, err = s.deletionRefs(ctx, `SELECT id,query_id FROM wfs_stored_queries WHERE workspace_id=?`, "stored_query", "scoped to workspace", workspaceID); err != nil {
		return nil, err
	}
	if plan.ClaimMappings, err = s.deletionRefs(ctx, `SELECT id,claim_name || '=' || claim_value FROM claim_role_mappings WHERE workspace_id=?`, "claim_mapping", "scoped to workspace", workspaceID); err != nil {
		return nil, err
	}
	if plan.Policies, err = s.deletionRefs(ctx, `SELECT CAST(id AS VARCHAR),ptype || ':' || coalesce(v0,'') || ':' || coalesce(v2,'') FROM casbin_rules WHERE v1=?`, "rbac_policy", "scoped to workspace", workspaceID); err != nil {
		return nil, err
	}
	plan.sort()
	return plan, nil
}

func (s *DuckDBStore) PlanServiceDeletion(ctx context.Context, workspaceID, serviceID string) (*DeletionPlan, error) {
	service, err := s.GetService(ctx, serviceID)
	if err != nil || service.WorkspaceID != workspaceID {
		if err == nil {
			err = ErrNotFound
		}
		return nil, err
	}
	plan := &DeletionPlan{Scope: DeletionScopeService, WorkspaceID: workspaceID,
		Target: DeletionRef{ID: service.ID, Name: service.Name, Kind: string(DeletionScopeService)}}
	if plan.Layers, err = s.deletionRefs(ctx, `SELECT id,public_id FROM layers WHERE service_id=?`, "layer", "owned by service", serviceID); err != nil {
		return nil, err
	}
	if plan.Coverages, err = s.deletionRefs(ctx, `SELECT id,public_id FROM coverages WHERE service_id=?`, "coverage", "owned by service", serviceID); err != nil {
		return nil, err
	}

	// A recursive service delete owns the transitive group closure: once a
	// resource disappears, every group that directly or indirectly contains it
	// would otherwise become an invalid publication.
	targetNames := make(map[string]bool)
	for _, ref := range append(append([]DeletionRef{}, plan.Layers...), plan.Coverages...) {
		targetNames[ref.Name] = true
	}
	groups, err := s.ListLayerGroups(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for changed := true; changed; {
		changed = false
		for _, group := range groups {
			if targetNames[group.PublicID] {
				continue
			}
			for _, member := range group.Members {
				if targetNames[member.Resource] {
					targetNames[group.PublicID] = true
					plan.LayerGroups = append(plan.LayerGroups, DeletionRef{ID: group.ID, Name: group.PublicID, Kind: "layer_group", Reason: "depends on deleted service resource"})
					changed = true
					break
				}
			}
		}
	}

	// Stored queries with an explicit static reference cannot survive service
	// removal. Parameter-only queries are retained.
	queries, err := s.ListWFSStoredQueries(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, query := range queries {
		encoded, _ := json.Marshal(query.ReturnTypes)
		haystack := string(encoded) + "\n" + query.QueryExpression
		for name := range targetNames {
			if strings.Contains(haystack, name) {
				plan.StoredQueries = append(plan.StoredQueries, DeletionRef{ID: query.ID, Name: query.QueryID, Kind: "stored_query", Reason: "references deleted service resource"})
				break
			}
		}
	}

	policies, err := s.deletionRefs(ctx, `SELECT CAST(id AS VARCHAR),ptype || ':' || coalesce(v0,'') || ':' || coalesce(v2,'') FROM casbin_rules WHERE v1=?`, "rbac_policy", "workspace policy", workspaceID)
	if err != nil {
		return nil, err
	}
	for _, policy := range policies {
		for name := range targetNames {
			if strings.HasSuffix(policy.Name, ":"+name) {
				policy.Reason = "references deleted service resource"
				plan.Policies = append(plan.Policies, policy)
				break
			}
		}
	}
	if err := s.addDatasetMapDeletionBlocker(ctx, plan); err != nil {
		return nil, err
	}
	plan.sort()
	return plan, nil
}

func (s *DuckDBStore) deletionRefs(ctx context.Context, query, kind, reason string, args ...any) ([]DeletionRef, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []DeletionRef
	for rows.Next() {
		var ref DeletionRef
		if err := rows.Scan(&ref.ID, &ref.Name); err != nil {
			return nil, err
		}
		ref.Kind, ref.Reason = kind, reason
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *DuckDBStore) BeginCatalogDeletion(ctx context.Context, plan DeletionPlan) (*DeletionOperation, bool, error) {
	s.datasetMapMu.Lock()
	defer s.datasetMapMu.Unlock()
	if plan.Target.ID == "" || plan.WorkspaceID == "" {
		return nil, false, errors.New("deletion plan target and workspace are required")
	}
	if existing, err := s.findActiveCatalogDeletion(ctx, plan.Scope, plan.Target.ID); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	// Check current dependencies under the same lock as selection and group writes.
	if plan.Scope == DeletionScopeService {
		current, err := s.PlanServiceDeletion(ctx, plan.WorkspaceID, plan.Target.ID)
		if err != nil {
			return nil, false, err
		}
		if len(current.Blockers) > 0 {
			return nil, false, ErrDatasetMapInUse
		}
		// Keep coordinator-enriched auxiliary counts, but capture current catalog dependencies.
		current.Auxiliary = plan.Auxiliary
		plan = *current
	}
	plan.sort()
	encoded, err := json.Marshal(plan)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	op := &DeletionOperation{ID: uuid.NewString(), Scope: plan.Scope, WorkspaceID: plan.WorkspaceID,
		TargetID: plan.Target.ID, TargetName: plan.Target.Name, Status: DeletionPending,
		Phase: DeletionPhasePlanned, Plan: plan, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO catalog_deletions
		(id,scope_kind,workspace_id,target_id,target_name,active_target,status,phase,plan_json,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`, op.ID, op.Scope, op.WorkspaceID, op.TargetID, op.TargetName, string(op.Scope)+":"+op.TargetID,
		op.Status, op.Phase, string(encoded), now, now)
	if err != nil {
		// Concurrent callers converge on the same active operation.
		if existing, findErr := s.findActiveCatalogDeletion(ctx, plan.Scope, plan.Target.ID); findErr == nil {
			return existing, false, nil
		}
		return nil, false, fmt.Errorf("begin catalog deletion: %w", err)
	}
	return op, true, nil
}

func (s *DuckDBStore) findActiveCatalogDeletion(ctx context.Context, scope DeletionScope, targetID string) (*DeletionOperation, error) {
	return scanDeletionOperation(s.db.QueryRowContext(ctx, `SELECT id,scope_kind,workspace_id,target_id,target_name,status,phase,
		plan_json,last_error,attempt_count,created_at,updated_at,completed_at FROM catalog_deletions
		WHERE scope_kind=? AND target_id=? AND status IN ('pending','running','failed') ORDER BY created_at DESC LIMIT 1`, scope, targetID))
}

func (s *DuckDBStore) GetCatalogDeletion(ctx context.Context, id string) (*DeletionOperation, error) {
	return scanDeletionOperation(s.db.QueryRowContext(ctx, `SELECT id,scope_kind,workspace_id,target_id,target_name,status,phase,
		plan_json,last_error,attempt_count,created_at,updated_at,completed_at FROM catalog_deletions WHERE id=?`, id))
}

// FindCatalogDeletionByTarget returns the newest deletion operation for a
// target, including completed operations. Import rollback recovery uses this
// to close the small crash window before its operation ID is persisted.
func (s *DuckDBStore) FindCatalogDeletionByTarget(ctx context.Context, scope DeletionScope, targetID string) (*DeletionOperation, error) {
	return scanDeletionOperation(s.db.QueryRowContext(ctx, `SELECT id,scope_kind,workspace_id,target_id,target_name,status,phase,
		plan_json,last_error,attempt_count,created_at,updated_at,completed_at FROM catalog_deletions
		WHERE scope_kind=? AND target_id=? ORDER BY created_at DESC LIMIT 1`, scope, targetID))
}

func (s *DuckDBStore) ListPendingCatalogDeletions(ctx context.Context) ([]*DeletionOperation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope_kind,workspace_id,target_id,target_name,status,phase,
		plan_json,last_error,attempt_count,created_at,updated_at,completed_at FROM catalog_deletions
		WHERE status IN ('pending','running','failed') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*DeletionOperation
	for rows.Next() {
		op, err := scanDeletionOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, op)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) ListCatalogDeletions(ctx context.Context, limit int) ([]*DeletionOperation, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope_kind,workspace_id,target_id,target_name,status,phase,
		plan_json,last_error,attempt_count,created_at,updated_at,completed_at FROM catalog_deletions
		ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*DeletionOperation, 0)
	for rows.Next() {
		operation, scanErr := scanDeletionOperation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, operation)
	}
	return result, rows.Err()
}

type deletionScanner interface{ Scan(...any) error }

func scanDeletionOperation(scanner deletionScanner) (*DeletionOperation, error) {
	var op DeletionOperation
	var encoded any
	var completed sql.NullTime
	if err := scanner.Scan(&op.ID, &op.Scope, &op.WorkspaceID, &op.TargetID, &op.TargetName,
		&op.Status, &op.Phase, &encoded, &op.LastError, &op.AttemptCount, &op.CreatedAt,
		&op.UpdatedAt, &completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var raw []byte
	switch value := encoded.(type) {
	case string:
		raw = []byte(value)
	case []byte:
		raw = value
	default:
		raw, _ = json.Marshal(value)
	}
	if err := json.Unmarshal(raw, &op.Plan); err != nil {
		return nil, fmt.Errorf("decode deletion plan: %w", err)
	}
	if completed.Valid {
		value := completed.Time.UTC()
		op.CompletedAt = &value
	}
	return &op, nil
}

func (s *DuckDBStore) UpdateCatalogDeletion(ctx context.Context, id string, status DeletionStatus, phase DeletionPhase, lastError string) error {
	now := time.Now().UTC()
	var completed any
	if status == DeletionCompleted {
		completed = now
	}
	result, err := s.db.ExecContext(ctx, `UPDATE catalog_deletions SET status=?,phase=?,last_error=?,active_target=CASE WHEN ?='completed' THEN NULL ELSE active_target END,
		attempt_count=attempt_count+CASE WHEN ?='running' AND status<>'running' THEN 1 ELSE 0 END,updated_at=?,
		completed_at=coalesce(?,completed_at) WHERE id=?`, status, phase, lastError, status, status, now, completed, id)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) CommitCatalogDeletion(ctx context.Context, operationID string) error {
	op, err := s.GetCatalogDeletion(ctx, operationID)
	if err != nil {
		return err
	}
	if op.Status == DeletionCompleted || op.Phase == DeletionPhaseCatalogCommitted || op.Phase == DeletionPhaseRuntimeCleared {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	switch op.Scope {
	case DeletionScopeWorkspace:
		err = deleteWorkspaceCatalog(ctx, tx, op.WorkspaceID)
	case DeletionScopeService:
		err = deleteServiceCatalog(ctx, tx, op)
	default:
		err = fmt.Errorf("unsupported deletion scope %q", op.Scope)
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_deletions SET status='running',phase=?,last_error='',updated_at=? WHERE id=?`, DeletionPhaseCatalogCommitted, now, operationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE workspaces SET capabilities_revision = capabilities_revision + 1 WHERE id = ?", op.WorkspaceID); err != nil {
		return err
	}
	return tx.Commit()
}

func deleteWorkspaceCatalog(ctx context.Context, tx *sql.Tx, workspaceID string) error {
	statements := []string{
		`DELETE FROM casbin_rules WHERE v1=?`,
		`DELETE FROM layers WHERE service_id IN (SELECT id FROM services WHERE workspace_id=?)`,
		`DELETE FROM coverages WHERE workspace_id=? OR service_id IN (SELECT id FROM services WHERE workspace_id=?)`,
		`DELETE FROM services WHERE workspace_id=?`,
		`DELETE FROM layer_groups WHERE workspace_id=?`,
		`DELETE FROM wfs_stored_queries WHERE workspace_id=?`,
		`DELETE FROM style_assets WHERE workspace_id=?`,
		`DELETE FROM styles WHERE workspace_id=?`,
		`DELETE FROM api_keys WHERE workspace_id=?`,
		`DELETE FROM claim_role_mappings WHERE workspace_id=?`,
		`DELETE FROM workspace_data_revisions WHERE workspace_id=?`,
		`DELETE FROM workspaces WHERE id=?`,
	}
	for _, statement := range statements {
		args := []any{workspaceID}
		if strings.Count(statement, "?") == 2 {
			args = append(args, workspaceID)
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return err
		}
	}
	return nil
}

func deleteServiceCatalog(ctx context.Context, tx *sql.Tx, op *DeletionOperation) error {
	resourceNames := make([]string, 0, len(op.Plan.Layers)+len(op.Plan.Coverages)+len(op.Plan.LayerGroups))
	for _, ref := range append(append(append([]DeletionRef{}, op.Plan.Layers...), op.Plan.Coverages...), op.Plan.LayerGroups...) {
		resourceNames = append(resourceNames, ref.Name)
	}
	if len(resourceNames) > 0 {
		query, args := inClause(`DELETE FROM casbin_rules WHERE v1=? AND v2 IN (%s)`, resourceNames)
		args = append([]any{op.WorkspaceID}, args...)
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM layers WHERE service_id=?`, op.TargetID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM coverages WHERE service_id=?`, op.TargetID); err != nil {
		return err
	}
	if err := deleteRefs(ctx, tx, "layer_groups", op.Plan.LayerGroups); err != nil {
		return err
	}
	if err := deleteRefs(ctx, tx, "wfs_stored_queries", op.Plan.StoredQueries); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM services WHERE id=? AND workspace_id=?`, op.TargetID, op.WorkspaceID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		// A crash after the catalog commit is idempotent. The operation phase may
		// lag the actual transaction only if the database itself was restored.
		return nil
	}
	return nil
}

func deleteRefs(ctx context.Context, tx *sql.Tx, table string, refs []DeletionRef) error {
	if len(refs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	query, args := inClause(`DELETE FROM `+table+` WHERE id IN (%s)`, ids)
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func inClause(template string, values []string) (string, []any) {
	marks := make([]string, len(values))
	args := make([]any, len(values))
	for i, value := range values {
		marks[i], args[i] = "?", value
	}
	return fmt.Sprintf(template, strings.Join(marks, ",")), args
}
