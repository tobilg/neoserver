package tilecache

import (
	"context"
	"strings"
	"testing"
)

func TestBaselineStyleSelectorSurvivesReopen(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	manager := newTestManager(t, root, 1024)
	for i, digest := range []string{"default#tms=one", "roads@digest#deps=children#dim=time#tms=two", "other@digest#tms=one"} {
		id := testIdentity(i)
		id.StyleDigest = digest
		id.StyleName = strings.Split(strings.Split(digest, "#")[0], "@")[0]
		if _, err := manager.Put(ctx, id, []byte("tile"), Policy{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
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
