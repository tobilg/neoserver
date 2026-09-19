package tilecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	DatabasePath        string
	EncryptionKey       string
	ObjectPrefix        string
	MaxBytes            int64
	AccessFlushInterval time.Duration
	MaintenanceInterval time.Duration
	// Ownership enables the shared-prefix single-active-node guard. Nil
	// disables it (filesystem backends, tests).
	Ownership *OwnershipConfig
}

type Manager struct {
	config       Config
	backend      BlobStore
	ownership    *ownershipGuard
	index        *metadataIndex
	jobs         JobStore
	mu           sync.Mutex
	accessMu     sync.Mutex
	accessed     map[string]time.Time
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	hits         atomic.Int64
	misses       atomic.Int64
	writes       atomic.Int64
	writeErrors  atomic.Int64
	evictions    atomic.Int64
	bytesEvicted atomic.Int64
	oversized    atomic.Int64
	orphans      atomic.Int64
}

type LifecycleOwner struct {
	WorkspaceID string `json:"workspace_id"`
	ResourceID  string `json:"resource_id,omitempty"`
}

func NewManager(ctx context.Context, cfg Config, backend BlobStore) (*Manager, error) {
	if cfg.MaxBytes <= 0 || cfg.DatabasePath == "" || backend == nil {
		return nil, errors.New("persistent tile cache requires an index, backend, and positive quota")
	}
	if cfg.AccessFlushInterval <= 0 {
		cfg.AccessFlushInterval = 30 * time.Second
	}
	if cfg.MaintenanceInterval <= 0 {
		cfg.MaintenanceInterval = time.Minute
	}
	index, err := openMetadataIndex(cfg.DatabasePath, cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	if err := backend.Health(ctx); err != nil {
		index.close()
		return nil, fmt.Errorf("persistent tile cache backend health check: %w", err)
	}
	var ownership *ownershipGuard
	if cfg.Ownership != nil {
		ownership, err = newOwnershipGuard(backend, *cfg.Ownership)
		if err != nil {
			index.close()
			return nil, err
		}
		if err := ownership.acquire(ctx); err != nil {
			index.close()
			return nil, err
		}
	}
	managerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m := &Manager{config: cfg, backend: backend, ownership: ownership, index: index, accessed: map[string]time.Time{}, ctx: managerCtx, cancel: cancel, done: make(chan struct{})}
	m.jobs = &fencedJobStore{manager: m, store: index}
	if err := m.reconcile(ctx); err != nil {
		if ownership != nil {
			ownership.release()
		}
		index.close()
		backend.Close()
		cancel()
		return nil, err
	}
	go m.maintenance()
	return m, nil
}

func (m *Manager) Get(ctx context.Context, identity Identity) ([]byte, *Entry, bool, error) {
	if err := identity.Validate(); err != nil {
		return nil, nil, false, err
	}
	key := identity.CanonicalKey()
	entry, err := m.index.getReady(ctx, key)
	if errors.Is(err, ErrBlobNotFound) {
		m.misses.Add(1)
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, normalizeMetadataError(err)
	}
	reader, err := m.backend.Get(ctx, entry.ObjectKey)
	if errors.Is(err, ErrBlobNotFound) {
		if end, fenceErr := m.beginMutation(); fenceErr == nil {
			m.mu.Lock()
			_ = m.index.remove(context.WithoutCancel(ctx), entry)
			m.mu.Unlock()
			end()
		}
		m.misses.Add(1)
		m.orphans.Add(1)
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, false, err
	}
	if m.Writable() {
		m.accessMu.Lock()
		m.accessed[key] = time.Now().UTC()
		m.accessMu.Unlock()
	}
	m.hits.Add(1)
	return data, entry, true, nil
}

func (m *Manager) Exists(ctx context.Context, identity Identity) (bool, error) {
	if err := identity.Validate(); err != nil {
		return false, err
	}
	entry, err := m.index.getReady(ctx, identity.CanonicalKey())
	if errors.Is(err, ErrBlobNotFound) {
		return false, nil
	}
	if err != nil {
		return false, normalizeMetadataError(err)
	}
	reader, err := m.backend.Get(ctx, entry.ObjectKey)
	if errors.Is(err, ErrBlobNotFound) {
		if end, fenceErr := m.beginMutation(); fenceErr == nil {
			m.mu.Lock()
			_ = m.index.remove(context.WithoutCancel(ctx), entry)
			m.mu.Unlock()
			end()
		}
		m.orphans.Add(1)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, reader.Close()
}

// Put stores a tile after enforcing resource, workspace, and global quotas.
// A false stored result is non-fatal: callers should still serve rendered data.
func (m *Manager) Put(ctx context.Context, identity Identity, content []byte, policy Policy) (bool, error) {
	if err := identity.Validate(); err != nil {
		return false, err
	}
	end, err := m.beginMutation()
	if err != nil {
		m.writeErrors.Add(1)
		return false, err
	}
	defer end()
	size := int64(len(content))
	limits := []int64{policy.ResourceQuotaBytes, policy.WorkspaceQuotaBytes, m.config.MaxBytes}
	for _, limit := range limits {
		if limit > 0 && size > limit {
			m.oversized.Add(1)
			return false, nil
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := identity.CanonicalKey()
	previous, err := m.index.getReady(ctx, key)
	if errors.Is(err, ErrBlobNotFound) {
		previous, err = nil, nil
	}
	if err != nil {
		return false, normalizeMetadataError(err)
	}
	delta := size
	if previous != nil {
		delta -= previous.SizeBytes
	}
	if delta > 0 {
		checks := []struct {
			scope, id string
			quota     int64
		}{
			{"resource", identity.WorkspaceID + ":" + identity.ResourceID, policy.ResourceQuotaBytes},
			{"workspace", identity.WorkspaceID, policy.WorkspaceQuotaBytes},
			{"global", "global", m.config.MaxBytes},
		}
		for _, check := range checks {
			if check.quota <= 0 {
				continue
			}
			if err := m.evictUntil(ctx, check.scope, check.id, identity, key, delta, check.quota); err != nil {
				m.writeErrors.Add(1)
				return false, err
			}
		}
	}
	sum := sha256.Sum256(content)
	entry := Entry{Identity: identity, CacheKey: key, ObjectKey: identity.ObjectKey(m.config.ObjectPrefix), SizeBytes: size, ETag: hex.EncodeToString(sum[:]), CreatedAt: time.Now().UTC(), LastAccessedAt: time.Now().UTC()}
	if err := m.index.markPending(ctx, entry, previous); err != nil {
		return false, normalizeMetadataError(err)
	}
	if err := m.backend.Put(ctx, entry.ObjectKey, content); err != nil {
		_ = m.index.restore(context.WithoutCancel(ctx), previous, key)
		m.writeErrors.Add(1)
		return false, err
	}
	if err := m.index.markReady(ctx, entry); err != nil {
		_ = m.backend.Delete(context.WithoutCancel(ctx), entry.ObjectKey)
		// PutObject/rename has already replaced the old payload. Restoring the
		// previous metadata here would point at bytes that no longer exist.
		_ = m.index.remove(context.WithoutCancel(ctx), &entry)
		m.writeErrors.Add(1)
		return false, normalizeMetadataError(err)
	}
	m.writes.Add(1)
	return true, nil
}

func (m *Manager) evictUntil(ctx context.Context, scope, scopeID string, identity Identity, excludeKey string, delta, quota int64) error {
	for {
		usage, err := m.index.usage(ctx, scope, scopeID)
		if err != nil {
			return normalizeMetadataError(err)
		}
		if usage.SizeBytes+delta <= quota {
			return nil
		}
		candidate, err := m.index.lru(ctx, scope, identity.WorkspaceID, identity.ResourceID, excludeKey)
		if errors.Is(err, ErrBlobNotFound) {
			return errors.New("persistent tile cache quota cannot be satisfied")
		}
		if err != nil {
			return normalizeMetadataError(err)
		}
		if err := m.backend.Delete(ctx, candidate.ObjectKey); err != nil {
			return err
		}
		if err := m.index.remove(ctx, candidate); err != nil {
			return normalizeMetadataError(err)
		}
		m.evictions.Add(1)
		m.bytesEvicted.Add(candidate.SizeBytes)
	}
}

func (m *Manager) Delete(ctx context.Context, selector Selector) (DeleteResult, error) {
	end, err := m.beginMutation()
	if err != nil {
		return DeleteResult{}, err
	}
	defer end()
	return m.delete(ctx, selector)
}

func (m *Manager) delete(ctx context.Context, selector Selector) (DeleteResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result DeleteResult
	for {
		entries, err := m.index.selectEntries(ctx, selector, 500)
		if err != nil {
			return result, normalizeMetadataError(err)
		}
		if len(entries) == 0 {
			return result, nil
		}
		keys := make([]string, len(entries))
		for index, entry := range entries {
			keys[index] = entry.ObjectKey
		}
		if err := m.backend.DeleteBatch(ctx, keys); err != nil {
			return result, err
		}
		for _, entry := range entries {
			if err := m.index.remove(ctx, entry); err != nil {
				return result, normalizeMetadataError(err)
			}
			result.Entries++
			result.Bytes += entry.SizeBytes
		}
	}
}

// DeleteIdentity removes one exact generation-aware tile, if present.
func (m *Manager) DeleteIdentity(ctx context.Context, identity Identity) (DeleteResult, error) {
	if err := identity.Validate(); err != nil {
		return DeleteResult{}, err
	}
	end, err := m.beginMutation()
	if err != nil {
		return DeleteResult{}, err
	}
	defer end()
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, err := m.index.getReady(ctx, identity.CanonicalKey())
	if errors.Is(err, ErrBlobNotFound) {
		return DeleteResult{}, nil
	}
	if err != nil {
		return DeleteResult{}, normalizeMetadataError(err)
	}
	if err := m.backend.Delete(ctx, entry.ObjectKey); err != nil {
		return DeleteResult{}, err
	}
	if err := m.index.remove(ctx, entry); err != nil {
		return DeleteResult{}, normalizeMetadataError(err)
	}
	return DeleteResult{Entries: 1, Bytes: entry.SizeBytes}, nil
}

func (m *Manager) Stats(ctx context.Context, workspaceID, resourceID string, workspaceQuota, resourceQuota int64) (Stats, error) {
	global, err := m.index.usage(ctx, "global", "global")
	if err != nil {
		return Stats{}, err
	}
	applyQuota(&global, m.config.MaxBytes)
	stats := Stats{Enabled: true, Backend: m.backend.Name(), Global: global, Hits: m.hits.Load(), Misses: m.misses.Load(), Writes: m.writes.Load(), WriteErrors: m.writeErrors.Load(), Evictions: m.evictions.Load(), BytesEvicted: m.bytesEvicted.Load(), OversizedSkipped: m.oversized.Load(), OrphansRepaired: m.orphans.Load()}
	if workspaceID != "" {
		usage, err := m.index.usage(ctx, "workspace", workspaceID)
		if err != nil {
			return Stats{}, err
		}
		quota := workspaceQuota
		if quota <= 0 {
			quota = m.config.MaxBytes
		}
		applyQuota(&usage, quota)
		stats.Workspace = &usage
	}
	if workspaceID != "" && resourceID != "" {
		usage, err := m.index.usage(ctx, "resource", workspaceID+":"+resourceID)
		if err != nil {
			return Stats{}, err
		}
		quota := resourceQuota
		if quota <= 0 {
			quota = workspaceQuota
		}
		if quota <= 0 {
			quota = m.config.MaxBytes
		}
		applyQuota(&usage, quota)
		stats.Resource = &usage
	}
	return stats, nil
}

func applyQuota(usage *Usage, quota int64) {
	usage.QuotaBytes = quota
	usage.RemainingBytes = max(0, quota-usage.SizeBytes)
	if quota > 0 {
		usage.Utilization = float64(usage.SizeBytes) / float64(quota)
	}
}

func (m *Manager) Health(ctx context.Context) error {
	return errors.Join(
		m.StorageHealth(ctx),
		wrapHealth("ownership", m.OwnershipHealth()),
	)
}

func (m *Manager) StorageHealth(ctx context.Context) error {
	return errors.Join(
		wrapHealth("metadata", m.index.health(ctx)),
		wrapHealth("backend", m.backend.Health(ctx)),
	)
}

func (m *Manager) OwnershipHealth() error {
	if m.ownership == nil {
		return nil
	}
	return m.ownership.health()
}

func wrapHealth(component string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("persistent tile cache %s: %w", component, err)
}

// Writable reports whether this process may mutate persistent cache state.
func (m *Manager) Writable() bool {
	if m.ownership == nil {
		return true
	}
	return m.ownership.health() == nil
}

func (m *Manager) beginMutation() (func(), error) {
	if m.ownership == nil {
		return func() {}, nil
	}
	return m.ownership.beginMutation()
}

// JobStore returns the durable job repository owned by this cache database.
func (m *Manager) JobStore() JobStore { return m.jobs }

// LifecycleStats reports persistent state owned by a workspace or by a set of
// resource IDs. It is used by catalog dependency reports without exposing the
// cache database itself.
func (m *Manager) LifecycleStats(ctx context.Context, workspaceID string, resourceIDs []string) (entries, bytes, jobs int64, err error) {
	if len(resourceIDs) == 0 {
		stats, statsErr := m.Stats(ctx, workspaceID, "", 0, 0)
		if statsErr != nil {
			return 0, 0, 0, statsErr
		}
		if stats.Workspace != nil {
			entries, bytes = stats.Workspace.EntryCount, stats.Workspace.SizeBytes
		}
	} else {
		for _, resourceID := range resourceIDs {
			stats, statsErr := m.Stats(ctx, workspaceID, resourceID, 0, 0)
			if statsErr != nil {
				return 0, 0, 0, statsErr
			}
			if stats.Resource != nil {
				entries += stats.Resource.EntryCount
				bytes += stats.Resource.SizeBytes
			}
		}
	}
	ids := stringSet(resourceIDs)
	items, err := m.index.lifecycleJobs(ctx, workspaceID, ids)
	if err != nil {
		return 0, 0, 0, err
	}
	return entries, bytes, int64(len(items)), nil
}

// PurgeLifecycle removes payloads, index rows, durable jobs, and job chunks
// after the job manager has quiesced the same scope.
func (m *Manager) PurgeLifecycle(ctx context.Context, workspaceID string, resourceIDs []string) (DeleteResult, int64, error) {
	end, err := m.beginMutation()
	if err != nil {
		return DeleteResult{}, 0, err
	}
	defer end()
	var result DeleteResult
	if len(resourceIDs) == 0 {
		deleted, err := m.delete(ctx, Selector{WorkspaceID: workspaceID})
		if err != nil {
			return result, 0, err
		}
		result = deleted
	} else {
		for _, resourceID := range resourceIDs {
			deleted, err := m.delete(ctx, Selector{WorkspaceID: workspaceID, ResourceID: resourceID})
			if err != nil {
				return result, 0, err
			}
			result.Entries += deleted.Entries
			result.Bytes += deleted.Bytes
		}
	}
	jobs, err := m.index.purgeLifecycleJobs(ctx, workspaceID, stringSet(resourceIDs))
	if err != nil {
		return result, jobs, err
	}
	if err := m.index.purgeLifecycleUsage(ctx, workspaceID, resourceIDs); err != nil {
		return result, jobs, err
	}
	return result, jobs, nil
}

func (m *Manager) CancelLifecycleJobs(ctx context.Context, workspaceID string, resourceIDs []string) error {
	end, err := m.beginMutation()
	if err != nil {
		return err
	}
	defer end()
	return m.index.cancelLifecycleJobs(ctx, workspaceID, stringSet(resourceIDs))
}

func (m *Manager) ActiveLifecycleJobs(ctx context.Context, workspaceID string, resourceIDs []string) (int64, error) {
	return m.index.activeLifecycleJobs(ctx, workspaceID, stringSet(resourceIDs))
}

func (m *Manager) LifecycleInventory(ctx context.Context) ([]LifecycleOwner, error) {
	owners := make(map[string]LifecycleOwner)
	rows, err := m.index.db.QueryContext(ctx, `SELECT DISTINCT workspace_id,resource_id FROM tile_entries`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var owner LifecycleOwner
		if err := rows.Scan(&owner.WorkspaceID, &owner.ResourceID); err != nil {
			rows.Close()
			return nil, err
		}
		owners[owner.WorkspaceID+"\x00"+owner.ResourceID] = owner
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	jobRows, err := m.index.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM tile_cache_jobs`)
	if err != nil {
		return nil, err
	}
	for jobRows.Next() {
		job, err := scanJob(jobRows)
		if err != nil {
			jobRows.Close()
			return nil, err
		}
		owner := LifecycleOwner{WorkspaceID: job.WorkspaceID, ResourceID: job.Request.ResourceID}
		owners[owner.WorkspaceID+"\x00"+owner.ResourceID] = owner
	}
	if err := jobRows.Close(); err != nil {
		return nil, err
	}
	result := make([]LifecycleOwner, 0, len(owners))
	for _, owner := range owners {
		result = append(result, owner)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].WorkspaceID != result[j].WorkspaceID {
			return result[i].WorkspaceID < result[j].WorkspaceID
		}
		return result[i].ResourceID < result[j].ResourceID
	})
	return result, nil
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func (m *Manager) reconcile(ctx context.Context) error {
	end, err := m.beginMutation()
	if err != nil {
		return err
	}
	defer end()
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := m.index.nonReady(ctx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		_ = m.backend.Delete(ctx, entry.ObjectKey)
		if err := m.index.remove(ctx, entry); err != nil {
			return err
		}
		m.orphans.Add(1)
	}
	return nil
}

func (m *Manager) maintenance() {
	defer close(m.done)
	accessTicker := time.NewTicker(m.config.AccessFlushInterval)
	maintenanceTicker := time.NewTicker(m.config.MaintenanceInterval)
	defer accessTicker.Stop()
	defer maintenanceTicker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			m.flushAccess(context.Background())
			return
		case <-m.ownershipLostSignal():
			return
		case <-accessTicker.C:
			m.flushAccess(m.ctx)
		case <-maintenanceTicker.C:
			_ = m.reconcile(m.ctx)
		}
	}
}

func (m *Manager) flushAccess(ctx context.Context) {
	end, err := m.beginMutation()
	if err != nil {
		return
	}
	defer end()
	m.accessMu.Lock()
	values := m.accessed
	m.accessed = map[string]time.Time{}
	m.accessMu.Unlock()
	if err := m.index.updateAccess(ctx, values); err != nil {
		m.accessMu.Lock()
		for key, value := range values {
			if current, ok := m.accessed[key]; !ok || value.After(current) {
				m.accessed[key] = value
			}
		}
		m.accessMu.Unlock()
	}
}

func (m *Manager) ownershipLostSignal() <-chan struct{} {
	if m.ownership == nil {
		return nil
	}
	return m.ownership.lostSignal()
}

func (m *Manager) Close() error {
	m.cancel()
	<-m.done
	if m.ownership != nil {
		m.ownership.release()
	}
	return errors.Join(m.backend.Close(), m.index.close())
}
