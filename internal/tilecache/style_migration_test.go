package tilecache

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStyleSelectorMigratesV1Fingerprints(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	manager := newTestManager(t, root, 1024)
	for i, digest := range []string{"default#tms=one", "roads@digest#deps=children#dim=time#tms=two", "other@digest#tms=one"} {
		id := testIdentity(i)
		id.StyleDigest = digest
		if _, err := manager.Put(ctx, id, []byte("tile"), Policy{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	index, err := openMetadataIndex(filepath.Join(root, "tile-cache.duckdb"), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the actual previous metadata shape without touching payloads.
	if _, err := index.db.Exec(`DROP INDEX tile_entries_global_lru; DROP INDEX tile_entries_workspace_lru; DROP INDEX tile_entries_resource_lru; DROP INDEX tile_entries_selector;
 ALTER TABLE tile_entries DROP COLUMN style_name; DELETE FROM cache_schema; INSERT INTO cache_schema(version) VALUES (1)`); err != nil {
		index.db.Close()
		t.Fatal(err)
	}
	index.db.Close()
	manager = newTestManager(t, root, 1024)
	defer manager.Close()
	for _, name := range []string{"default", "roads"} {
		result, err := manager.Delete(ctx, Selector{WorkspaceID: "workspace", StyleName: name})
		if err != nil || result.Entries != 1 {
			t.Fatalf("%s: %+v %v", name, result, err)
		}
	}
	stats, err := manager.Stats(ctx, "", "", 0, 0)
	if err != nil || stats.Global.EntryCount != 1 {
		t.Fatalf("unrelated entries lost: %+v %v", stats, err)
	}
}
