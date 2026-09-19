package tiles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/workspace"
	"golang.org/x/sync/singleflight"
)

// workspaceHandler handles OGC API Tiles requests for a specific workspace.
type workspaceHandler struct {
	cfg          conf.Config
	logger       *slog.Logger
	registry     *workspace.Registry
	cache        *cache.Manager
	mvtGen       *MVTGenerator
	mapGen       *MapTileGenerator
	renderSlots  chan struct{}
	queueTimeout time.Duration
	renderGroup  singleflight.Group
	engine       *Engine
	engineOnce   sync.Once
}

// RegisterWorkspaceRoutes registers OGC API Tiles routes for workspace-scoped access.
func RegisterWorkspaceRoutes(r chi.Router, deps WorkspaceDependencies) {
	engine := deps.Engine
	if engine == nil {
		engine = NewEngine(deps.Config, deps.Logger, deps.Cache, nil)
	}
	h := &workspaceHandler{
		cfg:          deps.Config,
		logger:       deps.Logger,
		registry:     deps.Registry,
		cache:        deps.Cache,
		mvtGen:       engine.mvtGen,
		mapGen:       engine.mapGen,
		renderSlots:  engine.renderSlots,
		queueTimeout: engine.queueTimeout,
		engine:       engine,
	}

	// Landing page and metadata
	r.Get("/", h.landing)
	r.Get("/conformance", h.conformance)
	r.Get("/api", h.api)

	// TileMatrixSets
	r.Get("/tileMatrixSets", h.tileMatrixSets)
	r.Get("/tileMatrixSets/{tileMatrixSetId}", h.tileMatrixSet)

	// Collections
	r.Get("/collections", h.collections)
	r.Get("/collections/{collectionId}", h.collection)

	// Vector tiles
	r.Get("/collections/{collectionId}/tiles", h.collectionTilesets)
	r.Get("/collections/{collectionId}/tiles/{tileMatrixSetId}", h.collectionTileset)
	r.Get("/collections/{collectionId}/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}", h.getVectorTile)

	// Map tiles
	r.Get("/collections/{collectionId}/map/tiles", h.collectionMapTilesets)
	r.Get("/collections/{collectionId}/map/tiles/{tileMatrixSetId}", h.collectionMapTileset)
	r.Get("/collections/{collectionId}/map/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}", h.getMapTile)

	// TileJSON
	r.Get("/collections/{collectionId}/tilejson.json", h.tileJSON)
}

func (h *workspaceHandler) acquireRender(ctx context.Context) (func(), bool) {
	timer := time.NewTimer(h.queueTimeout)
	defer timer.Stop()
	select {
	case h.renderSlots <- struct{}{}:
		return func() { <-h.renderSlots }, true
	case <-ctx.Done():
		return nil, false
	case <-timer.C:
		return nil, false
	}
}

func (h *workspaceHandler) tileEngine() *Engine {
	h.engineOnce.Do(func() {
		if h.engine != nil {
			return
		}
		// Tests and embedders historically constructed workspaceHandler directly.
		// Reuse those injected generators and limiters while routing through Engine.
		h.engine = &Engine{
			cfg: h.cfg, logger: h.logger, memory: h.cache, mvtGen: h.mvtGen, mapGen: h.mapGen,
			renderSlots: h.renderSlots, queueTimeout: h.queueTimeout, renderGroup: &h.renderGroup,
		}
		if h.engine.logger == nil {
			h.engine.logger = slog.Default()
		}
	})
	return h.engine
}

// requireAuth enforces authentication and workspace authorization for non-public
// services. For a non-public workspace it requires a valid identity that has access
// to this specific workspace (or is a super admin). It writes the appropriate error
// response (401 or 403) and returns true when the request must be rejected.
func (h *workspaceHandler) requireAuth(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, isPublic bool) bool {
	if isPublic {
		return false
	}
	if h.cfg.Auth.RequireHTTPS && !identity.IsSecureTransport(r.Context()) {
		w.Header().Set("Upgrade", "TLS/1.2, HTTP/1.1")
		writeErr(w, http.StatusUpgradeRequired, "HTTPSRequired", "HTTPS required")
		return true
	}
	id, ok := identity.FromContext(r.Context())
	if !ok || id == nil {
		writeErr(w, http.StatusUnauthorized, "Unauthorized", "authentication required")
		return true
	}
	if !id.HasWorkspaceAccess(ws.ID) {
		writeErr(w, http.StatusForbidden, "Forbidden", "no access to this workspace")
		return true
	}
	return false
}

