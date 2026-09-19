package wfs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/store"
)

// RuntimePersistence is the catalog-store surface the WFS runtime uses to
// make lock and version-metadata state survive restarts. It is satisfied by
// *store.DuckDBStore. A nil persistence keeps the runtime memory-only.
type RuntimePersistence interface {
	PutWFSLock(ctx context.Context, lock store.WFSLockRecord) error
	DeleteWFSLock(ctx context.Context, lockID string) error
	DeleteExpiredWFSLocks(ctx context.Context, now time.Time) error
	DeleteWorkspaceWFSLocks(ctx context.Context, workspaceID string) error
	ListWFSLocks(ctx context.Context) ([]store.WFSLockRecord, error)
	PutWFSFeatureVersion(ctx context.Context, rec store.WFSFeatureVersionRecord) error
	ListWFSFeatureVersions(ctx context.Context) ([]store.WFSFeatureVersionRecord, error)
	DeleteWFSFeatureVersions(ctx context.Context, workspaceID, layerID, featureID string) error
	DeleteWFSFeatureVersionsBelow(ctx context.Context, workspaceID, layerID, featureID string, minVersion int) error
	DeleteWorkspaceWFSFeatureVersions(ctx context.Context, workspaceID string) error
}

// RuntimeState owns bounded WFS lock and version state. The in-memory maps
// are the authoritative fast path; when persistence is configured, lock
// mutations are written through so held lockIds stay valid across a restart,
// and version metadata is recorded best-effort.
type RuntimeState struct {
	Locks    *LockStore
	Versions *VersionStore

	persistence RuntimePersistence
	logger      *slog.Logger
	stop        chan struct{}
	once        sync.Once
}

func NewRuntimeState(cfg conf.WFS, persistence RuntimePersistence, logger *slog.Logger) *RuntimeState {
	if logger == nil {
		logger = slog.Default()
	}
	state := &RuntimeState{
		Locks:       NewLockStore(cfg.MaxLockExpirySec, cfg.MaxLocksPerWorkspace, cfg.MaxLocksPerPrincipal, cfg.MaxFeaturesPerLock),
		Versions:    NewVersionStore(cfg.MaxVersionedFeatures, cfg.MaxVersionsPerFeature),
		persistence: persistence,
		logger:      logger,
		stop:        make(chan struct{}),
	}
	if persistence != nil {
		state.Locks.setPersistence(persistence, logger)
		state.Versions.setPersistence(persistence, logger)
		state.loadPersisted()
	}
	interval := time.Duration(cfg.LockCleanupIntervalSec) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				state.Locks.CleanExpired()
				if state.persistence != nil {
					if err := state.persistence.DeleteExpiredWFSLocks(context.Background(), time.Now()); err != nil {
						state.logger.Warn("prune expired persisted WFS locks", "error", err)
					}
				}
			case <-state.stop:
				return
			}
		}
	}()
	return state
}

// loadPersisted restores non-expired locks and version metadata recorded by a
// previous process. Lock quotas are not re-checked: the records were admitted
// under quota when acquired, and expiry remains enforced.
func (s *RuntimeState) loadPersisted() {
	ctx := context.Background()
	locks, err := s.persistence.ListWFSLocks(ctx)
	if err != nil {
		s.logger.Error("load persisted WFS locks", "error", err)
	} else {
		s.Locks.restore(locks)
		if len(locks) > 0 {
			s.logger.Info("restored persisted WFS locks", "count", len(locks))
		}
	}
	versions, err := s.persistence.ListWFSFeatureVersions(ctx)
	if err != nil {
		s.logger.Error("load persisted WFS feature versions", "error", err)
	} else {
		s.Versions.restore(versions)
	}
}

func (s *RuntimeState) Close() {
	if s != nil {
		s.once.Do(func() { close(s.stop) })
	}
}

func (s *RuntimeState) ClearWorkspace(workspaceID string) {
	if s == nil {
		return
	}
	s.Locks.ClearWorkspace(workspaceID)
	s.Versions.Clear(workspaceID)
	if s.persistence != nil {
		ctx := context.Background()
		if err := s.persistence.DeleteWorkspaceWFSLocks(ctx, workspaceID); err != nil {
			s.logger.Warn("delete persisted workspace WFS locks", "workspace", workspaceID, "error", err)
		}
		if err := s.persistence.DeleteWorkspaceWFSFeatureVersions(ctx, workspaceID); err != nil {
			s.logger.Warn("delete persisted workspace WFS feature versions", "workspace", workspaceID, "error", err)
		}
	}
}
