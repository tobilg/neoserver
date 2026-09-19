package store

import (
	"github.com/tobilg/neoserver/internal/testutil/schematest"
	"os"
	"path/filepath"
	"testing"
)

func TestFrozenBaselineMatchesFreshCatalog(t *testing.T) {
	cfg := Config{Path: filepath.Join(t.TempDir(), "baseline.db"), EncryptionKey: "abc123"}
	db, attached, err := newCatalogConnection()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ATTACH '" + escapeSQLLiteral(cfg.Path) + "' AS store (ENCRYPTION_KEY 'abc123'); USE store"); err != nil {
		t.Fatal(err)
	}
	attached()
	ddl, err := os.ReadFile("testdata/baseline-v25.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	baseline, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer baseline.Close()
	freshCfg := Config{Path: filepath.Join(t.TempDir(), "fresh.db"), EncryptionKey: "abc123"}
	fresh, _, err := Init(freshCfg)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Close()
	fresh, err = Open(freshCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	schematest.Equal(t, baseline.db, fresh.db,
		"SELECT id,name,description,is_system FROM roles ORDER BY id",
		"SELECT ptype,v0,v1,v2,v3,v4,v5 FROM casbin_rules ORDER BY ALL")
}
