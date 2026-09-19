package mosaiccatalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const jobColumns = `id, workspace_id, service_id, request_json, status, total_granules,
 processed_granules, succeeded_granules, failed_granules, generation, cancel_requested,
 error_message, created_by, created_at, started_at, completed_at, updated_at`

func scanJob(scanner interface{ Scan(...any) error }) (*Job, error) {
	var job Job
	var requestRaw any
	if err := scanner.Scan(&job.ID, &job.WorkspaceID, &job.ServiceID, &requestRaw, &job.Status,
		&job.TotalGranules, &job.Processed, &job.Succeeded, &job.Failed, &job.Generation,
		&job.CancelRequested, &job.ErrorMessage, &job.CreatedBy, &job.CreatedAt,
		&job.StartedAt, &job.CompletedAt, &job.UpdatedAt); err != nil {
		return nil, err
	}
	var data []byte
	switch value := requestRaw.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		data = encoded
	}
	if err := json.Unmarshal(data, &job.Request); err != nil {
		return nil, fmt.Errorf("decode mosaic harvest request: %w", err)
	}
	return &job, nil
}

func (c *catalog) createJob(ctx context.Context, workspaceID, serviceID, createdBy string, request HarvestRequest) (*Job, error) {
	raw, err := marshalRequest(request)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	job := &Job{ID: uuid.NewString(), WorkspaceID: workspaceID, ServiceID: serviceID, Request: request,
		Status: JobQueued, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now}
	_, err = c.db.ExecContext(ctx, `INSERT INTO mosaic_harvest_jobs
	 (id,workspace_id,service_id,request_json,status,created_by,created_at,updated_at)
	 VALUES(?,?,?,?,?,?,?,?)`, job.ID, workspaceID, serviceID, raw, job.Status, createdBy, now, now)
	return job, err
}

func (c *catalog) getJob(ctx context.Context, id string) (*Job, error) {
	job, err := scanJob(c.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM mosaic_harvest_jobs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return job, err
}

func (c *catalog) listJobs(ctx context.Context, workspaceID, serviceID string, limit int) ([]*Job, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query := `SELECT ` + jobColumns + ` FROM mosaic_harvest_jobs WHERE workspace_id=?`
	args := []any{workspaceID}
	if serviceID != "" {
		query += ` AND service_id=?`
		args = append(args, serviceID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, query, args...)
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
		result = append(result, job)
	}
	return result, rows.Err()
}

func (c *catalog) updateJob(ctx context.Context, job *Job) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	job.UpdatedAt = time.Now().UTC()
	_, err := c.db.ExecContext(ctx, `UPDATE mosaic_harvest_jobs SET
	 status=CASE WHEN cancel_requested AND ? <> 'cancelled' THEN 'cancelling' ELSE ? END,
	 total_granules=?,processed_granules=?,succeeded_granules=?,failed_granules=?,generation=?,
	 cancel_requested=(cancel_requested OR ?),error_message=?,started_at=?,completed_at=?,updated_at=?
	 WHERE id=? AND status IN ('queued','running','cancelling')`,
		job.Status, job.Status, job.TotalGranules, job.Processed, job.Succeeded, job.Failed, job.Generation,
		job.CancelRequested, job.ErrorMessage, job.StartedAt, job.CompletedAt, job.UpdatedAt, job.ID)
	return err
}

func (c *catalog) requestCancel(ctx context.Context, id string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	job, err := c.getJob(ctx, id)
	if err != nil {
		return err
	}
	if job.Generation > 0 && (job.Status == JobRunning || job.Status == JobCancelling) {
		active, err := c.activeGeneration(ctx, job.ServiceID)
		if err != nil {
			return err
		}
		if active == job.Generation {
			return errors.New("harvest activation has already committed; cancellation is no longer available")
		}
	}
	result, err := c.db.ExecContext(ctx, `UPDATE mosaic_harvest_jobs SET cancel_requested=true,
	 status=CASE WHEN status='queued' THEN 'cancelled' WHEN status='running' THEN 'cancelling' ELSE status END,
	 completed_at=CASE WHEN status='queued' THEN current_timestamp ELSE completed_at END,updated_at=current_timestamp
	 WHERE id=? AND status IN ('queued','running','cancelling')`, id)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		if _, err := c.getJob(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (c *catalog) resetInterrupted(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, `UPDATE mosaic_harvest_jobs SET status='queued',started_at=NULL,
	 error_message='',updated_at=current_timestamp WHERE status IN ('running','cancelling') AND cancel_requested=false`)
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `UPDATE mosaic_harvest_jobs SET status='cancelled',completed_at=current_timestamp,
	 updated_at=current_timestamp WHERE status IN ('running','cancelling','queued') AND cancel_requested=true`)
	return err
}
