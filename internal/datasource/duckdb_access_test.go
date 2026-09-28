package datasource

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

func TestRestrictDuckDBAccessConfinesAndLocks(t *testing.T) {
	root := t.TempDir()
	allowed, other := filepath.Join(root, "allowed.csv"), filepath.Join(root, "other.csv")
	for _, path := range []string{allowed, other} {
		if err := os.WriteFile(path, []byte("a\n1\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(3)
	if err := RestrictDuckDBAccess(db, []string{allowed}, nil); err != nil {
		t.Fatal(err)
	}
	read := func(path string) error {
		var count int
		return db.QueryRow("SELECT count(*) FROM read_csv(?)", path).Scan(&count)
	}
	if err := read(allowed); err != nil {
		t.Fatalf("allowed file: %v", err)
	}
	if err := read(other); err == nil {
		t.Fatal("read a file outside the allowlist")
	}
	for _, statement := range []string{
		"SET enable_external_access = true",
		"SET allowed_directories = ['/']",
		"SET lock_configuration = false",
	} {
		if _, err := db.Exec(statement); err == nil {
			t.Errorf("%s succeeded on a locked database", statement)
		}
	}
}