// workspaceRole returns the caller's role for the given workspace, or "" for an
// anonymous request (only possible on a public service). Used to filter layers
// by per-layer read access (Layer.VisibleToRole).
func workspaceRole(r *http.Request, workspaceID string) string {
	if id, ok := identity.FromContext(r.Context()); ok && id != nil {
		return id.GetWorkspaceRole(workspaceID)
	}
	return ""
}

func pathParam(r *http.Request, name string) string {
	raw := chi.URLParam(r, name)
	if decoded, err := url.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

// checkTilesEnabled checks if OGC Tiles API is enabled for the workspace.
func (h *workspaceHandler) checkTilesEnabled(ws *workspace.Workspace) bool {
	if ws.Settings == nil {
		return false
	}
	return ws.Settings.OGCTilesAPI.Enabled
}

func (h *workspaceHandler) tilesWorkspace(w http.ResponseWriter, r *http.Request) (*workspace.Workspace, bool) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok || ws == nil {
		writeErr(w, http.StatusInternalServerError, "ServerError", "workspace not found in request context")
		return nil, false
	}
	if !h.checkTilesEnabled(ws) {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Tiles is not enabled for this workspace")
		return nil, false
	}
	return ws, true
}

// landing handles GET / - landing page
func (h *workspaceHandler) landing(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}

	title := ws.Name
	description := ws.Description
	if ws.Settings.OGCTilesAPI.Title != "" {
		title = ws.Settings.OGCTilesAPI.Title
	}
	if ws.Settings.OGCTilesAPI.Abstract != "" {
		description = ws.Settings.OGCTilesAPI.Abstract
	}

	base := h.workspaceBaseURL(r, ws.Name)
	resp := LandingPage{
		Title:       title,
		Description: description,
		Links: []Link{
			{Href: base + "/", Rel: "self", Type: MediaTypeJSON, Title: "This document"},
			{Href: base + "/conformance", Rel: "conformance", Type: MediaTypeJSON, Title: "Conformance declaration"},
			{Href: base + "/api", Rel: "service-desc", Type: MediaTypeOpenAPI, Title: "OpenAPI definition"},
			{Href: base + "/tileMatrixSets", Rel: "http://www.opengis.net/def/rel/ogc/1.0/tiling-schemes", Type: MediaTypeJSON, Title: "List of tile matrix sets"},
			{Href: base + "/collections", Rel: "data", Type: MediaTypeJSON, Title: "List of collections"},
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

// conformance handles GET /conformance
func (h *workspaceHandler) conformance(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}

	resp := Conformance{
		ConformsTo: effectiveConformanceClasses(ws.Settings.OGCTilesAPI),
	}

	writeJSON(w, http.StatusOK, resp)
}

// tileMatrixSets handles GET /tileMatrixSets
func (h *workspaceHandler) tileMatrixSets(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}

	base := h.workspaceBaseURL(r, ws.Name)
	items := GetSupportedTileMatrixSets()

	// Add links
	for i := range items {
		items[i].Links = []Link{
			{Href: fmt.Sprintf("%s/tileMatrixSets/%s", base, urlPathEscape(items[i].ID)), Rel: "self", Type: MediaTypeJSON},
		}
	}

	resp := TileMatrixSetsResponse{
		TileMatrixSets: items,
	}

	writeJSON(w, http.StatusOK, resp)
}

// tileMatrixSet handles GET /tileMatrixSets/{tileMatrixSetId}
func (h *workspaceHandler) tileMatrixSet(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}

	tmsID := pathParam(r, "tileMatrixSetId")
	tms, err := GetTileMatrixSetDefinition(tmsID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "NotFound", fmt.Sprintf("TileMatrixSet not found: %s", tmsID))
		return
	}

	writeJSON(w, http.StatusOK, tms)
}

