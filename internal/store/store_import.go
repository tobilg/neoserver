package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ImportStatus string

const (
	ImportQueued         ImportStatus = "queued"
	ImportRunning        ImportStatus = "running"
	ImportAwaitingPlan   ImportStatus = "awaiting_plan"
	ImportReadyToPublish ImportStatus = "ready_to_publish"
	ImportPublishing     ImportStatus = "publishing"
	ImportPublished      ImportStatus = "published"
	ImportCancelling     ImportStatus = "cancelling"
	ImportCancelled      ImportStatus = "cancelled"
	ImportFailed         ImportStatus = "failed"
	ImportRollingBack    ImportStatus = "rolling_back"
	ImportRolledBack     ImportStatus = "rolled_back"
)

type ImportPhase string

const (
	ImportPhaseAcquire   ImportPhase = "acquire"
	ImportPhaseDiscover  ImportPhase = "discover"
	ImportPhaseValidate  ImportPhase = "validate"
	ImportPhaseTransform ImportPhase = "transform"
	ImportPhasePreview   ImportPhase = "preview"
	ImportPhasePublish   ImportPhase = "publish"
	ImportPhaseRollback  ImportPhase = "rollback"
)

type ImportProperty struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	JSONType string `json:"json_type,omitempty"`
	Ordinal  int    `json:"ordinal,omitempty"`
}

type ImportDiscoveredLayer struct {
	Name           string           `json:"name"`
	Title          string           `json:"title,omitempty"`
	Description    string           `json:"description,omitempty"`
	GeometryColumn string           `json:"geometry_column"`
	GeometryType   string           `json:"geometry_type"`
	SRID           int              `json:"srid"`
	IDColumn       string           `json:"id_column,omitempty"`
	FeatureCount   int64            `json:"feature_count"`
	NativeExtent   *SpatialExtent   `json:"native_extent,omitempty"`
	Properties     []ImportProperty `json:"properties,omitempty"`
}

type ImportDiscovery struct {
	Layers   []ImportDiscoveredLayer `json:"layers"`
	Warnings []string                `json:"warnings,omitempty"`
}

type ImportFieldMapping struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Type    string `json:"type,omitempty"`
	Include bool   `json:"include"`
}

type ImportLayerPlan struct {
	SourceLayer    string               `json:"source_layer"`
	PublicID       string               `json:"public_id"`
	Title          string               `json:"title,omitempty"`
	Description    string               `json:"description,omitempty"`
	Enabled        bool                 `json:"enabled"`
	Public         bool                 `json:"public"`
	AllowedRoles   []string             `json:"allowed_roles,omitempty"`
	Fields         []ImportFieldMapping `json:"fields,omitempty"`
	GeometryColumn string               `json:"geometry_column,omitempty"`
	TargetGeometry string               `json:"target_geometry_column,omitempty"`
	IDColumn       string               `json:"id_column,omitempty"`
	SourceSRID     int                  `json:"source_srid,omitempty"`
	TargetSRID     int                  `json:"target_srid,omitempty"`
}

type ImportPlan struct {
	ServiceName string            `json:"service_name"`
	Layers      []ImportLayerPlan `json:"layers"`
}

