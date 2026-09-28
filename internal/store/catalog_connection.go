package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync/atomic"

	"github.com/duckdb/duckdb-go/v2"
)

// defaultCatalogConnections applies when Config.MaxConnections is unset.
const defaultCatalogConnections = 10

// catalogConnection owns the DuckDB instance behind the catalog pools. The
// attached catalog belongs to the DuckDB database, but USE is session-local:
// every new session, including replacements database/sql opens after a
// cancelled transaction, is initialized before running an unqualified query.
type catalogConnection struct {
	connector *duckdb.Connector
	attached  atomic.Bool
}

// newCatalogConnection returns the single writer pool. DuckDB permits one
// writer per database, and the store relies on writes being serialized.
func newCatalogConnection() (*sql.DB, *catalogConnection, error) {
	catalog := &catalogConnection{}
	connector, err := duckdb.NewConnector("", func(conn driver.ExecerContext) error {
		// Column defaults such as current_timestamp are converted to TIMESTAMP in
		// the session time zone. Go writes UTC, so keep defaults in UTC too.
		if _, err := conn.ExecContext(context.Background(), "SET TimeZone = 'UTC'", nil); err != nil {
			return err
		}
		if !catalog.attached.Load() {
			return nil
		}
		_, err := conn.ExecContext(context.Background(), "USE store", nil)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	catalog.connector = connector
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, catalog, nil
}

func (c *catalogConnection) markAttached() { c.attached.Store(true) }

// readPool returns the pool used for statements outside a transaction. Its
// sessions share the writer's DuckDB instance and see committed writes. With
// maxConnections of one, reads share the writer connection as before.
func (c *catalogConnection) readPool(writer *sql.DB, maxConnections int) *sql.DB {
	if maxConnections <= 0 {
		maxConnections = defaultCatalogConnections
	}
	readers := maxConnections - 1
	if readers < 1 {
		return writer
	}
	// The writer pool owns the connector; closing the reader must not close it.
	reader := sql.OpenDB(sharedConnector{c.connector})
	reader.SetMaxOpenConns(readers)
	reader.SetMaxIdleConns(readers)
	return reader
}

// sharedConnector hides the connector's io.Closer from database/sql.
type sharedConnector struct{ driver.Connector }
