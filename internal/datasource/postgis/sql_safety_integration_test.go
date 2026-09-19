package postgis

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

// CI supplies a disposable PostGIS database. No SQL/file reads are executed by
// the denied inputs; testing with a superuser proves the parser rejects them
// before PostgreSQL privileges could accidentally allow them.
func TestLiveSQLViewReadOnlyBoundary(t *testing.T) {
	dsn := os.Getenv("NEOSRV_POSTGIS_SQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostGIS fixture")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test connection configuration")
	}
	cfg := DefaultConfig()
	cfg.Host = parsed.ConnConfig.Host
	cfg.Port = int(parsed.ConnConfig.Port)
	cfg.User = parsed.ConnConfig.User
	cfg.Password = parsed.ConnConfig.Password
	cfg.Database = parsed.ConnConfig.Database
	cfg.SSLMode = "disable"
	cfg.MaxOpenConns = 1
	cfg.MaxIdleConns = 1
	ds, err := New("sql-security", cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, query := range []string{`SELECT "pg_read_file"('/etc/passwd')`, `SELECT pg_catalog.pg_read_file/**/('/etc/passwd')`, `SELECT set_config('default_transaction_read_only','off',false)`} {
		if err := ds.ValidateSQLView(ctx, query); err == nil {
			t.Fatalf("unsafe SQL accepted: %s", query)
		}
	}
	if err := ds.ValidateSQLView(ctx, `SELECT 1 AS id, ST_SetSRID(ST_MakePoint(1,2),4326) AS geom`); err != nil {
		t.Fatal(err)
	}
	discovered, err := ds.DiscoverSQLViewColumns(ctx, `SELECT 1 AS id, ST_SetSRID(ST_MakePoint(1,2),4326) AS geom`)
	if err != nil {
		t.Fatalf("one-connection SQL view discovery: %v", err)
	}
	if discovered.GeometryColumn != "geom" || discovered.SuggestedIDColumn != "id" {
		t.Fatalf("unexpected SQL view discovery: %+v", discovered)
	}
	var readOnly, timeout string
	if err := ds.sqlViewPool.QueryRow(ctx, "SHOW default_transaction_read_only").Scan(&readOnly); err != nil || readOnly != "on" {
		t.Fatalf("read-only=%s: %v", readOnly, err)
	}
	if err := ds.sqlViewPool.QueryRow(ctx, "SHOW statement_timeout").Scan(&timeout); err != nil || timeout != "10s" {
		t.Fatalf("timeout=%s: %v", timeout, err)
	}
	if _, err := ds.sqlViewPool.Exec(ctx, "CREATE TABLE sql_view_must_not_create (id integer)"); err == nil {
		t.Fatal("read-only pool permitted DDL")
	}
	tx, err := ds.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "CREATE TEMP TABLE sql_view_write_pool_check(id integer) ON COMMIT DROP"); err != nil {
		t.Fatalf("ordinary WFS pool lost write support: %v", err)
	}
}
