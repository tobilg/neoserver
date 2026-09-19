package cache

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/ristretto/v2"
	"golang.org/x/sync/singleflight"
)

// CacheType identifies different cache types for management operations.
type CacheType string

const (
	CacheTypeCapabilities CacheType = "capabilities"
	CacheTypeCollections  CacheType = "collections"
	CacheTypeFeatures     CacheType = "features"
	CacheTypeTiles        CacheType = "tiles"
	CacheTypeCounts       CacheType = "counts"
)

// Config holds cache configuration.
type Config struct {
	Enabled               bool
	Profile               string
	MaxMemoryMB           int64 // Total memory limit in megabytes
	CounterRatio          int64
	BufferItems           int64
	MetricsEnabled        bool
	TTLTick               time.Duration
	FillCoalescingEnabled bool
	FillTimeout           time.Duration
	Capabilities          CapabilitiesConfig
	Collections           CollectionsConfig
	Features              FeaturesConfig
	Tiles                 TilesConfig
	Counts                CountsConfig
}

// CapabilitiesConfig configures GetCapabilities caching.
type CapabilitiesConfig struct {
	Enabled      bool
	TTL          time.Duration
	MaxMemoryMB  int64
	MaxEntrySize int64
}

// CollectionsConfig configures OGC collections caching.
type CollectionsConfig struct {
	Enabled      bool
	TTL          time.Duration
	MaxMemoryMB  int64
	MaxEntrySize int64
}

// FeaturesConfig configures feature query result caching.
type FeaturesConfig struct {
	Enabled      bool
	TTL          time.Duration
	MaxEntries   int64
	MaxEntrySize int64 // Maximum size of a single entry in bytes
	MaxMemoryMB  int64
}

type CountsConfig struct {
	Enabled     bool
	TTL         time.Duration
	MaxMemoryMB int64
}

// TilesConfig configures WMS tile caching.
type TilesConfig struct {
	Enabled      bool
	TTL          time.Duration
	MaxMemoryMB  int64 // Memory limit specifically for tiles
	MaxEntrySize int64
}

