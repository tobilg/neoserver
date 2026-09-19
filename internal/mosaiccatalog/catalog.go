package mosaiccatalog

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/store"
)

const schemaVersion = 1

type catalog struct {
	db      *sql.DB
	writeMu sync.Mutex
}

func openCatalog(path, encryptionKey string) (*catalog, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, err
	}
	attach := fmt.Sprintf("ATTACH IF NOT EXISTS '%s' AS mosaic_catalog", escapeSQLLiteral(abs))
	if encryptionKey != "" {
		attach += fmt.Sprintf(" (ENCRYPTION_KEY '%s')", escapeSQLLiteral(encryptionKey))
	}
	connector, err := duckdb.NewConnector("", func(execer driver.ExecerContext) error {
		if _, err := execer.ExecContext(context.Background(), attach, nil); err != nil {
			return err
		}
		_, err := execer.ExecContext(context.Background(), "USE mosaic_catalog", nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	const schema = `
CREATE TABLE IF NOT EXISTS mosaic_schema (
 version INTEGER PRIMARY KEY,
 applied_at TIMESTAMP DEFAULT current_timestamp
);
CREATE TABLE IF NOT EXISTS mosaic_services (
 service_id VARCHAR PRIMARY KEY,
 workspace_id VARCHAR NOT NULL,
 active_generation BIGINT NOT NULL DEFAULT 0,
 updated_at TIMESTAMP NOT NULL DEFAULT current_timestamp
);
CREATE TABLE IF NOT EXISTS mosaic_granules (
 id VARCHAR PRIMARY KEY,
 workspace_id VARCHAR NOT NULL,
 service_id VARCHAR NOT NULL,
 generation BIGINT NOT NULL,
 source_uri VARCHAR NOT NULL,
 crs VARCHAR NOT NULL,
 srid INTEGER NOT NULL,
 min_x DOUBLE NOT NULL,
 min_y DOUBLE NOT NULL,
 max_x DOUBLE NOT NULL,
 max_y DOUBLE NOT NULL,
 width INTEGER NOT NULL,
 height INTEGER NOT NULL,
 band_count INTEGER NOT NULL,
 data_type VARCHAR NOT NULL,
 resolution_x DOUBLE NOT NULL,
 resolution_y DOUBLE NOT NULL,
 time_value VARCHAR NOT NULL DEFAULT '',
 elevation DOUBLE,
 priority INTEGER NOT NULL DEFAULT 0,
 size_bytes BIGINT NOT NULL DEFAULT 0,
 modified_at TIMESTAMP,
 footprint_wkb BLOB,
 created_at TIMESTAMP NOT NULL DEFAULT current_timestamp,
 updated_at TIMESTAMP NOT NULL DEFAULT current_timestamp,
 UNIQUE(service_id, generation, source_uri)
);
CREATE INDEX IF NOT EXISTS mosaic_granules_service_generation ON mosaic_granules(service_id, generation, priority, source_uri);
CREATE INDEX IF NOT EXISTS mosaic_granules_dimensions ON mosaic_granules(service_id, generation, time_value, elevation);
CREATE TABLE IF NOT EXISTS mosaic_harvest_jobs (
 id VARCHAR PRIMARY KEY,
 workspace_id VARCHAR NOT NULL,
 service_id VARCHAR NOT NULL,
 request_json JSON NOT NULL,
 status VARCHAR NOT NULL,
 total_granules BIGINT NOT NULL DEFAULT 0,
 processed_granules BIGINT NOT NULL DEFAULT 0,
 succeeded_granules BIGINT NOT NULL DEFAULT 0,
 failed_granules BIGINT NOT NULL DEFAULT 0,
 generation BIGINT NOT NULL DEFAULT 0,
 cancel_requested BOOLEAN NOT NULL DEFAULT false,
 error_message VARCHAR NOT NULL DEFAULT '',
 created_by VARCHAR NOT NULL DEFAULT '',
 created_at TIMESTAMP NOT NULL DEFAULT current_timestamp,
 started_at TIMESTAMP,
 completed_at TIMESTAMP,
 updated_at TIMESTAMP NOT NULL DEFAULT current_timestamp
);
CREATE INDEX IF NOT EXISTS mosaic_jobs_service_created ON mosaic_harvest_jobs(service_id, created_at);
CREATE INDEX IF NOT EXISTS mosaic_jobs_status_created ON mosaic_harvest_jobs(status, created_at);`
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		// The connector's ATTACH runs lazily on the first connection, so a
		// lock conflict with another live process surfaces here.
		return nil, store.WrapAttachError(err, abs)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		db.Close()
		return nil, store.WrapAttachError(fmt.Errorf("create mosaic catalog: %w", err), abs)
	}
	var version sql.NullInt64
	if err := tx.QueryRow("SELECT max(version) FROM mosaic_schema").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version.Valid && version.Int64 != schemaVersion {
		db.Close()
		return nil, fmt.Errorf("mosaic catalog schema version %d is unsupported", version.Int64)
	}
	if !version.Valid {
		if _, err := tx.Exec("INSERT INTO mosaic_schema(version) VALUES (?)", schemaVersion); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return &catalog{db: db}, nil
}

func escapeSQLLiteral(value string) string { return strings.ReplaceAll(value, "'", "''") }

const granuleColumns = `id, workspace_id, service_id, generation, source_uri, crs, srid,
 min_x, min_y, max_x, max_y, width, height, band_count, data_type, resolution_x, resolution_y,
 time_value, elevation, priority, size_bytes, modified_at, footprint_wkb, created_at, updated_at`

func scanGranule(scanner interface{ Scan(...any) error }) (*Granule, error) {
	var item Granule
	var elevation sql.NullFloat64
	var modified sql.NullTime
	if err := scanner.Scan(&item.ID, &item.WorkspaceID, &item.ServiceID, &item.Generation, &item.SourceURI,
		&item.CRS, &item.SRID, &item.BBox[0], &item.BBox[1], &item.BBox[2], &item.BBox[3],
		&item.Width, &item.Height, &item.BandCount, &item.DataType, &item.ResolutionX, &item.ResolutionY,
		&item.Time, &elevation, &item.Priority, &item.SizeBytes, &modified, &item.FootprintWKB,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if elevation.Valid {
		item.Elevation = &elevation.Float64
	}
	if modified.Valid {
		item.ModifiedAt = &modified.Time
	}
	item.FootprintGeoJSON = footprintGeoJSON(item.BBox)
	return &item, nil
}

func (c *catalog) activeGeneration(ctx context.Context, serviceID string) (int64, error) {
	var generation int64
	err := c.db.QueryRowContext(ctx, "SELECT active_generation FROM mosaic_services WHERE service_id=?", serviceID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return generation, err
}

func (c *catalog) listGranules(ctx context.Context, workspaceID, serviceID string, filter GranuleFilter) ([]*Granule, error) {
	generation, err := c.activeGeneration(ctx, serviceID)
	if err != nil || generation == 0 {
		return nil, err
	}
	query := `SELECT ` + granuleColumns + ` FROM mosaic_granules WHERE workspace_id=? AND service_id=? AND generation=?`
	args := []any{workspaceID, serviceID, generation}
	if filter.BBox != nil {
		query += ` AND max_x>=? AND min_x<=? AND max_y>=? AND min_y<=?`
		args = append(args, filter.BBox[0], filter.BBox[2], filter.BBox[1], filter.BBox[3])
	}
	if filter.Time != "" {
		query += ` AND time_value=?`
		args = append(args, filter.Time)
	}
	if filter.Elevation != nil {
		query += ` AND elevation=?`
		args = append(args, *filter.Elevation)
	}
	limit := filter.Limit
	if limit <= 0 || limit > 1_000_000 {
		limit = 1000
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	query += ` ORDER BY priority, source_uri LIMIT ? OFFSET ?`
	args = append(args, limit, filter.Offset)
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Granule
	for rows.Next() {
		item, err := scanGranule(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (c *catalog) getGranule(ctx context.Context, workspaceID, serviceID, id string) (*Granule, error) {
	generation, err := c.activeGeneration(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	item, err := scanGranule(c.db.QueryRowContext(ctx, `SELECT `+granuleColumns+` FROM mosaic_granules WHERE id=? AND workspace_id=? AND service_id=? AND generation=?`, id, workspaceID, serviceID, generation))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (c *catalog) beginGeneration(ctx context.Context, workspaceID, serviceID string, preserve bool) (int64, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var active int64
	err = tx.QueryRowContext(ctx, "SELECT active_generation FROM mosaic_services WHERE service_id=?", serviceID).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		active = 0
		if _, err := tx.ExecContext(ctx, `INSERT INTO mosaic_services(service_id,workspace_id,active_generation) VALUES(?,?,0)`, serviceID, workspaceID); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	next := active + 1
	// An interrupted job can reuse active+1. Clear its partial rows before a
	// retry so a synchronize harvest cannot retain stale candidates.
	if _, err := tx.ExecContext(ctx, `DELETE FROM mosaic_granules WHERE service_id=? AND generation=?`, serviceID, next); err != nil {
		return 0, err
	}
	if preserve && active > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO mosaic_granules
			(id,workspace_id,service_id,generation,source_uri,crs,srid,min_x,min_y,max_x,max_y,width,height,band_count,data_type,resolution_x,resolution_y,time_value,elevation,priority,size_bytes,modified_at,footprint_wkb,created_at,updated_at)
			SELECT uuid(),workspace_id,service_id,?,source_uri,crs,srid,min_x,min_y,max_x,max_y,width,height,band_count,data_type,resolution_x,resolution_y,time_value,elevation,priority,size_bytes,modified_at,footprint_wkb,created_at,current_timestamp
			FROM mosaic_granules WHERE service_id=? AND generation=?`, next, serviceID, active)
		if err != nil {
			return 0, err
		}
	}
	return next, tx.Commit()
}

func (c *catalog) upsertGranule(ctx context.Context, item *Granule) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	item.UpdatedAt = now
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO mosaic_granules (`+granuleColumns+`)
	 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	 ON CONFLICT(service_id,generation,source_uri) DO UPDATE SET crs=excluded.crs,srid=excluded.srid,
	 min_x=excluded.min_x,min_y=excluded.min_y,max_x=excluded.max_x,max_y=excluded.max_y,width=excluded.width,
	 height=excluded.height,band_count=excluded.band_count,data_type=excluded.data_type,resolution_x=excluded.resolution_x,
	 resolution_y=excluded.resolution_y,time_value=excluded.time_value,elevation=excluded.elevation,priority=excluded.priority,
	 size_bytes=excluded.size_bytes,modified_at=excluded.modified_at,footprint_wkb=excluded.footprint_wkb,updated_at=excluded.updated_at`,
		item.ID, item.WorkspaceID, item.ServiceID, item.Generation, item.SourceURI, item.CRS, item.SRID,
		item.BBox[0], item.BBox[1], item.BBox[2], item.BBox[3], item.Width, item.Height, item.BandCount,
		item.DataType, item.ResolutionX, item.ResolutionY, item.Time, item.Elevation, item.Priority,
		item.SizeBytes, item.ModifiedAt, item.FootprintWKB, item.CreatedAt, item.UpdatedAt)
	return err
}

func (c *catalog) activateGeneration(ctx context.Context, workspaceID, serviceID string, generation int64) (int64, error) {
	return c.activateJobGeneration(ctx, workspaceID, serviceID, generation, "")
}

var errHarvestCancelled = errors.New("harvest cancelled before activation")

// Cancellation and generation activation share the catalog writer gate. A
// successful activation commit is the point after which cancellation is refused.
func (c *catalog) activateJobGeneration(ctx context.Context, workspaceID, serviceID string, generation int64, jobID string) (int64, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if jobID != "" {
		job, err := c.getJob(ctx, jobID)
		if err != nil {
			return 0, err
		}
		if job.CancelRequested || job.Status != JobRunning {
			return 0, errHarvestCancelled
		}
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var previous int64
	if err := tx.QueryRowContext(ctx, `SELECT active_generation FROM mosaic_services WHERE workspace_id=? AND service_id=?`, workspaceID, serviceID).Scan(&previous); err != nil {
		return 0, err
	}
	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM mosaic_granules WHERE workspace_id=? AND service_id=? AND generation=?`, workspaceID, serviceID, generation).Scan(&count); err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, errors.New("mosaic generation contains no granules")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mosaic_services SET workspace_id=?,active_generation=?,updated_at=current_timestamp WHERE service_id=?`, workspaceID, generation, serviceID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return previous, nil
}

func (c *catalog) restoreGeneration(ctx context.Context, serviceID string, failedGeneration, previousGeneration int64) error {
	result, err := c.db.ExecContext(ctx, `UPDATE mosaic_services SET active_generation=?,updated_at=current_timestamp WHERE service_id=? AND active_generation=?`, previousGeneration, serviceID, failedGeneration)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return errors.New("mosaic generation changed while runtime refresh was in progress")
	}
	return nil
}

func (c *catalog) pruneInactiveGenerations(ctx context.Context, serviceID string, activeGeneration int64) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM mosaic_granules WHERE service_id=? AND generation<>?`, serviceID, activeGeneration)
	return err
}

func (c *catalog) discardGeneration(ctx context.Context, serviceID string, generation int64) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM mosaic_granules WHERE service_id=? AND generation=?`, serviceID, generation)
	return err
}

func (c *catalog) deleteGranule(ctx context.Context, workspaceID, serviceID, id string) (int64, int64, error) {
	item, err := c.getGranule(ctx, workspaceID, serviceID, id)
	if err != nil {
		return 0, 0, err
	}
	next, err := c.beginGeneration(ctx, workspaceID, serviceID, true)
	if err != nil {
		return 0, 0, err
	}
	if _, err := c.db.ExecContext(ctx, `DELETE FROM mosaic_granules WHERE service_id=? AND generation=? AND source_uri=?`, serviceID, next, item.SourceURI); err != nil {
		_ = c.discardGeneration(ctx, serviceID, next)
		return 0, 0, err
	}
	previous, err := c.activateGeneration(ctx, workspaceID, serviceID, next)
	if err != nil {
		_ = c.discardGeneration(ctx, serviceID, next)
		return 0, 0, err
	}
	return next, previous, nil
}

func footprintWKB(bbox [4]float64) []byte {
	// Little-endian OGC WKB Polygon with one closed exterior ring.
	result := make([]byte, 1+4+4+4+5*16)
	result[0] = 1
	binary.LittleEndian.PutUint32(result[1:5], 3)
	binary.LittleEndian.PutUint32(result[5:9], 1)
	binary.LittleEndian.PutUint32(result[9:13], 5)
	points := [][2]float64{{bbox[0], bbox[1]}, {bbox[2], bbox[1]}, {bbox[2], bbox[3]}, {bbox[0], bbox[3]}, {bbox[0], bbox[1]}}
	offset := 13
	for _, point := range points {
		binary.LittleEndian.PutUint64(result[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(result[offset+8:offset+16], math.Float64bits(point[1]))
		offset += 16
	}
	return result
}

func footprintGeoJSON(bbox [4]float64) map[string]any {
	return map[string]any{"type": "Polygon", "coordinates": [][][]float64{{
		{bbox[0], bbox[1]}, {bbox[2], bbox[1]}, {bbox[2], bbox[3]}, {bbox[0], bbox[3]}, {bbox[0], bbox[1]},
	}}}
}

func (c *catalog) listRasterMosaicGranules(ctx context.Context, serviceID string) ([]store.RasterMosaicGranule, error) {
	var workspaceID string
	if err := c.db.QueryRowContext(ctx, `SELECT workspace_id FROM mosaic_services WHERE service_id=?`, serviceID).Scan(&workspaceID); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	items, err := c.listGranules(ctx, workspaceID, serviceID, GranuleFilter{Limit: 1_000_000})
	if err != nil {
		return nil, err
	}
	result := make([]store.RasterMosaicGranule, 0, len(items))
	for _, item := range items {
		result = append(result, store.RasterMosaicGranule{Path: item.SourceURI, Time: item.Time, Elevation: item.Elevation, Priority: item.Priority})
	}
	return result, nil
}

func marshalRequest(value HarvestRequest) (string, error) {
	data, err := json.Marshal(value)
	return string(data), err
}
