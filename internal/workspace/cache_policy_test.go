package workspace

import (
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

func TestServiceCachePolicies(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		svc := &Service{}
		enabled, ttl := svc.FeatureCachePolicy(5 * time.Minute)
		if !enabled || ttl != 5*time.Minute {
			t.Fatalf("FeatureCachePolicy() = %v, %v", enabled, ttl)
		}
	})

	t.Run("feature override", func(t *testing.T) {
		enabledValue, ttlValue := true, 30
		svc := &Service{CacheSettings: &store.ServiceCacheSettings{FeaturesEnabled: &enabledValue, FeaturesTTLSec: &ttlValue}}
		enabled, ttl := svc.FeatureCachePolicy(5 * time.Minute)
		if !enabled || ttl != 30*time.Second {
			t.Fatalf("FeatureCachePolicy() = %v, %v", enabled, ttl)
		}
	})

	t.Run("zero tile TTL disables", func(t *testing.T) {
		ttlValue := 0
		svc := &Service{CacheSettings: &store.ServiceCacheSettings{TilesTTLSec: &ttlValue}}
		enabled, _ := svc.TileCachePolicy(15 * time.Minute)
		if enabled {
			t.Fatal("TileCachePolicy() enabled with zero TTL")
		}
	})
}
