package dbschema

import (
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

func TestMigrationsAreOrderedAtomicAndIdempotent(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE versions(version INTEGER PRIMARY KEY); INSERT INTO versions VALUES(25)"); err != nil {
		t.Fatal(err)
	}
	steps := []Migration{{26, "CREATE TABLE probe(id INTEGER)"}, {27, "ALTER TABLE missing ADD COLUMN n INTEGER"}}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(tx, "test", "versions", 25, 27, 25, steps, "unsupported"); err == nil {
		t.Fatal("expected migration failure")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var version, tables int
	if err := db.QueryRow("SELECT max(version) FROM versions").Scan(&version); err != nil || version != 25 {
		t.Fatalf("version=%d: %v", version, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM duckdb_tables() WHERE table_name='probe'").Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("partial DDL survived: %d %v", tables, err)
	}
	steps[1].SQL = "ALTER TABLE probe ADD COLUMN n INTEGER"
	for _, start := range []int{25, 27} {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := Apply(tx, "test", "versions", 25, 27, start, steps, "unsupported"); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow("SELECT max(version) FROM versions").Scan(&version); err != nil || version != 27 {
		t.Fatalf("version=%d: %v", version, err)
	}
	for _, steps := range [][]Migration{nil, {{27, "DROP TABLE probe"}, {26, "SELECT 1"}}} {
		tx, _ := db.Begin()
		if err := Apply(tx, "test", "versions", 25, 27, 25, steps, "unsupported"); err == nil {
			t.Error("invalid migration list accepted")
		}
		tx.Rollback()
	}
}
