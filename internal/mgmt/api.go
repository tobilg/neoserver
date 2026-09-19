// Package mgmt provides the REST management API for neoserver.
package mgmt

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/audit"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/cataloglifecycle"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/httputil"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/importer"
	"github.com/tobilg/neoserver/internal/mosaiccatalog"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/tilejobs"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

// Dependencies for the management API handlers.
type Dependencies struct {
	Config     conf.Config
	Store      store.Store
	Registry   *workspace.Registry
	Enforcer   *rbac.Enforcer
	Logger     *slog.Logger
	Cache      *cache.Manager
	TileCache  *tilecache.Manager
	TileEngine *tiles.Engine
	TileJobs   *tilejobs.Manager
	Mosaic     *mosaiccatalog.Manager
	Lifecycle  *cataloglifecycle.Coordinator
	Importer   *importer.Manager
	Audit      *audit.Manager
}

// handler contains all management API handlers.
type handler struct {
	cfg        conf.Config
	store      store.Store
	registry   *workspace.Registry
	enforcer   *rbac.Enforcer
	logger     *slog.Logger
	cache      *cache.Manager
	tileCache  *tilecache.Manager
	tileEngine *tiles.Engine
	tileJobs   *tilejobs.Manager
	mosaic     *mosaiccatalog.Manager
	lifecycle  *cataloglifecycle.Coordinator
	importer   *importer.Manager
	audit      *audit.Manager
	loginRate  *loginRateLimiter
}