// collections handles GET /collections
func (h *workspaceHandler) collections(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}

	base := h.workspaceBaseURL(r, ws.Name)
	var cols []Collection

	for _, layer := range ws.VisibleLayers(workspaceRole(r, ws.ID)) {
		if !layer.Enabled {
			continue
		}
		cols = append(cols, Collection{
			ID:          layer.PublicID,
			Title:       layer.Title,
			Description: layer.Description,
			Links: []Link{
				{Href: fmt.Sprintf("%s/collections/%s", base, urlPathEscape(layer.PublicID)), Rel: "self", Type: MediaTypeJSON},
				{Href: fmt.Sprintf("%s/collections/%s/tiles", base, urlPathEscape(layer.PublicID)), Rel: "http://www.opengis.net/def/rel/ogc/1.0/tilesets-vector", Type: MediaTypeJSON, Title: "Vector tilesets"},
				{Href: fmt.Sprintf("%s/collections/%s/map/tiles", base, urlPathEscape(layer.PublicID)), Rel: "http://www.opengis.net/def/rel/ogc/1.0/tilesets-map", Type: MediaTypeJSON, Title: "Map tilesets"},
			},
		})
	}
	for _, coverage := range ws.VisibleCoverages(workspaceRole(r, ws.ID)) {
		resource := ws.GetResource(coverage.PublicID)
		if resource == nil || resource.Kind != workspace.ResourceCoverage {
			continue
		}
		links := []Link{{Href: fmt.Sprintf("%s/collections/%s", base, urlPathEscape(coverage.PublicID)), Rel: "self", Type: MediaTypeJSON}}
		if ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled {
			links = append(links, Link{Href: fmt.Sprintf("%s/collections/%s/map/tiles", base, urlPathEscape(coverage.PublicID)), Rel: "http://www.opengis.net/def/rel/ogc/1.0/tilesets-map", Type: MediaTypeJSON, Title: "Map tilesets"}, Link{Href: fmt.Sprintf("%s/collections/%s/tilejson.json", base, urlPathEscape(coverage.PublicID)), Rel: "describedby", Type: MediaTypeJSON, Title: "Map TileJSON metadata"})
		}
		cols = append(cols, Collection{ID: coverage.PublicID, Title: coverage.Title, Description: coverage.Description, Links: links})
	}

	resp := CollectionsResponse{
		Collections: cols,
		Links: []Link{
			{Href: base + "/collections", Rel: "self", Type: MediaTypeJSON},
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

// collection handles GET /collections/{collectionId}
func (h *workspaceHandler) collection(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}

	collectionID := pathParam(r, "collectionId")
	resource := ws.GetResource(collectionID)
	if resource == nil || !resourceVisible(ws, resource, workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "NotFound", "collection not found")
		return
	}

	publicID, title, description := resourceMetadata(resource)
	base := h.workspaceBaseURL(r, ws.Name)
	links := []Link{{Href: fmt.Sprintf("%s/collections/%s", base, urlPathEscape(publicID)), Rel: "self", Type: MediaTypeJSON}}
	if resource.Kind == workspace.ResourceFeature && ws.Settings.OGCTilesAPI.Settings.VectorTiles.Enabled {
		links = append(links,
			Link{Href: fmt.Sprintf("%s/collections/%s/tiles", base, urlPathEscape(publicID)), Rel: "http://www.opengis.net/def/rel/ogc/1.0/tilesets-vector", Type: MediaTypeJSON, Title: "Vector tilesets"},
			Link{Href: fmt.Sprintf("%s/collections/%s/tilejson.json", base, urlPathEscape(publicID)), Rel: "describedby", Type: MediaTypeJSON, Title: "TileJSON metadata"})
	}
	if ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled {
		links = append(links, Link{Href: fmt.Sprintf("%s/collections/%s/map/tiles", base, urlPathEscape(publicID)), Rel: "http://www.opengis.net/def/rel/ogc/1.0/tilesets-map", Type: MediaTypeJSON, Title: "Map tilesets"})
		if resource.Kind == workspace.ResourceCoverage || resource.Kind == workspace.ResourceGroup {
			links = append(links, Link{Href: fmt.Sprintf("%s/collections/%s/tilejson.json", base, urlPathEscape(publicID)), Rel: "describedby", Type: MediaTypeJSON, Title: "Map TileJSON metadata"})
		}
	}
	resp := Collection{
		ID:          publicID,
		Title:       title,
		Description: description,
		Links:       links,
	}

	writeJSON(w, http.StatusOK, resp)
}

