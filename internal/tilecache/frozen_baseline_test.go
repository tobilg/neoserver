package tilecache

import (
	"database/sql"
	"github.com/tobilg/neoserver/internal/testutil/schematest"
	"os"
	"path/filepath"
	"testing"
)

func TestFrozenBaselineMatchesFreshIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := os.ReadFile("testdata/baseline-v2.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	baseline, err := openMetadataIndex(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer baseline.db.Close()
	fresh, err := openMetadataIndex(filepath.Join(t.TempDir(), "fresh.duckdb"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.db.Close()
	schematest.Equal(t, baseline.db, fresh.db)
}
