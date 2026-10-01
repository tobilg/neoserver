package datasource

import (
	"context"
	"database/sql"
	"encoding/json"
)

type SQLViewStreamingDataSource interface {
	QuerySQLViewStream(context.Context, *SQLViewConfig, QueryParams) (FeatureStream, error)
}

// NewSQLFeatureStream retains exact JSON numbers and one source query snapshot.
func NewSQLFeatureStream(rows *sql.Rows) FeatureStream { return &sqlFeatureStream{rows: rows} }

type sqlFeatureStream struct {
	rows    *sql.Rows
	current json.RawMessage
	err     error
}

func (s *sqlFeatureStream) Next() bool {
	if s.err != nil || !s.rows.Next() {
		return false
	}
	var value string
	s.err = s.rows.Scan(&value)
	s.current = json.RawMessage(value)
	return s.err == nil
}
func (s *sqlFeatureStream) Feature() json.RawMessage { return s.current }
func (s *sqlFeatureStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.rows.Err()
}
func (s *sqlFeatureStream) Close() error { return s.rows.Close() }
