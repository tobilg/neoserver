package ogc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	claim "github.com/tobilg/neoserver/internal/conformance"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/workspace"
)

// WorkspaceDependencies contains dependencies for workspace-aware handlers.
type WorkspaceDependencies struct {
	Config   conf.Config
	Logger   *slog.Logger
	Registry *workspace.Registry
	Cache    *cache.Manager
}

// workspaceHandler handles OGC API requests for a specific workspace.
type workspaceHandler struct {
	cfg      conf.Config
	logger   *slog.Logger
	registry *workspace.Registry
	cache    *cache.Manager
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
		http.Error(w, "HTTPS required", http.StatusUpgradeRequired)
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

// RegisterWorkspaceRoutes registers OGC API routes for workspace-scoped access.
// The workspace must be loaded into the context before these routes are called.
func RegisterWorkspaceRoutes(r chi.Router, deps WorkspaceDependencies) {
	h := &workspaceHandler{
		cfg:      deps.Config,
		logger:   deps.Logger,
		registry: deps.Registry,
		cache:    deps.Cache,
	}

	r.Get("/", h.landing)
	r.Get("/conformance", h.conformance)
	r.Get("/collections", h.collections)
	r.Get("/collections/{collectionId}", h.collection)
	r.Get("/collections/{collectionId}/queryables", h.queryables)
	r.Get("/collections/{collectionId}/items", h.items)
	r.Get("/collections/{collectionId}/items/{featureId}", h.item)
	r.Get("/api", h.api)
	r.Get("/api.html", h.apiHTML)
}

func (h *workspaceHandler) landing(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusInternalServerError, "ServerError", "workspace not found in context")
		return
	}

	// Check if OGC API is enabled for this workspace
	if ws.Settings == nil || !ws.Settings.OGCAPI.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Features is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.OGCAPI.Public) {
		return
	}
	if !h.validateQueryParameters(w, r, map[string]struct{}{}) {
		return
	}

	// Use workspace OGC API settings for title and description
	title := ws.Name
	description := ws.Description
	if ws.Settings.OGCAPI.Title != "" {
		title = ws.Settings.OGCAPI.Title
	}
	if ws.Settings.OGCAPI.Abstract != "" {
		description = ws.Settings.OGCAPI.Abstract
	}

	base := h.workspaceBaseURL(r, ws.Name)
	resp := LandingPage{
		Title:       title,
		Description: description,
		Links: []Link{
			{Href: base + "/", Rel: "self", Type: "application/json", Title: "Landing page"},
			{Href: base + "/conformance", Rel: "conformance", Type: "application/json"},
			{Href: base + "/collections", Rel: "data", Type: "application/json"},
			{Href: base + "/api", Rel: "service-desc", Type: "application/vnd.oai.openapi+json;version=3.0"},
			{Href: base + "/api.html", Rel: "service-doc", Type: "text/html"},
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *workspaceHandler) conformance(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok || ws.Settings == nil || !ws.Settings.OGCAPI.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Features is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.OGCAPI.Public) {
		return
	}
	if !h.validateQueryParameters(w, r, map[string]struct{}{}) {
		return
	}

	resp := Conformance{
		ConformsTo: claim.URIs(claim.OGCAPIFeaturesKeys...),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *workspaceHandler) collections(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusInternalServerError, "ServerError", "workspace not found in context")
		return
	}

	if ws.Settings == nil || !ws.Settings.OGCAPI.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Features is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.OGCAPI.Public) {
		return
	}
	if !h.validateQueryParameters(w, r, map[string]struct{}{}) {
		return
	}

	// Filter by per-layer read access. The visible set depends on the caller's
	// role, so the cache key must include it to avoid serving one role's listing
	// to another.
	role := workspaceRole(r, ws.ID)
	cacheKey := cache.CollectionsKey(ws.ID) + "|role=" + role
	loader := func(context.Context) ([]byte, error) {
		base := h.workspaceBaseURL(r, ws.Name)
		cols := make([]Collection, 0)
		for _, layer := range ws.VisibleLayers(role) {
			cols = append(cols, Collection{
				ID: layer.PublicID, Title: layer.Title, Description: layer.Description,
				CRS: advertisedCRSs(layer), StorageCRS: storageCRS(layer), Extent: collectionExtent(layer),
				Links: []Link{
					{Href: fmt.Sprintf("%s/collections/%s", base, urlPathEscape(layer.PublicID)), Rel: "self", Type: "application/json"},
					{Href: fmt.Sprintf("%s/collections/%s/items", base, urlPathEscape(layer.PublicID)), Rel: "items", Type: "application/geo+json"},
					{Href: fmt.Sprintf("%s/collections/%s/queryables", base, urlPathEscape(layer.PublicID)), Rel: "http://www.opengis.net/def/rel/ogc/1.0/queryables", Type: "application/schema+json"},
				},
			})
		}
		return json.Marshal(Collections{Collections: cols, Links: []Link{{Href: base + "/collections", Rel: "self", Type: "application/json"}}})
	}
	var data []byte
	var err error
	source := cache.LoadSourceLoaded
	if h.cache != nil {
		data, source, err = h.cache.LoadBytes(r.Context(), cache.CacheTypeCollections, cacheKey, h.cache.CollectionsTTL(), loader)
	} else {
		data, err = loader(r.Context())
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to encode collections")
		return
	}
	if h.cache != nil {
		etag := cache.SetHTTPHeaders(w, r, ws.Settings.OGCAPI.Public, h.cache.CollectionsTTL(), data)
		if cache.IsNotModified(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", strings.ToUpper(string(source)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *workspaceHandler) collection(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusInternalServerError, "ServerError", "workspace not found in context")
		return
	}

	if ws.Settings == nil || !ws.Settings.OGCAPI.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Features is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.OGCAPI.Public) {
		return
	}
	if !h.validateQueryParameters(w, r, map[string]struct{}{}) {
		return
	}

	collectionID := pathParam(r, "collectionId")
	layer, _ := ws.GetLayer(collectionID)
	if layer == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "Not Found", "collection not found")
		return
	}

	base := h.workspaceBaseURL(r, ws.Name)
	resp := Collection{
		ID:          layer.PublicID,
		Title:       layer.Title,
		Description: layer.Description,
		Extent:      collectionExtent(layer),
		CRS:         advertisedCRSs(layer),
		StorageCRS:  storageCRS(layer),
		Links: []Link{
			{Href: fmt.Sprintf("%s/collections/%s", base, urlPathEscape(layer.PublicID)), Rel: "self", Type: "application/json"},
			{Href: fmt.Sprintf("%s/collections/%s/items", base, urlPathEscape(layer.PublicID)), Rel: "items", Type: "application/geo+json"},
			{Href: fmt.Sprintf("%s/collections/%s/queryables", base, urlPathEscape(layer.PublicID)), Rel: "http://www.opengis.net/def/rel/ogc/1.0/queryables", Type: "application/schema+json"},
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *workspaceHandler) items(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusInternalServerError, "ServerError", "workspace not found in context")
		return
	}

	if ws.Settings == nil || !ws.Settings.OGCAPI.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Features is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.OGCAPI.Public) {
		return
	}
	if !h.validateQueryParameters(w, r, itemsQueryParameters) {
		return
	}

	collectionID := pathParam(r, "collectionId")
	layer, svc := ws.GetLayer(collectionID)
	if layer == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "Not Found", "collection not found")
		return
	}

	if svc.DataSource == nil {
		writeErr(w, http.StatusServiceUnavailable, "ServiceUnavailable", "data source not available")
		return
	}
	cacheEnabled := h.cache != nil
	cacheTTL := time.Duration(0)
	if h.cache != nil {
		cacheEnabled, cacheTTL = svc.FeatureCachePolicy(h.cache.FeatureTTL())
	}

	// Parse pagination parameters
	limitDefault, limitMax, maxOffset := h.cfg.Paging.LimitDefault, h.cfg.Paging.LimitMax, h.cfg.Paging.MaxOffset
	if ws.Settings != nil {
		if ws.Settings.OGCAPI.LimitDefault > 0 {
			limitDefault = min(limitDefault, ws.Settings.OGCAPI.LimitDefault)
		}
		if ws.Settings.OGCAPI.LimitMax > 0 {
			limitMax = min(limitMax, ws.Settings.OGCAPI.LimitMax)
		}
		if ws.Settings.OGCAPI.MaxOffset > 0 {
			if maxOffset == 0 {
				maxOffset = ws.Settings.OGCAPI.MaxOffset
			} else {
				maxOffset = min(maxOffset, ws.Settings.OGCAPI.MaxOffset)
			}
		}
	}
	limit := limitDefault
	if v := r.URL.Query().Get("limit"); v != "" {
		li, err := strconv.Atoi(v)
		if err != nil || li <= 0 {
			writeErr(w, http.StatusBadRequest, "InvalidParameterValue", "limit must be a positive integer")
			return
		}
		if li > limitMax {
			li = limitMax
		}
		limit = li
	}

	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		off, err := strconv.Atoi(v)
		if err != nil || off < 0 {
			writeErr(w, http.StatusBadRequest, "InvalidParameterValue", "offset must be a non-negative integer")
			return
		}
		offset = off
	}
	if maxOffset > 0 && offset > maxOffset {
		writeErr(w, http.StatusBadRequest, "InvalidParameterValue", fmt.Sprintf("offset exceeds maximum of %d", maxOffset))
		return
	}

	propertiesStr := r.URL.Query().Get("properties")
	var properties []string
	for _, name := range strings.Split(propertiesStr, ",") {
		if name = strings.TrimSpace(name); name != "" {
			properties = append(properties, name)
		}
	}

	// Parse bbox
	bboxStr := r.URL.Query().Get("bbox")
	var bbox *datasource.BBox
	if bboxStr != "" {
		b, err := query.ParseBBox(bboxStr)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "InvalidParameterValue", err.Error())
			return
		}
		bbox = &datasource.BBox{
			MinX: b.MinX,
			MinY: b.MinY,
			MaxX: b.MaxX,
			MaxY: b.MaxY,
		}
	}

	// Parse CRS parameters. GeoJSON defaults to CRS84 regardless of the native
	// storage CRS (OGC API - Features Part 1 and Part 2).
	crsStr := r.URL.Query().Get("crs")
	outSRID, err := parseAdvertisedCRS(crsStr, layer)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "InvalidParameterValue", err.Error())
		return
	}

	bboxCrsStr := r.URL.Query().Get("bbox-crs")
	bboxSRID := 4326
	if bboxCrsStr != "" {
		srid, err := parseAdvertisedCRS(bboxCrsStr, layer)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "InvalidParameterValue", err.Error())
			return
		}
		bboxSRID = srid
	}
	if bbox != nil && bbox.MinX > bbox.MaxX && bboxSRID != 4326 {
		writeErr(w, http.StatusBadRequest, "InvalidParameterValue", "an antimeridian-crossing bbox is only valid in CRS84")
		return
	}

	// Parse filter
	filterStr := r.URL.Query().Get("filter")
	filterLang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("filter-lang")))
	if filterLang == "" {
		filterLang = "cql2-text"
	}
	if filterLang != "cql2-text" {
		writeErr(w, http.StatusBadRequest, "InvalidParameterValue", "filter-lang must be cql2-text")
		return
	}
	filterCrsStr := r.URL.Query().Get("filter-crs")
	filterSRID := 4326
	if filterCrsStr != "" {
		srid, err := parseAdvertisedCRS(filterCrsStr, layer)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "InvalidParameterValue", err.Error())
			return
		}
		filterSRID = srid
	}

	dateTimeStr := r.URL.Query().Get("datetime")
	dateTimeSelection, err := parseDateTime(dateTimeStr, timeDimension(layer))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "InvalidParameterValue", err.Error())
		return
	}
	effectiveFilter := combineCQL(filterStr, dateTimeCQL(dateTimeSelection))
	if effectiveFilter != "" {
		meta, metaErr := queryableMetadataForLayer(r.Context(), layer, svc)
		if metaErr != nil {
			h.logger.Error("feature metadata failed", "collection", collectionID, "err", metaErr)
			writeErr(w, http.StatusInternalServerError, "ServerError", "failed to inspect collection schema")
			return
		}
		if err := validateCQL2Text(meta, svc.DataSource.Type(), effectiveFilter, filterSRID); err != nil {
			writeErr(w, http.StatusBadRequest, "InvalidParameterValue", "invalid CQL2 filter: "+err.Error())
			return
		}
	}

	// Parse sortby
	sortByStr := r.URL.Query().Get("sortby")
	sortBy, err := h.parseSortBy(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "InvalidParameterValue", err.Error())
		return
	}

	// Build cache key from query parameters
	cacheKey := cache.FeaturesKey(cache.FeaturesParams{
		Workspace:  ws.ID,
		Collection: collectionID,
		Limit:      limit,
		Offset:     offset,
		BBox:       bboxStr,
		BBoxCRS:    bboxCrsStr,
		Filter:     filterStr,
		FilterLang: filterLang,
		FilterCRS:  filterCrsStr,
		DateTime:   dateTimeStr,
		CRS:        crsStr,
		SortBy:     sortByStr,
		Properties: propertiesStr,
	})

	cacheFill := h.cache.BeginFill(cache.CacheTypeFeatures, cacheKey)
	readCache, writeCache := cache.RequestCachePolicy(r)
	cacheEnabled = cacheEnabled && writeCache

	// Try to get from cache first
	if cacheEnabled && readCache {
		if cached, ok := h.cache.GetFeatures(cacheKey); ok {
			etag := cache.SetHTTPHeaders(w, r, ws.Settings.OGCAPI.Public, cacheTTL, cached)
			if cache.IsNotModified(r, etag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			outCRSURI := query.CRSURIFromSRID(outSRID)
			w.Header().Set("Content-Type", "application/geo+json")
			w.Header().Set("Content-Crs", "<"+outCRSURI+">")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			w.Write(cached)
			return
		}
	}

	// Build query params
	queryParams := datasource.QueryParams{
		Limit:      limit + 1,
		Offset:     offset,
		BBox:       bbox,
		BBoxSRID:   bboxSRID,
		OutputSRID: outSRID,
		Filter:     filterStr,
		FilterSRID: filterSRID,
		SortBy:     sortBy,
		Properties: properties,
		DateTime:   dateTimeSelection,
	}

	// Query features - use SQL View query if this is a SQL View layer
	var features []json.RawMessage
	var queryErr error
	if layer.IsSQLView {
		features, queryErr = layer.QueryFeatures(r.Context(), svc.DataSource, queryParams)
	} else if streaming, ok := svc.DataSource.(datasource.StreamingDataSource); ok {
		stream, err := streaming.QueryStream(r.Context(), layer.SourceLayer, queryParams)
		if err != nil {
			queryErr = err
		} else {
			defer stream.Close()
			for stream.Next() {
				features = append(features, append(json.RawMessage(nil), stream.Feature()...))
			}
			if stream.Err() != nil {
				queryErr = stream.Err()
			}
		}
	} else {
		features, queryErr = svc.DataSource.Query(r.Context(), layer.SourceLayer, queryParams)
	}
	if queryErr != nil {
		// Log full detail server-side; return a generic message so raw DB/query
		// errors (SQL fragments, schema names) are not disclosed to the client.
		h.logger.Error("list features failed", "collection", collectionID, "err", queryErr)
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to query features")
		return
	}

	base := h.workspaceBaseURL(r, ws.Name)
	outCRSURI := query.CRSURIFromSRID(outSRID)
	if features == nil {
		features = make([]json.RawMessage, 0)
	}
	hasNext := len(features) > limit
	if hasNext {
		features = features[:limit]
	}

	// numberMatched is an exact count over the same selection, independent of
	// paging and projection. Keep it bounded so a slow count cannot turn an
	// otherwise healthy feature page into an unbounded request.
	countParams := queryParams
	countParams.Limit = 0
	countParams.Offset = 0
	countParams.OutputSRID = 0
	countParams.SortBy = nil
	countParams.Properties = nil
	countCacheKey := cache.FeaturesKey(cache.FeaturesParams{
		Workspace: ws.ID, Collection: collectionID, BBox: bboxStr, BBoxCRS: bboxCrsStr,
		Filter: filterStr, FilterLang: filterLang, FilterCRS: filterCrsStr, DateTime: dateTimeStr,
	}) + ":count"
	countTimeout := h.cfg.Paging.CountTimeoutMS
	if countTimeout <= 0 {
		countTimeout = 5000
	}
	countCtx, cancelCount := context.WithTimeout(r.Context(), time.Duration(countTimeout)*time.Millisecond)
	defer cancelCount()
	countLoader := func(loadCtx context.Context) (int, error) {
		// Cache fill coalescing may detach from the request context; apply the
		// count deadline again so the backing database operation stays bounded.
		boundedCtx, cancel := context.WithTimeout(loadCtx, time.Duration(countTimeout)*time.Millisecond)
		defer cancel()
		return layer.CountFeatures(boundedCtx, svc.DataSource, countParams)
	}
	var numberMatched *int
	var matched int
	countFill := h.cache.BeginFill(cache.CacheTypeCounts, countCacheKey)
	if h.cache != nil && readCache {
		matched, _, err = h.cache.LoadCount(countCtx, countCacheKey, countLoader)
	} else {
		matched, err = countLoader(countCtx)
		if err == nil && h.cache != nil && writeCache {
			h.cache.SetCount(countCacheKey, matched, countFill)
		}
	}
	if err == nil {
		numberMatched = &matched
	} else {
		h.logger.Warn("feature count unavailable; omitting numberMatched", "collection", collectionID, "error", err)
	}

	resp := FeatureCollection{
		Type:           "FeatureCollection",
		Features:       features,
		NumberMatched:  numberMatched,
		NumberReturned: len(features),
		TimeStamp:      time.Now().UTC().Format(time.RFC3339Nano),
		Links:          itemsPageLinks(base, layer, r, limit, offset, hasNext),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to encode features")
		return
	}
	if cacheEnabled {
		h.cache.SetFeatures(cacheKey, data, cacheTTL, cacheFill)
		cache.SetHTTPHeaders(w, r, ws.Settings.OGCAPI.Public, cacheTTL, data)
	}
	if !writeCache {
		cache.SetHTTPHeaders(w, r, false, 0, data)
	}
	w.Header().Set("Content-Crs", "<"+outCRSURI+">")
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *workspaceHandler) item(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusInternalServerError, "ServerError", "workspace not found in context")
		return
	}

	if ws.Settings == nil || !ws.Settings.OGCAPI.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Features is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.OGCAPI.Public) {
		return
	}
	if !h.validateQueryParameters(w, r, itemQueryParameters) {
		return
	}

	collectionID := pathParam(r, "collectionId")
	layer, svc := ws.GetLayer(collectionID)
	if layer == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "Not Found", "collection not found")
		return
	}

	if svc.DataSource == nil {
		writeErr(w, http.StatusServiceUnavailable, "ServiceUnavailable", "data source not available")
		return
	}
	cacheEnabled := h.cache != nil
	cacheTTL := time.Duration(0)
	if h.cache != nil {
		cacheEnabled, cacheTTL = svc.FeatureCachePolicy(h.cache.FeatureTTL())
	}

	featureID := pathParam(r, "featureId")

	// GeoJSON defaults to CRS84, independent of storage CRS.
	outSRID, err := parseAdvertisedCRS(r.URL.Query().Get("crs"), layer)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "InvalidParameterValue", err.Error())
		return
	}
	itemCacheKey := cache.SingleFeatureKey(ws.ID, collectionID, featureID, strconv.Itoa(outSRID))
	cacheFill := h.cache.BeginFill(cache.CacheTypeFeatures, itemCacheKey)
	if cacheEnabled {
		if body, ok := h.cache.GetFeatures(itemCacheKey); ok {
			etag := cache.SetHTTPHeaders(w, r, ws.Settings.OGCAPI.Public, cacheTTL, body)
			if cache.IsNotModified(r, etag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Type", "application/geo+json")
			w.Header().Set("Content-Crs", contentCRSValue(outSRID))
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
	}

	feat, found, err := layer.FeatureByID(r.Context(), svc.DataSource, featureID, outSRID)
	if err != nil {
		if _, ok := err.(datasource.InvalidFeatureIDError); ok {
			writeErr(w, http.StatusNotFound, "Not Found", "feature not found")
			return
		}
		h.logger.Error("get feature failed", "collection", collectionID, "err", err)
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to query feature")
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "Not Found", "feature not found")
		return
	}

	// Add self link to the feature
	base := h.workspaceBaseURL(r, ws.Name)
	itemPath := fmt.Sprintf("%s/collections/%s/items/%s", base, urlPathEscape(collectionID), urlPathEscape(featureID))
	itemQuery := cloneValues(r.URL.Query())
	itemQuery.Del("apikey")
	var featureMap map[string]any
	if err := json.Unmarshal(feat, &featureMap); err != nil {
		h.logger.Error("failed to parse feature json", "err", err)
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to process feature")
		return
	}
	featureMap["links"] = []Link{
		{Href: withQuery(itemPath, itemQuery), Rel: "self", Type: "application/geo+json"},
		{Href: fmt.Sprintf("%s/collections/%s", base, urlPathEscape(collectionID)), Rel: "collection", Type: "application/json"},
	}

	outCRSURI := query.CRSURIFromSRID(outSRID)
	body, err := json.Marshal(featureMap)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to process feature")
		return
	}
	if cacheEnabled {
		h.cache.SetFeatures(itemCacheKey, body, cacheTTL, cacheFill)
		cache.SetHTTPHeaders(w, r, ws.Settings.OGCAPI.Public, cacheTTL, body)
	}
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Content-Crs", "<"+outCRSURI+">")
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *workspaceHandler) api(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusInternalServerError, "ServerError", "workspace not found in context")
		return
	}

	if ws.Settings == nil || !ws.Settings.OGCAPI.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Features is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.OGCAPI.Public) {
		return
	}
	if !h.validateQueryParameters(w, r, map[string]struct{}{}) {
		return
	}

	// Build OpenAPI spec for this workspace
	doc := h.buildWorkspaceOpenAPI(ws)
	base := h.workspaceBaseURL(r, ws.Name)
	doc.Servers = openapi3.Servers{{URL: base}}
	data, err := json.Marshal(doc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to encode OpenAPI document")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.0")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *workspaceHandler) apiHTML(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok || ws.Settings == nil || !ws.Settings.OGCAPI.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Features is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.OGCAPI.Public) {
		return
	}
	if !h.validateQueryParameters(w, r, map[string]struct{}{}) {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerUIHTML()))
}

