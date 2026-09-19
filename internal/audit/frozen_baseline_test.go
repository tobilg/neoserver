package audit

import (
	"database/sql"
	"github.com/tobilg/neoserver/internal/testutil/schematest"
	"os"
	"testing"
)

func TestFrozenBaselineMatchesFreshAudit(t *testing.T) {
	baseline, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer baseline.Close()
	ddl, err := os.ReadFile("testdata/baseline-v1.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baseline.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	if err := initSchema(baseline); err != nil {
		t.Fatal(err)
	}
	fresh, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err := initSchema(fresh); err != nil {
		t.Fatal(err)
	}
	schematest.Equal(t, baseline, fresh)
}