// collectionTilesets handles GET /collections/{collectionId}/tiles
func (h *workspaceHandler) collectionTilesets(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}
	if !ws.Settings.OGCTilesAPI.Settings.VectorTiles.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "Vector tiles are not enabled")
		return
	}

	collectionID := pathParam(r, "collectionId")
	layer, _ := ws.GetLayer(collectionID)
	if layer == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "NotFound", "collection not found")
		return
	}

	base := h.workspaceBaseURL(r, ws.Name)
	var tilesets []TileSetMetadata

	// Create tileset entry for each enabled TileMatrixSet
	for _, tmsID := range ws.Settings.OGCTilesAPI.Settings.TileMatrixSets {
		tms, err := GetTileMatrixSetDefinition(tmsID)
		if err != nil {
			continue
		}

		tileset := TileSetMetadata{
			Title:           layer.Title,
			Description:     layer.Description,
			DataType:        DataTypeVector,
			TileMatrixSetID: tmsID,
			CRS:             tms.CRS,
			Links: []Link{
				{Href: fmt.Sprintf("%s/collections/%s/tiles/%s", base, urlPathEscape(collectionID), urlPathEscape(tmsID)), Rel: "self", Type: MediaTypeJSON},
				{Href: ogcTileItemURL(base, collectionID, "tiles", tmsID), Rel: "item", Type: MediaTypeMVT},
			},
		}
		tilesets = append(tilesets, tileset)
	}

	resp := TileSetList{
		TileSets: tilesets,
		Links: []Link{
			{Href: fmt.Sprintf("%s/collections/%s/tiles", base, urlPathEscape(collectionID)), Rel: "self", Type: MediaTypeJSON},
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

// collectionTileset handles GET /collections/{collectionId}/tiles/{tileMatrixSetId}
func (h *workspaceHandler) collectionTileset(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}
	if !ws.Settings.OGCTilesAPI.Settings.VectorTiles.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "Vector tiles are not enabled")
		return
	}

	collectionID := pathParam(r, "collectionId")
	tmsID := pathParam(r, "tileMatrixSetId")

	layer, _ := ws.GetLayer(collectionID)
	if layer == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "NotFound", "collection not found")
		return
	}

	tms, err := GetTileMatrixSetDefinition(tmsID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "NotFound", fmt.Sprintf("TileMatrixSet not found: %s", tmsID))
		return
	}

	base := h.workspaceBaseURL(r, ws.Name)
	resp := TileSetMetadata{
		Title:           layer.Title,
		Description:     layer.Description,
		DataType:        DataTypeVector,
		TileMatrixSetID: tmsID,
		CRS:             tms.CRS,
		Links: []Link{
			{Href: fmt.Sprintf("%s/collections/%s/tiles/%s", base, urlPathEscape(collectionID), urlPathEscape(tmsID)), Rel: "self", Type: MediaTypeJSON},
			{Href: ogcTileItemURL(base, collectionID, "tiles", tmsID), Rel: "item", Type: MediaTypeMVT},
			{Href: fmt.Sprintf("%s/tileMatrixSets/%s", base, urlPathEscape(tmsID)), Rel: "http://www.opengis.net/def/rel/ogc/1.0/tiling-scheme", Type: MediaTypeJSON},
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

// getVectorTile handles GET /collections/{collectionId}/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}
func (h *workspaceHandler) getVectorTile(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}

	// Check if vector tiles are enabled
	if !ws.Settings.OGCTilesAPI.Settings.VectorTiles.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "Vector tiles are not enabled")
		return
	}

	collectionID := pathParam(r, "collectionId")
	tmsID := pathParam(r, "tileMatrixSetId")

	coordinates, invalidParameter := parseOGCTileCoordinates(r)
	if invalidParameter != "" {
		writeErr(w, http.StatusBadRequest, "InvalidParameter", "invalid "+invalidParameter+" coordinate")
		return
	}

	// Validate coordinates
	if err := ValidateTileCoords(tmsID, coordinates.Matrix, coordinates.Column, coordinates.Row); err != nil {
		writeErr(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}
	if coordinates.Matrix < h.cfg.Tiles.MinZoom || coordinates.Matrix > h.cfg.Tiles.MaxZoom {
		writeErr(w, http.StatusBadRequest, "InvalidParameter", "zoom level outside configured range")
		return
	}

	layer, svc := ws.GetLayer(collectionID)
	if layer == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "NotFound", "collection not found")
		return
	}

	if svc.DataSource == nil {
		writeErr(w, http.StatusServiceUnavailable, "ServiceUnavailable", "data source not available")
		return
	}
	aliasKey := cache.MVTTileKey(ws.ID, collectionID, tmsID, coordinates.Matrix, coordinates.Column, coordinates.Row)
	cacheFill := h.cache.BeginFill(cache.CacheTypeTiles, aliasKey)
	result, err := h.tileEngine().Fetch(r.Context(), EngineRequest{
		Workspace: ws, Resource: &workspace.PublishedResource{Kind: workspace.ResourceFeature, Service: svc, Layer: layer},
		TileType: "vector", MatrixSet: tmsID, Zoom: coordinates.Matrix, Column: coordinates.Column, Row: coordinates.Row,
		Format: MediaTypeMVT, UseCache: ws.Settings.OGCTilesAPI.Settings.CacheEnabled,
	})
	if err != nil {
		if errors.Is(err, ErrRenderQueueFull) {
			writeErr(w, http.StatusServiceUnavailable, "ServerBusy", "tile render queue is full")
			return
		}
		h.logger.Error("failed to generate MVT tile", "collection", collectionID, "tileMatrix", coordinates.Matrix, "tileCol", coordinates.Column, "tileRow", coordinates.Row, "error", err)
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to generate tile")
		return
	}
	// Keep the historical in-memory alias during the cache-key migration. The
	// durable entry remains protocol-neutral and canonical.
	if h.cache != nil && ws.Settings.OGCTilesAPI.Settings.CacheEnabled {
		h.cache.SetTile(aliasKey, result.Data, 0, cacheFill)
	}
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("X-Cache", result.CacheStatus)
	w.Header().Set("X-Cache-Tier", result.CacheTier)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Data)
}

