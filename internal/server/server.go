package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/tobilg/neoserver/internal/admin"
	internalaudit "github.com/tobilg/neoserver/internal/audit"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/cataloglifecycle"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	duckdbsource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/datasource/rastermosaic"
	"github.com/tobilg/neoserver/internal/httputil"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/importer"
	"github.com/tobilg/neoserver/internal/mgmt"
	"github.com/tobilg/neoserver/internal/mosaiccatalog"
	"github.com/tobilg/neoserver/internal/protocolrequest"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/tilejobs"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	// Import datasource implementations to register their factory functions
	_ "github.com/tobilg/neoserver/internal/datasource/geoparquet"
	_ "github.com/tobilg/neoserver/internal/datasource/postgis"
	_ "github.com/tobilg/neoserver/internal/datasource/rasterfile"
	_ "github.com/tobilg/neoserver/internal/datasource/vectorfile"
)

type Server struct {
	cfg             conf.Config
	logger          *slog.Logger
	http            *http.Server
	store           *store.DuckDBStore
	registry        *workspace.Registry
	cache           *cache.Manager
	tileCache       *tilecache.Manager
	tileEngine      *tiles.Engine
	tileJobs        *tilejobs.Manager
	mosaicCatalog   *mosaiccatalog.Manager
	lifecycle       *cataloglifecycle.Coordinator
	importer        *importer.Manager
	audit           *internalaudit.Manager
	workspaceRouter *WorkspaceRouter
}

type newOptions struct {
	tileCacheTakeoverOwnerID string
}

// Option supplies ephemeral process-start behavior that must not be persisted
// in the server configuration.
type Option func(*newOptions)

func WithTileCacheTakeoverOwner(ownerID string) Option {
	return func(options *newOptions) { options.tileCacheTakeoverOwnerID = ownerID }
}

