package tilecache

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/neoserver/internal/store"
)

const cacheSchemaVersion = 2

type metadataIndex struct {
	db      *sql.DB
	writeMu sync.Mutex
}

func (i *metadataIndex) health(ctx context.Context) error { return i.db.PingContext(ctx) }

func openMetadataIndex(path, encryptionKey string) (*metadataIndex, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, err
	}
	attach := fmt.Sprintf("ATTACH IF NOT EXISTS '%s' AS tile_cache", escapeSQLLiteral(abs))
	if encryptionKey != "" {
		attach += fmt.Sprintf(" (ENCRYPTION_KEY '%s')", escapeSQLLiteral(encryptionKey))
	}
	connector, err := duckdb.NewConnector("", func(execer driver.ExecerContext) error {
		if _, err := execer.ExecContext(context.Background(), attach, nil); err != nil {
			return err
		}
		_, err := execer.ExecContext(context.Background(), "USE tile_cache", nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	const schema = `
CREATE TABLE IF NOT EXISTS cache_schema (
 version INTEGER PRIMARY KEY,
 applied_at TIMESTAMP DEFAULT current_timestamp
);
CREATE TABLE IF NOT EXISTS tile_entries (
 cache_key TEXT PRIMARY KEY,
 object_key TEXT NOT NULL UNIQUE,
 workspace_id TEXT NOT NULL,
	workspace_revision BIGINT NOT NULL,
 resource_id TEXT NOT NULL,
 resource_kind TEXT NOT NULL,
 generation BIGINT NOT NULL,
 tile_type TEXT NOT NULL,
 matrix_set TEXT NOT NULL,
 zoom INTEGER NOT NULL,
 tile_col INTEGER NOT NULL,
 tile_row INTEGER NOT NULL,
 style_digest TEXT NOT NULL,
 style_name TEXT NOT NULL DEFAULT '',
 format TEXT NOT NULL,
 size_bytes BIGINT NOT NULL DEFAULT 0,
 etag TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL,
 created_at BIGINT NOT NULL,
 last_accessed_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS tile_entries_global_lru ON tile_entries(state, last_accessed_at);
CREATE INDEX IF NOT EXISTS tile_entries_workspace_lru ON tile_entries(workspace_id, state, last_accessed_at);
CREATE INDEX IF NOT EXISTS tile_entries_resource_lru ON tile_entries(workspace_id, resource_id, state, last_accessed_at);
CREATE INDEX IF NOT EXISTS tile_entries_selector ON tile_entries(workspace_id, resource_id, tile_type, matrix_set, zoom, format);
CREATE TABLE IF NOT EXISTS cache_usage (
 scope_type TEXT NOT NULL,
 scope_id TEXT NOT NULL,
 size_bytes BIGINT NOT NULL DEFAULT 0,
 entry_count BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(scope_type, scope_id)
);
CREATE TABLE IF NOT EXISTS tile_cache_jobs (
 id VARCHAR PRIMARY KEY,
 workspace_id VARCHAR NOT NULL,
 request_json JSON NOT NULL,
 status VARCHAR NOT NULL,
 total_tiles BIGINT DEFAULT 0,
 processed_tiles BIGINT DEFAULT 0,
 succeeded_tiles BIGINT DEFAULT 0,
 skipped_tiles BIGINT DEFAULT 0,
 failed_tiles BIGINT DEFAULT 0,
 bytes_written BIGINT DEFAULT 0,
 bytes_deleted BIGINT DEFAULT 0,
 cancel_requested BOOLEAN DEFAULT false,
 error_message VARCHAR DEFAULT '',
 created_by VARCHAR DEFAULT '',
 created_at TIMESTAMP DEFAULT current_timestamp,
 started_at TIMESTAMP,
 completed_at TIMESTAMP,
 updated_at TIMESTAMP DEFAULT current_timestamp
);
CREATE INDEX IF NOT EXISTS tile_cache_jobs_workspace_created ON tile_cache_jobs(workspace_id, created_at);
CREATE INDEX IF NOT EXISTS tile_cache_jobs_status_created ON tile_cache_jobs(status, created_at);
CREATE TABLE IF NOT EXISTS tile_cache_job_chunks (
 id VARCHAR PRIMARY KEY,
 job_id VARCHAR NOT NULL,
 zoom INTEGER NOT NULL,
 min_col INTEGER NOT NULL,
 max_col INTEGER NOT NULL,
 min_row INTEGER NOT NULL,
 max_row INTEGER NOT NULL,
 next_offset BIGINT DEFAULT 0,
 status VARCHAR NOT NULL DEFAULT 'queued',
 attempts INTEGER DEFAULT 0,
 last_error VARCHAR DEFAULT '',
 updated_at TIMESTAMP DEFAULT current_timestamp
);`
	if err := initializeCacheSchema(db, schema); err != nil {
		db.Close()
		// The connector's ATTACH runs lazily on the first connection, so a
		// lock conflict with another live process surfaces here.
		return nil, store.WrapAttachError(fmt.Errorf("create tile cache index: %w", err), abs)
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return &metadataIndex{db: db}, nil
}

func initializeCacheSchema(db *sql.DB, schema string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	var version sql.NullInt64
	if err := tx.QueryRow("SELECT max(version) FROM cache_schema").Scan(&version); err != nil {
		return err
	}
	if version.Valid && version.Int64 > cacheSchemaVersion {
		return fmt.Errorf("tile cache schema version %d is newer than supported version %d", version.Int64, cacheSchemaVersion)
	}
	if !version.Valid {
		if _, err := tx.Exec("INSERT INTO cache_schema(version) VALUES (?)", cacheSchemaVersion); err != nil {
			return err
		}
	} else if version.Int64 < cacheSchemaVersion {
		// V1 names allow neither '#' nor '@'; decode its documented fingerprint
		// once during migration, then query the independent logical column.
		if _, err := tx.Exec(`ALTER TABLE tile_entries ADD COLUMN style_name TEXT DEFAULT '';
 UPDATE tile_entries SET style_name=CASE WHEN tile_type='map' THEN split_part(split_part(style_digest,'#',1),'@',1) ELSE '' END;
 INSERT INTO cache_schema(version) VALUES (2);`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func escapeSQLLiteral(value string) string { return strings.ReplaceAll(value, "'", "''") }

const entryColumns = `cache_key, object_key, workspace_id, workspace_revision, resource_id, resource_kind, generation,
 tile_type, matrix_set, zoom, tile_col, tile_row, style_digest, style_name, format, size_bytes, etag,
 created_at, last_accessed_at`

func scanEntry(scanner interface{ Scan(...any) error }) (*Entry, error) {
	var entry Entry
	var created, accessed int64
	err := scanner.Scan(&entry.CacheKey, &entry.ObjectKey, &entry.Identity.WorkspaceID,
		&entry.Identity.WorkspaceRevision, &entry.Identity.ResourceID, &entry.Identity.ResourceKind, &entry.Identity.Generation,
		&entry.Identity.TileType, &entry.Identity.MatrixSet, &entry.Identity.Zoom,
		&entry.Identity.Column, &entry.Identity.Row, &entry.Identity.StyleDigest, &entry.Identity.StyleName,
		&entry.Identity.Format, &entry.SizeBytes, &entry.ETag, &created, &accessed)
	if err != nil {
		return nil, err
	}
	entry.CreatedAt = time.UnixMilli(created).UTC()
	entry.LastAccessedAt = time.UnixMilli(accessed).UTC()
	return &entry, nil
}

func (i *metadataIndex) getReady(ctx context.Context, key string) (*Entry, error) {
	entry, err := scanEntry(i.db.QueryRowContext(ctx, `SELECT `+entryColumns+` FROM tile_entries WHERE cache_key=? AND state='ready'`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBlobNotFound
	}
	return entry, err
}

func (i *metadataIndex) markPending(ctx context.Context, entry Entry, previous *Entry) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if previous != nil {
		if err := adjustUsage(ctx, tx, previous, -previous.SizeBytes, -1); err != nil {
			return err
		}
	}
	now := time.Now().UTC().UnixMilli()
	_, err = tx.ExecContext(ctx, `INSERT INTO tile_entries (`+entryColumns+`, state)
	 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'pending')
	 ON CONFLICT(cache_key) DO UPDATE SET object_key=excluded.object_key,workspace_id=excluded.workspace_id,
	 workspace_revision=excluded.workspace_revision,resource_id=excluded.resource_id,resource_kind=excluded.resource_kind,generation=excluded.generation,
 tile_type=excluded.tile_type,matrix_set=excluded.matrix_set,zoom=excluded.zoom,tile_col=excluded.tile_col,
 tile_row=excluded.tile_row,style_digest=excluded.style_digest,style_name=excluded.style_name,format=excluded.format,size_bytes=excluded.size_bytes,
 etag=excluded.etag,state='pending',created_at=excluded.created_at,last_accessed_at=excluded.last_accessed_at`,
		entry.CacheKey, entry.ObjectKey, entry.Identity.WorkspaceID, entry.Identity.WorkspaceRevision, entry.Identity.ResourceID,
		entry.Identity.ResourceKind, entry.Identity.Generation, entry.Identity.TileType,
		entry.Identity.MatrixSet, entry.Identity.Zoom, entry.Identity.Column, entry.Identity.Row,
		entry.Identity.StyleDigest, entry.Identity.StyleName, entry.Identity.Format, entry.SizeBytes, entry.ETag, now, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (i *metadataIndex) markReady(ctx context.Context, entry Entry) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE tile_entries SET state='ready',size_bytes=?,etag=? WHERE cache_key=? AND state='pending'`, entry.SizeBytes, entry.ETag, entry.CacheKey)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return errors.New("pending tile metadata disappeared")
	}
	if err := adjustUsage(ctx, tx, &entry, entry.SizeBytes, 1); err != nil {
		return err
	}
	return tx.Commit()
}

func (i *metadataIndex) restore(ctx context.Context, previous *Entry, pendingKey string) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM tile_entries WHERE cache_key=?", pendingKey); err != nil {
		return err
	}
	if previous != nil {
		created, accessed := previous.CreatedAt.UnixMilli(), previous.LastAccessedAt.UnixMilli()
		_, err := tx.ExecContext(ctx, `INSERT INTO tile_entries (`+entryColumns+`,state)
	 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'ready')`, previous.CacheKey, previous.ObjectKey,
			previous.Identity.WorkspaceID, previous.Identity.WorkspaceRevision, previous.Identity.ResourceID, previous.Identity.ResourceKind,
			previous.Identity.Generation, previous.Identity.TileType, previous.Identity.MatrixSet,
			previous.Identity.Zoom, previous.Identity.Column, previous.Identity.Row,
			previous.Identity.StyleDigest, previous.Identity.StyleName, previous.Identity.Format, previous.SizeBytes, previous.ETag,
			created, accessed)
		if err != nil {
			return err
		}
		if err := adjustUsage(ctx, tx, previous, previous.SizeBytes, 1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func adjustUsage(ctx context.Context, exec sqlExecutor, entry *Entry, bytes, count int64) error {
	for _, scope := range [][2]string{{"global", "global"}, {"workspace", entry.Identity.WorkspaceID}, {"resource", entry.Identity.WorkspaceID + ":" + entry.Identity.ResourceID}} {
		_, err := exec.ExecContext(ctx, `INSERT INTO cache_usage(scope_type,scope_id,size_bytes,entry_count)
 VALUES(?,?,?,?) ON CONFLICT(scope_type,scope_id) DO UPDATE SET
 size_bytes=GREATEST(0,cache_usage.size_bytes+excluded.size_bytes),entry_count=GREATEST(0,cache_usage.entry_count+excluded.entry_count)`,
			scope[0], scope[1], bytes, count)
		if err != nil {
			return err
		}
	}
	return nil
}

func (i *metadataIndex) remove(ctx context.Context, entry *Entry) error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	err = tx.QueryRowContext(ctx, "SELECT state FROM tile_entries WHERE cache_key=?", entry.CacheKey).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM tile_entries WHERE cache_key=?", entry.CacheKey)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows > 0 && state == "ready" {
		if err := adjustUsage(ctx, tx, entry, -entry.SizeBytes, -1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (i *metadataIndex) usage(ctx context.Context, scopeType, scopeID string) (Usage, error) {
	var usage Usage
	err := i.db.QueryRowContext(ctx, `SELECT size_bytes,entry_count FROM cache_usage WHERE scope_type=? AND scope_id=?`, scopeType, scopeID).Scan(&usage.SizeBytes, &usage.EntryCount)
	if errors.Is(err, sql.ErrNoRows) {
		return usage, nil
	}
	return usage, err
}

func (i *metadataIndex) lru(ctx context.Context, scopeType, workspaceID, resourceID, excludeKey string) (*Entry, error) {
	query := `SELECT ` + entryColumns + ` FROM tile_entries WHERE state='ready' AND cache_key<>?`
	args := []any{excludeKey}
	switch scopeType {
	case "workspace":
		query += ` AND workspace_id=?`
		args = append(args, workspaceID)
	case "resource":
		query += ` AND workspace_id=? AND resource_id=?`
		args = append(args, workspaceID, resourceID)
	}
	query += ` ORDER BY last_accessed_at ASC, created_at ASC LIMIT 1`
	entry, err := scanEntry(i.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBlobNotFound
	}
	return entry, err
}

func (i *metadataIndex) updateAccess(ctx context.Context, values map[string]time.Time) error {
	if len(values) == 0 {
		return nil
	}
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, "UPDATE tile_entries SET last_accessed_at=? WHERE cache_key=? AND state='ready'")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for key, timestamp := range values {
		if _, err := stmt.ExecContext(ctx, timestamp.UnixMilli(), key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (i *metadataIndex) nonReady(ctx context.Context) ([]*Entry, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT `+entryColumns+` FROM tile_entries WHERE state<>'ready'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []*Entry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (i *metadataIndex) selectEntries(ctx context.Context, selector Selector, limit int) ([]*Entry, error) {
	query := `SELECT ` + entryColumns + ` FROM tile_entries WHERE state='ready'`
	args := []any{}
	filters := []struct{ clause, value string }{
		{"workspace_id=?", selector.WorkspaceID}, {"resource_id=?", selector.ResourceID},
		{"tile_type=?", selector.TileType}, {"matrix_set=?", selector.MatrixSet},
		{"format=?", selector.Format}, {"style_digest=?", selector.StyleDigest},
		{"style_name=?", selector.StyleName},
	}
	for _, filter := range filters {
		if filter.value != "" {
			query += " AND " + filter.clause
			args = append(args, filter.value)
		}
	}
	if selector.MinZoom != nil {
		query += " AND zoom>=?"
		args = append(args, *selector.MinZoom)
	}
	if selector.MaxZoom != nil {
		query += " AND zoom<=?"
		args = append(args, *selector.MaxZoom)
	}
	query += " ORDER BY cache_key LIMIT ?"
	args = append(args, limit)
	rows, err := i.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []*Entry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (i *metadataIndex) close() error {
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	_, checkpointErr := i.db.Exec("CHECKPOINT tile_cache")
	return errors.Join(checkpointErr, i.db.Close())
}

func normalizeMetadataError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("tile cache metadata: %w", err)
}

func joinClauses(values []string) string { return strings.Join(values, " AND ") }
