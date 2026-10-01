package staccatalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/neoserver/internal/sqlutil"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/store"
)

type Catalog struct {
	db, read *sql.DB
	key      []byte
	maxItems int64
}
type sharedConnector struct{ driver.Connector }

func Open(path, key string, maxItems int64) (*Catalog, error) {
	if err := stacmodel.Ready(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	var attached atomic.Bool
	conn, err := duckdb.NewConnector("", func(exec driver.ExecerContext) error {
		if _, e := exec.ExecContext(context.Background(), "SET TimeZone='UTC'", nil); e != nil {
			return e
		}
		if attached.Load() {
			_, e := exec.ExecContext(context.Background(), "USE stac", nil)
			return e
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(conn)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(err error) (*Catalog, error) { db.Close(); return nil, err }
	if _, err = db.Exec("LOAD spatial"); err != nil {
		return fail(fmt.Errorf("load STAC spatial extension: %w", err))
	}
	attach := "ATTACH '" + sqlutil.EscapeLiteral(abs) + "' AS stac"
	if key != "" {
		attach += " (ENCRYPTION_KEY '" + sqlutil.EscapeLiteral(key) + "')"
	}
	if _, err = db.Exec(attach); err != nil {
		return fail(store.WrapAttachError(err, abs))
	}
	attached.Store(true)
	if _, err = db.Exec("USE stac"); err != nil {
		return fail(err)
	}
	var exists int
	if err = db.QueryRow("SELECT count(*) FROM information_schema.tables WHERE table_catalog='stac' AND table_name='stac_schema'").Scan(&exists); err != nil {
		return fail(err)
	}
	if exists > 0 {
		var version int
		if err = db.QueryRow("SELECT max(version) FROM stac_schema").Scan(&version); err != nil {
			return fail(err)
		}
		if version < 1 || version > schemaVersion {
			return fail(fmt.Errorf("unsupported STAC schema version %d", version))
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return fail(err)
	}
	if _, err = tx.Exec(schemaSQL); err != nil {
		tx.Rollback()
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	if err = migrate(db); err != nil {
		return fail(err)
	}
	if err = os.Chmod(abs, 0600); err != nil {
		return fail(err)
	}
	read := sql.OpenDB(sharedConnector{conn})
	read.SetMaxOpenConns(8)
	read.SetMaxIdleConns(8)
	c := &Catalog{db: db, read: read, key: make([]byte, 32), maxItems: maxItems}
	if c.maxItems <= 0 {
		c.maxItems = 1000000
	}
	if _, err = rand.Read(c.key); err != nil {
		read.Close()
		return fail(err)
	}
	// Interrupted scans/uploads are failed; complete staged imports remain ready.
	_, err = db.Exec("UPDATE stac_jobs SET status='failed', error='Interrupted by server restart', updated_at=current_timestamp WHERE status IN ('running','uploading')")
	if err != nil {
		read.Close()
		return fail(err)
	}
	return c, nil
}
func (c *Catalog) Close() error                     { c.read.Close(); return c.db.Close() }
func (c *Catalog) Health(ctx context.Context) error { return c.read.PingContext(ctx) }

const schemaVersion = 2

// migrations upgrade the version 1 baseline created by schemaSQL.
var migrations = []struct {
	version int
	sql     string
}{
	// The fingerprint of the source state behind the published generation lets
	// reconciliation skip unchanged sources across restarts.
	{2, "ALTER TABLE stac_collections ADD COLUMN source_fingerprint VARCHAR DEFAULT ''"},
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("SELECT max(version) FROM stac_schema").Scan(&version); err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= version {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(m.sql); err == nil {
			_, err = tx.Exec("INSERT INTO stac_schema VALUES(?)", m.version)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("migrate STAC schema to version %d: %w", m.version, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS stac_schema(version INTEGER PRIMARY KEY);
INSERT INTO stac_schema SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM stac_schema);
CREATE TABLE IF NOT EXISTS stac_collections(
 workspace_id VARCHAR NOT NULL,id VARCHAR NOT NULL,document VARCHAR NOT NULL,
 public BOOLEAN NOT NULL,roles VARCHAR NOT NULL,binding VARCHAR,
 revision BIGINT NOT NULL DEFAULT 1,generation VARCHAR NOT NULL DEFAULT '',
 item_count BIGINT NOT NULL DEFAULT 0,updated_at TIMESTAMP NOT NULL DEFAULT current_timestamp,
 PRIMARY KEY(workspace_id,id));
CREATE TABLE IF NOT EXISTS stac_items(
 workspace_id VARCHAR NOT NULL,collection_id VARCHAR NOT NULL,generation VARCHAR NOT NULL,
 id VARCHAR NOT NULL,document VARCHAR NOT NULL,start_time VARCHAR NOT NULL,end_time VARCHAR NOT NULL,
 geom GEOMETRY,min_x DOUBLE,min_y DOUBLE,max_x DOUBLE,max_y DOUBLE,min_z DOUBLE,max_z DOUBLE,
 PRIMARY KEY(workspace_id,collection_id,generation,id));
CREATE INDEX IF NOT EXISTS stac_items_space ON stac_items USING RTREE(geom);
CREATE INDEX IF NOT EXISTS stac_items_time ON stac_items(workspace_id,start_time,end_time);
CREATE TABLE IF NOT EXISTS stac_jobs(
 workspace_id VARCHAR NOT NULL,id VARCHAR NOT NULL,collection_id VARCHAR NOT NULL,
 kind VARCHAR NOT NULL,status VARCHAR NOT NULL,revision BIGINT NOT NULL,processed BIGINT NOT NULL DEFAULT 0,
 error VARCHAR NOT NULL DEFAULT '',request VARCHAR NOT NULL DEFAULT '{}',
 created_at TIMESTAMP NOT NULL DEFAULT current_timestamp,updated_at TIMESTAMP NOT NULL DEFAULT current_timestamp,
 PRIMARY KEY(workspace_id,id));
CREATE INDEX IF NOT EXISTS stac_jobs_status ON stac_jobs(status,created_at);
CREATE TABLE IF NOT EXISTS stac_item_overrides(workspace_id VARCHAR NOT NULL,collection_id VARCHAR NOT NULL,item_id VARCHAR NOT NULL,patch VARCHAR NOT NULL,PRIMARY KEY(workspace_id,collection_id,item_id));
CREATE TABLE IF NOT EXISTS stac_local_assets(
 workspace_id VARCHAR NOT NULL,collection_id VARCHAR NOT NULL,item_id VARCHAR NOT NULL,
 asset_key VARCHAR NOT NULL,path VARCHAR NOT NULL,media_type VARCHAR NOT NULL,generation VARCHAR NOT NULL DEFAULT '',
 PRIMARY KEY(workspace_id,collection_id,item_id,asset_key,generation));
`

func jsonString(v any) string { raw, _ := json.Marshal(v); return string(raw) }

const collectionColumns = "document, public, roles, binding, revision, item_count, updated_at, coalesce(source_fingerprint,'')"

func scanCollection(row interface{ Scan(...any) error }) (*Collection, error) {
	c := &Collection{}
	var doc, roles string
	var binding sql.NullString
	if err := row.Scan(&doc, &c.Public, &roles, &binding, &c.Revision, &c.ItemCount, &c.UpdatedAt, &c.SourceFingerprint); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var err error
	c.Document, err = stacmodel.Decode([]byte(doc))
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(roles), &c.AllowedRoles); err != nil {
		return nil, err
	}
	if binding.Valid && binding.String != "null" {
		if err = json.Unmarshal([]byte(binding.String), &c.Binding); err != nil {
			return nil, err
		}
	}
	return c, nil
}
func (c *Catalog) GetCollection(ctx context.Context, ws, id string) (*Collection, error) {
	return scanCollection(c.read.QueryRowContext(ctx, "SELECT "+collectionColumns+" FROM stac_collections WHERE workspace_id=? AND id=?", ws, id))
}
func (c *Catalog) Collections(ctx context.Context, ws string) ([]*Collection, error) {
	rows, err := c.read.QueryContext(ctx, "SELECT "+collectionColumns+" FROM stac_collections WHERE workspace_id=? ORDER BY id", ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Collection{}
	for rows.Next() {
		v, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (c *Catalog) PutCollection(ctx context.Context, ws string, item Collection, create bool) error {
	if err := stacmodel.Validate(item.Document, "collection"); err != nil {
		return err
	}
	id := item.Document.String("id")
	if create {
		result, err := c.db.ExecContext(ctx, "INSERT INTO stac_collections(workspace_id,id,document,public,roles,binding) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING", ws, id, jsonString(item.Document), item.Public, jsonString(item.AllowedRoles), nil)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return ErrConflict
		}
		return nil
	}
	result, err := c.db.ExecContext(ctx, "UPDATE stac_collections SET document=?,public=?,roles=?,revision=revision+1,updated_at=current_timestamp WHERE workspace_id=? AND id=? AND revision=?", jsonString(item.Document), item.Public, jsonString(item.AllowedRoles), ws, id, item.Revision)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (c *Catalog) DeleteCollection(ctx context.Context, ws, id string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"stac_items", "stac_jobs", "stac_local_assets", "stac_item_overrides"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE workspace_id=? AND collection_id=?", ws, id); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM stac_collections WHERE workspace_id=? AND id=?", ws, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
func (c *Catalog) PutBinding(ctx context.Context, ws, collection string, binding *Binding, revision int64) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw sql.NullString
	var previousRevision int64
	if err = tx.QueryRowContext(ctx, "SELECT binding,revision FROM stac_collections WHERE workspace_id=? AND id=?", ws, collection).Scan(&raw, &previousRevision); err == sql.ErrNoRows {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if previousRevision != revision {
		return ErrConflict
	}
	var previous Binding
	if raw.Valid && raw.String != "null" {
		if err = json.Unmarshal([]byte(raw.String), &previous); err != nil {
			return err
		}
	}
	retargeted := previous.ID != "" && (previous.ServiceID != binding.ServiceID || previous.ResourceID != binding.ResourceID || previous.ResourceKind != binding.ResourceKind || previous.Mode != binding.Mode)
	// Preserve the existing publication and its source access rules. Reusing its
	// generation under a different source could disclose previously restricted data.
	if retargeted {
		return fmt.Errorf("create a separate Collection to publish a different source or mode; the existing Items remain unchanged")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE stac_jobs SET status='cancelled',updated_at=current_timestamp WHERE workspace_id=? AND collection_id=? AND status IN ('queued','running','uploading','ready')", ws, collection); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE stac_collections SET binding=?,revision=revision+1,updated_at=current_timestamp WHERE workspace_id=? AND id=?", jsonString(binding), ws, collection); err != nil {
		return err
	}
	return tx.Commit()
}
func (c *Catalog) GetItem(ctx context.Context, ws, collection, id string) (stacmodel.Document, error) {
	var raw string
	err := c.read.QueryRowContext(ctx, `SELECT json_merge_patch(i.document,coalesce(o.patch,'{}'))::VARCHAR FROM stac_items i LEFT JOIN stac_item_overrides o ON o.workspace_id=i.workspace_id AND o.collection_id=i.collection_id AND o.item_id=i.id JOIN stac_collections c ON c.workspace_id=i.workspace_id AND c.id=i.collection_id AND c.generation=i.generation WHERE i.workspace_id=? AND i.collection_id=? AND i.id=?`, ws, collection, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return stacmodel.Decode([]byte(raw))
}