// collectionMapTilesets handles GET /collections/{collectionId}/map/tiles
func (h *workspaceHandler) collectionMapTilesets(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}
	if !ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "Map tiles are not enabled")
		return
	}

	collectionID := pathParam(r, "collectionId")
	resource := ws.GetResource(collectionID)
	if resource == nil || !resourceVisible(ws, resource, workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "NotFound", "collection not found")
		return
	}

	base := h.workspaceBaseURL(r, ws.Name)
	var tilesets []TileSetMetadata

	for _, tmsID := range ws.Settings.OGCTilesAPI.Settings.TileMatrixSets {
		tms, err := GetTileMatrixSetDefinition(tmsID)
		if err != nil {
			continue
		}

		links := []Link{{Href: fmt.Sprintf("%s/collections/%s/map/tiles/%s", base, urlPathEscape(collectionID), urlPathEscape(tmsID)), Rel: "self", Type: MediaTypeJSON}}
		links = append(links, mapTileItemLinks(base, collectionID, tmsID, ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats)...)
		tileset := TileSetMetadata{
			Title:           resourceTitle(resource),
			Description:     resourceDescription(resource),
			DataType:        DataTypeMap,
			TileMatrixSetID: tmsID,
			CRS:             tms.CRS,
			Links:           links,
		}
		tilesets = append(tilesets, tileset)
	}

	resp := TileSetList{
		TileSets: tilesets,
		Links: []Link{
			{Href: fmt.Sprintf("%s/collections/%s/map/tiles", base, urlPathEscape(collectionID)), Rel: "self", Type: MediaTypeJSON},
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

// collectionMapTileset handles GET /collections/{collectionId}/map/tiles/{tileMatrixSetId}
func (h *workspaceHandler) collectionMapTileset(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}
	if !ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "Map tiles are not enabled")
		return
	}

	collectionID := pathParam(r, "collectionId")
	tmsID := pathParam(r, "tileMatrixSetId")

	resource := ws.GetResource(collectionID)
	if resource == nil || !resourceVisible(ws, resource, workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "NotFound", "collection not found")
		return
	}

	tms, err := GetTileMatrixSetDefinition(tmsID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "NotFound", fmt.Sprintf("TileMatrixSet not found: %s", tmsID))
		return
	}

	base := h.workspaceBaseURL(r, ws.Name)
	links := []Link{{Href: fmt.Sprintf("%s/collections/%s/map/tiles/%s", base, urlPathEscape(collectionID), urlPathEscape(tmsID)), Rel: "self", Type: MediaTypeJSON}}
	links = append(links, mapTileItemLinks(base, collectionID, tmsID, ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats)...)
	links = append(links, Link{Href: fmt.Sprintf("%s/tileMatrixSets/%s", base, urlPathEscape(tmsID)), Rel: "http://www.opengis.net/def/rel/ogc/1.0/tiling-scheme", Type: MediaTypeJSON})
	resp := TileSetMetadata{
		Title:           resourceTitle(resource),
		Description:     resourceDescription(resource),
		DataType:        DataTypeMap,
		TileMatrixSetID: tmsID,
		CRS:             tms.CRS,
		Links:           links,
	}

	writeJSON(w, http.StatusOK, resp)
}