type ImportJob struct {
	ID                  string           `json:"id"`
	WorkspaceID         string           `json:"workspace_id"`
	Name                string           `json:"name"`
	SourceKind          string           `json:"source_kind"`
	SourceLocator       string           `json:"source_locator,omitempty"`
	SourcePath          string           `json:"-"`
	SourceRelativePath  string           `json:"-"`
	SourceFilename      string           `json:"source_filename,omitempty"`
	Status              ImportStatus     `json:"status"`
	Phase               ImportPhase      `json:"phase"`
	Plan                *ImportPlan      `json:"plan,omitempty"`
	Discovery           *ImportDiscovery `json:"discovery,omitempty"`
	ServiceID           string           `json:"service_id,omitempty"`
	ManagedPath         string           `json:"-"`
	RollbackOperationID string           `json:"rollback_operation_id,omitempty"`
	ProcessedBytes      int64            `json:"processed_bytes"`
	ProcessedFeatures   int64            `json:"processed_features"`
	ProcessedLayers     int              `json:"processed_layers"`
	TotalLayers         int              `json:"total_layers"`
	RetryCount          int              `json:"retry_count"`
	CancelRequested     bool             `json:"cancel_requested"`
	ErrorMessage        string           `json:"error_message,omitempty"`
	CreatedBy           string           `json:"created_by,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	StartedAt           *time.Time       `json:"started_at,omitempty"`
	CompletedAt         *time.Time       `json:"completed_at,omitempty"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

// ImportJobEvent is an immutable snapshot of a meaningful import transition.
// Progress counters are included so the console can render a useful timeline
// without attempting to reconstruct past mutable job rows.
type ImportJobEvent struct {
	ID                string       `json:"id"`
	ImportID          string       `json:"import_id"`
	WorkspaceID       string       `json:"workspace_id"`
	Status            ImportStatus `json:"status"`
	Phase             ImportPhase  `json:"phase"`
	Warnings          []string     `json:"warnings,omitempty"`
	ErrorMessage      string       `json:"error_message,omitempty"`
	ProcessedBytes    int64        `json:"processed_bytes"`
	ProcessedFeatures int64        `json:"processed_features"`
	ProcessedLayers   int          `json:"processed_layers"`
	CreatedAt         time.Time    `json:"created_at"`
}

type CreateImportJobInput struct {
	WorkspaceID, Name, SourceKind, SourceLocator, SourcePath, SourceFilename, CreatedBy string
	SourceRelativePath                                                                  string
}

type ImportJobUpdate struct {
	SourcePath          *string
	SourceRelativePath  *string
	Status              *ImportStatus
	Phase               *ImportPhase
	Plan                *ImportPlan
	Discovery           *ImportDiscovery
	ServiceID           *string
	ManagedPath         *string
	RollbackOperationID *string
	ProcessedBytes      *int64
	ProcessedFeatures   *int64
	ProcessedLayers     *int
	TotalLayers         *int
	RetryCount          *int
	CancelRequested     *bool
	ErrorMessage        *string
	StartedAt           *time.Time
	CompletedAt         *time.Time
	ClearCompletedAt    bool
}

type ManagedAsset struct {
	ImportID, WorkspaceID, ServiceID, Path, EncryptionKey string
	CreatedAt                                             time.Time
}

type PublishImportInput struct {
	ImportID, WorkspaceID, ManagedPath, EncryptionKey string
	Service                                           CreateServiceInput
	Layers                                            []CreateLayerInput
}

type ImportStore interface {
	CreateImportJob(context.Context, CreateImportJobInput) (*ImportJob, error)
	GetImportJob(context.Context, string) (*ImportJob, error)
	ListImportJobs(context.Context, string, int) ([]*ImportJob, error)
	ListImportJobsPage(context.Context, string, ImportListOptions) (*ImportJobPage, error)
	ListRunnableImportJobs(context.Context) ([]*ImportJob, error)
	ListInterruptedImportJobs(context.Context) ([]*ImportJob, error)
	ListImportJobEvents(context.Context, string, int) ([]*ImportJobEvent, error)
	UpdateImportJob(context.Context, string, ImportJobUpdate) (*ImportJob, error)
	RequestImportCancel(context.Context, string) error
	ResetInterruptedImportJobs(context.Context) error
	PublishImport(context.Context, PublishImportInput) (*Service, []*Layer, error)
	GetManagedAssetByImport(context.Context, string) (*ManagedAsset, error)
	GetManagedAssetByService(context.Context, string) (*ManagedAsset, error)
	ListManagedAssets(context.Context) ([]*ManagedAsset, error)
	UpsertStagedManagedAsset(context.Context, ManagedAsset) error
	CompleteImportTransform(context.Context, ManagedAsset) error
	UpdateManagedAssetPath(context.Context, string, string) error
	DeleteManagedAsset(context.Context, string) error
}

func (s *DuckDBStore) CreateImportJob(ctx context.Context, input CreateImportJobInput) (*ImportJob, error) {
	now := time.Now().UTC()
	job := &ImportJob{ID: uuid.NewString(), WorkspaceID: input.WorkspaceID, Name: input.Name,
		SourceKind: input.SourceKind, SourceLocator: input.SourceLocator, SourcePath: input.SourcePath,
		SourceFilename: input.SourceFilename, Status: ImportQueued, Phase: ImportPhaseAcquire,
		SourceRelativePath: input.SourceRelativePath,
		CreatedBy:          input.CreatedBy, CreatedAt: now, UpdatedAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create import job: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO import_jobs
		(id,workspace_id,name,source_kind,source_locator,source_path,source_filename,status,phase,created_by,created_at,updated_at,source_relative_path)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, job.ID, job.WorkspaceID, job.Name, job.SourceKind, job.SourceLocator,
		job.SourcePath, job.SourceFilename, job.Status, job.Phase, job.CreatedBy, now, now, job.SourceRelativePath)
	if err != nil {
		return nil, fmt.Errorf("create import job: %w", err)
	}
	if err = insertImportEvent(ctx, tx, job, nil); err != nil {
		return nil, fmt.Errorf("create import event: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("create import job: %w", err)
	}
	return job, nil
}

type importScanner interface{ Scan(...any) error }

func scanImportJob(row importScanner) (*ImportJob, error) {
	var job ImportJob
	var status, phase string
	var planRaw, discoveryRaw any
	err := row.Scan(&job.ID, &job.WorkspaceID, &job.Name, &job.SourceKind, &job.SourceLocator, &job.SourcePath, &job.SourceFilename,
		&status, &phase, &planRaw, &discoveryRaw, &job.ServiceID, &job.ManagedPath, &job.RollbackOperationID, &job.ProcessedBytes, &job.ProcessedFeatures,
		&job.ProcessedLayers, &job.TotalLayers, &job.RetryCount, &job.CancelRequested, &job.ErrorMessage, &job.CreatedBy,
		&job.CreatedAt, &job.StartedAt, &job.CompletedAt, &job.UpdatedAt, &job.SourceRelativePath)
	if err != nil {
		return nil, err
	}
	job.Status, job.Phase = ImportStatus(status), ImportPhase(phase)
	if planRaw != nil {
		var plan ImportPlan
		if decodeJSONValue(planRaw, &plan) == nil {
			job.Plan = &plan
		}
	}
	if discoveryRaw != nil {
		var discovery ImportDiscovery
		if decodeJSONValue(discoveryRaw, &discovery) == nil {
			job.Discovery = &discovery
		}
	}
	return &job, nil
}

const importSelect = `SELECT id,workspace_id,name,source_kind,source_locator,source_path,source_filename,status,phase,
	plan_json,discovery_json,coalesce(service_id,''),coalesce(managed_path,''),coalesce(rollback_operation_id,''),processed_bytes,processed_features,
	processed_layers,total_layers,retry_count,cancel_requested,error_message,created_by,created_at,started_at,completed_at,updated_at,coalesce(source_relative_path,'')
	FROM import_jobs`

func (s *DuckDBStore) GetImportJob(ctx context.Context, id string) (*ImportJob, error) {
	job, err := scanImportJob(s.db.QueryRowContext(ctx, importSelect+` WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get import job: %w", err)
	}
	return job, nil
}

func (s *DuckDBStore) ListImportJobs(ctx context.Context, workspaceID string, limit int) ([]*ImportJob, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, importSelect+` WHERE workspace_id=? ORDER BY created_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list import jobs: %w", err)
	}
	defer rows.Close()
	var result []*ImportJob
	for rows.Next() {
		job, err := scanImportJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) ListRunnableImportJobs(ctx context.Context) ([]*ImportJob, error) {
	rows, err := s.db.QueryContext(ctx, importSelect+` WHERE status='queued' ORDER BY created_at LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*ImportJob
	for rows.Next() {
		job, err := scanImportJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) ListInterruptedImportJobs(ctx context.Context) ([]*ImportJob, error) {
	rows, err := s.db.QueryContext(ctx, importSelect+` WHERE status IN ('publishing','published','rolling_back','cancelling','cancelled') ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list interrupted import jobs: %w", err)
	}
	defer rows.Close()
	var result []*ImportJob
	for rows.Next() {
		job, err := scanImportJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) UpdateImportJob(ctx context.Context, id string, update ImportJobUpdate) (*ImportJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	job, err := scanImportJob(tx.QueryRowContext(ctx, importSelect+` WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	previousStatus, previousPhase, previousError := job.Status, job.Phase, job.ErrorMessage
	if job.CancelRequested && update.Status != nil && *update.Status != ImportCancelled && *update.Status != ImportCancelling {
		return nil, context.Canceled
	}
	previousWarnings := importWarnings(job)
	if update.Status != nil {
		job.Status = *update.Status
	}
	if update.SourcePath != nil {
		job.SourcePath = *update.SourcePath
	}
	if update.SourceRelativePath != nil {
		job.SourceRelativePath = *update.SourceRelativePath
	}
	if update.Phase != nil {
		job.Phase = *update.Phase
	}
	if update.Plan != nil {
		job.Plan = update.Plan
	}
	if update.Discovery != nil {
		job.Discovery = update.Discovery
	}
	if update.ServiceID != nil {
		job.ServiceID = *update.ServiceID
	}
	if update.ManagedPath != nil {
		job.ManagedPath = *update.ManagedPath
	}
	if update.RollbackOperationID != nil {
		job.RollbackOperationID = *update.RollbackOperationID
	}
	if update.ProcessedBytes != nil {
		job.ProcessedBytes = *update.ProcessedBytes
	}
	if update.ProcessedFeatures != nil {
		job.ProcessedFeatures = *update.ProcessedFeatures
	}
	if update.ProcessedLayers != nil {
		job.ProcessedLayers = *update.ProcessedLayers
	}
	if update.TotalLayers != nil {
		job.TotalLayers = *update.TotalLayers
	}
	if update.RetryCount != nil {
		job.RetryCount = *update.RetryCount
	}
	if update.CancelRequested != nil {
		job.CancelRequested = *update.CancelRequested
	}
	if update.ErrorMessage != nil {
		job.ErrorMessage = *update.ErrorMessage
	}
	if update.StartedAt != nil {
		job.StartedAt = update.StartedAt
	}
	if update.CompletedAt != nil {
		job.CompletedAt = update.CompletedAt
	}
	if update.ClearCompletedAt {
		job.CompletedAt = nil
	}
	job.UpdatedAt = time.Now().UTC()
	_, err = tx.ExecContext(ctx, `UPDATE import_jobs SET source_path=?,status=?,phase=?,plan_json=?,discovery_json=?,service_id=?,managed_path=?,rollback_operation_id=?,
		processed_bytes=?,processed_features=?,processed_layers=?,total_layers=?,retry_count=?,cancel_requested=?,error_message=?,
		started_at=?,completed_at=?,updated_at=?,source_relative_path=? WHERE id=?`, job.SourcePath, job.Status, job.Phase, nullableJSON(job.Plan), nullableJSON(job.Discovery),
		nullableString(job.ServiceID), nullableString(job.ManagedPath), nullableString(job.RollbackOperationID), job.ProcessedBytes, job.ProcessedFeatures, job.ProcessedLayers,
		job.TotalLayers, job.RetryCount, job.CancelRequested, job.ErrorMessage, job.StartedAt, job.CompletedAt, job.UpdatedAt, job.SourceRelativePath, id)
	if err != nil {
		return nil, fmt.Errorf("update import job: %w", err)
	}
	if previousStatus != job.Status || previousPhase != job.Phase || previousError != job.ErrorMessage || !equalStrings(previousWarnings, importWarnings(job)) {
		if err = insertImportEvent(ctx, tx, job, nil); err != nil {
			return nil, fmt.Errorf("record import event: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("update import job: %w", err)
	}
	return job, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *DuckDBStore) RequestImportCancel(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE import_jobs SET cancel_requested=true,
		status='cancelling',completed_at=NULL,updated_at=?
		WHERE id=? AND status IN ('queued','running','cancelling','awaiting_plan','ready_to_publish','failed','cancelled')`, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	job, err := scanImportJob(tx.QueryRowContext(ctx, importSelect+` WHERE id=?`, id))
	if err != nil {
		return err
	}
	if err = insertImportEvent(ctx, tx, job, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DuckDBStore) ResetInterruptedImportJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE import_jobs SET status=CASE WHEN cancel_requested THEN 'cancelling' ELSE 'queued' END,
		error_message=CASE WHEN error_message='' THEN 'resumed after restart' ELSE error_message END,updated_at=?
		WHERE status='running'`, time.Now().UTC())
	return err
}

func (s *DuckDBStore) PublishImport(ctx context.Context, input PublishImportInput) (*Service, []*Layer, error) {
	existing, err := s.GetImportJob(ctx, input.ImportID)
	if err != nil {
		return nil, nil, err
	}
	if existing.Status == ImportPublished && existing.ServiceID != "" {
		svc, getErr := s.GetService(ctx, existing.ServiceID)
		return svc, nil, getErr
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	job, err := scanImportJob(tx.QueryRowContext(ctx, importSelect+` WHERE id=?`, input.ImportID))
	if err != nil {
		return nil, nil, err
	}
	_ = job
	now := time.Now().UTC()
	serviceID := uuid.NewString()
	connection := string(input.Service.ConnectionInfo)
	if connection == "" {
		connection = "{}"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO services(id,workspace_id,name,type,connection_info,cache_settings,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, serviceID, input.WorkspaceID, input.Service.Name, string(input.Service.Type), connection,
		nullableJSON(input.Service.CacheSettings), input.Service.Enabled, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, nil, ErrDuplicateKey
		}
		return nil, nil, err
	}
	layers := make([]*Layer, 0, len(input.Layers))
	for _, item := range input.Layers {
		id := uuid.NewString()
		crs := item.CRSDefault
		if crs == 0 {
			crs = 4326
		}
		var nativeExtent any
		if item.NativeExtent != nil {
			nativeExtent = marshalJSON(item.NativeExtent, "null")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO layers(id,service_id,source_layer,public_id,title,description,enabled,crs_default,
			dimensions,is_sql_view,sql_view_config,public,allowed_roles,default_style,styles,native_extent,tile_cache_quota_bytes,
			tile_cache_generation,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?, '[]',false,NULL,?,?,?, '[]',?,0,1,?,?)`,
			id, serviceID, item.SourceLayer, item.PublicID, item.Title, item.Description, item.Enabled, crs, item.Public,
			marshalAllowedRoles(item.AllowedRoles), item.DefaultStyle, nativeExtent, now, now)
		if err != nil {
			return nil, nil, err
		}
		layers = append(layers, &Layer{ID: id, ServiceID: serviceID, SourceLayer: item.SourceLayer, PublicID: item.PublicID, Title: item.Title,
			Description: item.Description, Enabled: item.Enabled, CRSDefault: crs, Public: item.Public, AllowedRoles: item.AllowedRoles,
			DefaultStyle: item.DefaultStyle, NativeExtent: item.NativeExtent, TileCacheGeneration: 1, CreatedAt: now, UpdatedAt: now})
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO managed_assets(import_id,workspace_id,service_id,path,encryption_key,created_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(import_id) DO UPDATE SET workspace_id=excluded.workspace_id,
		service_id=excluded.service_id,path=excluded.path,encryption_key=excluded.encryption_key`, input.ImportID, input.WorkspaceID, serviceID, input.ManagedPath, input.EncryptionKey, now)
	if err != nil {
		return nil, nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE import_jobs SET status='published',phase='publish',service_id=?,managed_path=?,
		completed_at=?,updated_at=?,error_message='' WHERE id=?`, serviceID, input.ManagedPath, now, now, input.ImportID)
	if err != nil {
		return nil, nil, err
	}
	job.Status, job.Phase, job.ServiceID, job.ManagedPath = ImportPublished, ImportPhasePublish, serviceID, input.ManagedPath
	job.ErrorMessage, job.CompletedAt, job.UpdatedAt = "", &now, now
	if err = insertImportEvent(ctx, tx, job, nil); err != nil {
		return nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE workspaces SET capabilities_revision = capabilities_revision + 1, tile_revision=tile_revision+1,updated_at=? WHERE id=?`, now, input.WorkspaceID); err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &Service{ID: serviceID, WorkspaceID: input.WorkspaceID, Name: input.Service.Name, Type: input.Service.Type,
		ConnectionInfo: input.Service.ConnectionInfo, CacheSettings: input.Service.CacheSettings, Enabled: input.Service.Enabled, CreatedAt: now, UpdatedAt: now}, layers, nil
}

func scanManagedAsset(row *sql.Row) (*ManagedAsset, error) {
	var item ManagedAsset
	err := row.Scan(&item.ImportID, &item.WorkspaceID, &item.ServiceID, &item.Path, &item.EncryptionKey, &item.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &item, err
}

func (s *DuckDBStore) GetManagedAssetByImport(ctx context.Context, id string) (*ManagedAsset, error) {
	return scanManagedAsset(s.db.QueryRowContext(ctx, `SELECT import_id,workspace_id,coalesce(service_id,''),path,encryption_key,created_at FROM managed_assets WHERE import_id=?`, id))
}
func (s *DuckDBStore) GetManagedAssetByService(ctx context.Context, id string) (*ManagedAsset, error) {
	return scanManagedAsset(s.db.QueryRowContext(ctx, `SELECT import_id,workspace_id,coalesce(service_id,''),path,encryption_key,created_at FROM managed_assets WHERE service_id=?`, id))
}
func (s *DuckDBStore) ListManagedAssets(ctx context.Context) ([]*ManagedAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT import_id,workspace_id,coalesce(service_id,''),path,encryption_key,created_at FROM managed_assets ORDER BY created_at,import_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*ManagedAsset
	for rows.Next() {
		var item ManagedAsset
		if err := rows.Scan(&item.ImportID, &item.WorkspaceID, &item.ServiceID, &item.Path, &item.EncryptionKey, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, &item)
	}
	return result, rows.Err()
}
func (s *DuckDBStore) UpsertStagedManagedAsset(ctx context.Context, item ManagedAsset) error {
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO managed_assets(import_id,workspace_id,service_id,path,encryption_key,created_at)
		VALUES(?,?,NULL,?,?,?) ON CONFLICT(import_id) DO UPDATE SET workspace_id=excluded.workspace_id,
		service_id=NULL,path=excluded.path,encryption_key=excluded.encryption_key`, item.ImportID, item.WorkspaceID, item.Path, item.EncryptionKey, item.CreatedAt)
	return err
}
func (s *DuckDBStore) UpdateManagedAssetPath(ctx context.Context, importID, path string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE managed_assets SET path=? WHERE import_id=?`, path, importID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `UPDATE import_jobs SET managed_path=?,updated_at=? WHERE id=?`, path, time.Now().UTC(), importID); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *DuckDBStore) DeleteManagedAsset(ctx context.Context, importID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM managed_assets WHERE import_id=?`, importID)
	return err
}

// MarshalImportPlan provides a stable JSON representation for job APIs and tests.
func MarshalImportPlan(plan ImportPlan) json.RawMessage { value, _ := json.Marshal(plan); return value }

type importEventExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func importWarnings(job *ImportJob) []string {
	if job == nil || job.Discovery == nil {
		return nil
	}
	return job.Discovery.Warnings
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func insertImportEvent(ctx context.Context, execer importEventExecer, job *ImportJob, createdAt *time.Time) error {
	now := time.Now().UTC()
	if createdAt != nil {
		now = *createdAt
	}
	warnings, _ := json.Marshal(importWarnings(job))
	_, err := execer.ExecContext(ctx, `INSERT INTO import_job_events
		(id,import_id,workspace_id,status,phase,warnings,error_message,processed_bytes,processed_features,processed_layers,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, uuid.NewString(), job.ID, job.WorkspaceID, job.Status, job.Phase, string(warnings),
		job.ErrorMessage, job.ProcessedBytes, job.ProcessedFeatures, job.ProcessedLayers, now)
	return err
}

func (s *DuckDBStore) ListImportJobEvents(ctx context.Context, importID string, limit int) ([]*ImportJobEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,import_id,workspace_id,status,phase,warnings,error_message,
		processed_bytes,processed_features,processed_layers,created_at FROM import_job_events
		WHERE import_id=? ORDER BY created_at,id LIMIT ?`, importID, limit)
	if err != nil {
		return nil, fmt.Errorf("list import events: %w", err)
	}
	defer rows.Close()
	events := make([]*ImportJobEvent, 0)
	for rows.Next() {
		var event ImportJobEvent
		var status, phase string
		var warningsRaw any
		if err = rows.Scan(&event.ID, &event.ImportID, &event.WorkspaceID, &status, &phase, &warningsRaw,
			&event.ErrorMessage, &event.ProcessedBytes, &event.ProcessedFeatures, &event.ProcessedLayers, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.Status, event.Phase = ImportStatus(status), ImportPhase(phase)
		_ = decodeJSONValue(warningsRaw, &event.Warnings)
		events = append(events, &event)
	}
	return events, rows.Err()
}
