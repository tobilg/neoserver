package store

import (
	"github.com/tobilg/neoserver/internal/testutil/schematest"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRejectsCatalogOlderThanBaselineWithoutChangingIt(t *testing.T) {
	cfg := Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"}
	s, _, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM schema_info; INSERT INTO schema_info(version) VALUES(?)", catalogBaselineVersion-1); err != nil {
		t.Fatal(err)
	}
	s.Close()
	assertUnchanged := schematest.Unchanged(t, cfg.Path)
	for i := 0; i < 2; i++ {
		opened, err := Open(cfg)
		if opened != nil {
			opened.Close()
			t.Fatal("opened a catalog older than the baseline")
		}
		if err == nil || !strings.Contains(err.Error(), "older than the oldest supported version") {
			t.Fatalf("error=%v", err)
		}
	}

	assertUnchanged()
	db, attached, err := newCatalogConnection()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("ATTACH '" + escapeSQLLiteral(cfg.Path) + "' AS store (ENCRYPTION_KEY 'abc123'); USE store;"); err != nil {
		t.Fatal(err)
	}
	attached()
	var version int
	if err = db.QueryRow("SELECT max(version) FROM schema_info").Scan(&version); err != nil || version != catalogBaselineVersion-1 {
		t.Fatalf("version=%d %v", version, err)
	}
}
