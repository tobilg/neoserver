package cache

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Enabled {
		t.Error("expected default config to be enabled")
	}
	if cfg.MaxMemoryMB != 512 {
		t.Errorf("expected MaxMemoryMB=512, got %d", cfg.MaxMemoryMB)
	}

	// Capabilities
	if !cfg.Capabilities.Enabled {
		t.Error("expected capabilities caching to be enabled")
	}
	if cfg.Capabilities.TTL != 1*time.Hour {
		t.Errorf("expected capabilities TTL=1h, got %v", cfg.Capabilities.TTL)
	}

	// Collections
	if !cfg.Collections.Enabled {
		t.Error("expected collections caching to be enabled")
	}
	if cfg.Collections.TTL != 30*time.Minute {
		t.Errorf("expected collections TTL=30m, got %v", cfg.Collections.TTL)
	}

	// Features
	if !cfg.Features.Enabled {
		t.Error("expected features caching to be enabled")
	}
	if cfg.Features.TTL != 5*time.Minute {
		t.Errorf("expected features TTL=5m, got %v", cfg.Features.TTL)
	}
	if cfg.Features.MaxEntries != 1000 {
		t.Errorf("expected features MaxEntries=1000, got %d", cfg.Features.MaxEntries)
	}
	if cfg.Features.MaxEntrySize != 10*1024*1024 {
		t.Errorf("expected features MaxEntrySize=10MB, got %d", cfg.Features.MaxEntrySize)
	}

	// Tiles
	if !cfg.Tiles.Enabled {
		t.Error("expected tiles caching to be enabled")
	}
	if cfg.Tiles.TTL != 15*time.Minute {
		t.Errorf("expected tiles TTL=15m, got %v", cfg.Tiles.TTL)
	}
	if cfg.Tiles.MaxMemoryMB != 256 {
		t.Errorf("expected tiles MaxMemoryMB=256, got %d", cfg.Tiles.MaxMemoryMB)
	}
}

func TestNewManagerDisabled(t *testing.T) {
	cfg := Config{Enabled: false}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	if m.IsEnabled() {
		t.Error("manager should not be enabled")
	}

	// Operations should be no-ops when disabled
	m.SetCapabilities("key", []byte("value"))
	if _, found := m.GetCapabilities("key"); found {
		t.Error("should not find value when disabled")
	}
}

func TestNewManagerEnabled(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	if !m.IsEnabled() {
		t.Error("manager should be enabled")
	}
}

func TestCapabilitiesCache(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Capabilities.TTL = 100 * time.Millisecond // Short TTL for testing
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	key := "test:wms:capabilities"
	value := []byte("<WMS_Capabilities>...</WMS_Capabilities>")

	// Initially not found
	if _, found := m.GetCapabilities(key); found {
		t.Error("should not find value initially")
	}

	// Set and get
	m.SetCapabilities(key, value)
	// Allow time for ristretto to process
	time.Sleep(10 * time.Millisecond)

	got, found := m.GetCapabilities(key)
	if !found {
		t.Error("should find value after set")
	}
	if string(got) != string(value) {
		t.Errorf("expected %q, got %q", value, got)
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)
	if _, found := m.GetCapabilities(key); found {
		t.Error("should not find value after TTL expiration")
	}
}

func TestCollectionsCache(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Collections.TTL = 100 * time.Millisecond
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	key := "test:collections"
	value := []byte(`{"collections":[]}`)

	// Initially not found
	if _, found := m.GetCollections(key); found {
		t.Error("should not find value initially")
	}

	// Set and get
	m.SetCollections(key, value)
	time.Sleep(10 * time.Millisecond)

	got, found := m.GetCollections(key)
	if !found {
		t.Error("should find value after set")
	}
	if string(got) != string(value) {
		t.Errorf("expected %q, got %q", value, got)
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)
	if _, found := m.GetCollections(key); found {
		t.Error("should not find value after TTL expiration")
	}
}