// getMapTile handles GET /collections/{collectionId}/map/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}
func (h *workspaceHandler) getMapTile(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}

	// Check if map tiles are enabled
	if !ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "Map tiles are not enabled")
		return
	}

	collectionID := pathParam(r, "collectionId")
	tmsID := pathParam(r, "tileMatrixSetId")

	coordinates, invalidParameter := parseOGCTileCoordinates(r)
	if invalidParameter != "" {
		writeErr(w, http.StatusBadRequest, "InvalidParameter", "invalid "+invalidParameter+" coordinate")
		return
	}

	// Parse format
	requestedFormat := r.URL.Query().Get("f")
	if requestedFormat == "" && !containsString(ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats, MediaTypePNG) && len(ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats) > 0 {
		requestedFormat = ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats[0]
	}
	format, err := ParseTileFormat(requestedFormat)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}
	if !containsString(ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats, GetContentType(format)) {
		writeErr(w, http.StatusBadRequest, "InvalidParameter", "tile format is not enabled")
		return
	}

	// Parse style
	styleName := r.URL.Query().Get("style")
	if styleName != "" {
		if !sld.ValidStyleName(styleName) {
			writeErr(w, http.StatusBadRequest, "InvalidParameter", "invalid style name")
			return
		}
		style, exists := ws.Styles[styleName]
		if !exists {
			writeErr(w, http.StatusBadRequest, "InvalidParameter", "style not found")
			return
		}
		_, parseErr := style.CompiledDocument()
		if parseErr != nil {
			writeErr(w, http.StatusBadRequest, "InvalidParameter", "style is invalid")
			return
		}
	}

	// Validate coordinates
	if err := ValidateTileCoords(tmsID, coordinates.Matrix, coordinates.Column, coordinates.Row); err != nil {
		writeErr(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}
	if coordinates.Matrix < h.cfg.Tiles.MinZoom || coordinates.Matrix > h.cfg.Tiles.MaxZoom {
		writeErr(w, http.StatusBadRequest, "InvalidParameter", "zoom level outside configured range")
		return
	}

	resource := ws.GetResource(collectionID)
	if resource == nil || !resourceVisible(ws, resource, workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "NotFound", "collection not found")
		return
	}
	if resource.Kind == workspace.ResourceFeature && (resource.Service == nil || resource.Service.DataSource == nil) {
		writeErr(w, http.StatusServiceUnavailable, "ServiceUnavailable", "data source not available")
		return
	}
	result, err := h.tileEngine().Fetch(r.Context(), EngineRequest{
		Workspace: ws, Resource: resource, TileType: "map", MatrixSet: tmsID,
		Zoom: coordinates.Matrix, Column: coordinates.Column, Row: coordinates.Row, Format: string(format), Style: styleName,
		UseCache: ws.Settings.OGCTilesAPI.Settings.CacheEnabled,
		Time:     firstNonEmpty(r.URL.Query().Get("datetime"), r.URL.Query().Get("time")), Elevation: r.URL.Query().Get("elevation"),
	})
	if err != nil {
		if errors.Is(err, ErrRenderQueueFull) {
			writeErr(w, http.StatusServiceUnavailable, "ServerBusy", "tile render queue is full")
			return
		}
		h.logger.Error("failed to generate map tile", "collection", collectionID, "tileMatrix", coordinates.Matrix, "tileCol", coordinates.Column, "tileRow", coordinates.Row, "error", err)
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to generate tile")
		return
	}
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("X-Cache", result.CacheStatus)
	w.Header().Set("X-Cache-Tier", result.CacheTier)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Data)
}

