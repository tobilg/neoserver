package tilecache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var ErrJobNotFound = errors.New("tile cache job not found")

type Operation string

const (
	OperationSeed     Operation = "seed"
	OperationReseed   Operation = "reseed"
	OperationTruncate Operation = "truncate"
)

type JobStatus string

const (
	JobQueued     JobStatus = "queued"
	JobRunning    JobStatus = "running"
	JobCancelling JobStatus = "cancelling"
	JobCancelled  JobStatus = "cancelled"
	JobSucceeded  JobStatus = "succeeded"
	JobFailed     JobStatus = "failed"
)

type Bounds struct {
	BBox [4]float64 `json:"bbox"`
	CRS  string     `json:"crs"`
}

// JobRequest is persisted after resource identifiers have been resolved.
// Public management requests may initially provide Resource only.
type JobRequest struct {
	Operation     Operation `json:"operation"`
	Resource      string    `json:"resource,omitempty"`
	ResourceID    string    `json:"resource_id,omitempty"`
	ResourceKind  string    `json:"resource_kind,omitempty"`
	AllResources  bool      `json:"all_resources,omitempty"`
	TileType      string    `json:"tile_type,omitempty"`
	TileMatrixSet string    `json:"tile_matrix_set,omitempty"`
	Format        string    `json:"format,omitempty"`
	Style         string    `json:"style,omitempty"`
	MinZoom       *int      `json:"min_zoom,omitempty"`
	MaxZoom       *int      `json:"max_zoom,omitempty"`
	Bounds        *Bounds   `json:"bounds,omitempty"`
	Generation    int64     `json:"generation,omitempty"`
}