func TestFeaturesCache(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Features.TTL = 100 * time.Millisecond
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	key := "test:layer:features"
	value := []byte(`{"type":"FeatureCollection","features":[]}`)

	// Initially not found
	if _, found := m.GetFeatures(key); found {
		t.Error("should not find value initially")
	}

	// Set with default TTL
	m.SetFeatures(key, value, 0)
	time.Sleep(10 * time.Millisecond)

	got, found := m.GetFeatures(key)
	if !found {
		t.Error("should find value after set")
	}
	if string(got) != string(value) {
		t.Errorf("expected %q, got %q", value, got)
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)
	if _, found := m.GetFeatures(key); found {
		t.Error("should not find value after TTL expiration")
	}
}

func TestFeaturesCacheCustomTTL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Features.TTL = 1 * time.Hour // Default is long
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	key := "test:layer:features:custom"
	value := []byte(`{"features":[]}`)

	// Set with custom short TTL
	m.SetFeatures(key, value, 50*time.Millisecond)
	time.Sleep(10 * time.Millisecond)

	if _, found := m.GetFeatures(key); !found {
		t.Error("should find value after set")
	}

	// Wait for custom TTL expiration
	time.Sleep(100 * time.Millisecond)
	if _, found := m.GetFeatures(key); found {
		t.Error("should not find value after custom TTL expiration")
	}
}

func TestFeaturesCacheMaxEntrySize(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Features.MaxEntrySize = 100 // 100 bytes max
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Small entry should be cached
	smallKey := "small"
	smallValue := []byte("small value")
	m.SetFeatures(smallKey, smallValue, 0)
	time.Sleep(10 * time.Millisecond)
	if _, found := m.GetFeatures(smallKey); !found {
		t.Error("small entry should be cached")
	}

	// Large entry should be rejected
	largeKey := "large"
	largeValue := make([]byte, 200)
	m.SetFeatures(largeKey, largeValue, 0)
	time.Sleep(10 * time.Millisecond)
	if _, found := m.GetFeatures(largeKey); found {
		t.Error("large entry should not be cached")
	}
}

func TestTilesCache(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tiles.TTL = 100 * time.Millisecond
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	key := "test:tile:hash"
	value := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes

	// Initially not found
	if _, found := m.GetTile(key); found {
		t.Error("should not find value initially")
	}

	// Set with default TTL
	m.SetTile(key, value, 0)
	time.Sleep(10 * time.Millisecond)

	got, found := m.GetTile(key)
	if !found {
		t.Error("should find value after set")
	}
	if string(got) != string(value) {
		t.Errorf("expected %v, got %v", value, got)
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)
	if _, found := m.GetTile(key); found {
		t.Error("should not find value after TTL expiration")
	}
}

func TestInvalidateCapabilities(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Add some entries
	m.SetCapabilities("ws1:wms", []byte("cap1"))
	m.SetCapabilities("ws2:wms", []byte("cap2"))
	time.Sleep(10 * time.Millisecond)

	// Verify entries exist
	if _, found := m.GetCapabilities("ws1:wms"); !found {
		t.Error("ws1:wms should exist")
	}

	// Invalidate
	m.InvalidateCapabilities("ws1")
	time.Sleep(10 * time.Millisecond)

	// All should be cleared (ristretto clears entire cache)
	if _, found := m.GetCapabilities("ws1:wms"); found {
		t.Error("ws1:wms should be invalidated")
	}
}

func TestInvalidateWorkspace(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Add entries across all cache types
	m.SetCapabilities("ws1:wms", []byte("cap"))
	m.SetCollections("ws1:collections", []byte("col"))
	m.SetFeatures("ws1:layer:features", []byte("feat"), 0)
	m.SetTile("ws1:tile", []byte("tile"), 0)
	time.Sleep(10 * time.Millisecond)

	// Verify all exist
	if _, found := m.GetCapabilities("ws1:wms"); !found {
		t.Error("capabilities should exist")
	}

	// Invalidate workspace
	m.InvalidateWorkspace("ws1")
	time.Sleep(10 * time.Millisecond)

	// All should be cleared
	if _, found := m.GetCapabilities("ws1:wms"); found {
		t.Error("capabilities should be invalidated")
	}
	if _, found := m.GetCollections("ws1:collections"); found {
		t.Error("collections should be invalidated")
	}
	if _, found := m.GetFeatures("ws1:layer:features"); found {
		t.Error("features should be invalidated")
	}
	if _, found := m.GetTile("ws1:tile"); found {
		t.Error("tiles should be invalidated")
	}
}

