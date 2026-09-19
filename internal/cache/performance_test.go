package cache

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkFeaturesKey(b *testing.B) {
	params := FeaturesParams{Workspace: "demo", Collection: "roads", Limit: 1000, BBox: "1,2,3,4", CRS: "EPSG:4326", Filter: "class = 'primary'", SortBy: "id"}
	b.ReportAllocs()
	for b.Loop() {
		_ = FeaturesKey(params)
	}
}

func BenchmarkAdmissionAtCapacity(b *testing.B) {
	cfg := DefaultConfig()
	cfg.Features.MaxMemoryMB = 1
	m, err := NewManager(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()
	value := make([]byte, 1024)
	var sequence atomic.Int64
	b.ReportAllocs()
	for b.Loop() {
		m.SetFeatures(fmt.Sprintf("ws:bench:%d", sequence.Add(1)), value, time.Minute)
	}
}

func BenchmarkCoalescedFeatureLoad(b *testing.B) {
	m, err := NewManager(DefaultConfig())
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()
	loader := func(context.Context) ([]byte, error) {
		return []byte(`{"type":"FeatureCollection","features":[]}`), nil
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, err := m.LoadBytes(context.Background(), CacheTypeFeatures, "ws:bench:same", time.Minute, loader)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})
}