func tileLayerInfo(ctx context.Context, ds datasource.DataSource, layer *workspace.Layer) (*datasource.LayerInfo, *datasource.SQLViewConfig, error) {
	if !layer.IsSQLView || layer.SQLViewConfig == nil {
		info, err := ds.GetLayerInfo(ctx, layer.SourceLayer)
		return info, nil, err
	}
	if _, ok := ds.(datasource.SQLViewDataSource); !ok {
		return nil, nil, fmt.Errorf("data source does not support SQL views")
	}
	config := &datasource.SQLViewConfig{SQL: layer.SQLViewConfig.SQL, GeometryColumn: layer.SQLViewConfig.GeometryColumn, GeometryType: layer.SQLViewConfig.GeometryType, SRID: layer.SQLViewConfig.SRID, IDColumn: layer.SQLViewConfig.IDColumn, ReadOnly: layer.SQLViewConfig.ReadOnly}
	info := &datasource.LayerInfo{Name: layer.PublicID, GeometryColumn: config.GeometryColumn, GeometryType: config.GeometryType, SRID: config.SRID, IDColumn: config.IDColumn}
	for index, property := range layer.SQLViewConfig.Properties {
		config.Properties = append(config.Properties, &datasource.SQLViewProperty{Name: property.Name, Type: property.Type})
		jsonType := datasource.JSONTypeString
		switch property.Type {
		case "integer":
			jsonType = datasource.JSONTypeInteger
		case "number":
			jsonType = datasource.JSONTypeNumber
		case "boolean":
			jsonType = datasource.JSONTypeBoolean
		}
		info.Properties = append(info.Properties, datasource.PropertyInfo{Name: property.Name, Type: property.Type, JSONType: jsonType, Ordinal: index})
	}
	return info, config, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type ogcTileCoordinates struct {
	Matrix int
	Row    int
	Column int
}

func parseOGCTileCoordinates(r *http.Request) (ogcTileCoordinates, string) {
	var coordinates ogcTileCoordinates
	parameters := []struct {
		name        string
		destination *int
	}{
		{name: "tileMatrix", destination: &coordinates.Matrix},
		{name: "tileRow", destination: &coordinates.Row},
		{name: "tileCol", destination: &coordinates.Column},
	}
	for _, parameter := range parameters {
		value, err := strconv.Atoi(chi.URLParam(r, parameter.name))
		if err != nil {
			return ogcTileCoordinates{}, parameter.name
		}
		*parameter.destination = value
	}
	return coordinates, ""
}

func ogcTileItemURL(base, collectionID, tilesPath, tmsID string) string {
	return fmt.Sprintf("%s/collections/%s/%s/%s/{tileMatrix}/{tileRow}/{tileCol}", base, urlPathEscape(collectionID), tilesPath, urlPathEscape(tmsID))
}

func mapTileItemLinks(base, collectionID, tmsID string, formats []string) []Link {
	links := make([]Link, 0, len(formats))
	for _, format := range formats {
		parameter := "png"
		switch format {
		case MediaTypeJPEG:
			parameter = "jpeg"
		case MediaTypeWEBP:
			parameter = "webp"
		case MediaTypePNG:
		default:
			continue
		}
		links = append(links, Link{Href: ogcTileItemURL(base, collectionID, "map/tiles", tmsID) + "?f=" + parameter, Rel: "item", Type: format})
	}
	return links
}

// tileJSON handles GET /collections/{collectionId}/tilejson.json
func (h *workspaceHandler) tileJSON(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.tilesWorkspace(w, r)
	if !ok {
		return
	}

	if h.requireAuth(w, r, ws, ws.Settings.OGCTilesAPI.Public) {
		return
	}
	collectionID := pathParam(r, "collectionId")
	resource := ws.GetResource(collectionID)
	if resource == nil || !resourceVisible(ws, resource, workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "NotFound", "collection not found")
		return
	}
	if resource.Kind == workspace.ResourceCoverage || resource.Kind == workspace.ResourceGroup {
		if !ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled {
			writeErr(w, http.StatusForbidden, "ServiceDisabled", "Map tiles are not enabled")
			return
		}
		tmsID := TMSWebMercatorQuad
		if len(ws.Settings.OGCTilesAPI.Settings.TileMatrixSets) > 0 {
			tmsID = ws.Settings.OGCTilesAPI.Settings.TileMatrixSets[0]
		}
		format := "png"
		if len(ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats) > 0 {
			switch ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats[0] {
			case MediaTypeJPEG:
				format = "jpeg"
			case MediaTypeWEBP:
				format = "webp"
			}
		}
		publicID, title, description := resourceMetadata(resource)
		pseudo := &workspace.Layer{PublicID: publicID, Title: title, Description: description}
		tj := GenerateTileJSON(pseudo, nil, TileJSONOptions{BaseURL: h.getBaseURL(r), WorkspaceName: ws.Name, CollectionID: collectionID, TileMatrixSet: tmsID, DataType: DataTypeMap, MinZoom: h.cfg.Tiles.MinZoom, MaxZoom: h.cfg.Tiles.MaxZoom, Format: format})
		writeJSON(w, http.StatusOK, tj)
		return
	}
	if !ws.Settings.OGCTilesAPI.Settings.VectorTiles.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "Vector tiles are not enabled")
		return
	}
	layer, svc := resource.Layer, resource.Service

	// Resolve metadata through the same SQL-aware path used by the renderer.
	if svc == nil || svc.DataSource == nil {
		writeErr(w, http.StatusServiceUnavailable, "Unavailable", "data source is not available")
		return
	}
	layerInfo, _, err := tileLayerInfo(r.Context(), svc.DataSource, layer)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "MetadataError", "could not resolve vector layer metadata")
		return
	}

	// Get preferred TileMatrixSet
	tmsID := TMSWebMercatorQuad
	if len(ws.Settings.OGCTilesAPI.Settings.TileMatrixSets) > 0 {
		tmsID = ws.Settings.OGCTilesAPI.Settings.TileMatrixSets[0]
	}

	baseURL := h.getBaseURL(r)
	opts := TileJSONOptions{
		BaseURL:       baseURL,
		WorkspaceName: ws.Name,
		CollectionID:  collectionID,
		TileMatrixSet: tmsID,
		DataType:      DataTypeVector,
		MinZoom:       h.cfg.Tiles.MinZoom,
		MaxZoom:       h.cfg.Tiles.MaxZoom,
		Format:        "mvt",
	}

	tj := GenerateTileJSON(layer, layerInfo, opts)

	writeJSON(w, http.StatusOK, tj)
}