type Job struct {
	ID              string     `json:"id"`
	WorkspaceID     string     `json:"workspace_id"`
	Request         JobRequest `json:"request"`
	Status          JobStatus  `json:"status"`
	TotalTiles      int64      `json:"total_tiles"`
	ProcessedTiles  int64      `json:"processed_tiles"`
	SucceededTiles  int64      `json:"succeeded_tiles"`
	SkippedTiles    int64      `json:"skipped_tiles"`
	FailedTiles     int64      `json:"failed_tiles"`
	BytesWritten    int64      `json:"bytes_written"`
	BytesDeleted    int64      `json:"bytes_deleted"`
	CancelRequested bool       `json:"cancel_requested"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	CreatedBy       string     `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CreateJobInput struct {
	WorkspaceID string
	Request     JobRequest
	TotalTiles  int64
	CreatedBy   string
}

// JobUpdate is a full progress snapshot. Pointer fields allow status
// transitions without accidentally clearing timestamps or error text.
type JobUpdate struct {
	Status          *JobStatus
	TotalTiles      *int64
	ProcessedTiles  *int64
	SucceededTiles  *int64
	SkippedTiles    *int64
	FailedTiles     *int64
	BytesWritten    *int64
	BytesDeleted    *int64
	CancelRequested *bool
	ErrorMessage    *string
	StartedAt       *time.Time
	CompletedAt     *time.Time
}

type JobChunk struct {
	ID         string    `json:"id"`
	JobID      string    `json:"job_id"`
	Zoom       int       `json:"zoom"`
	MinCol     int       `json:"min_col"`
	MaxCol     int       `json:"max_col"`
	MinRow     int       `json:"min_row"`
	MaxRow     int       `json:"max_row"`
	NextOffset int64     `json:"next_offset"`
	Status     string    `json:"status"`
	Attempts   int       `json:"attempts"`
	LastError  string    `json:"last_error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// JobStore persists durable cache jobs and their resumable chunks in the
// standalone tile-cache database.
type JobStore interface {
	ClaimNextTileCacheJob(ctx context.Context) (*Job, error)
	CreateTileCacheJob(ctx context.Context, input CreateJobInput) (*Job, error)
	GetTileCacheJob(ctx context.Context, id string) (*Job, error)
	ListTileCacheJobs(ctx context.Context, workspaceID string, limit int) ([]*Job, error)
	UpdateTileCacheJob(ctx context.Context, id string, update JobUpdate) (*Job, error)
	RequestTileCacheJobCancel(ctx context.Context, id string) error
	ResetInterruptedTileCacheJobs(ctx context.Context) error
	ReplaceTileCacheJobChunks(ctx context.Context, jobID string, chunks []JobChunk) error
	ListTileCacheJobChunks(ctx context.Context, jobID string) ([]JobChunk, error)
	UpdateTileCacheJobChunk(ctx context.Context, chunk JobChunk) error
}

// Claim work independently from bounded UI history, under the metadata writer
// lock and a conditional SQL update so a job cannot be returned twice.
func (i *metadataIndex) ClaimNextTileCacheJob(ctx context.Context) (*Job, error) {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	now := time.Now().UTC()
	job, err := scanJob(i.db.QueryRowContext(ctx, `UPDATE tile_cache_jobs SET status='running',started_at=?,updated_at=?
 WHERE id=(SELECT id FROM tile_cache_jobs WHERE status='queued' AND cancel_requested=false ORDER BY created_at,id LIMIT 1)
 AND status='queued' AND cancel_requested=false RETURNING `+jobColumns, now, now))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

const jobColumns = `id, workspace_id, request_json, status, total_tiles, processed_tiles,
 succeeded_tiles, skipped_tiles, failed_tiles, bytes_written, bytes_deleted, cancel_requested,
 error_message, created_by, created_at, started_at, completed_at, updated_at`

func scanJob(scanner interface{ Scan(...any) error }) (*Job, error) {
	var job Job
	var requestRaw any
	if err := scanner.Scan(&job.ID, &job.WorkspaceID, &requestRaw, &job.Status, &job.TotalTiles,
		&job.ProcessedTiles, &job.SucceededTiles, &job.SkippedTiles, &job.FailedTiles,
		&job.BytesWritten, &job.BytesDeleted, &job.CancelRequested, &job.ErrorMessage,
		&job.CreatedBy, &job.CreatedAt, &job.StartedAt, &job.CompletedAt, &job.UpdatedAt); err != nil {
		return nil, err
	}
	var data []byte
	switch value := requestRaw.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		encoded, err := json.Marshal(requestRaw)
		if err != nil {
			return nil, fmt.Errorf("encode tile cache job request type %T: %w", requestRaw, err)
		}
		data = encoded
	}
	if err := json.Unmarshal(data, &job.Request); err != nil {
		return nil, fmt.Errorf("decode tile cache job request: %w", err)
	}
	return &job, nil
}

func (i *metadataIndex) CreateTileCacheJob(ctx context.Context, input CreateJobInput) (*Job, error) {
	requestJSON, err := json.Marshal(input.Request)
	if err != nil {
		return nil, fmt.Errorf("marshal tile cache job request: %w", err)
	}
	now := time.Now().UTC()
	job := &Job{ID: uuid.NewString(), WorkspaceID: input.WorkspaceID, Request: input.Request,
		Status: JobQueued, TotalTiles: input.TotalTiles, CreatedBy: input.CreatedBy, CreatedAt: now, UpdatedAt: now}
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	_, err = i.db.ExecContext(ctx, `INSERT INTO tile_cache_jobs
		(id, workspace_id, request_json, status, total_tiles, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, job.ID, job.WorkspaceID, string(requestJSON), job.Status,
		job.TotalTiles, job.CreatedBy, now, now)
	if err != nil {
		return nil, fmt.Errorf("create tile cache job: %w", err)
	}
	return job, nil
}

func (i *metadataIndex) GetTileCacheJob(ctx context.Context, id string) (*Job, error) {
	return i.getTileCacheJob(ctx, id)
}

func (i *metadataIndex) getTileCacheJob(ctx context.Context, id string) (*Job, error) {
	job, err := scanJob(i.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM tile_cache_jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tile cache job: %w", err)
	}
	return job, nil
}

func (i *metadataIndex) ListTileCacheJobs(ctx context.Context, workspaceID string, limit int) ([]*Job, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query := `SELECT ` + jobColumns + ` FROM tile_cache_jobs`
	args := []any{}
	if workspaceID != "" {
		query += ` WHERE workspace_id = ?`
		args = append(args, workspaceID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := i.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tile cache jobs: %w", err)
	}
	defer rows.Close()
	var jobs []*Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tile cache job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (i *metadataIndex) UpdateTileCacheJob(ctx context.Context, id string, update JobUpdate) (*Job, error) {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	job, err := i.getTileCacheJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if update.Status != nil {
		job.Status = *update.Status
	}
	if update.TotalTiles != nil {
		job.TotalTiles = *update.TotalTiles
	}
	if update.ProcessedTiles != nil {
		job.ProcessedTiles = *update.ProcessedTiles
	}
	if update.SucceededTiles != nil {
		job.SucceededTiles = *update.SucceededTiles
	}
	if update.SkippedTiles != nil {
		job.SkippedTiles = *update.SkippedTiles
	}
	if update.FailedTiles != nil {
		job.FailedTiles = *update.FailedTiles
	}
	if update.BytesWritten != nil {
		job.BytesWritten = *update.BytesWritten
	}
	if update.BytesDeleted != nil {
		job.BytesDeleted = *update.BytesDeleted
	}
	if update.CancelRequested != nil {
		job.CancelRequested = *update.CancelRequested
	}
	if update.ErrorMessage != nil {
		job.ErrorMessage = *update.ErrorMessage
	}
	if update.StartedAt != nil {
		value := update.StartedAt.UTC()
		job.StartedAt = &value
	}
	if update.CompletedAt != nil {
		value := update.CompletedAt.UTC()
		job.CompletedAt = &value
	}
	job.UpdatedAt = time.Now().UTC()
	_, err = i.db.ExecContext(ctx, `UPDATE tile_cache_jobs SET status=?, total_tiles=?, processed_tiles=?,
		succeeded_tiles=?, skipped_tiles=?, failed_tiles=?, bytes_written=?, bytes_deleted=?,
		cancel_requested=?, error_message=?, started_at=?, completed_at=?, updated_at=? WHERE id=?`,
		job.Status, job.TotalTiles, job.ProcessedTiles, job.SucceededTiles, job.SkippedTiles,
		job.FailedTiles, job.BytesWritten, job.BytesDeleted, job.CancelRequested, job.ErrorMessage,
		job.StartedAt, job.CompletedAt, job.UpdatedAt, id)
	if err != nil {
		return nil, fmt.Errorf("update tile cache job: %w", err)
	}
	return job, nil
}

func (i *metadataIndex) RequestTileCacheJobCancel(ctx context.Context, id string) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	now := time.Now().UTC()
	result, err := i.db.ExecContext(ctx, `UPDATE tile_cache_jobs SET cancel_requested=true,
		status=CASE WHEN status='queued' THEN 'cancelled' ELSE 'cancelling' END,
		completed_at=CASE WHEN status='queued' THEN ? ELSE completed_at END, updated_at=?
		WHERE id=? AND status IN ('queued','running','cancelling')`, now, now, id)
	if err != nil {
		return fmt.Errorf("cancel tile cache job: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		job, err := i.getTileCacheJob(ctx, id)
		if err != nil {
			return err
		}
		return fmt.Errorf("tile cache job is already %s", job.Status)
	}
	return nil
}

func (i *metadataIndex) ResetInterruptedTileCacheJobs(ctx context.Context) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE tile_cache_job_chunks SET status='queued', updated_at=? WHERE status='running'`, now); err != nil {
		return fmt.Errorf("reset tile cache chunks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tile_cache_jobs SET status='queued', updated_at=? WHERE status='running'`, now); err != nil {
		return fmt.Errorf("reset tile cache jobs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tile_cache_jobs SET status='cancelled', completed_at=?, updated_at=? WHERE status='cancelling'`, now, now); err != nil {
		return fmt.Errorf("finish cancelled tile cache jobs: %w", err)
	}
	return tx.Commit()
}

func (i *metadataIndex) ReplaceTileCacheJobChunks(ctx context.Context, jobID string, chunks []JobChunk) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM tile_cache_job_chunks WHERE job_id=?", jobID); err != nil {
		return err
	}
	now := time.Now().UTC()
	for index := range chunks {
		chunk := &chunks[index]
		if chunk.ID == "" {
			chunk.ID = uuid.NewString()
		}
		if chunk.Status == "" {
			chunk.Status = "queued"
		}
		chunk.JobID, chunk.UpdatedAt = jobID, now
		if _, err := tx.ExecContext(ctx, `INSERT INTO tile_cache_job_chunks
			(id,job_id,zoom,min_col,max_col,min_row,max_row,next_offset,status,attempts,last_error,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, chunk.ID, jobID, chunk.Zoom, chunk.MinCol, chunk.MaxCol,
			chunk.MinRow, chunk.MaxRow, chunk.NextOffset, chunk.Status, chunk.Attempts, chunk.LastError, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (i *metadataIndex) ListTileCacheJobChunks(ctx context.Context, jobID string) ([]JobChunk, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT id,job_id,zoom,min_col,max_col,min_row,max_row,next_offset,
		status,attempts,last_error,updated_at FROM tile_cache_job_chunks WHERE job_id=? ORDER BY zoom,min_row,min_col`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []JobChunk
	for rows.Next() {
		var chunk JobChunk
		if err := rows.Scan(&chunk.ID, &chunk.JobID, &chunk.Zoom, &chunk.MinCol, &chunk.MaxCol,
			&chunk.MinRow, &chunk.MaxRow, &chunk.NextOffset, &chunk.Status, &chunk.Attempts,
			&chunk.LastError, &chunk.UpdatedAt); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
}

func (i *metadataIndex) UpdateTileCacheJobChunk(ctx context.Context, chunk JobChunk) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	_, err := i.db.ExecContext(ctx, `UPDATE tile_cache_job_chunks SET next_offset=?,status=?,attempts=?,last_error=?,updated_at=? WHERE id=?`,
		chunk.NextOffset, chunk.Status, chunk.Attempts, chunk.LastError, time.Now().UTC(), chunk.ID)
	return err
}

func (i *metadataIndex) lifecycleJobs(ctx context.Context, workspaceID string, resourceIDs map[string]bool) ([]*Job, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM tile_cache_jobs WHERE workspace_id=? ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		if len(resourceIDs) == 0 || job.Request.AllResources || resourceIDs[job.Request.ResourceID] {
			result = append(result, job)
		}
	}
	return result, rows.Err()
}

func (i *metadataIndex) purgeLifecycleJobs(ctx context.Context, workspaceID string, resourceIDs map[string]bool) (int64, error) {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	jobs, err := i.lifecycleJobs(ctx, workspaceID, resourceIDs)
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, job := range jobs {
		if job.Status == JobQueued || job.Status == JobRunning || job.Status == JobCancelling {
			return 0, fmt.Errorf("tile cache job %s is still %s", job.ID, job.Status)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tile_cache_job_chunks WHERE job_id=?`, job.ID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tile_cache_jobs WHERE id=?`, job.ID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(jobs)), nil
}

func (i *metadataIndex) cancelLifecycleJobs(ctx context.Context, workspaceID string, resourceIDs map[string]bool) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	jobs, err := i.lifecycleJobs(ctx, workspaceID, resourceIDs)
	if err != nil {
		return err
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	for _, job := range jobs {
		if job.Status != JobQueued && job.Status != JobRunning && job.Status != JobCancelling {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tile_cache_jobs SET cancel_requested=true,
			status=CASE WHEN status='queued' THEN 'cancelled' ELSE 'cancelling' END,
			completed_at=CASE WHEN status='queued' THEN ? ELSE completed_at END,updated_at=? WHERE id=?`, now, now, job.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (i *metadataIndex) activeLifecycleJobs(ctx context.Context, workspaceID string, resourceIDs map[string]bool) (int64, error) {
	jobs, err := i.lifecycleJobs(ctx, workspaceID, resourceIDs)
	if err != nil {
		return 0, err
	}
	var active int64
	for _, job := range jobs {
		if job.Status == JobQueued || job.Status == JobRunning || job.Status == JobCancelling {
			active++
		}
	}
	return active, nil
}

func (i *metadataIndex) purgeLifecycleUsage(ctx context.Context, workspaceID string, resourceIDs []string) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	if len(resourceIDs) == 0 {
		_, err := i.db.ExecContext(ctx, `DELETE FROM cache_usage WHERE
			(scope_type='workspace' AND scope_id=?) OR
			(scope_type='resource' AND starts_with(scope_id,?))`, workspaceID, workspaceID+":")
		return err
	}
	for _, resourceID := range resourceIDs {
		if _, err := i.db.ExecContext(ctx, `DELETE FROM cache_usage WHERE scope_type='resource' AND scope_id=?`, workspaceID+":"+resourceID); err != nil {
			return err
		}
	}
	return nil
}
