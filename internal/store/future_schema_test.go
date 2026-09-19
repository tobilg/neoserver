package store

import (
	"context"
	"github.com/tobilg/neoserver/internal/testutil/schematest"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRejectsFutureCatalogAndReleasesConnection(t *testing.T) {
	cfg := Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"}
	s, _, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("INSERT INTO schema_info(version) VALUES(?)", schemaVersion+1); err != nil {
		t.Fatal(err)
	}
	s.Close()
	assertUnchanged := schematest.Unchanged(t, cfg.Path)
	for i := 0; i < 2; i++ {
		opened, err := Open(cfg)
		if opened != nil {
			opened.Close()
			t.Fatal("opened future schema for writes")
		}
		if err == nil || !strings.Contains(err.Error(), "newer than this binary supports") {
			t.Fatalf("error=%v", err)
		}
	}

	assertUnchanged()
	// Reattach using the low-level connection only to inspect the untouched fixture.
	db, attached, err := newCatalogConnection()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("ATTACH '" + escapeSQLLiteral(cfg.Path) + "' AS store (ENCRYPTION_KEY 'abc123'); USE store;"); err != nil {
		t.Fatal(err)
	}
	attached()
	var version, count int
	if err = db.QueryRow("SELECT max(version) FROM schema_info").Scan(&version); err != nil || version != schemaVersion+1 {
		t.Fatalf("version=%d %v", version, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM workspaces").Scan(&count); err != nil || count != 0 {
		t.Fatalf("catalog changed: %d %v", count, err)
	}
	if _, err = db.Exec("DELETE FROM schema_info WHERE version=?", schemaVersion+1); err != nil {
		t.Fatal(err)
	}
	db.Close()
	opened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if _, err = opened.CreateWorkspace(context.Background(), CreateWorkspaceInput{Name: "supported"}); err != nil {
		t.Fatal(err)
	}
}
