package cache

import (
	"testing"
	"time"
)

func TestCacheKeysEncodeComponentBoundaries(t *testing.T) {
	pairs := [][2]string{
		{SingleFeatureKey("ws", "a:b", "c", "4326"), SingleFeatureKey("ws", "a", "b:c", "4326")},
		{SingleFeatureKey("ws:a", "b", "c", ""), SingleFeatureKey("ws", "a:b", "c", "")},
		{FeaturesKey(FeaturesParams{Filter: "x&properties=y"}), FeaturesKey(FeaturesParams{Filter: "x", Properties: "y"})},
		{TileKey(TileParams{Layers: "a&styles=b"}), TileKey(TileParams{Layers: "a", Styles: "b"})},
		{WFSGetFeatureKey(WFSGetFeatureParams{Filter: "x&sortby=y"}), WFSGetFeatureKey(WFSGetFeatureParams{Filter: "x", SortBy: "y"})},
		{MapTileKey("ws", "a", "tms", 0, 0, 0, "b:c", "d"), MapTileKey("ws", "a", "tms", 0, 0, 0, "b", "c:d")},
	}
	for _, pair := range pairs {
		if pair[0] == pair[1] {
			t.Fatalf("ambiguous key: %s", pair[0])
		}
	}
}

func TestVersionedCacheScopeInvalidation(t *testing.T) {
	m, err := NewManager(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, workspace := range []string{"ws", "ws:other", "ü:世界"} {
		key := SingleFeatureKey(workspace, "a:b", "c:d", "")
		if got := cacheScope(key + ":count"); got != workspace {
			t.Fatalf("scope %q != %q", got, workspace)
		}
		m.SetFeatures(key, []byte(workspace), time.Minute)
	}
	m.features.Wait()
	m.InvalidateWorkspace("ws")
	for _, workspace := range []string{"ws", "ws:other", "ü:世界"} {
		_, exists := m.GetFeatures(SingleFeatureKey(workspace, "a:b", "c:d", ""))
		if exists != (workspace != "ws") {
			t.Fatalf("wrong invalidation for %q", workspace)
		}
	}
}

func FuzzSingleFeatureKeyTupleIdentity(f *testing.F) {
	f.Add("a:b", "c", "a", "b:c")
	f.Add("", "é", "%3A", "世界")
	f.Fuzz(func(t *testing.T, c1, id1, c2, id2 string) {
		if (c1 != c2 || id1 != id2) && SingleFeatureKey("ws", c1, id1, "") == SingleFeatureKey("ws", c2, id2, "") {
			t.Fatal("distinct tuples collide")
		}
	})
}
