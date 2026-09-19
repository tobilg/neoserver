// Package dbschema implements version guards and transactional upgrades for
// neoserver's DuckDB state databases. Baselines never move with the current version.
package dbschema

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

type Migration struct {
	Version int
	SQL     string
}

// Version reads an existing version table without creating or repairing it.
func Version(tx *sql.Tx, table string) (sql.NullInt64, error) {
	var exists int
	var version sql.NullInt64
	err := tx.QueryRow(`SELECT count(*) FROM duckdb_tables() WHERE database_name=current_database() AND schema_name='main' AND table_name=?`, table).Scan(&exists)
	if err != nil || exists == 0 {
		return version, err
	}
	err = tx.QueryRow("SELECT max(version) FROM " + table).Scan(&version)
	if err == nil && !version.Valid {
		err = fmt.Errorf("%s contains no schema version", table)
	}
	return version, err
}

// Apply runs inside the caller's transaction, including version recording.
// Identifiers and SQL are compile-time application inputs, never user input.
func Apply(tx *sql.Tx, name, table string, baseline, current, version int, steps []Migration, olderHint string) error {
	if version > current {
		return fmt.Errorf("%s schema version %d is newer than this binary supports (%d); use a compatible newer binary or restore a pre-upgrade backup", name, version, current)
	}
	if version < baseline {
		return fmt.Errorf("%s schema version %d is older than the oldest supported version %d (neoserver 0.1.0); %s", name, version, baseline, olderHint)
	}
	if len(steps) != current-baseline {
		return fmt.Errorf("%s migration list does not cover baseline %d through version %d", name, baseline, current)
	}
	for i, step := range steps {
		if step.Version != baseline+i+1 {
			return fmt.Errorf("%s migrations must be consecutive and ordered after baseline %d", name, baseline)
		}
	}
	for _, step := range steps {
		if step.Version <= version {
			continue
		}
		if _, err := tx.Exec(step.SQL); err != nil {
			return fmt.Errorf("%s migration to version %d: %w", name, step.Version, err)
		}
		if _, err := tx.Exec("INSERT INTO "+table+" (version) VALUES (?)", step.Version); err != nil {
			return fmt.Errorf("record %s schema version %d: %w", name, step.Version, err)
		}
	}
	return nil
}

type Queryer interface {
	Query(string, ...any) (*sql.Rows, error)
}

// Shape returns a deterministic, database-name-independent schema description.
// It excludes physical column order and generated constraint names, which can
// differ between ALTER TABLE and fresh DDL without changing the schema contract.
func Shape(db Queryer) ([]string, error) {
	queries := []string{
		`SELECT table_name FROM duckdb_tables() WHERE database_name=current_database() AND schema_name='main' ORDER BY ALL`,
		`SELECT table_name,column_name,data_type,is_nullable,column_default FROM duckdb_columns() WHERE database_name=current_database() AND schema_name='main' ORDER BY ALL`,
		`SELECT table_name,constraint_type,constraint_text FROM duckdb_constraints() WHERE database_name=current_database() AND schema_name='main' ORDER BY ALL`,
		`SELECT table_name,index_name,is_unique,is_primary,expressions FROM duckdb_indexes() WHERE database_name=current_database() AND schema_name='main' ORDER BY ALL`,
		`SELECT sequence_name,start_value,min_value,max_value,increment_by,cycle FROM duckdb_sequences() WHERE database_name=current_database() AND schema_name='main' ORDER BY ALL`,
	}
	var shape []string
	for i, query := range queries {
		rows, err := Rows(db, query)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			shape = append(shape, fmt.Sprintf("%d:%s", i, row))
		}
	}
	return shape, nil
}

// Rows captures structural metadata or deterministic seed values for comparison.
func Rows(db Queryer, query string) ([]string, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var result []string
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		args := make([]any, len(columns))
		for i := range values {
			args[i] = &values[i]
		}
		if err := rows.Scan(args...); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return nil, err
		}
		result = append(result, string(encoded))
	}
	return result, rows.Err()
}