func TestInvalidateLayer(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Add entries
	m.SetCapabilities("ws1:wms", []byte("cap"))
	m.SetCollections("ws1:collections", []byte("col"))
	m.SetFeatures("ws1:layer1:features", []byte("feat"), 0)
	time.Sleep(10 * time.Millisecond)

	// Invalidate layer
	m.InvalidateLayer("ws1", "layer1")
	time.Sleep(10 * time.Millisecond)

	// Capabilities and collections should be cleared
	if _, found := m.GetCapabilities("ws1:wms"); found {
		t.Error("capabilities should be invalidated")
	}
	if _, found := m.GetCollections("ws1:collections"); found {
		t.Error("collections should be invalidated")
	}
}

func TestClearAll(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Add entries to all caches
	m.SetCapabilities("key1", []byte("value1"))
	m.SetCollections("key2", []byte("value2"))
	m.SetFeatures("key3", []byte("value3"), 0)
	m.SetTile("key4", []byte("value4"), 0)
	time.Sleep(10 * time.Millisecond)

	// Clear all
	m.ClearAll()
	time.Sleep(10 * time.Millisecond)

	// Verify all cleared
	if _, found := m.GetCapabilities("key1"); found {
		t.Error("capabilities should be cleared")
	}
	if _, found := m.GetCollections("key2"); found {
		t.Error("collections should be cleared")
	}
	if _, found := m.GetFeatures("key3"); found {
		t.Error("features should be cleared")
	}
	if _, found := m.GetTile("key4"); found {
		t.Error("tiles should be cleared")
	}
}

func TestClearByType(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Add entries to all caches
	m.SetCapabilities("key1", []byte("value1"))
	m.SetCollections("key2", []byte("value2"))
	m.SetFeatures("key3", []byte("value3"), 0)
	m.SetTile("key4", []byte("value4"), 0)
	time.Sleep(10 * time.Millisecond)

	// Clear only capabilities
	m.ClearByType(CacheTypeCapabilities)
	time.Sleep(10 * time.Millisecond)

	// Capabilities cleared, others remain
	if _, found := m.GetCapabilities("key1"); found {
		t.Error("capabilities should be cleared")
	}
	if _, found := m.GetCollections("key2"); !found {
		t.Error("collections should still exist")
	}
	if _, found := m.GetFeatures("key3"); !found {
		t.Error("features should still exist")
	}
	if _, found := m.GetTile("key4"); !found {
		t.Error("tiles should still exist")
	}

	// Clear collections
	m.ClearByType(CacheTypeCollections)
	time.Sleep(10 * time.Millisecond)
	if _, found := m.GetCollections("key2"); found {
		t.Error("collections should be cleared")
	}

	// Clear features
	m.ClearByType(CacheTypeFeatures)
	time.Sleep(10 * time.Millisecond)
	if _, found := m.GetFeatures("key3"); found {
		t.Error("features should be cleared")
	}

	// Clear tiles
	m.ClearByType(CacheTypeTiles)
	time.Sleep(10 * time.Millisecond)
	if _, found := m.GetTile("key4"); found {
		t.Error("tiles should be cleared")
	}
}

func TestGetMetrics(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Generate some hits and misses
	m.GetCapabilities("nonexistent") // Miss
	m.SetCapabilities("key", []byte("value"))
	time.Sleep(10 * time.Millisecond)
	m.GetCapabilities("key") // Hit

	metrics := m.GetMetrics()

	if metrics.Capabilities.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", metrics.Capabilities.Hits)
	}
	if metrics.Capabilities.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", metrics.Capabilities.Misses)
	}
	if metrics.Capabilities.HitRate != 0.5 {
		t.Errorf("expected hit rate 0.5, got %f", metrics.Capabilities.HitRate)
	}
}

