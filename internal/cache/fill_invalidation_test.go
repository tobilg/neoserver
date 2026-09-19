package cache

import (
	"context"
	"testing"
	"time"
)

func TestInvalidationSeparatesInFlightLoads(t *testing.T) {
	for _, kind := range []CacheType{CacheTypeCapabilities, CacheTypeCollections, CacheTypeFeatures, CacheTypeTiles, CacheTypeCounts} {
		for _, clear := range []string{"workspace", "type", "all"} {
			t.Run(string(kind)+"/"+clear, func(t *testing.T) {
				m, err := NewManager(DefaultConfig())
				if err != nil {
					t.Fatal(err)
				}
				defer m.Close()
				key := SingleFeatureKey("workspace:one", "layer", "id", "")
				started, release := make(chan struct{}), make(chan struct{})
				load := func(old bool) (string, error) {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					read := func() int {
						if old {
							close(started)
							<-release
							return 1
						}
						return 2
					}
					if kind == CacheTypeCounts {
						n, _, err := m.LoadCount(ctx, key, func(context.Context) (int, error) { return read(), nil })
						return string(rune('0' + n)), err
					}
					b, _, err := m.LoadBytes(ctx, kind, key, time.Minute, func(context.Context) ([]byte, error) { return []byte{byte('0' + read())}, nil })
					return string(b), err
				}
				done := make(chan struct{})
				go func() { defer close(done); _, _ = load(true) }()
				<-started
				switch clear {
				case "workspace":
					m.InvalidateWorkspace("workspace:one")
				case "type":
					m.ClearByType(kind)
				case "all":
					m.ClearAll()
				}
				// A post-clear request must not subscribe to the blocked old fill.
				fresh, err := load(false)
				close(release)
				<-done
				if err != nil || fresh != "2" {
					t.Fatalf("post-clear load = %q, %v", fresh, err)
				}
				m.capabilities.Wait()
				m.collections.Wait()
				m.features.Wait()
				m.tiles.Wait()
				m.counts.Wait()
				if kind == CacheTypeCounts {
					if n, ok := m.GetCount(key); !ok || n != 2 {
						t.Fatalf("stale count admitted: %d, %v", n, ok)
					}
				} else if b, ok := m.getBytes(kind, key); !ok || string(b) != "2" {
					t.Fatalf("stale response admitted: %s, %v", b, ok)
				}
			})
		}
	}
}

func TestDirectFillTokenCannotRepopulateInvalidatedScope(t *testing.T) {
	m, err := NewManager(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	key := SingleFeatureKey("ws", "layer", "1", "")
	other := SingleFeatureKey("other", "layer", "1", "")
	old, unaffected := m.BeginFill(CacheTypeFeatures, key), m.BeginFill(CacheTypeFeatures, other)
	m.InvalidateLayer("ws", "layer")
	m.SetFeatures(key, []byte("old"), time.Minute, old)
	m.SetFeatures(other, []byte("valid"), time.Minute, unaffected)
	m.features.Wait()
	if _, ok := m.GetFeatures(key); ok {
		t.Fatal("stale direct fill survived invalidation")
	}
	if b, ok := m.GetFeatures(other); !ok || string(b) != "valid" {
		t.Fatal("unrelated fill was discarded")
	}
}
