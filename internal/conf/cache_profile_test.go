package conf

import "testing"

func TestApplyCacheProfile(t *testing.T) {
	tests := []struct {
		profile  string
		features int
		tiles    int
	}{
		{"balanced", 208, 256},
		{"feature-heavy", 307, 157},
		{"map-heavy", 128, 336},
	}
	for _, test := range tests {
		t.Run(test.profile, func(t *testing.T) {
			cfg := Cache{Profile: test.profile, MaxMemoryMB: 512}
			applyCacheProfile(&cfg)
			if cfg.Features.MaxMemoryMB != test.features || cfg.Tiles.MaxMemoryMB != test.tiles {
				t.Fatalf("profile budgets: features=%d tiles=%d", cfg.Features.MaxMemoryMB, cfg.Tiles.MaxMemoryMB)
			}
			total := cfg.Capabilities.MaxMemoryMB + cfg.Collections.MaxMemoryMB + cfg.Features.MaxMemoryMB + cfg.Tiles.MaxMemoryMB + cfg.Counts.MaxMemoryMB
			if total != 512 {
				t.Fatalf("profile total = %d", total)
			}
		})
	}
}

func TestApplyCacheProfilePreservesOverrides(t *testing.T) {
	cfg := Cache{Profile: "balanced", MaxMemoryMB: 512, Features: CacheFeatures{MaxMemoryMB: 300}, Counts: CacheCounts{MaxMemoryMB: 20}}
	applyCacheProfile(&cfg)
	if cfg.Features.MaxMemoryMB != 300 || cfg.Counts.MaxMemoryMB != 20 {
		t.Fatalf("explicit budgets were replaced: %+v", cfg)
	}
}

func TestValidateRejectsUnknownCacheProfile(t *testing.T) {
	cfg := Config{Server: Server{HttpPort: 9000, UrlBase: "http://localhost:9000"}, Paging: Paging{LimitDefault: 10, LimitMax: 100}, Cache: Cache{
		Enabled: true, Profile: "fifo", MaxMemoryMB: 5, CounterRatio: 10, BufferItems: 64, TTLTickSec: 5, FillTimeoutSec: 60,
		Capabilities: CacheCapabilities{Enabled: true, TTLSec: 1, MaxMemoryMB: 1}, Collections: CacheCollections{Enabled: true, TTLSec: 1, MaxMemoryMB: 1},
		Features: CacheFeatures{Enabled: true, TTLSec: 1, MaxMemoryMB: 1}, Tiles: CacheTiles{Enabled: true, TTLSec: 1, MaxMemoryMB: 1}, Counts: CacheCounts{Enabled: true, TTLSec: 1, MaxMemoryMB: 1},
	}}
	if err := validate(cfg); err == nil {
		t.Fatal("unknown cache profile was accepted")
	}
}