// ServiceCacheConfig allows per-service cache overrides.
type ServiceCacheConfig struct {
	FeaturesEnabled bool          `json:"features_enabled,omitempty"`
	FeaturesTTL     time.Duration `json:"features_ttl,omitempty"`
	TilesEnabled    bool          `json:"tiles_enabled,omitempty"`
	TilesTTL        time.Duration `json:"tiles_ttl,omitempty"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled: true, Profile: "balanced", MaxMemoryMB: 512, CounterRatio: 10, BufferItems: 64,
		MetricsEnabled: true, TTLTick: 5 * time.Second, FillCoalescingEnabled: true, FillTimeout: time.Minute,
		Capabilities: CapabilitiesConfig{
			Enabled:      true,
			TTL:          1 * time.Hour,
			MaxMemoryMB:  16,
			MaxEntrySize: 1 << 20,
		},
		Collections: CollectionsConfig{
			Enabled:      true,
			TTL:          30 * time.Minute,
			MaxMemoryMB:  16,
			MaxEntrySize: 4 << 20,
		},
		Features: FeaturesConfig{
			Enabled:      true,
			TTL:          5 * time.Minute,
			MaxEntries:   1000,
			MaxEntrySize: 10 * 1024 * 1024, // 10MB
			MaxMemoryMB:  208,
		},
		Tiles: TilesConfig{
			Enabled:      true,
			TTL:          15 * time.Minute,
			MaxMemoryMB:  256,
			MaxEntrySize: 10 << 20,
		},
		Counts: CountsConfig{Enabled: true, TTL: time.Minute, MaxMemoryMB: 16},
	}
}

// CacheEntry carries the value and enough identity to clean the invalidation
// index when Ristretto expires, evicts, rejects, or deletes it.
type CacheEntry struct {
	epoch      *cacheEpoch
	Value      []byte
	Kind       CacheType
	Scope      string
	Key        string
	Generation uint64
}

// Metrics holds cache statistics for a single cache.
type Metrics struct {
	Hits                int64      `json:"hits"`
	Misses              int64      `json:"misses"`
	HitRate             float64    `json:"hit_rate"`
	EntryCount          int64      `json:"entry_count"`
	SizeBytes           int64      `json:"size_bytes"`
	MaxSizeBytes        int64      `json:"max_size_bytes"`
	RemainingBytes      int64      `json:"remaining_bytes"`
	CapacityUtilization float64    `json:"capacity_utilization"`
	KeysEvicted         uint64     `json:"keys_evicted"`
	BytesEvicted        uint64     `json:"bytes_evicted"`
	SetsDropped         uint64     `json:"sets_dropped"`
	SetsRejected        uint64     `json:"sets_rejected"`
	GetsDropped         uint64     `json:"gets_dropped"`
	GetsKept            uint64     `json:"gets_kept"`
	OversizedSkipped    int64      `json:"oversized_skipped"`
	TTLExpired          int64      `json:"ttl_expired"`
	LoadsShared         int64      `json:"loads_shared"`
	LoadErrors          int64      `json:"load_errors"`
	InvalidationKeys    int64      `json:"invalidation_keys"`
	EvictionLifeSeconds *Histogram `json:"eviction_life_seconds,omitempty"`
}

type Histogram struct {
	Bounds         []float64 `json:"bounds"`
	Count          int64     `json:"count"`
	CountPerBucket []int64   `json:"count_per_bucket"`
	Min            int64     `json:"min"`
	Max            int64     `json:"max"`
	Sum            int64     `json:"sum"`
}

// AllMetrics holds statistics for all caches.
type AllMetrics struct {
	Profile               string  `json:"profile"`
	Capabilities          Metrics `json:"capabilities"`
	Collections           Metrics `json:"collections"`
	Features              Metrics `json:"features"`
	Tiles                 Metrics `json:"tiles"`
	Counts                Metrics `json:"counts"`
	TotalSizeBytes        int64   `json:"total_size_bytes"`
	TotalMaxSizeBytes     int64   `json:"total_max_size_bytes"`
	TotalInvalidationKeys int64   `json:"total_invalidation_keys"`
}

type LoadSource string

const (
	LoadSourceHit    LoadSource = "hit"
	LoadSourceLoaded LoadSource = "loaded"
	LoadSourceShared LoadSource = "shared"
)

// Manager manages all caches.
type Manager struct {
	epochMu      sync.RWMutex
	epochs       map[CacheType]map[string]*cacheEpoch
	config       Config
	capabilities *ristretto.Cache[string, *CacheEntry]
	collections  *ristretto.Cache[string, *CacheEntry]
	features     *ristretto.Cache[string, *CacheEntry]
	tiles        *ristretto.Cache[string, *CacheEntry]
	counts       *ristretto.Cache[string, *CacheEntry]

	// Metrics counters
	capHits, capMisses     int64
	colHits, colMisses     int64
	featHits, featMisses   int64
	tilesHits, tilesMisses int64
	countHits, countMisses int64
	keys                   map[CacheType]map[string]map[string]uint64
	oversized              map[CacheType]*atomic.Int64
	loadsShared            map[CacheType]*atomic.Int64
	loadErrors             map[CacheType]*atomic.Int64
	ttlExpired             map[CacheType]*atomic.Int64
	generation             atomic.Uint64
	lastDropWarning        atomic.Int64
	lastIndexWarning       atomic.Int64

	mu        sync.RWMutex
	loadGroup singleflight.Group
}

// NewManager creates a new cache manager.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.CounterRatio <= 0 {
		cfg.CounterRatio = 10
	}
	if cfg.BufferItems <= 0 {
		cfg.BufferItems = 64
	}
	if cfg.TTLTick <= 0 {
		cfg.TTLTick = 5 * time.Second
	}
	if cfg.FillTimeout <= 0 {
		cfg.FillTimeout = time.Minute
	}
	if !cfg.Enabled {
		return &Manager{config: cfg, keys: make(map[CacheType]map[string]map[string]uint64), oversized: makeCounterMap(), loadsShared: makeCounterMap(), loadErrors: makeCounterMap(), ttlExpired: makeCounterMap()}, nil
	}
	m := &Manager{
		config:    cfg,
		keys:      make(map[CacheType]map[string]map[string]uint64),
		oversized: makeCounterMap(), loadsShared: makeCounterMap(), loadErrors: makeCounterMap(), ttlExpired: makeCounterMap(),
	}

	// Create capabilities cache (small, long TTL)
	capCache, err := m.newCache(CacheTypeCapabilities, nonzeroMB(cfg.Capabilities.MaxMemoryMB, 16), 100)
	if err != nil {
		return nil, err
	}
	m.capabilities = capCache

	// Create collections cache (small, moderate TTL)
	colCache, err := m.newCache(CacheTypeCollections, nonzeroMB(cfg.Collections.MaxMemoryMB, 16), 100)
	if err != nil {
		return nil, err
	}
	m.collections = colCache

	// Create features cache (larger, short TTL)
	expectedFeatures := cfg.Features.MaxEntries
	if expectedFeatures <= 0 {
		expectedFeatures = 1000
	}
	featCache, err := m.newCache(CacheTypeFeatures, nonzeroMB(cfg.Features.MaxMemoryMB, 208), expectedFeatures)
	if err != nil {
		return nil, err
	}
	m.features = featCache

	// Create tiles cache (largest, memory-limited)
	tilesCache, err := m.newCache(CacheTypeTiles, nonzeroMB(cfg.Tiles.MaxMemoryMB, 256), 10000)
	if err != nil {
		return nil, err
	}
	m.tiles = tilesCache
	countCache, err := m.newCache(CacheTypeCounts, nonzeroMB(cfg.Counts.MaxMemoryMB, 16), 1000)
	if err != nil {
		return nil, err
	}
	m.counts = countCache

	return m, nil
}

func makeCounterMap() map[CacheType]*atomic.Int64 {
	result := make(map[CacheType]*atomic.Int64, 5)
	for _, kind := range []CacheType{CacheTypeCapabilities, CacheTypeCollections, CacheTypeFeatures, CacheTypeTiles, CacheTypeCounts} {
		result[kind] = &atomic.Int64{}
	}
	return result
}

func (m *Manager) newCache(kind CacheType, maxMemoryMB, expectedEntries int64) (*ristretto.Cache[string, *CacheEntry], error) {
	tickSeconds := int64(m.config.TTLTick / time.Second)
	if tickSeconds < 1 {
		tickSeconds = 1
	}
	return ristretto.NewCache(&ristretto.Config[string, *CacheEntry]{
		NumCounters:            max(1, expectedEntries*m.config.CounterRatio),
		MaxCost:                maxMemoryMB << 20,
		BufferItems:            m.config.BufferItems,
		Metrics:                m.config.MetricsEnabled,
		TtlTickerDurationInSec: tickSeconds,
		OnExit: func(entry *CacheEntry) {
			m.untrack(entry)
		},
		OnEvict: func(item *ristretto.Item[*CacheEntry]) {
			if !item.Expiration.IsZero() && !time.Now().Before(item.Expiration) {
				m.ttlExpired[kind].Add(1)
			}
		},
	})
}

// Close releases all cache resources.
func (m *Manager) Close() {
	if m.capabilities != nil {
		m.capabilities.Close()
	}
	if m.collections != nil {
		m.collections.Close()
	}
	if m.features != nil {
		m.features.Close()
	}
	if m.tiles != nil {
		m.tiles.Close()
	}
	if m.counts != nil {
		m.counts.Close()
	}
}

// IsEnabled returns true if caching is enabled.
func (m *Manager) IsEnabled() bool {
	return m.config.Enabled
}

func (m *Manager) FeatureTTL() time.Duration      { return m.config.Features.TTL }
func (m *Manager) TileTTL() time.Duration         { return m.config.Tiles.TTL }
func (m *Manager) CollectionsTTL() time.Duration  { return m.config.Collections.TTL }
func (m *Manager) CapabilitiesTTL() time.Duration { return m.config.Capabilities.TTL }

// GetCapabilities retrieves a cached capabilities document.
func (m *Manager) GetCapabilities(key string) ([]byte, bool) {
	if !m.config.Enabled || !m.config.Capabilities.Enabled || m.capabilities == nil {
		return nil, false
	}

	entry, found := m.capabilities.Get(key)
	if !found || !currentEntry(entry) {
		atomic.AddInt64(&m.capMisses, 1)
		return nil, false
	}

	atomic.AddInt64(&m.capHits, 1)
	return entry.Value, true
}

// SetCapabilities stores a capabilities document in cache.
func (m *Manager) SetCapabilities(key string, value []byte, tokens ...FillToken) {
	if !m.config.Enabled || !m.config.Capabilities.Enabled || m.capabilities == nil {
		return
	}

	m.set(CacheTypeCapabilities, m.capabilities, key, value, m.config.Capabilities.TTL, m.config.Capabilities.MaxEntrySize, tokens...)
}

// GetCollections retrieves cached collections response.
func (m *Manager) GetCollections(key string) ([]byte, bool) {
	if !m.config.Enabled || !m.config.Collections.Enabled || m.collections == nil {
		return nil, false
	}

	entry, found := m.collections.Get(key)
	if !found || !currentEntry(entry) {
		atomic.AddInt64(&m.colMisses, 1)
		return nil, false
	}

	atomic.AddInt64(&m.colHits, 1)
	return entry.Value, true
}

// SetCollections stores a collections response in cache.
func (m *Manager) SetCollections(key string, value []byte, tokens ...FillToken) {
	if !m.config.Enabled || !m.config.Collections.Enabled || m.collections == nil {
		return
	}

	m.set(CacheTypeCollections, m.collections, key, value, m.config.Collections.TTL, m.config.Collections.MaxEntrySize, tokens...)
}

// GetFeatures retrieves cached feature query results.
func (m *Manager) GetFeatures(key string) ([]byte, bool) {
	if !m.config.Enabled || !m.config.Features.Enabled || m.features == nil {
		return nil, false
	}

	entry, found := m.features.Get(key)
	if !found || !currentEntry(entry) {
		atomic.AddInt64(&m.featMisses, 1)
		return nil, false
	}

	atomic.AddInt64(&m.featHits, 1)
	return entry.Value, true
}

// SetFeatures stores feature query results in cache.
func (m *Manager) SetFeatures(key string, value []byte, ttl time.Duration, tokens ...FillToken) {
	if !m.config.Enabled || !m.config.Features.Enabled || m.features == nil {
		return
	}

	// Use provided TTL or default
	if ttl == 0 {
		ttl = m.config.Features.TTL
	}

	m.set(CacheTypeFeatures, m.features, key, value, ttl, m.config.Features.MaxEntrySize, tokens...)
}

// GetTile retrieves a cached WMS tile.
func (m *Manager) GetTile(key string) ([]byte, bool) {
	if !m.config.Enabled || !m.config.Tiles.Enabled || m.tiles == nil {
		return nil, false
	}

	entry, found := m.tiles.Get(key)
	if !found || !currentEntry(entry) {
		atomic.AddInt64(&m.tilesMisses, 1)
		return nil, false
	}

	atomic.AddInt64(&m.tilesHits, 1)
	return entry.Value, true
}

// SetTile stores a WMS tile in cache.
func (m *Manager) SetTile(key string, value []byte, ttl time.Duration, tokens ...FillToken) {
	if !m.config.Enabled || !m.config.Tiles.Enabled || m.tiles == nil {
		return
	}

	// Use provided TTL or default
	if ttl == 0 {
		ttl = m.config.Tiles.TTL
	}

	m.set(CacheTypeTiles, m.tiles, key, value, ttl, m.config.Tiles.MaxEntrySize, tokens...)
}

func (m *Manager) GetCount(key string) (int, bool) {
	if !m.config.Enabled || !m.config.Counts.Enabled || m.counts == nil {
		return 0, false
	}
	entry, ok := m.counts.Get(key)
	if !ok || !currentEntry(entry) {
		atomic.AddInt64(&m.countMisses, 1)
		return 0, false
	}
	var value int
	if _, err := fmt.Sscanf(string(entry.Value), "%d", &value); err != nil {
		return 0, false
	}
	atomic.AddInt64(&m.countHits, 1)
	return value, true
}

func (m *Manager) SetCount(key string, value int, tokens ...FillToken) {
	if !m.config.Enabled || !m.config.Counts.Enabled || m.counts == nil {
		return
	}
	b := []byte(fmt.Sprintf("%d", value))
	m.set(CacheTypeCounts, m.counts, key, b, m.config.Counts.TTL, 0, tokens...)
}

func (m *Manager) set(kind CacheType, target *ristretto.Cache[string, *CacheEntry], key string, value []byte, ttl time.Duration, maxEntrySize int64, tokens ...FillToken) bool {
	if ttl <= 0 || target == nil {
		return false
	}
	if maxEntrySize > 0 && int64(len(value)) > maxEntrySize {
		m.oversized[kind].Add(1)
		return false
	}
	var token FillToken
	if len(tokens) == 0 {
		token = m.BeginFill(kind, key)
	} else {
		token = tokens[0]
	}
	m.epochMu.RLock()
	defer m.epochMu.RUnlock()
	if token.manager != m || token.kind != kind || token.scope != cacheScope(key) || token.epoch == nil || token.epoch.invalidated.Load() {
		return false
	}
	entry := &CacheEntry{epoch: token.epoch, Value: value, Kind: kind, Scope: cacheScope(key), Key: key, Generation: m.generation.Add(1)}
	m.track(entry)
	accepted := target.SetWithTTL(key, entry, int64(len(key)+len(value)+64), ttl)
	if !accepted {
		m.untrack(entry)
		m.warnRateLimited(&m.lastDropWarning, "cache admission dropped under write contention", "cache_type", kind)
	}
	return accepted
}

// LoadCount coalesces concurrent count cache misses for the same query.
func (m *Manager) LoadCount(ctx context.Context, key string, loader func(context.Context) (int, error)) (int, bool, error) {
	token := m.BeginFill(CacheTypeCounts, key)
	if value, ok := m.GetCount(key); ok {
		return value, false, nil
	}
	load := func() (any, error) {
		if value, ok := m.GetCount(key); ok {
			return value, nil
		}
		loadCtx, cancel := m.fillContext(ctx)
		defer cancel()
		value, err := loader(loadCtx)
		if err == nil {
			m.SetCount(key, value, token)
		} else {
			m.loadErrors[CacheTypeCounts].Add(1)
		}
		return value, err
	}
	if !m.config.FillCoalescingEnabled {
		value, err := load()
		return value.(int), false, err
	}
	ch := m.loadGroup.DoChan(token.CoalescingKey(string(CacheTypeCounts)+":"+key), load)
	select {
	case <-ctx.Done():
		return 0, false, ctx.Err()
	case result := <-ch:
		if result.Shared {
			m.loadsShared[CacheTypeCounts].Add(1)
		}
		if result.Err != nil {
			return 0, result.Shared, result.Err
		}
		return result.Val.(int), result.Shared, nil
	}
}

// LoadBytes coalesces concurrent misses while leaving admission best-effort.
// A successfully loaded value is returned even if Ristretto rejects the write.
func (m *Manager) LoadBytes(ctx context.Context, kind CacheType, key string, ttl time.Duration, loader func(context.Context) ([]byte, error)) ([]byte, LoadSource, error) {
	token := m.BeginFill(kind, key)
	if value, ok := m.getBytes(kind, key); ok {
		return value, LoadSourceHit, nil
	}
	load := func() (any, error) {
		if value, ok := m.getBytes(kind, key); ok {
			return value, nil
		}
		loadCtx, cancel := m.fillContext(ctx)
		defer cancel()
		value, err := loader(loadCtx)
		if err != nil {
			m.loadErrors[kind].Add(1)
			return nil, err
		}
		m.setBytes(kind, key, value, ttl, token)
		return value, nil
	}
	if !m.config.FillCoalescingEnabled {
		value, err := load()
		if err != nil {
			return nil, LoadSourceLoaded, err
		}
		return value.([]byte), LoadSourceLoaded, nil
	}
	ch := m.loadGroup.DoChan(token.CoalescingKey(string(kind)+":"+key), load)
	select {
	case <-ctx.Done():
		return nil, LoadSourceShared, ctx.Err()
	case result := <-ch:
		source := LoadSourceLoaded
		if result.Shared {
			m.loadsShared[kind].Add(1)
			source = LoadSourceShared
		}
		if result.Err != nil {
			return nil, source, result.Err
		}
		return result.Val.([]byte), source, nil
	}
}

func (m *Manager) fillContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), m.config.FillTimeout)
}

func (m *Manager) getBytes(kind CacheType, key string) ([]byte, bool) {
	switch kind {
	case CacheTypeCapabilities:
		return m.GetCapabilities(key)
	case CacheTypeCollections:
		return m.GetCollections(key)
	case CacheTypeFeatures:
		return m.GetFeatures(key)
	case CacheTypeTiles:
		return m.GetTile(key)
	default:
		return nil, false
	}
}

func (m *Manager) setBytes(kind CacheType, key string, value []byte, ttl time.Duration, tokens ...FillToken) {
	switch kind {
	case CacheTypeCapabilities:
		if !m.config.Enabled || !m.config.Capabilities.Enabled {
			return
		}
		m.set(CacheTypeCapabilities, m.capabilities, key, value, chooseTTL(ttl, m.config.Capabilities.TTL), m.config.Capabilities.MaxEntrySize, tokens...)
	case CacheTypeCollections:
		if !m.config.Enabled || !m.config.Collections.Enabled {
			return
		}
		m.set(CacheTypeCollections, m.collections, key, value, chooseTTL(ttl, m.config.Collections.TTL), m.config.Collections.MaxEntrySize, tokens...)
	case CacheTypeFeatures:
		m.SetFeatures(key, value, ttl, tokens...)
	case CacheTypeTiles:
		m.SetTile(key, value, ttl, tokens...)
	}
}

func chooseTTL(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func nonzeroMB(value, fallback int64) int64 {
	if value > 0 {
		return value
	}
	return fallback
}

func cacheScope(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) >= 4 && parts[0] == "v2" {
		scope, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err == nil {
			return string(scope)
		}
		return ""
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 1 {
		switch parts[0] {
		case "capabilities", "collections", "collection", "features", "feature", "tiles", "wfs-features", "counts", "mvt", "maptile":
			return parts[1]
		}
	}
	return parts[0]
}

func (m *Manager) track(entry *CacheEntry) {
	if entry == nil || entry.Scope == "" {
		return
	}
	m.mu.Lock()
	if m.keys[entry.Kind] == nil {
		m.keys[entry.Kind] = make(map[string]map[string]uint64)
	}
	if m.keys[entry.Kind][entry.Scope] == nil {
		m.keys[entry.Kind][entry.Scope] = make(map[string]uint64)
	}
	m.keys[entry.Kind][entry.Scope][entry.Key] = entry.Generation
	scopeSize := len(m.keys[entry.Kind][entry.Scope])
	m.mu.Unlock()
	if scopeSize > 100000 {
		m.warnRateLimited(&m.lastIndexWarning, "cache invalidation index is unusually large", "cache_type", entry.Kind, "scope", entry.Scope, "keys", scopeSize)
	}
}

func (m *Manager) warnRateLimited(last *atomic.Int64, message string, args ...any) {
	now := time.Now().Unix()
	previous := last.Load()
	if now-previous < 60 || !last.CompareAndSwap(previous, now) {
		return
	}
	slog.Warn(message, args...)
}

func (m *Manager) untrack(entry *CacheEntry) {
	if entry == nil || entry.Scope == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	byScope := m.keys[entry.Kind]
	keys := byScope[entry.Scope]
	if keys[entry.Key] != entry.Generation {
		return
	}
	delete(keys, entry.Key)
	if len(keys) == 0 {
		delete(byScope, entry.Scope)
	}
}

func (m *Manager) invalidateScope(kind CacheType, scope string) {
	m.epochMu.Lock()
	defer m.epochMu.Unlock()
	m.invalidateEpoch(kind, scope)
	m.mu.Lock()
	keys := m.keys[kind][scope]
	delete(m.keys[kind], scope)
	m.mu.Unlock()
	for key := range keys {
		switch kind {
		case CacheTypeCapabilities:
			m.capabilities.Del(key)
		case CacheTypeCollections:
			m.collections.Del(key)
		case CacheTypeFeatures:
			m.features.Del(key)
		case CacheTypeTiles:
			m.tiles.Del(key)
		case CacheTypeCounts:
			m.counts.Del(key)
		}
	}
}

// InvalidateCapabilities removes capabilities cache entries matching the prefix.
func (m *Manager) InvalidateCapabilities(prefix string) {
	if m.capabilities == nil {
		return
	}
	// Ristretto doesn't support prefix deletion, so we clear the whole cache
	// This is acceptable since capabilities cache is small
	m.invalidateScope(CacheTypeCapabilities, prefix)
}

// InvalidateCollections removes collections cache entries matching the prefix.
func (m *Manager) InvalidateCollections(prefix string) {
	if m.collections == nil {
		return
	}
	m.invalidateScope(CacheTypeCollections, prefix)
}

// InvalidateFeatures removes feature cache entries matching the prefix.
func (m *Manager) InvalidateFeatures(workspaceID string) {
	if m.features == nil {
		return
	}
	// For now, clear all features cache on any invalidation
	// A more sophisticated implementation could track keys by workspace/layer
	m.invalidateScope(CacheTypeFeatures, workspaceID)
}

// InvalidateTiles removes tile cache entries.
func (m *Manager) InvalidateTiles(prefix string) {
	if m.tiles == nil {
		return
	}
	m.invalidateScope(CacheTypeTiles, prefix)
}

// InvalidateWorkspace invalidates all caches for a workspace.
func (m *Manager) InvalidateWorkspace(workspaceID string) {
	m.InvalidateCapabilities(workspaceID)
	m.InvalidateCollections(workspaceID)
	m.InvalidateFeatures(workspaceID)
	m.InvalidateTiles(workspaceID)
	m.invalidateScope(CacheTypeCounts, workspaceID)
}

// InvalidateLayer invalidates caches for a specific layer.
func (m *Manager) InvalidateLayer(workspaceID, layerID string) {
	// Invalidate capabilities and collections since layer list changed
	m.InvalidateCapabilities(workspaceID)
	m.InvalidateCollections(workspaceID)
	// Invalidate features for this layer
	m.InvalidateFeatures(workspaceID)
	// Invalidate tiles that might contain this layer
	m.InvalidateTiles(workspaceID)
}

// ClearAll clears all caches.
func (m *Manager) ClearAll() {
	m.epochMu.Lock()
	defer m.epochMu.Unlock()
	for kind, scopes := range m.epochs {
		for scope := range scopes {
			m.invalidateEpoch(kind, scope)
		}
	}
	m.epochs = nil
	if m.capabilities != nil {
		m.capabilities.Clear()
	}
	if m.collections != nil {
		m.collections.Clear()
	}
	if m.features != nil {
		m.features.Clear()
	}
	if m.tiles != nil {
		m.tiles.Clear()
	}
	if m.counts != nil {
		m.counts.Clear()
	}
	m.mu.Lock()
	m.keys = make(map[CacheType]map[string]map[string]uint64)
	m.mu.Unlock()
}

// ClearByType clears a specific cache type.
func (m *Manager) ClearByType(cacheType CacheType) {
	m.epochMu.Lock()
	defer m.epochMu.Unlock()
	for scope := range m.epochs[cacheType] {
		m.invalidateEpoch(cacheType, scope)
	}
	switch cacheType {
	case CacheTypeCapabilities:
		if m.capabilities != nil {
			m.capabilities.Clear()
		}
	case CacheTypeCollections:
		if m.collections != nil {
			m.collections.Clear()
		}
	case CacheTypeFeatures:
		if m.features != nil {
			m.features.Clear()
		}
	case CacheTypeTiles:
		if m.tiles != nil {
			m.tiles.Clear()
		}
	case CacheTypeCounts:
		if m.counts != nil {
			m.counts.Clear()
		}
	}
	m.mu.Lock()
	delete(m.keys, cacheType)
	m.mu.Unlock()
}

// GetMetrics returns current cache statistics.
func (m *Manager) GetMetrics() AllMetrics {
	metrics := AllMetrics{
		Profile:      m.config.Profile,
		Capabilities: m.cacheMetrics(CacheTypeCapabilities, m.capabilities, atomic.LoadInt64(&m.capHits), atomic.LoadInt64(&m.capMisses)),
		Collections:  m.cacheMetrics(CacheTypeCollections, m.collections, atomic.LoadInt64(&m.colHits), atomic.LoadInt64(&m.colMisses)),
		Features:     m.cacheMetrics(CacheTypeFeatures, m.features, atomic.LoadInt64(&m.featHits), atomic.LoadInt64(&m.featMisses)),
		Tiles:        m.cacheMetrics(CacheTypeTiles, m.tiles, atomic.LoadInt64(&m.tilesHits), atomic.LoadInt64(&m.tilesMisses)),
		Counts:       m.cacheMetrics(CacheTypeCounts, m.counts, atomic.LoadInt64(&m.countHits), atomic.LoadInt64(&m.countMisses)),
	}
	metrics.TotalSizeBytes = metrics.Capabilities.SizeBytes + metrics.Collections.SizeBytes + metrics.Features.SizeBytes + metrics.Tiles.SizeBytes + metrics.Counts.SizeBytes
	metrics.TotalMaxSizeBytes = metrics.Capabilities.MaxSizeBytes + metrics.Collections.MaxSizeBytes + metrics.Features.MaxSizeBytes + metrics.Tiles.MaxSizeBytes + metrics.Counts.MaxSizeBytes
	metrics.TotalInvalidationKeys = metrics.Capabilities.InvalidationKeys + metrics.Collections.InvalidationKeys + metrics.Features.InvalidationKeys + metrics.Tiles.InvalidationKeys + metrics.Counts.InvalidationKeys
	return metrics
}

func (m *Manager) cacheMetrics(kind CacheType, target *ristretto.Cache[string, *CacheEntry], hits, misses int64) Metrics {
	if target == nil {
		return Metrics{}
	}
	maximum, remaining := target.MaxCost(), target.RemainingCost()
	used := maximum - remaining
	if used < 0 {
		used = 0
	}
	result := Metrics{Hits: hits, Misses: misses, HitRate: calculateHitRate(hits, misses), SizeBytes: used, MaxSizeBytes: maximum, RemainingBytes: remaining,
		OversizedSkipped: m.oversized[kind].Load(), LoadsShared: m.loadsShared[kind].Load(), LoadErrors: m.loadErrors[kind].Load(), InvalidationKeys: m.trackedCount(kind)}
	result.TTLExpired = m.ttlExpired[kind].Load()
	if maximum > 0 {
		result.CapacityUtilization = float64(used) / float64(maximum)
	}
	if rm := target.Metrics; rm != nil {
		result.EntryCount = int64(rm.KeysAdded()) - int64(rm.KeysEvicted())
		if result.EntryCount < 0 {
			result.EntryCount = 0
		}
		result.KeysEvicted, result.BytesEvicted = rm.KeysEvicted(), rm.CostEvicted()
		result.SetsDropped, result.SetsRejected = rm.SetsDropped(), rm.SetsRejected()
		result.GetsDropped, result.GetsKept = rm.GetsDropped(), rm.GetsKept()
		if life := rm.LifeExpectancySeconds(); life != nil && life.Count > 0 {
			result.EvictionLifeSeconds = &Histogram{Bounds: life.Bounds, Count: life.Count, CountPerBucket: life.CountPerBucket, Min: life.Min, Max: life.Max, Sum: life.Sum}
		}
	}
	return result
}

func (m *Manager) trackedCount(kind CacheType) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var count int64
	for _, keys := range m.keys[kind] {
		count += int64(len(keys))
	}
	return count
}

func calculateHitRate(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// ConfigFromSettings creates a cache Config from configuration settings.
// This is a helper to bridge between the conf package and the cache package.
type ConfigSettings struct {
	Enabled                  bool
	Profile                  string
	MaxMemoryMB              int
	CounterRatio             int
	BufferItems              int
	MetricsEnabled           bool
	TTLTickSec               int
	FillCoalescingEnabled    bool
	FillTimeoutSec           int
	CapabilitiesEnabled      bool
	CapabilitiesTTLSec       int
	CollectionsEnabled       bool
	CollectionsTTLSec        int
	FeaturesEnabled          bool
	FeaturesTTLSec           int
	FeaturesMaxEntries       int
	FeaturesMaxEntrySize     int
	TilesEnabled             bool
	TilesTTLSec              int
	TilesMaxMemoryMB         int
	CapabilitiesMaxMemoryMB  int
	CapabilitiesMaxEntrySize int
	CollectionsMaxMemoryMB   int
	CollectionsMaxEntrySize  int
	FeaturesMaxMemoryMB      int
	TilesMaxEntrySize        int
	CountsEnabled            bool
	CountsTTLSec             int
	CountsMaxMemoryMB        int
}

// NewConfigFromSettings creates a Config from ConfigSettings.
func NewConfigFromSettings(s ConfigSettings) Config {
	return Config{
		Enabled: s.Enabled, Profile: s.Profile, MaxMemoryMB: int64(s.MaxMemoryMB), CounterRatio: int64(s.CounterRatio), BufferItems: int64(s.BufferItems),
		MetricsEnabled: s.MetricsEnabled, TTLTick: time.Duration(s.TTLTickSec) * time.Second, FillCoalescingEnabled: s.FillCoalescingEnabled, FillTimeout: time.Duration(s.FillTimeoutSec) * time.Second,
		Capabilities: CapabilitiesConfig{
			Enabled:      s.CapabilitiesEnabled,
			TTL:          time.Duration(s.CapabilitiesTTLSec) * time.Second,
			MaxMemoryMB:  int64(s.CapabilitiesMaxMemoryMB),
			MaxEntrySize: int64(s.CapabilitiesMaxEntrySize),
		},
		Collections: CollectionsConfig{
			Enabled:      s.CollectionsEnabled,
			TTL:          time.Duration(s.CollectionsTTLSec) * time.Second,
			MaxMemoryMB:  int64(s.CollectionsMaxMemoryMB),
			MaxEntrySize: int64(s.CollectionsMaxEntrySize),
		},
		Features: FeaturesConfig{
			Enabled:      s.FeaturesEnabled,
			TTL:          time.Duration(s.FeaturesTTLSec) * time.Second,
			MaxEntries:   int64(s.FeaturesMaxEntries),
			MaxEntrySize: int64(s.FeaturesMaxEntrySize),
			MaxMemoryMB:  int64(s.FeaturesMaxMemoryMB),
		},
		Tiles: TilesConfig{
			Enabled:      s.TilesEnabled,
			TTL:          time.Duration(s.TilesTTLSec) * time.Second,
			MaxMemoryMB:  int64(s.TilesMaxMemoryMB),
			MaxEntrySize: int64(s.TilesMaxEntrySize),
		},
		Counts: CountsConfig{Enabled: s.CountsEnabled, TTL: time.Duration(s.CountsTTLSec) * time.Second, MaxMemoryMB: int64(s.CountsMaxMemoryMB)},
	}
}