func New(ctx context.Context, cfg conf.Config, logger *slog.Logger, s *store.DuckDBStore, supplied ...Option) (*Server, error) {
	var options newOptions
	for _, apply := range supplied {
		apply(&options)
	}
	if options.tileCacheTakeoverOwnerID != "" && (!cfg.PersistentCache.Enabled || !strings.EqualFold(strings.TrimSpace(cfg.PersistentCache.Backend), "s3") || !cfg.PersistentCache.S3.OwnershipEnabled) {
		return nil, errors.New("--take-over-tile-cache-owner requires an enabled S3 persistent cache with ownership enabled")
	}
	rastermosaic.SetMaxGranules(cfg.Datasource.MosaicMaxGranules)
	rastermosaic.SetMaxRenderGranules(cfg.WMS.MaxMosaicGranulesPerRender)
	// Configure the datasource path/URL allowlist (guards against LFI/SSRF via
	// admin-supplied file paths). Deny-by-default: only allowlisted globs are readable.
	if err := pathpolicy.ConfigureWithOptions(cfg.Datasource.AllowedPaths, pathpolicy.Options{
		RemoteCachePath: cfg.Datasource.RemoteCachePath,
		RemoteMaxBytes:  cfg.Datasource.RemoteMaxBytes,
		RemoteTimeout:   time.Duration(cfg.Datasource.RemoteTimeoutSec) * time.Second,
	}); err != nil {
		return nil, fmt.Errorf("configure datasource path policy: %w", err)
	}

	// Preload DuckDB extensions (spatial, httpfs) to avoid slow first requests
	if err := datasource.PreloadExtensions(ctx, logger); err != nil {
		logger.Warn("failed to preload DuckDB extensions", "error", err)
		// Continue anyway - extensions will be loaded on first use
	}

	// Initialize cache manager
	cacheConfig := toCacheConfig(cfg.Cache)
	cacheManager, err := cache.NewManager(cacheConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cache manager: %w", err)
	}
	if cacheConfig.Enabled {
		logger.Info("cache manager initialized",
			"capabilities_enabled", cacheConfig.Capabilities.Enabled,
			"collections_enabled", cacheConfig.Collections.Enabled,
			"features_enabled", cacheConfig.Features.Enabled,
			"tiles_enabled", cacheConfig.Tiles.Enabled,
		)
	}
	records, err := s.ListTileMatrixSets(ctx)
	if err != nil {
		cacheManager.Close()
		return nil, fmt.Errorf("load custom tile matrix sets: %w", err)
	}
	definitions := make([]*tiles.TileMatrixSetDefinition, 0, len(records))
	for _, record := range records {
		var definition tiles.TileMatrixSetDefinition
		if err := json.Unmarshal(record.Definition, &definition); err != nil {
			cacheManager.Close()
			return nil, fmt.Errorf("decode custom tile matrix set %q: %w", record.ID, err)
		}
		definitions = append(definitions, &definition)
	}
	if err := tiles.ReplaceCustomTileMatrixSets(definitions); err != nil {
		cacheManager.Close()
		return nil, fmt.Errorf("configure custom tile matrix sets: %w", err)
	}
	var auditManager *internalaudit.Manager
	if cfg.Audit.Enabled {
		auditManager, err = internalaudit.Open(ctx, cfg.Audit, cfg.Store.EncryptionKey, logger, s)
		if err != nil {
			cacheManager.Close()
			return nil, fmt.Errorf("initialize durable audit log: %w", err)
		}
	}

	var persistentTileCache *tilecache.Manager
	if cfg.PersistentCache.Enabled {
		persistentTileCache, err = newPersistentTileCache(ctx, cfg.PersistentCache, cfg.Store.EncryptionKey, options.tileCacheTakeoverOwnerID)
		if err != nil {
			if auditManager != nil {
				_ = auditManager.Close(context.Background())
			}
			cacheManager.Close()
			return nil, fmt.Errorf("failed to initialize persistent tile cache: %w", err)
		}
		logger.Info("persistent tile cache initialized", "backend", cfg.PersistentCache.Backend, "max_bytes", cfg.PersistentCache.MaxBytes)
	}
	tileEngine := tiles.NewEngine(cfg, logger, cacheManager, persistentTileCache)
	var mosaicManager *mosaiccatalog.Manager
	if cfg.MosaicCatalog.Enabled {
		mosaicManager, err = mosaiccatalog.Open(ctx, cfg.MosaicCatalog, cfg.Store.EncryptionKey)
		if err != nil {
			if persistentTileCache != nil {
				_ = persistentTileCache.Close()
			}
			if auditManager != nil {
				_ = auditManager.Close(context.Background())
			}
			cacheManager.Close()
			return nil, fmt.Errorf("failed to initialize mosaic catalog: %w", err)
		}
		rastermosaic.SetCatalog(mosaicManager)
		logger.Info("mosaic catalog initialized", "database", cfg.MosaicCatalog.DatabasePath)
	}
	// Initialize workspace registry with data source factory
	duckdbsource.ConfigureManagedResolver(managedAssetResolver(s))
	registry := workspace.NewRegistry(s, datasource.CreateFromService)
	registry.SetCacheManager(cacheManager)
	var tileJobManager *tilejobs.Manager
	cleanupTileResources := func() {
		if tileJobManager != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = tileJobManager.Close(cleanupCtx)
			cancel()
		}
		if mosaicManager != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = mosaicManager.Close(cleanupCtx)
			cancel()
			rastermosaic.SetCatalog(nil)
		}
		registry.Close()
		if persistentTileCache != nil {
			_ = persistentTileCache.Close()
		}
		cacheManager.Close()
		if auditManager != nil {
			_ = auditManager.Close(context.Background())
		}
	}
	if err := s.RecoverDataWrites(ctx); err != nil {
		cleanupTileResources()
		return nil, fmt.Errorf("recover data cache barriers: %w", err)
	}
	if cfg.Importer.Enabled {
		if err := importer.ReconcileManagedPaths(ctx, cfg.Importer, s); err != nil {
			cleanupTileResources()
			return nil, fmt.Errorf("reconcile managed import paths: %w", err)
		}
	}

	// Load existing workspaces from store
	if err := registry.Load(ctx); err != nil {
		cleanupTileResources()
		return nil, fmt.Errorf("failed to load workspaces: %w", err)
	}
	logger.Info("workspace registry loaded", "workspaces", len(registry.List()))
	if mosaicManager != nil {
		if err := mosaicManager.Start(registry); err != nil {
			cleanupTileResources()
			return nil, fmt.Errorf("failed to start mosaic harvest jobs: %w", err)
		}
	}

	if persistentTileCache != nil {
		tileJobManager, err = tilejobs.NewManager(ctx, cfg.PersistentCache.Jobs, cfg.Tiles, persistentTileCache.JobStore(), registry, tileEngine, persistentTileCache, cacheManager)
		if err != nil {
			cleanupTileResources()
			return nil, fmt.Errorf("failed to initialize tile cache jobs: %w", err)
		}
	}

	// Load the shared authorization model from the encrypted catalog so custom
	// service and operation grants survive restart.
	rbacAdapter := rbac.NewStoreAdapter(s)
	enforcer, err := rbac.NewEnforcerWithDefaults(rbacAdapter)
	if err != nil {
		cleanupTileResources()
		return nil, fmt.Errorf("failed to create RBAC enforcer: %w", err)
	}
	wsRouter := NewWorkspaceRouter(cfg, logger, registry, enforcer, s, cacheManager, tileEngine)
	lifecycleDeps := cataloglifecycle.Dependencies{
		Catalog: s, Deletions: s, Registry: registry, Cache: cacheManager,
		Enforcer: enforcer, WFSState: wsRouter.WFSState(), StyleAssetRoot: cfg.WMS.StyleAssetPath,
		ManagedImports: s, Logger: logger,
	}
	// Optional concrete pointers must not become non-nil interface values when
	// their subsystem is disabled (integrity/deletion call these interfaces).
	if persistentTileCache != nil {
		lifecycleDeps.TileCache = persistentTileCache
	}
	if tileJobManager != nil {
		lifecycleDeps.TileJobs = tileJobManager
	}
	if mosaicManager != nil {
		lifecycleDeps.Mosaic = mosaicManager
	}
	lifecycle, err := cataloglifecycle.New(ctx, lifecycleDeps)
	if err != nil {
		wsRouter.Close()
		cleanupTileResources()
		return nil, fmt.Errorf("failed to initialize catalog lifecycle: %w", err)
	}
	if err := lifecycle.Recover(ctx); err != nil {
		lifecycle.Close()
		wsRouter.Close()
		cleanupTileResources()
		return nil, fmt.Errorf("failed to recover catalog deletions: %w", err)
	}

	var importManager *importer.Manager
	if cfg.Importer.Enabled {
		importManager, err = importer.New(ctx, cfg.Importer, s, registry, lifecycle, logger)
		if err != nil {
			lifecycle.Close()
			wsRouter.Close()
			cleanupTileResources()
			return nil, fmt.Errorf("failed to initialize importer: %w", err)
		}
		logger.Info("durable importer initialized", "workers", cfg.Importer.WorkerCount, "max_concurrent_jobs", cfg.Importer.MaxConcurrentJobs, "root", cfg.Importer.Root)
	}

	// Build router
	r := chi.NewRouter()
	transportSecurity, err := identity.TransportSecurity(identity.TransportConfig{
		RequireHTTPS:       cfg.Auth.RequireHTTPS,
		TrustedProxyCIDRs:  cfg.Server.TrustedProxyCIDRs,
		AllowAPIKeyInQuery: cfg.Auth.AllowAPIKeyInQuery,
	})
	if err != nil {
		if importManager != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = importManager.Close(closeCtx)
			cancel()
		}
		lifecycle.Close()
		wsRouter.Close()
		cleanupTileResources()
		return nil, fmt.Errorf("configure transport security: %w", err)
	}

	r.Use(middleware.RequestID)
	r.Use(httputil.ManagementErrorScope("/api/v1"))
	r.Use(transportSecurity)
	r.Use(middleware.Recoverer)
	importUploadLimit := int64(0)
	if cfg.Importer.Enabled {
		importUploadLimit = cfg.Importer.MaxUploadBytes
	}
	r.Use(limitBody(cfg.Server.MaxBodyBytes, importUploadLimit))
	r.Use(protocolrequest.Middleware(cfg.Server.BasePath))
	r.Use(middleware.Compress(5))
	r.Use(securityHeaders)

	// CORS. The default allowed-origins is "*" (configurable via Server.CORSOrigins).
	// Credentialed cross-origin fetch is disabled. Browser sessions separately
	// require operation-aware CSRF protection, including mutating GET operations.
	// Do NOT set AllowCredentials: true while origins may
	// be "*" — pair credentials only with an explicit origin allowlist.
	corsOrigins := splitComma(cfg.Server.CORSOrigins)
	allowCredentials := false
	for _, o := range corsOrigins {
		if o == "*" && allowCredentials {
			// Guard against a future misconfiguration that would be unsafe.
			panic("CORS misconfiguration: wildcard origin cannot be combined with credentials")
		}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "HEAD", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Origin", "X-API-Key"},
		ExposedHeaders:   []string{"Content-Length", "Content-Type"},
		AllowCredentials: allowCredentials,
		MaxAge:           300,
	}))

	// Liveness and core dependency readiness (no auth).
	var oidcValidator *identity.RecoveringOIDCValidator
	if oidcConfig := toOIDCConfig(cfg.Auth.OIDC); oidcConfig != nil {
		oidcValidator = identity.NewRecoveringOIDCValidator(s, *oidcConfig)
	}
	r.Method(http.MethodGet, "/health", HealthHandler())
	r.Method(http.MethodHead, "/health", HealthHandler())
	readinessChecks := []readinessCheck{
		{name: "catalog", enabled: true, check: s.Health},
		{name: "oidc", enabled: oidcValidator != nil, check: func(ctx context.Context) error { return oidcValidator.Health(ctx) }},
		{name: "tile_cache", enabled: persistentTileCache != nil, check: func(ctx context.Context) error { return persistentTileCache.StorageHealth(ctx) }},
		{name: "tile_cache_lease", enabled: persistentTileCache != nil && strings.EqualFold(cfg.PersistentCache.Backend, "s3") && cfg.PersistentCache.S3.OwnershipEnabled, check: func(context.Context) error { return persistentTileCache.OwnershipHealth() }},
		{name: "tile_jobs", enabled: tileJobManager != nil, check: func(ctx context.Context) error { return tileJobManager.Health(ctx) }},
		{name: "mosaic_catalog", enabled: mosaicManager != nil, check: func(ctx context.Context) error { return mosaicManager.Health(ctx) }},
		{name: "catalog_lifecycle", enabled: true, check: lifecycle.Health},
		{name: "imports", enabled: importManager != nil, check: func(ctx context.Context) error { return importManager.Health(ctx) }},
		{name: "audit", enabled: auditManager != nil, check: func(ctx context.Context) error { return auditManager.Health(ctx) }},
	}
	readyHandler := ReadinessHandler(logger, readinessChecks)
	r.Method(http.MethodGet, "/ready", readyHandler)
	r.Method(http.MethodHead, "/ready", readyHandler)

	// Server-level static/basic auth credentials, enabled per configured method.
	if cfg.WFS.AllowAnonymousMutations {
		logger.Warn("WFS anonymous mutations are enabled; public WFS workspaces allow unauthenticated transactions, locking, and stored-query management")
	}

	var staticAPIKey string
	var basicAuthUsers map[string]string
	if cfg.Auth.Enabled {
		switch cfg.Auth.Method {
		case "apikey":
			staticAPIKey = cfg.Auth.ApiKey
		case "basic":
			basicAuthUsers = cfg.Auth.Users
		}
	}

	// Least-privilege guard: static-key / basic-auth principals are granted
	// Auth.DefaultRole globally. If that role is super_admin, a single shared
	// credential becomes a full global admin (including the management API), so
	// surface it loudly rather than letting it pass silently.
	if cfg.Auth.DefaultRole == "super_admin" && (staticAPIKey != "" || len(basicAuthUsers) > 0) {
		logger.Warn("Auth.DefaultRole=super_admin grants full global admin to static-apikey/basic-auth principals; " +
			"set Auth.DefaultRole to a least-privilege role (e.g. viewer) unless this is intentional")
	}

	// Identity middleware for all authenticated routes
	identityMiddleware := identity.Middleware(identity.MiddlewareConfig{
		Store:              s,
		Logger:             logger,
		OIDCConfig:         toOIDCConfig(cfg.Auth.OIDC),
		OIDCValidator:      oidcValidator,
		RequireAuth:        false, // Let individual handlers decide
		StaticAPIKey:       staticAPIKey,
		BasicAuthUsers:     basicAuthUsers,
		DefaultRole:        cfg.Auth.DefaultRole,
		AllowAPIKeyInQuery: cfg.Auth.AllowAPIKeyInQuery,
		Session: &identity.SessionConfig{
			IdleTimeout: time.Duration(cfg.Auth.Session.IdleTimeoutSec) * time.Second,
		},
	})

	// Management API at /api/v1
	r.Route("/api/v1", func(r chi.Router) {
		if auditManager != nil {
			r.Use(auditManager.Middleware)
		}
		r.Use(identityMiddleware)
		mgmt.RegisterRoutes(r, mgmt.Dependencies{
			Config:     cfg,
			Store:      s,
			Registry:   registry,
			Enforcer:   enforcer,
			Logger:     logger,
			Cache:      cacheManager,
			TileCache:  persistentTileCache,
			TileEngine: tileEngine,
			TileJobs:   tileJobManager,
			Mosaic:     mosaicManager,
			Lifecycle:  lifecycle,
			Importer:   importManager,
			Audit:      auditManager,
		})
		if cfg.Observability.Pprof.Enabled {
			r.Group(func(r chi.Router) {
				r.Use(identity.RequireSecureTransport(cfg.Auth.RequireHTTPS))
				r.Use(identity.RequireSuperAdmin)
				r.Get("/debug/pprof/", pprof.Index)
				r.Get("/debug/pprof/cmdline", pprof.Cmdline)
				r.Get("/debug/pprof/profile", pprof.Profile)
				r.Post("/debug/pprof/symbol", pprof.Symbol)
				r.Get("/debug/pprof/symbol", pprof.Symbol)
				r.Get("/debug/pprof/trace", pprof.Trace)
				r.Get("/debug/pprof/{profile}", func(w http.ResponseWriter, req *http.Request) {
					pprof.Handler(chi.URLParam(req, "profile")).ServeHTTP(w, req)
				})
			})
		}
	})

	// Workspace-scoped OGC routes at /workspaces/{workspaceId}
	r.Group(func(r chi.Router) {
		if auditManager != nil {
			r.Use(auditManager.Middleware)
		}
		r.Use(identityMiddleware)
		wsRouter.Mount(r)
	})
	if cfg.Server.AdminUI && !cfg.Server.DisableUI {
		admin.RegisterRoutes(r, cfg)
	}

	// Apply base path if configured
	basePath := cfg.Server.BasePath
	if basePath != "" {
		wrapped := chi.NewRouter()
		wrapped.Mount(basePath, r)
		r = wrapped
	}

	addr := net.JoinHostPort(cfg.Server.HttpHost, fmt.Sprintf("%d", cfg.Server.HttpPort))
	var handler http.Handler = r
	if cfg.Observability.OTel.Enabled {
		handler = otelhttp.NewHandler(handler, "neoserver.http")
	}
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout:      time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &Server{
		cfg:             cfg,
		logger:          logger,
		http:            httpServer,
		store:           s,
		registry:        registry,
		cache:           cacheManager,
		tileCache:       persistentTileCache,
		tileEngine:      tileEngine,
		tileJobs:        tileJobManager,
		mosaicCatalog:   mosaicManager,
		lifecycle:       lifecycle,
		importer:        importManager,
		audit:           auditManager,
		workspaceRouter: wsRouter,
	}, nil
}

