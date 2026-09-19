package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync/atomic"

	"github.com/duckdb/duckdb-go/v2"
)

// The attached catalog belongs to the DuckDB database, but USE is session-local.
// database/sql may replace its sole connection after a cancelled transaction;
// initialize replacement sessions before allowing an unqualified catalog query.
func newCatalogConnection() (*sql.DB, func(), error) {
	var attached atomic.Bool
	connector, err := duckdb.NewConnector("", func(conn driver.ExecerContext) error {
		// Column defaults such as current_timestamp are converted to TIMESTAMP in
		// the session time zone. Go writes UTC, so keep defaults in UTC too.
		if _, err := conn.ExecContext(context.Background(), "SET TimeZone = 'UTC'", nil); err != nil {
			return err
		}
		if !attached.Load() {
			return nil
		}
		_, err := conn.ExecContext(context.Background(), "USE store", nil)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, func() { attached.Store(true) }, nil
}