// RegisterRoutes registers all management API routes.
func RegisterRoutes(r chi.Router, deps Dependencies) {
	r.Use(httputil.ManagementErrors)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Not Found", "management route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "Method Not Allowed", "method not supported for this management route")
	})
	h := &handler{
		cfg:        deps.Config,
		store:      deps.Store,
		registry:   deps.Registry,
		enforcer:   deps.Enforcer,
		logger:     deps.Logger,
		cache:      deps.Cache,
		tileCache:  deps.TileCache,
		tileEngine: deps.TileEngine,
		tileJobs:   deps.TileJobs,
		mosaic:     deps.Mosaic,
		lifecycle:  deps.Lifecycle,
		importer:   deps.Importer,
		audit:      deps.Audit,
		loginRate:  newLoginRateLimiter(),
	}

	// OpenAPI documentation (no auth required) - defined in separate group
	r.Group(func(r chi.Router) {
		r.Get("/api", func(w http.ResponseWriter, req *http.Request) {
			h.api(w, req, deps.Config)
		})
		r.Get("/api.html", h.apiHTML)
		r.Get("/api.js", h.apiJS)
		r.With(identity.RequireSecureTransport(deps.Config.Auth.RequireHTTPS)).Get("/console/config", h.consoleConfig)
		r.With(identity.RequireSecureTransport(deps.Config.Auth.RequireHTTPS)).Post("/auth/login", h.login)
	})

	// All management routes require authentication - use Group to avoid middleware order issue
	r.Group(func(r chi.Router) {
		r.Use(identity.RequireSecureTransport(deps.Config.Auth.RequireHTTPS))
		r.Use(identity.RequireIdentity)
		r.Get("/auth/me", h.authMe)
		r.Post("/auth/logout", h.logout)
		r.Post("/auth/refresh", h.refreshSession)
		r.Route("/auth/sessions", func(r chi.Router) {
			r.Use(rbac.RequireSuperAdmin())
			r.Get("/", h.listSessions)
			r.Delete("/{session}", h.revokeSession)
		})

		// Cache management routes (super admin only)
		r.Route("/cache", func(r chi.Router) {
			r.Use(rbac.RequireSuperAdmin())
			r.Get("/stats", h.getCacheStats)
			r.Post("/clear", h.clearAllCaches)
			r.Post("/clear/{cacheType}", h.clearCacheByType)
		})
		r.With(rbac.RequireSuperAdmin()).Get("/catalog/integrity", h.getCatalogIntegrity)
		r.With(rbac.RequireSuperAdmin()).Post("/catalog/integrity/repair", h.repairCatalogIntegrity)
		r.Route("/audit", func(r chi.Router) {
			r.Use(rbac.RequireSuperAdmin())
			r.Get("/", h.listAuditEvents)
			r.Post("/retention", h.runAuditRetention)
		})
		r.Route("/tile-matrix-sets", func(r chi.Router) {
			r.Use(rbac.RequireSuperAdmin())
			r.Get("/", h.listTileMatrixSets)
			r.Get("/{tileMatrixSet}", h.getTileMatrixSet)
			r.Put("/{tileMatrixSet}", h.putTileMatrixSet)
			r.Delete("/{tileMatrixSet}", h.deleteTileMatrixSet)
		})

		r.Route("/deletions", func(r chi.Router) {
			r.Get("/", h.listDeletionOperations)
			r.Get("/{operation}", h.getDeletionOperation)
			r.Post("/{operation}/retry", h.retryDeletionOperation)
		})

		// Workspace routes
		r.Route("/workspaces", func(r chi.Router) {
			r.With(rbac.RequireSuperAdmin()).Get("/", h.listWorkspaces)
			r.With(rbac.RequireSuperAdmin()).Post("/", h.createWorkspace)

			r.Route("/{workspace}", func(r chi.Router) {
				r.Use(h.canonicalWorkspace)
				r.Use(rbac.RequireWorkspaceAdmin(deps.Enforcer))
				r.Get("/summary", h.workspaceSummary)
				r.Get("/roles", h.listWorkspaceRoles)
				r.Get("/tile-matrix-sets", h.listWorkspaceTileMatrixSets)

				r.Route("/imports", func(r chi.Router) {
					r.Get("/", h.listImports)
					r.Post("/", h.createImport)
					r.Route("/{import}", func(r chi.Router) {
						r.Get("/", h.getImport)
						r.Get("/history", h.getImportHistory)
						r.Put("/plan", h.setImportPlan)
						r.Get("/preview", h.previewImport)
						r.Post("/publish", h.publishImport)
						r.Post("/retry", h.retryImport)
						r.Post("/rollback", h.rollbackImport)
						r.Delete("/", h.cancelImport)
					})
				})

				r.Get("/", h.getWorkspace)
				r.Put("/", h.updateWorkspace)
				r.Get("/deletion-plan", h.getWorkspaceDeletionPlan)
				r.With(rbac.RequireSuperAdmin()).Delete("/", h.deleteWorkspace)

				// Service routes
				r.Route("/services", func(r chi.Router) {
					r.Get("/", h.listServices)
					r.Post("/", h.createService)
					r.Post("/test-connection", h.testNewServiceConnection)

					r.Route("/{service}", func(r chi.Router) {
						r.Get("/", h.getService)
						r.Put("/", h.updateService)
						r.Post("/test-connection", h.testExistingServiceConnection)
						r.Get("/deletion-plan", h.getServiceDeletionPlan)
						r.Delete("/", h.deleteService)
						r.Post("/discover", h.discoverLayers)
						r.Post("/discover-coverages", h.discoverCoverages)
						r.Post("/validate-sql", h.validateSQL) // SQL View validation
						r.Route("/mosaic", func(r chi.Router) {
							r.Get("/granules", h.listMosaicGranules)
							r.Get("/granules/{granule}", h.getMosaicGranule)
							r.Delete("/granules/{granule}", h.deleteMosaicGranule)
							r.Get("/harvest-jobs", h.listMosaicHarvestJobs)
							r.Post("/harvest-jobs", h.createMosaicHarvestJob)
							r.Get("/harvest-jobs/{job}", h.getMosaicHarvestJob)
							r.Delete("/harvest-jobs/{job}", h.cancelMosaicHarvestJob)
						})

						// Layer routes
						r.Route("/layers", func(r chi.Router) {
							r.Get("/", h.listLayers)
							r.Post("/", h.createLayer)

							r.Route("/{layer}", func(r chi.Router) {
								r.Get("/", h.getLayer)
								r.Put("/", h.updateLayer)
								r.Delete("/", h.deleteLayer)
							})
						})

						// Coverage routes are independent of feature layers.
						r.Route("/coverages", func(r chi.Router) {
							r.Get("/", h.listCoverages)
							r.Post("/", h.createCoverage)
							r.Route("/{coverage}", func(r chi.Router) {
								r.Get("/", h.getCoverage)
								r.Put("/", h.updateCoverage)
								r.Delete("/", h.deleteCoverage)
							})
						})
					})
				})

				// API key routes
				r.Route("/apikeys", func(r chi.Router) {
					r.Get("/", h.listAPIKeys)
					r.Post("/", h.createAPIKey)

					r.Route("/{keyId}", func(r chi.Router) {
						r.Get("/", h.getAPIKey)
						r.Delete("/", h.revokeAPIKey)
						r.Delete("/permanent", h.deleteAPIKey)
					})
				})

				// Claim mappings routes
				r.Route("/claim-mappings", func(r chi.Router) {
					r.Get("/", h.listClaimMappings)
					r.Post("/", h.createClaimMapping)

					r.Route("/{mappingId}", func(r chi.Router) {
						r.Get("/", h.getClaimMapping)
						r.Delete("/", h.deleteClaimMapping)
					})
				})

				// Style routes
				r.Route("/styles", func(r chi.Router) {
					r.Get("/", h.listStyles)
					r.Post("/", h.createStyle)

					r.Route("/{style}", func(r chi.Router) {
						r.Get("/", h.getStyle)
						r.Put("/", h.updateStyle)
						r.Delete("/", h.deleteStyle)
					})
				})

				r.Route("/style-assets", func(r chi.Router) {
					r.Get("/", h.listStyleAssets)
					r.Route("/{asset}", func(r chi.Router) {
						r.Get("/", h.getStyleAsset)
						r.Put("/", h.putStyleAsset)
						r.Delete("/", h.deleteStyleAsset)
					})
				})

				r.Route("/layer-groups", func(r chi.Router) {
					r.Get("/", h.listLayerGroups)
					r.Post("/", h.createLayerGroup)
					r.Route("/{group}", func(r chi.Router) {
						r.Get("/", h.getLayerGroup)
						r.Put("/", h.updateLayerGroup)
						r.Delete("/", h.deleteLayerGroup)
					})
				})

				// Settings routes (per service type)
				r.Route("/settings", func(r chi.Router) {
					r.Route("/wms", func(r chi.Router) {
						r.Get("/", h.getWMSSettings)
						r.Put("/", h.updateWMSSettings)
					})
					r.Route("/wfs", func(r chi.Router) {
						r.Get("/", h.getWFSSettings)
						r.Put("/", h.updateWFSSettings)
					})
					r.Route("/ogcapi", func(r chi.Router) {
						r.Get("/", h.getOGCAPISettings)
						r.Put("/", h.updateOGCAPISettings)
					})
					r.Route("/ogc-tiles", func(r chi.Router) {
						r.Get("/", h.getOGCTilesAPISettings)
						r.Put("/", h.updateOGCTilesAPISettings)
					})
					r.Route("/wcs", func(r chi.Router) {
						r.Get("/", h.getWCSSettings)
						r.Put("/", h.updateWCSSettings)
					})
					r.Route("/wmts", func(r chi.Router) {
						r.Get("/", h.getWMTSSettings)
						r.Put("/", h.updateWMTSSettings)
					})
				})

				// Workspace cache management
				r.Post("/cache/clear", h.clearWorkspaceCache)
				r.Route("/tile-cache", func(r chi.Router) {
					r.Get("/stats", h.getPersistentTileCacheStats)
					r.Route("/jobs", func(r chi.Router) {
						r.Get("/", h.listTileCacheJobs)
						r.Post("/", h.createTileCacheJob)
						r.Get("/{job}", h.getTileCacheJob)
						r.Get("/{job}/progress", h.getTileCacheJobProgress)
						r.Delete("/{job}", h.cancelTileCacheJob)
					})
				})
			})
		})

		// Global claim mappings (super admin only)
		r.Route("/claim-mappings", func(r chi.Router) {
			r.Use(rbac.RequireSuperAdmin())
			r.Get("/", h.listGlobalClaimMappings)
			r.Post("/", h.createGlobalClaimMapping)
			r.Delete("/{mappingId}", h.deleteGlobalClaimMapping)
		})

		// Role management (super admin only)
		r.Route("/roles", func(r chi.Router) {
			r.Use(rbac.RequireSuperAdmin())
			r.Get("/", h.listRoles)
			r.Post("/", h.createRole)
			r.Delete("/{roleId}", h.deleteRole)
			r.Get("/{roleId}/deletion-plan", h.roleDeletionPlan)
			r.Get("/{roleId}/policies", h.listRolePolicies)
			r.Post("/{roleId}/policies", h.addRolePolicy)
			r.Delete("/{roleId}/policies", h.removeRolePolicy)
		})
	})
}

// Error response types
type errorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, code int, v any) {
	httputil.WriteJSON(w, code, v)
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, code int, message string, detail string) {
	httputil.WriteJSON(w, code, errorResponse{Code: code, Message: message, Detail: detail})
}

// readJSON reads JSON from the request body.
func readJSON(r *http.Request, v any) error {
	return httputil.ReadJSON(r, v)
}