func TestCalculateHitRate(t *testing.T) {
	tests := []struct {
		hits     int64
		misses   int64
		expected float64
	}{
		{0, 0, 0},
		{1, 0, 1},
		{0, 1, 0},
		{1, 1, 0.5},
		{3, 1, 0.75},
		{100, 0, 1},
	}

	for _, tc := range tests {
		got := calculateHitRate(tc.hits, tc.misses)
		if got != tc.expected {
			t.Errorf("calculateHitRate(%d, %d) = %f, expected %f",
				tc.hits, tc.misses, got, tc.expected)
		}
	}
}

func TestNewConfigFromSettings(t *testing.T) {
	settings := ConfigSettings{
		Enabled:              true,
		MaxMemoryMB:          1024,
		CapabilitiesEnabled:  true,
		CapabilitiesTTLSec:   3600,
		CollectionsEnabled:   true,
		CollectionsTTLSec:    1800,
		FeaturesEnabled:      true,
		FeaturesTTLSec:       300,
		FeaturesMaxEntries:   500,
		FeaturesMaxEntrySize: 5 * 1024 * 1024,
		TilesEnabled:         true,
		TilesTTLSec:          900,
		TilesMaxMemoryMB:     512,
	}

	cfg := NewConfigFromSettings(settings)

	if !cfg.Enabled {
		t.Error("expected enabled=true")
	}
	if cfg.MaxMemoryMB != 1024 {
		t.Errorf("expected MaxMemoryMB=1024, got %d", cfg.MaxMemoryMB)
	}
	if cfg.Capabilities.TTL != 1*time.Hour {
		t.Errorf("expected capabilities TTL=1h, got %v", cfg.Capabilities.TTL)
	}
	if cfg.Collections.TTL != 30*time.Minute {
		t.Errorf("expected collections TTL=30m, got %v", cfg.Collections.TTL)
	}
	if cfg.Features.TTL != 5*time.Minute {
		t.Errorf("expected features TTL=5m, got %v", cfg.Features.TTL)
	}
	if cfg.Features.MaxEntries != 500 {
		t.Errorf("expected features MaxEntries=500, got %d", cfg.Features.MaxEntries)
	}
	if cfg.Tiles.TTL != 15*time.Minute {
		t.Errorf("expected tiles TTL=15m, got %v", cfg.Tiles.TTL)
	}
	if cfg.Tiles.MaxMemoryMB != 512 {
		t.Errorf("expected tiles MaxMemoryMB=512, got %d", cfg.Tiles.MaxMemoryMB)
	}
}

func TestCacheDisabledPerType(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Capabilities.Enabled = false
	cfg.Collections.Enabled = false
	cfg.Features.Enabled = false
	cfg.Tiles.Enabled = false

	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// All operations should be no-ops
	m.SetCapabilities("key", []byte("value"))
	m.SetCollections("key", []byte("value"))
	m.SetFeatures("key", []byte("value"), 0)
	m.SetTile("key", []byte("value"), 0)
	time.Sleep(10 * time.Millisecond)

	if _, found := m.GetCapabilities("key"); found {
		t.Error("should not find capabilities when disabled")
	}
	if _, found := m.GetCollections("key"); found {
		t.Error("should not find collections when disabled")
	}
	if _, found := m.GetFeatures("key"); found {
		t.Error("should not find features when disabled")
	}
	if _, found := m.GetTile("key"); found {
		t.Error("should not find tiles when disabled")
	}
}

func TestConcurrentAccess(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Run concurrent reads and writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := "concurrent:key"
				m.SetCapabilities(key, []byte("value"))
				m.GetCapabilities(key)
				m.SetFeatures(key, []byte("value"), 0)
				m.GetFeatures(key)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
