package tilecache

import (
	"github.com/tobilg/neoserver/internal/testutil/schematest"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexRefusesSchemaOlderThanBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tile-cache.duckdb")
	index, err := openMetadataIndex(path, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.db.Exec(`DELETE FROM cache_schema; INSERT INTO cache_schema(version) VALUES (?)`, cacheSchemaVersion-1); err != nil {
		index.db.Close()
		t.Fatal(err)
	}
	index.db.Close()
	defer schematest.Unchanged(t, path)()
	for i := 0; i < 2; i++ {
		reopened, err := openMetadataIndex(path, "abc123")
		if reopened != nil {
			reopened.db.Close()
			t.Fatal("opened an index older than the baseline")
		}
		if err == nil || !strings.Contains(err.Error(), "delete the cache index and its tile payloads") {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestIndexRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tile-cache.duckdb")
	index, err := openMetadataIndex(path, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.db.Exec(`INSERT INTO cache_schema(version) VALUES (?)`, cacheSchemaVersion+1); err != nil {
		index.db.Close()
		t.Fatal(err)
	}
	index.db.Close()
	defer schematest.Unchanged(t, path)()
	reopened, err := openMetadataIndex(path, "abc123")
	if reopened != nil {
		reopened.db.Close()
		t.Fatal("opened an index newer than this binary")
	}
	if err == nil || !strings.Contains(err.Error(), "newer than this binary supports") {
		t.Fatalf("error=%v", err)
	}
}
