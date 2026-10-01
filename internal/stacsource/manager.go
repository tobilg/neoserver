package stacsource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/mosaiccatalog"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/workspace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type Manager struct {
	Catalog   *staccatalog.Catalog
	Adapter   *Adapter
	registry  *workspace.Registry
	cfg       conf.STAC
	logger    *slog.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	processed metric.Int64Counter
	failures  metric.Int64Counter
	duration  metric.Float64Histogram
}

func New(ctx context.Context, cfg conf.Config, registry *workspace.Registry, mosaic *mosaiccatalog.Manager, logger *slog.Logger) (*Manager, error) {
	catalog, err := staccatalog.Open(cfg.STAC.DatabasePath, cfg.Store.EncryptionKey, cfg.STAC.MaxItems)
	if err != nil {
		return nil, err
	}
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{Catalog: catalog, Adapter: &Adapter{Config: cfg, Mosaic: mosaic}, registry: registry, cfg: cfg.STAC, logger: logger, ctx: run, cancel: cancel}
	meter := otel.Meter("github.com/tobilg/neoserver/stac")
	m.processed, _ = meter.Int64Counter("neoserver.stac.rows_processed")
	m.failures, _ = meter.Int64Counter("neoserver.stac.refresh_failures")
	m.duration, _ = meter.Float64Histogram("neoserver.stac.refresh_duration_seconds")
	workers := cfg.STAC.WorkerCount
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	m.wg.Add(1)
	go m.reconcileLoop()
	return m, nil
}
func (m *Manager) Close() error { m.cancel(); m.wg.Wait(); return m.Catalog.Close() }
func (m *Manager) Health(ctx context.Context) error {
	if err := m.ctx.Err(); err != nil {
		return err
	}
	return m.Catalog.Health(ctx)
}
func (m *Manager) worker() {
	defer m.wg.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-tick.C:
		}
		for {
			j, err := m.Catalog.ClaimJob(m.ctx)
			if errors.Is(err, staccatalog.ErrNotFound) {
				break
			}
			if err != nil {
				if m.ctx.Err() == nil {
					m.logger.Error("STAC job claim failed", "error", err)
				}
				break
			}
			started := time.Now()
			if err = m.refresh(j); err != nil {
				m.failures.Add(m.ctx, 1)
				_ = m.Catalog.SetJobStatus(context.WithoutCancel(m.ctx), j.WorkspaceID, j.ID, "failed", err.Error())
				m.logger.Warn("STAC refresh failed", "workspace", j.WorkspaceID, "collection", j.CollectionID, "job", j.ID, "error", err)
			}
			m.duration.Record(m.ctx, time.Since(started).Seconds())
		}
	}
}
func (m *Manager) refresh(j *staccatalog.Job) error {
	ws, release, ok := m.registry.AcquireByID(j.WorkspaceID)
	if !ok {
		return staccatalog.ErrNotFound
	}
	defer release()
	c, err := m.Catalog.GetCollection(m.ctx, ws.ID, j.CollectionID)
	if err != nil {
		return err
	}
	if c.Binding == nil {
		return fmt.Errorf("Collection has no source binding")
	}
	if c.Revision != j.Revision {
		return staccatalog.ErrConflict
	}
	// Feature writes are tracked per workspace and only affect vector sources.
	// Coverage refreshes rely on their fingerprint and ignore feature writes.
	readsFeatures := c.Binding.ResourceKind == "layer"
	revision, pending := ws.DataCacheState()
	if readsFeatures && pending {
		return fmt.Errorf("source write is in progress; retry refresh")
	}
	before, err := m.fingerprint(m.ctx, ws, c.Binding)
	if err != nil {
		return err
	}
	batch := make([]stacmodel.Document, 0, 200)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := m.Catalog.Stage(m.ctx, j, batch); err != nil {
			return err
		}
		m.processed.Add(m.ctx, int64(len(batch)))
		batch = batch[:0]
		return nil
	}
	doc, err := m.Adapter.Scan(m.ctx, ws, c, m.cfg.MaxItems, func(d stacmodel.Document, assets []staccatalog.LocalAsset) error {
		for _, a := range assets {
			if err := m.Catalog.BindLocalAsset(m.ctx, ws.ID, j.ID, a); err != nil {
				return err
			}
		}
		batch = append(batch, d)
		if len(batch) == cap(batch) {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err = flush(); err != nil {
		return err
	}
	current, releaseCurrent, ok := m.registry.AcquireByID(ws.ID)
	if !ok {
		return staccatalog.ErrNotFound
	}
	defer releaseCurrent()
	now, dirty := current.DataCacheState()
	after, err := m.fingerprint(m.ctx, current, c.Binding)
	if err != nil {
		return err
	}
	if (readsFeatures && (dirty || revision != now)) || before != after {
		return fmt.Errorf("source changed during refresh; retry using current publication")
	}
	return m.Catalog.PublishRefresh(m.ctx, ws.ID, j.ID, doc, before)
}

// fingerprint identifies everything a refresh of this binding reads: the bound
// source and resource, the binding, the generated service links, and the source
// data. Unrelated workspace settings and other publications are excluded so
// they do not trigger rescans.
func (m *Manager) fingerprint(ctx context.Context, ws *workspace.Workspace, b *staccatalog.Binding) (string, error) {
	s, l, c := Resolve(ws, b)
	if s == nil {
		return "", fmt.Errorf("source publication is missing or disabled")
	}
	generation := int64(0)
	if m.Adapter.Mosaic != nil && string(s.Type) == "raster_mosaic" {
		var err error
		generation, err = m.Adapter.Mosaic.Generation(ctx, ws.ID, s.ID)
		if err != nil {
			return "", err
		}
	}
	// Feature writes are only tracked per workspace, so vector bindings follow
	// the workspace data revision. Coverages are not changed by feature writes.
	revision := int64(0)
	if l != nil {
		revision, _ = ws.DataCacheState()
	}
	var links [4]bool
	if ws.Settings != nil {
		links = [4]bool{ws.Settings.OGCAPI.Enabled, ws.Settings.WFS.Enabled, ws.Settings.WCS.Enabled, ws.Settings.WMS.Enabled}
	}
	raw, err := json.Marshal([]any{m.Adapter.Base(ws), s.ID, s.Type, s.ConnectionInfo, l, c, b, links, revision, generation})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum), nil
}
func (m *Manager) reconcileLoop() {
	defer m.wg.Done()
	changes, unsubscribe := m.registry.SubscribePublicationChanges()
	defer unsubscribe()
	tick := time.NewTicker(60 * time.Second)
	defer tick.Stop()
	for {
		if err := m.reconcile(); err != nil && m.ctx.Err() == nil {
			m.logger.Warn("STAC reconciliation failed", "error", err)
		}
		select {
		case <-m.ctx.Done():
			return
		case <-tick.C:
		case <-changes:
		}
	}
}
func (m *Manager) reconcile() error {
	if err := m.Catalog.Prune(m.ctx); err != nil {
		return err
	}
	owners, err := m.Catalog.WorkspaceIDs(m.ctx)
	if err != nil {
		return err
	}
	for _, id := range owners {
		ws, release, ok := m.registry.AcquireByID(id)
		if !ok {
			err = m.deleteIfRemoved(id, "", "", func() error { return m.Catalog.DeleteWorkspace(m.ctx, id) })
		} else {
			err = m.reconcileWorkspace(ws)
			release()
		}
		// One workspace's failure must not stop reconciliation of the others.
		if err != nil && m.ctx.Err() == nil {
			m.logger.Warn("STAC workspace reconciliation failed", "workspace", id, "error", err)
		}
	}
	return nil
}

// deleteIfRemoved deletes STAC state only when the catalog confirms that its
// source was deleted. A publication that is merely absent from the runtime
// registry stays hidden, because Visible resolves it through the registry.
func (m *Manager) deleteIfRemoved(workspaceID, kind, resourceID string, remove func() error) error {
	deleted, err := m.registry.PublicationDeleted(m.ctx, workspaceID, kind, resourceID)
	if err != nil || !deleted {
		return err
	}
	if err = remove(); err != nil && !errors.Is(err, staccatalog.ErrNotFound) {
		return err
	}
	return nil
}
func (m *Manager) reconcileWorkspace(ws *workspace.Workspace) error {
	collections, err := m.Catalog.Collections(m.ctx, ws.ID)
	if err != nil {
		return err
	}
	jobs, err := m.Catalog.LatestRefreshJobs(m.ctx, ws.ID)
	if err != nil {
		return err
	}
	latest := map[string]*staccatalog.Job{}
	for _, j := range jobs {
		if j.Kind == "refresh" && latest[j.CollectionID] == nil {
			latest[j.CollectionID] = j
		}
	}
	for _, c := range collections {
		if c.Binding == nil {
			continue
		}
		id := c.Document.String("id")
		fingerprint, err := m.fingerprint(m.ctx, ws, c.Binding)
		if err != nil {
			// The source is missing, disabled or unreadable. Delete the
			// Collection only once the catalog confirms the source is gone.
			if err = m.deleteIfRemoved(ws.ID, c.Binding.ResourceKind, c.Binding.ResourceID, func() error { return m.Catalog.DeleteCollection(m.ctx, ws.ID, id) }); err != nil {
				m.logger.Warn("STAC source removal check failed", "workspace", ws.ID, "collection", id, "error", err)
			}
			continue
		}
		interval := c.Binding.RefreshIntervalSec
		if interval == 0 {
			interval = m.cfg.RefreshIntervalSec
		}
		if interval < 60 {
			interval = 900
		}
		j := latest[id]
		// A change is scheduled once per fingerprint. A failed attempt at the
		// same fingerprint is retried by the failure backoff below.
		changed := fingerprint != c.SourceFingerprint && (j == nil || j.Request.String("fingerprint") != fingerprint)
		due := j == nil || ((j.Status == "succeeded" || j.Status == "cancelled") && time.Since(j.UpdatedAt) >= time.Duration(interval)*time.Second) || (j.Status == "failed" && time.Since(j.UpdatedAt) >= 60*time.Second)
		if changed || due {
			if _, err = m.Catalog.CreateJob(m.ctx, ws.ID, id, "refresh", "queued", stacmodel.Document{"fingerprint": fingerprint}); err != nil {
				m.logger.Warn("STAC refresh scheduling failed", "workspace", ws.ID, "collection", id, "error", err)
			}
		}
	}
	return nil
}

// Lifecycle methods make recursive deletions include independent Collections.
func (m *Manager) LifecycleStats(ctx context.Context, ws string, services []string) (int64, int64, error) {
	collections, err := m.Catalog.Collections(ctx, ws)
	if err != nil {
		return 0, 0, err
	}
	var count, items int64
	for _, c := range collections {
		if matchesServices(c, services) {
			count++
			items += c.ItemCount
		}
	}
	return count, items, nil
}
func matchesServices(c *staccatalog.Collection, services []string) bool {
	if services == nil {
		return true
	}
	if c.Binding == nil {
		return false
	}
	for _, id := range services {
		if c.Binding.ServiceID == id {
			return true
		}
	}
	return false
}
func (m *Manager) QuiesceAndDeleteLifecycle(ctx context.Context, ws string, services []string) error {
	if services == nil {
		return m.Catalog.DeleteWorkspace(ctx, ws)
	}
	collections, err := m.Catalog.Collections(ctx, ws)
	if err != nil {
		return err
	}
	for _, c := range collections {
		if matchesServices(c, services) {
			if err = m.Catalog.DeleteCollection(ctx, ws, c.Document.String("id")); err != nil && !errors.Is(err, staccatalog.ErrNotFound) {
				return err
			}
		}
	}
	return nil
}
