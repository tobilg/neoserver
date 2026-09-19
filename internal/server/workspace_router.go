package server

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/ogc"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/wcs"
	"github.com/tobilg/neoserver/internal/wfs"
	"github.com/tobilg/neoserver/internal/wms"
	"github.com/tobilg/neoserver/internal/wmts"
	"github.com/tobilg/neoserver/internal/workspace"
)

// WorkspaceRouter creates routes for workspace-scoped OGC services.
type WorkspaceRouter struct {
	cfg        conf.Config
	logger     *slog.Logger
	registry   *workspace.Registry
	enforcer   *rbac.Enforcer
	store      store.Store
	cache      *cache.Manager
	tileEngine *tiles.Engine
	wfsState   *wfs.RuntimeState
}

// NewWorkspaceRouter creates a new workspace router.
func NewWorkspaceRouter(cfg conf.Config, logger *slog.Logger, registry *workspace.Registry, enforcer *rbac.Enforcer, s store.Store, cacheManager *cache.Manager, engines ...*tiles.Engine) *WorkspaceRouter {
	wr := &WorkspaceRouter{
		cfg:      cfg,
		logger:   logger,
		registry: registry,
		enforcer: enforcer,
		store:    s,
		cache:    cacheManager,
	}
	if len(engines) > 0 {
		wr.tileEngine = engines[0]
	}
	if cfg.WFS.Enabled {
		// The concrete DuckDB store persists locks and version metadata so
		// they survive restarts; other store implementations fall back to
		// memory-only runtime state.
		persistence, _ := s.(wfs.RuntimePersistence)
		wr.wfsState = wfs.NewRuntimeState(cfg.WFS, persistence, logger)
	}
	return wr
}

func (wr *WorkspaceRouter) WFSState() *wfs.RuntimeState { return wr.wfsState }

// Mount mounts workspace routes on the given router.
// Routes are mounted at /workspaces/{workspaceId}/ogc/*, /workspaces/{workspaceId}/wms, /workspaces/{workspaceId}/wfs
func (wr *WorkspaceRouter) Mount(r chi.Router) {
	// Workspace-scoped routes under /workspaces/{workspaceId}
	r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
		// Load workspace into context
		r.Use(workspace.Middleware(wr.registry))

		// Mount OGC API Features
		r.Route("/ogc", func(r chi.Router) {
			r.Use(rbac.RequireServiceOperation(wr.enforcer, "ogcapi"))
			ogc.RegisterWorkspaceRoutes(r, ogc.WorkspaceDependencies{
				Config:   wr.cfg,
				Logger:   wr.logger,
				Registry: wr.registry,
				Cache:    wr.cache,
			})
		})

		// Mount WMS if enabled globally
		if wr.cfg.WMS.Enabled {
			r.Route("/wms", func(r chi.Router) {
				r.Use(rbac.RequireServiceOperation(wr.enforcer, "wms"))
				wms.RegisterWorkspaceRoutes(r, wms.WorkspaceDependencies{
					Config:   wr.cfg,
					Logger:   wr.logger,
					Registry: wr.registry,
					Cache:    wr.cache,
				})
			})
			wr.logger.Info("WMS workspace routes enabled", "path", "/workspaces/{workspaceId}/wms")
		}

		// Mount WFS if enabled globally
		if wr.cfg.WFS.Enabled {
			// Create WFS handler
			wfsDeps := wfs.WorkspaceDependencies{
				Config:   wr.cfg,
				Logger:   wr.logger,
				Registry: wr.registry,
				Cache:    wr.cache,
				Store:    wr.store,
				State:    wr.wfsState,
			}

			// Register routes for both /wfs and /wfs/ to avoid redirect issues
			// that can cause POST requests to be converted to GET
			r.Route("/wfs", func(r chi.Router) {
				r.Use(rbac.RequireServiceOperation(wr.enforcer, "wfs"))
				wfs.RegisterWorkspaceRoutes(r, wfsDeps)
			})
			wr.logger.Info("WFS workspace routes enabled", "path", "/workspaces/{workspaceId}/wfs")
		}

		// Mount OGC API Tiles if enabled globally
		if wr.cfg.Tiles.Enabled {
			r.Route("/ogc-tiles", func(r chi.Router) {
				r.Use(rbac.RequireServiceOperation(wr.enforcer, "ogc-tiles"))
				tiles.RegisterWorkspaceRoutes(r, tiles.WorkspaceDependencies{
					Config:   wr.cfg,
					Logger:   wr.logger,
					Registry: wr.registry,
					Cache:    wr.cache,
					Engine:   wr.tileEngine,
				})
			})
			wr.logger.Info("OGC API Tiles workspace routes enabled", "path", "/workspaces/{workspaceId}/ogc-tiles")
		}

		if wr.cfg.WMTS.Enabled {
			r.Route("/wmts", func(r chi.Router) {
				r.Use(rbac.RequireServiceOperation(wr.enforcer, "wmts"))
				wmts.RegisterWorkspaceRoutes(r, wmts.WorkspaceDependencies{Config: wr.cfg, Logger: wr.logger, Engine: wr.tileEngine})
			})
			wr.logger.Info("WMTS workspace routes enabled", "path", "/workspaces/{workspaceId}/wmts")
		}

		// Mount WCS 2.1/2.0 GET/KVP if enabled globally.
		if wr.cfg.WCS.Enabled {
			r.Route("/wcs", func(r chi.Router) {
				r.Use(rbac.RequireServiceOperation(wr.enforcer, "wcs"))
				wcs.RegisterWorkspaceRoutes(r, wcs.WorkspaceDependencies{Config: wr.cfg, Logger: wr.logger, Registry: wr.registry, Cache: wr.cache})
			})
			wr.logger.Info("WCS workspace routes enabled", "path", "/workspaces/{workspaceId}/wcs")
		}

	})
}

func (wr *WorkspaceRouter) Close() {
	if wr != nil && wr.wfsState != nil {
		wr.wfsState.Close()
	}
}
