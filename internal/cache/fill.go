package cache

import (
	"fmt"
	"sync/atomic"
)

type cacheEpoch struct {
	id          uint64
	invalidated atomic.Bool
}

// FillToken captures a scope's generation before reading source data. Pass it
// to a Set method after a direct read, or use LoadBytes/LoadCount which do so
// automatically. Resetting a cache invalidates existing tokens.
type FillToken struct {
	manager *Manager
	kind    CacheType
	scope   string
	epoch   *cacheEpoch
}

func (m *Manager) BeginFill(kind CacheType, key string) FillToken {
	if m == nil {
		return FillToken{}
	}
	scope := cacheScope(key)
	m.epochMu.Lock()
	defer m.epochMu.Unlock()
	if m.epochs == nil {
		m.epochs = make(map[CacheType]map[string]*cacheEpoch)
	}
	if m.epochs[kind] == nil {
		m.epochs[kind] = make(map[string]*cacheEpoch)
	}
	epoch := m.epochs[kind][scope]
	if epoch == nil {
		epoch = &cacheEpoch{id: m.generation.Add(1)}
		m.epochs[kind][scope] = epoch
	}
	return FillToken{manager: m, kind: kind, scope: scope, epoch: epoch}
}

func (t FillToken) CoalescingKey(key string) string {
	if t.epoch == nil {
		return key
	}
	return fmt.Sprintf("%s:epoch=%d", key, t.epoch.id)
}

func currentEntry(entry *CacheEntry) bool {
	return entry != nil && (entry.epoch == nil || !entry.epoch.invalidated.Load())
}

// Caller holds epochMu exclusively; no new fill can capture an old epoch.
func (m *Manager) invalidateEpoch(kind CacheType, scope string) {
	if epoch := m.epochs[kind][scope]; epoch != nil {
		epoch.invalidated.Store(true)
		delete(m.epochs[kind], scope)
	}
}
