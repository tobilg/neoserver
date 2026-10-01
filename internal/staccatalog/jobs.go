package staccatalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/stacmodel"
)

// MaxPendingImportsPerWorkspace bounds unpublished imports in one workspace.
const MaxPendingImportsPerWorkspace = 8

// StagedImportTTL is how long a fully staged import waits for publication
// before it is cancelled and its staging is discarded.
const StagedImportTTL = 24 * time.Hour

const jobColumns = "id,workspace_id,collection_id,kind,status,revision,processed,error,request,created_at,updated_at"

func (c *Catalog) LatestRefreshJobs(ctx context.Context, ws string) ([]*Job, error) {
	rows, err := c.read.QueryContext(ctx, "SELECT "+jobColumns+" FROM stac_jobs WHERE workspace_id=? AND kind='refresh' QUALIFY row_number() OVER(PARTITION BY collection_id ORDER BY created_at DESC)=1", ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func scanJob(row interface{ Scan(...any) error }) (*Job, error) {
	j := &Job{}
	var raw string
	err := row.Scan(&j.ID, &j.WorkspaceID, &j.CollectionID, &j.Kind, &j.Status, &j.Revision, &j.Processed, &j.Error, &raw, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	j.Request, err = stacmodel.Decode([]byte(raw))
	return j, err
}
func (c *Catalog) GetJob(ctx context.Context, ws, id string) (*Job, error) {
	return scanJob(c.read.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM stac_jobs WHERE workspace_id=? AND id=?", ws, id))
}
func (c *Catalog) Jobs(ctx context.Context, ws string) ([]*Job, error) {
	rows, err := c.read.QueryContext(ctx, "SELECT "+jobColumns+" FROM stac_jobs WHERE workspace_id=? ORDER BY created_at DESC LIMIT 200", ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (c *Catalog) CreateJob(ctx context.Context, ws, collection, kind, status string, request stacmodel.Document) (*Job, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var revision int64
	var binding sql.NullString
	if err = tx.QueryRowContext(ctx, "SELECT revision,binding FROM stac_collections WHERE workspace_id=? AND id=?", ws, collection).Scan(&revision, &binding); err == sql.ErrNoRows {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if kind == "import" && binding.Valid && binding.String != "null" {
		return nil, fmt.Errorf("imports require a standalone Collection")
	}
	if kind == "refresh" {
		j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM stac_jobs WHERE workspace_id=? AND collection_id=? AND kind='refresh' AND status IN ('queued','running') ORDER BY created_at DESC LIMIT 1", ws, collection))
		if err == nil {
			return j, nil
		}
		if err != ErrNotFound {
			return nil, err
		}
	}
	// Refresh jobs are already limited to one active job per Collection. Only
	// imports, which a workspace administrator can leave staged, are capped, and
	// per workspace so one workspace cannot block another.
	if kind == "import" {
		var pending int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM stac_jobs WHERE workspace_id=? AND kind='import' AND status IN ('queued','running','uploading','ready')", ws).Scan(&pending); err != nil {
			return nil, err
		}
		if pending >= MaxPendingImportsPerWorkspace {
			return nil, fmt.Errorf("too many pending STAC imports in this workspace; publish or cancel staged imports")
		}
	}
	id := uuid.NewString()
	if request == nil {
		request = stacmodel.Document{}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO stac_jobs(workspace_id,id,collection_id,kind,status,revision,request) VALUES(?,?,?,?,?,?,?)", ws, id, collection, kind, status, revision, jsonString(request)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return c.GetJob(ctx, ws, id)
}
func (c *Catalog) SetJobStatus(ctx context.Context, ws, id, status, message string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "UPDATE stac_jobs SET status=?,error=?,updated_at=current_timestamp WHERE workspace_id=? AND id=? AND status NOT IN ('succeeded','cancelled')", status, message, ws, id)
	if err != nil {
		return err
	}
	if status == "failed" {
		for _, table := range []string{"stac_items", "stac_local_assets"} {
			if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE workspace_id=? AND generation=? AND EXISTS(SELECT 1 FROM stac_jobs WHERE workspace_id=? AND id=? AND status='failed')", ws, id, ws, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (c *Catalog) ClaimJob(ctx context.Context) (*Job, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM stac_jobs WHERE status='queued' ORDER BY created_at LIMIT 1"))
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE stac_jobs SET status='running',updated_at=current_timestamp WHERE workspace_id=? AND id=?", j.WorkspaceID, j.ID); err != nil {
		return nil, err
	}
	j.Status = "running"
	return j, tx.Commit()
}

// Stage writes bounded batches to an unpublished generation. Duplicate IDs are
// errors, including duplicates in separate batches, rather than silent upserts.
func (c *Catalog) Stage(ctx context.Context, j *Job, documents []stacmodel.Document) error {
	if len(documents) == 0 {
		return nil
	}
	values := make([]string, 0, len(documents))
	args := []any{}
	for index, d := range documents {
		if d.String("collection") != j.CollectionID {
			return fmt.Errorf("record %d: collection does not match target", j.Processed+int64(index)+1)
		}
		if err := stacmodel.Validate(d, "item"); err != nil {
			return fmt.Errorf("record %d: %w", j.Processed+int64(index)+1, err)
		}
		start, end, _ := stacmodel.ItemTime(d)
		var geom any
		var bounds [6]any
		if g := d.Object("geometry"); g != nil {
			b, err := stacmodel.GeometryBounds(g)
			if err != nil {
				return err
			}
			geom = jsonString(g)
			for i, v := range b {
				bounds[i] = v
			}
		}
		values = append(values, "(?,?,?,?,?,?,?,ST_GeomFromGeoJSON(CAST(? AS VARCHAR)),?,?,?,?,?,?)")
		args = append(args, j.WorkspaceID, j.CollectionID, j.ID, d.String("id"), jsonString(d), start, end, geom, bounds[0], bounds[1], bounds[3], bounds[4], bounds[2], bounds[5])
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var count int64
	if err = tx.QueryRowContext(ctx, "SELECT status,processed FROM stac_jobs WHERE workspace_id=? AND id=?", j.WorkspaceID, j.ID).Scan(&status, &count); err != nil {
		return ErrNotFound
	}
	if status != "running" && status != "uploading" {
		return fmt.Errorf("job is no longer accepting records")
	}
	// Refresh staging is bounded by the worker count and the per-job Item limit.
	// Only imports share the staging budget, so staged imports never block
	// source refreshes.
	if j.Kind != "refresh" {
		var staged int64
		if err = tx.QueryRowContext(ctx, "SELECT coalesce(sum(processed),0) FROM stac_jobs WHERE status IN ('running','uploading','ready')").Scan(&staged); err != nil {
			return err
		}
		if staged+int64(len(documents)) > 2*c.maxItems {
			return fmt.Errorf("STAC staging capacity exceeded; publish or cancel pending imports")
		}
	}
	if count+int64(len(documents)) > c.maxItems {
		return fmt.Errorf("STAC item limit exceeded")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO stac_items VALUES "+strings.Join(values, ","), args...); err != nil {
		return fmt.Errorf("unable to stage records (IDs must be unique): %w", err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE stac_jobs SET processed=processed+?,updated_at=current_timestamp WHERE workspace_id=? AND id=?", len(documents), j.WorkspaceID, j.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		j.Processed = count + int64(len(documents))
	}
	return err
}

// Publish swaps complete generations and derived extents in one transaction.
// A changed/deleted Collection or cancelled job cannot be resurrected.
func (c *Catalog) Publish(ctx context.Context, ws, id string, upsert bool, document stacmodel.Document) error {
	return c.publish(ctx, ws, id, upsert, document, "")
}

// PublishRefresh publishes a refresh generation together with the fingerprint
// of the source state it was built from.
func (c *Catalog) PublishRefresh(ctx context.Context, ws, id string, document stacmodel.Document, fingerprint string) error {
	return c.publish(ctx, ws, id, false, document, fingerprint)
}

func (c *Catalog) publish(ctx context.Context, ws, id string, upsert bool, document stacmodel.Document, fingerprint string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM stac_jobs WHERE workspace_id=? AND id=?", ws, id))
	if err != nil {
		return err
	}
	if j.Status == "succeeded" {
		return nil
	}
	if j.Status != "ready" && j.Status != "running" {
		return fmt.Errorf("job is not ready to publish")
	}
	var raw, generation string
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT document,generation,revision FROM stac_collections WHERE workspace_id=? AND id=?", ws, j.CollectionID).Scan(&raw, &generation, &revision); err == sql.ErrNoRows {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if revision != j.Revision {
		return ErrConflict
	}
	if j.Kind == "import" {
		if !upsert {
			var duplicates int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM stac_items staged JOIN stac_items old ON old.workspace_id=staged.workspace_id AND old.collection_id=staged.collection_id AND old.id=staged.id WHERE staged.workspace_id=? AND staged.collection_id=? AND staged.generation=? AND old.generation=?`, ws, j.CollectionID, id, generation).Scan(&duplicates); err != nil {
				return err
			}
			if duplicates > 0 {
				return fmt.Errorf("%d existing IDs conflict; explicitly select upsert", duplicates)
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO stac_items SELECT workspace_id,collection_id,?,id,document,start_time,end_time,geom,min_x,min_y,max_x,max_y,min_z,max_z FROM stac_items old WHERE workspace_id=? AND collection_id=? AND generation=? AND NOT EXISTS(SELECT 1 FROM stac_items staged WHERE staged.workspace_id=old.workspace_id AND staged.collection_id=old.collection_id AND staged.generation=? AND staged.id=old.id)`, id, ws, j.CollectionID, generation, id)
		if err != nil {
			return err
		}
	}
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM stac_items i JOIN stac_collections c ON c.workspace_id=i.workspace_id AND c.id=i.collection_id WHERE (i.workspace_id=? AND i.collection_id=? AND i.generation=?) OR ((i.workspace_id<>? OR i.collection_id<>?) AND i.generation=c.generation)`, ws, j.CollectionID, id, ws, j.CollectionID).Scan(&total); err != nil {
		return err
	}
	if total > c.maxItems {
		return fmt.Errorf("server STAC item limit exceeded")
	}
	if document == nil {
		document, err = stacmodel.Decode([]byte(raw))
		if err != nil {
			return err
		}
	}
	count, err := updateExtent(ctx, tx, ws, j.CollectionID, id, document)
	if err != nil {
		return err
	}
	if err = stacmodel.Validate(document, "collection"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE stac_collections SET generation=?,document=?,item_count=?,revision=revision+1,updated_at=current_timestamp WHERE workspace_id=? AND id=?", id, jsonString(document), count, ws, j.CollectionID); err != nil {
		return err
	}
	if j.Kind == "refresh" {
		if _, err = tx.ExecContext(ctx, "UPDATE stac_collections SET source_fingerprint=? WHERE workspace_id=? AND id=?", fingerprint, ws, j.CollectionID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE stac_jobs SET status='succeeded',updated_at=current_timestamp WHERE workspace_id=? AND id=?", ws, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM stac_items WHERE workspace_id=? AND collection_id=? AND generation=?", ws, j.CollectionID, generation); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM stac_local_assets WHERE workspace_id=? AND collection_id=? AND generation=? AND generation<>''", ws, j.CollectionID, generation); err != nil {
		return err
	}
	return tx.Commit()
}
func updateExtent(ctx context.Context, tx *sql.Tx, ws, collection, generation string, d stacmodel.Document) (int64, error) {
	var count int64
	var b [4]sql.NullFloat64
	var start, end sql.NullString
	err := tx.QueryRowContext(ctx, "SELECT count(*),min(min_x),min(min_y),max(max_x),max(max_y),min(start_time),max(end_time) FROM stac_items WHERE workspace_id=? AND collection_id=? AND generation=?", ws, collection, generation).Scan(&count, &b[0], &b[1], &b[2], &b[3], &start, &end)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		box := []float64{-180, -90, 180, 90}
		if b[0].Valid {
			for i, v := range b {
				box[i] = v.Float64
			}
		}
		var lo, hi any
		if start.Valid {
			lo = start.String
			hi = end.String
		}
		d["extent"] = stacmodel.Document{"spatial": stacmodel.Document{"bbox": [][]float64{box}}, "temporal": stacmodel.Document{"interval": [][]any{{lo, hi}}}}
	}
	return count, nil
}

func (c *Catalog) PutItem(ctx context.Context, ws, collection string, d stacmodel.Document, upsert bool) error {
	col, err := c.GetCollection(ctx, ws, collection)
	if err != nil {
		return err
	}
	if col.Binding != nil {
		if !upsert {
			return fmt.Errorf("linked Items are created by their source")
		}
		return c.overrideItem(ctx, ws, collection, d)
	}
	j, err := c.CreateJob(ctx, ws, collection, "import", "uploading", nil)
	if err != nil {
		return err
	}
	if err = c.Stage(ctx, j, []stacmodel.Document{d}); err != nil {
		_ = c.SetJobStatus(ctx, ws, j.ID, "failed", err.Error())
		return err
	}
	if err = c.SetJobStatus(ctx, ws, j.ID, "ready", ""); err != nil {
		return err
	}
	return c.Publish(ctx, ws, j.ID, upsert, nil)
}
func (c *Catalog) DeleteItem(ctx context.Context, ws, collection, id string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var generation, raw string
	var binding sql.NullString
	if err = tx.QueryRowContext(ctx, "SELECT generation,document,binding FROM stac_collections WHERE workspace_id=? AND id=?", ws, collection).Scan(&generation, &raw, &binding); err == sql.ErrNoRows {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if binding.Valid && binding.String != "null" {
		return fmt.Errorf("linked Items are owned by their source")
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM stac_items WHERE workspace_id=? AND collection_id=? AND generation=? AND id=?", ws, collection, generation, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	doc, err := stacmodel.Decode([]byte(raw))
	if err != nil {
		return err
	}
	count, err := updateExtent(ctx, tx, ws, collection, generation, doc)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE stac_collections SET document=?,item_count=?,revision=revision+1,updated_at=current_timestamp WHERE workspace_id=? AND id=?", jsonString(doc), count, ws, collection)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM stac_local_assets WHERE workspace_id=? AND collection_id=? AND item_id=?", ws, collection, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Prune expires staged imports that were never published, removes abandoned
// staging, and removes old terminal history while retaining the latest job for
// each Collection, so refresh scheduling survives restarts.
func (c *Catalog) Prune(ctx context.Context) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE stac_jobs SET status='cancelled',error='Staged import expired before publication',updated_at=current_timestamp WHERE kind='import' AND status='ready' AND updated_at < current_timestamp - to_seconds(?)", int64(StagedImportTTL/time.Second)); err != nil {
		return err
	}
	for _, table := range []string{"stac_items", "stac_local_assets"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" a WHERE a.generation<>'' AND EXISTS(SELECT 1 FROM stac_jobs j WHERE j.workspace_id=a.workspace_id AND j.id=a.generation AND j.status IN ('failed','cancelled'))"); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM stac_jobs WHERE status IN ('succeeded','failed','cancelled') AND updated_at < current_timestamp - INTERVAL 30 DAY AND (workspace_id,id) NOT IN (SELECT workspace_id,id FROM stac_jobs QUALIFY row_number() OVER(PARTITION BY workspace_id,collection_id,kind ORDER BY created_at DESC)=1)`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