func (h *workspaceHandler) workspaceBaseURL(r *http.Request, wsName string) string {
	return strings.TrimRight(h.cfg.Server.UrlBase, "/") + h.cfg.Server.BasePath + "/workspaces/" + urlPathEscape(wsName) + "/ogc"
}

func (h *workspaceHandler) parseSortBy(r *http.Request) ([]datasource.SortField, error) {
	v := r.URL.Query().Get("sortby")
	if v == "" {
		return nil, nil
	}

	parts := strings.Split(v, ",")
	out := make([]datasource.SortField, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		desc := false
		if strings.HasPrefix(part, "-") {
			desc = true
			part = strings.TrimPrefix(part, "-")
		} else if strings.HasPrefix(part, "+") {
			part = strings.TrimPrefix(part, "+")
		}
		out = append(out, datasource.SortField{Name: part, Desc: desc})
	}
	return out, nil
}

// convertWorkspaceSQLViewConfig converts workspace.SQLViewConfig to datasource.SQLViewConfig.
func convertWorkspaceSQLViewConfig(cfg *workspace.SQLViewConfig) *datasource.SQLViewConfig {
	if cfg == nil {
		return nil
	}
	dsCfg := &datasource.SQLViewConfig{
		SQL:            cfg.SQL,
		GeometryColumn: cfg.GeometryColumn,
		GeometryType:   cfg.GeometryType,
		SRID:           cfg.SRID,
		IDColumn:       cfg.IDColumn,
		ReadOnly:       cfg.ReadOnly,
	}
	if len(cfg.Properties) > 0 {
		dsCfg.Properties = make([]*datasource.SQLViewProperty, len(cfg.Properties))
		for i, prop := range cfg.Properties {
			dsCfg.Properties[i] = &datasource.SQLViewProperty{
				Name: prop.Name,
				Type: prop.Type,
			}
		}
	}
	return dsCfg
}