func (s *Server) Start() error {
	s.logger.Info("http server starting", "addr", s.http.Addr, "basePath", s.cfg.Server.BasePath)
	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	defer cancelCleanup()
	if interval := time.Duration(s.cfg.Auth.Session.CleanupIntervalSec) * time.Second; interval > 0 {
		go s.cleanupBrowserSessions(cleanupCtx, interval)
	}
	err := s.http.ListenAndServe()
	if err == nil || err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) cleanupBrowserSessions(ctx context.Context, interval time.Duration) {
	cleanup := func() {
		if _, err := s.store.DeleteExpiredBrowserSessions(ctx, time.Now().UTC()); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Warn("failed to clean up browser sessions", "error", err)
		}
	}
	cleanup()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("http server shutting down")
	err := s.http.Shutdown(ctx)
	if s.importer != nil {
		jobsCtx := ctx
		if timeout := time.Duration(s.cfg.Importer.ShutdownTimeoutSec) * time.Second; timeout > 0 {
			var cancel context.CancelFunc
			jobsCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		_ = s.importer.Close(jobsCtx)
	}
	if s.audit != nil {
		auditCtx := ctx
		if timeout := time.Duration(s.cfg.Audit.ShutdownTimeoutSec) * time.Second; timeout > 0 {
			var cancel context.CancelFunc
			auditCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		if closeErr := s.audit.Close(auditCtx); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close audit manager: %w", closeErr))
		}
	}
	if s.lifecycle != nil {
		s.lifecycle.Close()
	}
	if s.tileJobs != nil {
		jobsCtx := ctx
		if timeout := time.Duration(s.cfg.PersistentCache.Jobs.ShutdownTimeoutSec) * time.Second; timeout > 0 {
			var cancel context.CancelFunc
			jobsCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		_ = s.tileJobs.Close(jobsCtx)
	}
	if s.mosaicCatalog != nil {
		jobsCtx := ctx
		if timeout := time.Duration(s.cfg.MosaicCatalog.ShutdownTimeoutSec) * time.Second; timeout > 0 {
			var cancel context.CancelFunc
			jobsCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		_ = s.mosaicCatalog.Close(jobsCtx)
		rastermosaic.SetCatalog(nil)
	}
	if s.registry != nil {
		s.registry.Close()
	}
	if s.cache != nil {
		s.cache.Close()
	}
	if s.tileCache != nil {
		_ = s.tileCache.Close()
	}
	if s.workspaceRouter != nil {
		s.workspaceRouter.Close()
	}
	return err
}

// Cache returns the cache manager.
func (s *Server) Cache() *cache.Manager {
	return s.cache
}

func splitComma(s string) []string {
	parts := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return []string{"*"}
	}
	return parts
}

func toOIDCConfig(cfg conf.OIDCConfig) *identity.OIDCConfig {
	if cfg.IssuerURL == "" {
		return nil
	}
	return &identity.OIDCConfig{
		IssuerURL:       cfg.IssuerURL,
		ClientID:        cfg.ClientID,
		RequiredScopes:  cfg.RequiredScopes,
		SkipIssuerCheck: cfg.SkipIssuerCheck,
		GroupClaims:     cfg.GroupClaims,
	}
}

// toCacheConfig converts application cache configuration to cache.Config.
func toCacheConfig(c conf.Cache) cache.Config {
	return cache.NewConfigFromSettings(cache.ConfigSettings{
		Enabled:                  c.Enabled,
		Profile:                  c.Profile,
		MaxMemoryMB:              c.MaxMemoryMB,
		CounterRatio:             c.CounterRatio,
		BufferItems:              c.BufferItems,
		MetricsEnabled:           c.MetricsEnabled,
		TTLTickSec:               c.TTLTickSec,
		FillCoalescingEnabled:    c.FillCoalescingEnabled,
		FillTimeoutSec:           c.FillTimeoutSec,
		CapabilitiesEnabled:      c.Capabilities.Enabled,
		CapabilitiesTTLSec:       c.Capabilities.TTLSec,
		CapabilitiesMaxMemoryMB:  c.Capabilities.MaxMemoryMB,
		CapabilitiesMaxEntrySize: c.Capabilities.MaxEntrySize,
		CollectionsEnabled:       c.Collections.Enabled,
		CollectionsTTLSec:        c.Collections.TTLSec,
		CollectionsMaxMemoryMB:   c.Collections.MaxMemoryMB,
		CollectionsMaxEntrySize:  c.Collections.MaxEntrySize,
		FeaturesEnabled:          c.Features.Enabled,
		FeaturesTTLSec:           c.Features.TTLSec,
		FeaturesMaxEntries:       c.Features.MaxEntries,
		FeaturesMaxEntrySize:     c.Features.MaxEntrySize,
		FeaturesMaxMemoryMB:      c.Features.MaxMemoryMB,
		TilesEnabled:             c.Tiles.Enabled,
		TilesTTLSec:              c.Tiles.TTLSec,
		TilesMaxMemoryMB:         c.Tiles.MaxMemoryMB,
		TilesMaxEntrySize:        c.Tiles.MaxEntrySize,
		CountsEnabled:            c.Counts.Enabled, CountsTTLSec: c.Counts.TTLSec, CountsMaxMemoryMB: c.Counts.MaxMemoryMB,
	})
}

// limitBody caps the size of incoming request bodies to guard against
// memory-exhaustion denial of service. A non-positive limit disables the cap.
func limitBody(maxBytes, importMaxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestLimit := maxBytes
			if isImportUpload(r) && importMaxBytes > requestLimit {
				// Multipart framing is small but not fixed, so leave a bounded margin
				// beyond the importer's independently enforced file-byte limit.
				requestLimit = importMaxBytes + 1<<20
			}
			if requestLimit > 0 && r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, requestLimit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isImportUpload(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return false
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	const marker = "/api/v1/workspaces/"
	index := strings.LastIndex(path, marker)
	if index < 0 {
		return false
	}
	parts := strings.Split(path[index+len(marker):], "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] == "imports"
}

// securityHeaders adds security-related HTTP headers to all responses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Prevent clickjacking attacks
		w.Header().Set("X-Frame-Options", "DENY")

		// Enable XSS filter in browsers (legacy, but still useful)
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Control referrer information
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Prevent caching of sensitive responses by default
		// Individual handlers can override this for cacheable content
		if w.Header().Get("Cache-Control") == "" {
			w.Header().Set("Cache-Control", "no-store")
		}

		// Content Security Policy - restrictive default
		// Allows inline styles for map rendering, restricts scripts to self
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'")

		// Note: Strict-Transport-Security (HSTS) should only be set when
		// the server is behind HTTPS. It's typically set by a reverse proxy
		// or load balancer in production. Uncomment the following line if
		// the server always runs behind HTTPS:
		// w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		next.ServeHTTP(w, r)
	})
}