// workspaceBaseURL returns the base URL for workspace OGC Tiles API.
func (h *workspaceHandler) workspaceBaseURL(r *http.Request, wsName string) string {
	return strings.TrimRight(h.cfg.Server.UrlBase, "/") + h.cfg.Server.BasePath + "/workspaces/" + urlPathEscape(wsName) + "/ogc-tiles"
}

// getBaseURL returns the base URL without workspace path.
func (h *workspaceHandler) getBaseURL(r *http.Request) string {
	return strings.TrimRight(h.cfg.Server.UrlBase, "/") + h.cfg.Server.BasePath
}

// Helper functions

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", MediaTypeJSON)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errType, detail string) {
	w.Header().Set("Content-Type", MediaTypeJSON)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"type":   errType,
		"detail": detail,
	})
}

func urlPathEscape(s string) string {
	return url.PathEscape(s)
}

func resourceVisible(ws *workspace.Workspace, resource *workspace.PublishedResource, role string) bool {
	if resource == nil {
		return false
	}
	if resource.Layer != nil {
		return resource.Layer.VisibleToRole(role)
	}
	if resource.Coverage != nil {
		return resource.Coverage.VisibleToRole(role)
	}
	if resource.Group != nil {
		return ws != nil && ws.GroupVisibleToRole(resource.Group, role)
	}
	return false
}
func resourceMetadata(resource *workspace.PublishedResource) (string, string, string) {
	if resource.Layer != nil {
		return resource.Layer.PublicID, resource.Layer.Title, resource.Layer.Description
	}
	if resource.Coverage != nil {
		return resource.Coverage.PublicID, resource.Coverage.Title, resource.Coverage.Description
	}
	if resource.Group != nil {
		return resource.Group.PublicID, resource.Group.Title, resource.Group.Description
	}
	return "", "", ""
}
func resourceTitle(resource *workspace.PublishedResource) string {
	_, value, _ := resourceMetadata(resource)
	return value
}
func resourceDescription(resource *workspace.PublishedResource) string {
	_, _, value := resourceMetadata(resource)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
