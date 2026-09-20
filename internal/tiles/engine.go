package tiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/workspace"
	_ "golang.org/x/image/webp"
	"golang.org/x/sync/singleflight"
)

var ErrRenderQueueFull = errors.New("tile render queue is full")

// EngineRequest is the protocol-neutral request used by OGC API - Tiles,
// WMTS, and cache jobs. Row and Column always use the OGC tile-matrix order.
type EngineRequest struct {
	Workspace *workspace.Workspace
	Resource  *workspace.PublishedResource
	TileType  string
	MatrixSet string
	Zoom      int
	Column    int
	Row       int
	Format    string
	Style     string
	Time      string
	Elevation string
	UseCache  bool
	Force     bool
}

type EngineResult struct {
	Data        []byte
	ContentType string
	CacheStatus string
	CacheTier   string
	Identity    tilecache.Identity
}

// Engine owns the common render and canonical cache path. Protocol handlers
// remain responsible for their own negotiation, authorization, and errors.
type Engine struct {
	cfg          conf.Config
	logger       *slog.Logger
	memory       *cache.Manager
	persistent   *tilecache.Manager
	mvtGen       *MVTGenerator
	mapGen       *MapTileGenerator
	renderSlots  chan struct{}
	queueTimeout time.Duration
	renderGroup  *singleflight.Group
	configHash   uint64
}

func NewEngine(cfg conf.Config, logger *slog.Logger, memory *cache.Manager, persistent *tilecache.Manager) *Engine {
	renderer.ConfigureFontPaths(cfg.WMS.FontPaths)
	mvtGen := NewMVTGenerator(cfg.Tiles.TileSize)
	mvtGen.SetLimits(cfg.Tiles.MaxFeatures, cfg.Tiles.MaxVertices, cfg.Tiles.MaxTileBytes, cfg.Tiles.StatementTimeoutMS)
	mapGen := NewMapTileGenerator(256)
	mapGen.SetLimits(cfg.Tiles.MaxFeatures, cfg.Tiles.MaxVertices, cfg.Tiles.MaxTileBytes)
	mapGen.SetGraphicConfig(cfg.WMS)
	concurrency := cfg.Tiles.MaxConcurrentRenders
	if concurrency <= 0 {
		concurrency = runtime.GOMAXPROCS(0) / 2
		if concurrency < 1 {
			concurrency = 1
		}
		if concurrency > 4 {
			concurrency = 4
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		cfg: cfg, logger: logger, memory: memory, persistent: persistent,
		mvtGen: mvtGen, mapGen: mapGen, renderSlots: make(chan struct{}, concurrency),
		queueTimeout: time.Duration(cfg.Tiles.RenderQueueTimeoutMS) * time.Millisecond,
		renderGroup:  &singleflight.Group{},
		configHash:   tileRenderConfigHash(cfg.Tiles),
	}
}

func (e *Engine) acquireRender(ctx context.Context) (func(), bool) {
	timeout := e.queueTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case e.renderSlots <- struct{}{}:
		return func() { <-e.renderSlots }, true
	case <-ctx.Done():
		return nil, false
	case <-timer.C:
		return nil, false
	}
}

func (e *Engine) Fetch(ctx context.Context, request EngineRequest) (*EngineResult, error) {
	identity, renderStyle, contentType, err := e.identity(request)
	if err != nil {
		return nil, err
	}
	request.Style = renderStyle
	_, dataPending := request.Workspace.DataCacheState()
	cacheBypass := request.UseCache && (dataPending || !cacheParametersAllow(request))
	if cacheBypass {
		request.UseCache = false
		request.Force = false
	}
	memoryKey := canonicalMemoryKey(identity)
	cacheFill := e.memory.BeginFill(cache.CacheTypeTiles, memoryKey)
	if request.UseCache && !request.Force {
		if data, ok := e.getMemory(memoryKey); ok {
			return &EngineResult{Data: data, ContentType: contentType, CacheStatus: "HIT", CacheTier: "memory", Identity: identity}, nil
		}
		if data, _, ok, err := e.getPersistent(ctx, identity); err != nil {
			e.logger.Warn("persistent tile cache read failed", "key", identity.CanonicalKey(), "error", err)
		} else if ok {
			e.setMemory(memoryKey, data, cacheFill)
			return &EngineResult{Data: data, ContentType: contentType, CacheStatus: "HIT", CacheTier: "persistent", Identity: identity}, nil
		}
	}

	value, err, _ := e.renderGroup.Do(cacheFill.CoalescingKey(identity.CanonicalKey()), func() (any, error) {
		if request.UseCache && !request.Force {
			if data, ok := e.getMemory(memoryKey); ok {
				return data, nil
			}
			if data, _, ok, readErr := e.getPersistent(ctx, identity); readErr == nil && ok {
				e.setMemory(memoryKey, data, cacheFill)
				return data, nil
			}
		}
		release, ok := e.acquireRender(ctx)
		if !ok {
			return nil, ErrRenderQueueFull
		}
		defer release()
		data, renderErr := e.render(ctx, request)
		if renderErr != nil {
			return nil, renderErr
		}
		if request.UseCache {
			e.setMemory(memoryKey, data, cacheFill)
			if e.persistent != nil {
				stored, writeErr := e.persistent.Put(context.WithoutCancel(ctx), identity, data, tilecache.Policy{
					WorkspaceQuotaBytes: request.Workspace.Settings.OGCTilesAPI.Settings.PersistentCacheQuotaBytes,
					ResourceQuotaBytes:  resourceQuota(request.Resource),
				})
				if writeErr != nil {
					e.logger.Warn("persistent tile cache write failed", "key", identity.CanonicalKey(), "error", writeErr)
				} else if !stored {
					e.logger.Debug("persistent tile cache skipped tile due to quota", "key", identity.CanonicalKey())
				}
			}
		}
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	status := "MISS"
	if cacheBypass {
		status = "BYPASS"
	}
	return &EngineResult{Data: value.([]byte), ContentType: contentType, CacheStatus: status, CacheTier: "render", Identity: identity}, nil
}

func cacheParametersAllow(request EngineRequest) bool {
	if request.Resource == nil || request.Resource.Layer == nil {
		return true
	}
	policy := request.Resource.Layer.TileCacheParameters
	if policy == nil {
		// Preserve caching for the finite style catalog, but never admit an
		// unbounded dimension value without an explicit allowlist.
		return request.Time == "" && request.Elevation == ""
	}
	style := request.Style
	if style == "" {
		style = "default"
	}
	return allowedCacheValue(policy.Styles, style, true) && allowedCacheValue(policy.Times, request.Time, false) && allowedCacheValue(policy.Elevations, request.Elevation, false)
}

func allowedCacheValue(allowlist []string, value string, allowAnyWhenEmpty bool) bool {
	if len(allowlist) == 0 {
		return allowAnyWhenEmpty || value == ""
	}
	return slices.Contains(allowlist, value)
}

// ResolveIdentity validates a request and returns the exact durable cache key
// components without reading or rendering the tile.
func (e *Engine) ResolveIdentity(request EngineRequest) (tilecache.Identity, error) {
	identity, _, _, err := e.identity(request)
	return identity, err
}

func (e *Engine) ResolveStyleDigest(ws *workspace.Workspace, resource *workspace.PublishedResource, style string) (string, error) {
	digest, _, err := resolvedStyle(ws, resource, style)
	return digest, err
}

func (e *Engine) ResolveStyleName(ws *workspace.Workspace, resource *workspace.PublishedResource, style string) (string, error) {
	_, name, err := resolvedStyle(ws, resource, style)
	if name == "" {
		name = "default"
	}
	return name, err
}

func (e *Engine) identity(request EngineRequest) (tilecache.Identity, string, string, error) {
	if request.Workspace == nil || request.Resource == nil {
		return tilecache.Identity{}, "", "", errors.New("tile request has no workspace or resource")
	}
	if request.Workspace.Settings == nil {
		return tilecache.Identity{}, "", "", errors.New("tile request workspace has no settings")
	}
	if request.TileType != "map" && request.TileType != "vector" {
		return tilecache.Identity{}, "", "", fmt.Errorf("unsupported tile type %q", request.TileType)
	}
	if err := ValidateTileCoords(request.MatrixSet, request.Zoom, request.Column, request.Row); err != nil {
		return tilecache.Identity{}, "", "", err
	}
	matrixSet, err := GetTileMatrixSetDefinition(request.MatrixSet)
	if err != nil {
		return tilecache.Identity{}, "", "", err
	}
	if request.Zoom < e.cfg.Tiles.MinZoom || request.Zoom > e.cfg.Tiles.MaxZoom {
		return tilecache.Identity{}, "", "", errors.New("zoom level outside configured range")
	}

	resourceID, generation, kind := resourceIdentity(request.Resource)
	if resourceID == "" {
		return tilecache.Identity{}, "", "", errors.New("tile resource has no stable identifier")
	}
	format := request.Format
	contentType := MediaTypeMVT
	styleDigest, styleName := "", ""
	if request.TileType == "vector" {
		if request.Resource.Kind != workspace.ResourceFeature {
			return tilecache.Identity{}, "", "", errors.New("vector tiles require a feature layer")
		}
		format = MediaTypeMVT
	} else {
		tileFormat, err := ParseTileFormat(format)
		if err != nil {
			return tilecache.Identity{}, "", "", err
		}
		format, contentType = GetContentType(tileFormat), GetContentType(tileFormat)
		styleDigest, styleName, err = resolvedStyle(request.Workspace, request.Resource, request.Style)
		if err != nil {
			return tilecache.Identity{}, "", "", err
		}
		if err = e.validateStyleExtensions(request.Workspace, request.Resource, styleName); err != nil {
			return tilecache.Identity{}, "", "", err
		}
		if request.Resource.Kind == workspace.ResourceGroup {
			dependencies, dependencyErr := groupDependencyDigest(request.Workspace, request.Resource.Group, styleName, make(map[string]bool), 1)
			if dependencyErr != nil {
				return tilecache.Identity{}, "", "", dependencyErr
			}
			styleDigest += "#deps=" + dependencies
		}
		if request.Time != "" || request.Elevation != "" {
			digest := sha256.Sum256([]byte(request.Time + "\x00" + request.Elevation))
			styleDigest += "#dim=" + hex.EncodeToString(digest[:8])
		}
	}
	styleDigest += "#tms=" + matrixSet.cacheDigest
	styleDigest += "#assets=" + request.Workspace.StyleAssetDigest
	dataRevision, _ := request.Workspace.DataCacheState()
	styleDigest += fmt.Sprintf("#data=%d", dataRevision)
	if generation <= 0 {
		generation = 1
	}
	workspaceRevision := effectiveRenderRevision(request.Workspace.TileRevision, e.configHash)
	identity := tilecache.Identity{
		WorkspaceID: request.Workspace.ID, WorkspaceRevision: workspaceRevision,
		ResourceID: resourceID, ResourceKind: kind,
		Generation: generation, TileType: request.TileType, MatrixSet: request.MatrixSet,
		Zoom: request.Zoom, Column: request.Column, Row: request.Row,
		StyleDigest: styleDigest, Format: format,
	}
	if request.TileType == "map" {
		identity.StyleName = styleName
		if identity.StyleName == "" {
			identity.StyleName = "default"
		}
	}
	return identity, styleName, contentType, identity.Validate()
}

func (e *Engine) validateStyleExtensions(ws *workspace.Workspace, resource *workspace.PublishedResource, styleName string) error {
	if styleName == "" {
		return nil
	}
	stored := ws.GetStyle(styleName)
	if stored == nil {
		return errors.New("style not found")
	}
	doc, err := stored.CompiledDocument()
	if err != nil {
		return errors.New("style is invalid")
	}
	style, err := doc.GetStyle(resource.PublicID(), "")
	if err != nil {
		style, err = doc.GetDefaultStyle()
	}
	if err != nil {
		return errors.New("style is invalid")
	}
	enabled := func(name string) bool {
		return ws.Settings != nil && slices.Contains(e.cfg.WMS.Extensions, name) && slices.Contains(ws.Settings.WMS.Extensions, name)
	}
	if !sld.IsSLDFormat(stored.Format) && !enabled("dynamic-style") {
		return errors.New("style requires the disabled dynamic-style extension")
	}
	if sld.StyleUsesAdvancedLabels(style) && !enabled("advanced-labels") {
		return errors.New("style requires the disabled advanced-labels extension")
	}
	if sld.StyleUsesTransformation(style) && !enabled("rendering-transformations") {
		return errors.New("style requires the disabled rendering-transformations extension")
	}
	if sld.StyleUsesCompositing(style) && !enabled("compositing") {
		return errors.New("style requires the disabled compositing extension")
	}
	if sld.StyleUsesZOrder(style) && !enabled("z-order") {
		return errors.New("style requires the disabled z-order extension")
	}
	if sld.StyleUsesRemoteGraphics(style) && !enabled("remote-graphics") {
		return errors.New("style requires the disabled remote-graphics extension")
	}
	if sld.StyleUsesDynamicExpressions(style) && !enabled("dynamic-style") {
		return errors.New("style requires the disabled dynamic-style extension")
	}
	return nil
}

func resolvedStyle(ws *workspace.Workspace, resource *workspace.PublishedResource, requested string) (string, string, error) {
	if requested == "default" {
		requested = ""
	}
	name := requested
	if name == "" && resource.Kind == workspace.ResourceFeature {
		name = resource.Layer.DefaultStyle
	}
	if name == "" && resource.Kind == workspace.ResourceCoverage {
		name = resource.Coverage.DefaultStyle
	}
	if name == "" && resource.Kind == workspace.ResourceGroup {
		name = resource.Group.DefaultStyle
	}
	if name == "" {
		return "default", "", nil
	}
	if !sld.ValidStyleName(name) {
		return "", "", errors.New("invalid style name")
	}
	stored := ws.GetStyle(name)
	if stored == nil {
		return "", "", errors.New("style not found")
	}
	_, err := stored.CompiledDocument()
	if err != nil {
		return "", "", errors.New("style is invalid")
	}
	hash := sha256.Sum256([]byte(stored.SLDBody))
	return name + "@" + hex.EncodeToString(hash[:16]), name, nil
}

func groupDependencyDigest(ws *workspace.Workspace, group *workspace.LayerGroup, inheritedStyle string, visiting map[string]bool, depth int) (string, error) {
	if ws == nil || group == nil || depth > 32 || visiting[group.PublicID] {
		return "", errors.New("invalid layer group dependencies")
	}
	visiting[group.PublicID] = true
	defer delete(visiting, group.PublicID)
	var value strings.Builder
	fmt.Fprintf(&value, "group-composite-v2|%s:%d", group.PublicID, group.TileCacheGeneration)
	for _, member := range group.Members {
		resource := ws.GetResource(member.Resource)
		if resource == nil {
			return "", fmt.Errorf("layer group member %q does not exist", member.Resource)
		}
		resourceID, generation, kind := resourceIdentity(resource)
		style := member.Style
		if inheritedStyle != "" {
			style = inheritedStyle
		}
		styleDigest, renderStyle, err := resolvedStyle(ws, resource, style)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&value, "|%s:%s:%d:%s:%g:%s", kind, resourceID, generation, styleDigest, member.EffectiveOpacity(), member.Composite)
		if resource.Kind == workspace.ResourceGroup {
			nested, err := groupDependencyDigest(ws, resource.Group, renderStyle, visiting, depth+1)
			if err != nil {
				return "", err
			}
			value.WriteString("{" + nested + "}")
		}
	}
	digest := sha256.Sum256([]byte(value.String()))
	return hex.EncodeToString(digest[:16]), nil
}

func resourceIdentity(resource *workspace.PublishedResource) (string, int64, string) {
	if resource.Kind == workspace.ResourceFeature && resource.Layer != nil {
		id := resource.Layer.ID
		if id == "" {
			id = resource.Layer.PublicID
		}
		return id, resource.Layer.TileCacheGeneration, string(workspace.ResourceFeature)
	}
	if resource.Kind == workspace.ResourceCoverage && resource.Coverage != nil {
		id := resource.Coverage.ID
		if id == "" {
			id = resource.Coverage.PublicID
		}
		return id, resource.Coverage.TileCacheGeneration, string(workspace.ResourceCoverage)
	}
	if resource.Kind == workspace.ResourceGroup && resource.Group != nil {
		id := resource.Group.ID
		if id == "" {
			id = resource.Group.PublicID
		}
		return id, resource.Group.TileCacheGeneration, string(workspace.ResourceGroup)
	}
	return "", 0, ""
}

func resourceQuota(resource *workspace.PublishedResource) int64 {
	if resource == nil {
		return 0
	}
	if resource.Layer != nil {
		return resource.Layer.TileCacheQuotaBytes
	}
	if resource.Coverage != nil {
		return resource.Coverage.TileCacheQuotaBytes
	}
	if resource.Group != nil {
		return resource.Group.TileCacheQuotaBytes
	}
	return 0
}

func canonicalMemoryKey(identity tilecache.Identity) string {
	prefix := "maptile"
	if identity.TileType == "vector" {
		prefix = "mvt"
	}
	return fmt.Sprintf("%s:%s:%s", prefix, identity.WorkspaceID, identity.CanonicalKey())
}

func (e *Engine) getMemory(key string) ([]byte, bool) {
	if e.memory == nil {
		return nil, false
	}
	return e.memory.GetTile(key)
}

func (e *Engine) setMemory(key string, data []byte, token cache.FillToken) {
	if e.memory != nil {
		e.memory.SetTile(key, data, 0, token)
	}
}

func (e *Engine) getPersistent(ctx context.Context, identity tilecache.Identity) ([]byte, *tilecache.Entry, bool, error) {
	if e.persistent == nil {
		return nil, nil, false, nil
	}
	return e.persistent.Get(ctx, identity)
}

func (e *Engine) render(ctx context.Context, request EngineRequest) ([]byte, error) {
	resource := request.Resource
	if resource.Kind == workspace.ResourceGroup {
		return e.renderLayerGroup(ctx, request, make(map[string]bool), 1)
	}
	if request.TileType == "map" && resource.Kind == workspace.ResourceCoverage {
		format, err := ParseTileFormat(request.Format)
		if err != nil {
			return nil, err
		}
		return e.generateCoverageMapTile(ctx, request.Workspace, resource, request.MatrixSet, request.Zoom, request.Column, request.Row, format, request.Style, request.Time, request.Elevation)
	}
	layer, service := resource.Layer, resource.Service
	if layer == nil || service == nil || service.DataSource == nil {
		return nil, errors.New("feature data source is not available")
	}
	layerInfo, sqlViewConfig, err := tileLayerInfo(ctx, service.DataSource, layer)
	if err != nil {
		return nil, err
	}
	limits := request.Workspace.Settings.OGCTilesAPI.Settings
	if request.TileType == "vector" {
		if sqlViewConfig != nil {
			sqlDS, ok := service.DataSource.(datasource.SQLViewDataSource)
			if !ok {
				return nil, errors.New("data source does not support SQL views")
			}
			return e.mvtGen.GenerateSQLViewTileWithLimits(ctx, sqlDS, sqlViewConfig, layerInfo, request.MatrixSet, request.Zoom, request.Column, request.Row, limits.MaxFeatures, limits.MaxVertices, limits.MaxTileBytes, layer.PublicID)
		}
		return e.mvtGen.GenerateTileWithLimits(ctx, service.DataSource, layerInfo, request.MatrixSet, request.Zoom, request.Column, request.Row, limits.MaxFeatures, limits.MaxVertices, limits.MaxTileBytes, layer.PublicID)
	}
	format, err := ParseTileFormat(request.Format)
	if err != nil {
		return nil, err
	}
	dimensionFilter, err := tileDimensionFilter(layer, request.Time, request.Elevation)
	if err != nil {
		return nil, err
	}
	if policy := layer.TileCacheParameters; policy != nil && (policy.MetatileFactor > 1 || policy.GutterPixels > 0) {
		return e.renderFeatureMetatile(ctx, request, service.DataSource, layer, layerInfo, sqlViewConfig, format, dimensionFilter)
	}
	if sqlViewConfig != nil {
		sqlDS, ok := service.DataSource.(datasource.SQLViewDataSource)
		if !ok {
			return nil, errors.New("data source does not support SQL views")
		}
		return e.mapGen.GenerateSQLViewTileWithFilterLimits(ctx, sqlDS, sqlViewConfig, layerInfo, request.Workspace, request.MatrixSet, request.Zoom, request.Column, request.Row, format, request.Style, layer.PublicID, true, nil, limits.MaxFeatures, limits.MaxVertices, limits.MaxTileBytes, dimensionFilter)
	}
	return e.mapGen.GenerateTileWithFilterLimits(ctx, service.DataSource, layerInfo, request.Workspace, request.MatrixSet, request.Zoom, request.Column, request.Row, format, request.Style, layer.PublicID, true, nil, limits.MaxFeatures, limits.MaxVertices, limits.MaxTileBytes, dimensionFilter)
}

func (e *Engine) renderFeatureMetatile(ctx context.Context, request EngineRequest, source datasource.DataSource, layer *workspace.Layer, layerInfo *datasource.LayerInfo, sqlViewConfig *datasource.SQLViewConfig, format TileFormat, filter string) ([]byte, error) {
	policy := layer.TileCacheParameters
	factor, gutter := max(1, policy.MetatileFactor), max(0, policy.GutterPixels)
	factor = min(factor, max(1, e.cfg.Tiles.MaxMetatileFactor))
	gutter = min(gutter, max(0, e.cfg.Tiles.MaxGutterPixels))
	definition, err := GetTileMatrixSetDefinition(request.MatrixSet)
	if err != nil {
		return nil, err
	}
	matrix := matrixForZoom(definition, request.Zoom)
	if matrix == nil {
		return nil, errors.New("tile matrix is unavailable")
	}
	startColumn := request.Column / factor * factor
	startRow := request.Row / factor * factor
	endColumn := min(startColumn+factor-1, matrix.MatrixWidth-1)
	endRow := min(startRow+factor-1, matrix.MatrixHeight-1)
	nativeStart, err := TileBBox(request.MatrixSet, request.Zoom, startColumn, startRow)
	if err != nil {
		return nil, err
	}
	nativeEnd, err := TileBBox(request.MatrixSet, request.Zoom, endColumn, endRow)
	if err != nil {
		return nil, err
	}
	wgsStart, err := TileBBoxWGS84(request.MatrixSet, request.Zoom, startColumn, startRow)
	if err != nil {
		return nil, err
	}
	wgsEnd, err := TileBBoxWGS84(request.MatrixSet, request.Zoom, endColumn, endRow)
	if err != nil {
		return nil, err
	}
	nativePadX := (nativeStart.MaxX - nativeStart.MinX) * float64(gutter) / 256
	nativePadY := (nativeStart.MaxY - nativeStart.MinY) * float64(gutter) / 256
	wgsPadX := (wgsStart.MaxX - wgsStart.MinX) * float64(gutter) / 256
	wgsPadY := (wgsStart.MaxY - wgsStart.MinY) * float64(gutter) / 256
	nativeBounds := &TileBounds{MinX: nativeStart.MinX - nativePadX, MinY: nativeEnd.MinY - nativePadY, MaxX: nativeEnd.MaxX + nativePadX, MaxY: nativeStart.MaxY + nativePadY}
	wgsBounds := &TileBounds{MinX: wgsStart.MinX - wgsPadX, MinY: wgsEnd.MinY - wgsPadY, MaxX: wgsEnd.MaxX + wgsPadX, MaxY: wgsStart.MaxY + wgsPadY}
	columns, rows := endColumn-startColumn+1, endRow-startRow+1
	width, height := columns*256+2*gutter, rows*256+2*gutter
	limited := *e.mapGen
	limits := request.Workspace.Settings.OGCTilesAPI.Settings
	if limits.MaxFeatures > 0 {
		limited.maxFeatures = min(limited.maxFeatures, limits.MaxFeatures)
	}
	if limits.MaxVertices > 0 {
		limited.maxVertices = min(limited.maxVertices, limits.MaxVertices)
	}
	if limits.MaxTileBytes > 0 {
		limited.maxTileBytes = min(limited.maxTileBytes, limits.MaxTileBytes*columns*rows)
	}
	var override renderQuery
	if sqlViewConfig != nil {
		sqlSource, ok := source.(datasource.SQLViewDataSource)
		if !ok {
			return nil, errors.New("data source does not support SQL views")
		}
		override = func(queryCtx context.Context, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
			params.Filter = filter
			return sqlSource.QuerySQLViewWKB(queryCtx, sqlViewConfig, params)
		}
	} else if filter != "" {
		override = func(queryCtx context.Context, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
			params.Filter = filter
			return source.QueryWKB(queryCtx, layer.SourceLayer, params)
		}
	}
	data, err := limited.generateRegion(ctx, source, layerInfo, request.Workspace, nativeBounds, wgsBounds, GetTMSSRID(request.MatrixSet), width, height, format, request.Style, layer.PublicID, true, nil, override)
	if err != nil {
		return nil, err
	}
	full, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	x := gutter + (request.Column-startColumn)*256
	y := gutter + (request.Row-startRow)*256
	cropped := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(cropped, cropped.Bounds(), full, image.Pt(x, y), draw.Src)
	return e.mapGen.encodeImage(cropped, format)
}

func (e *Engine) renderLayerGroup(ctx context.Context, request EngineRequest, visiting map[string]bool, depth int) ([]byte, error) {
	group := request.Resource.Group
	if group == nil || request.TileType != "map" {
		return nil, errors.New("layer groups support map tiles only")
	}
	maximum := e.cfg.WMS.MaxGroupDepth
	if maximum <= 0 {
		maximum = 8
	}
	if depth > maximum || visiting[group.PublicID] {
		return nil, errors.New("invalid layer group nesting")
	}
	if len(group.Members) > 0 {
		needsComposite := false
		for _, member := range group.Members {
			if member.Composite != "" || member.EffectiveOpacity() != 1 {
				needsComposite = true
			}
		}
		if needsComposite && (!slices.Contains(e.cfg.WMS.Extensions, "compositing") || request.Workspace.Settings == nil || !slices.Contains(request.Workspace.Settings.WMS.Extensions, "compositing")) {
			return nil, errors.New("layer group requires the disabled compositing extension")
		}
	}
	visiting[group.PublicID] = true
	defer delete(visiting, group.PublicID)
	bounds, err := TileBBox(request.MatrixSet, request.Zoom, request.Column, request.Row)
	if err != nil {
		return nil, err
	}
	canvas := renderer.NewMapRenderer(renderer.NewTransform(query.BBox{MinX: bounds.MinX, MinY: bounds.MinY, MaxX: bounds.MaxX, MaxY: bounds.MaxY}, 256, 256), true, nil)
	groupStyle := request.Style
	if groupStyle == "" {
		groupStyle = group.DefaultStyle
	}
	for _, member := range group.Members {
		resource := request.Workspace.GetResource(member.Resource)
		if resource == nil {
			return nil, fmt.Errorf("layer group member %q does not exist", member.Resource)
		}
		style := member.Style
		if groupStyle != "" {
			style = groupStyle
		}
		_, renderStyle, styleErr := resolvedStyle(request.Workspace, resource, style)
		if styleErr != nil {
			return nil, styleErr
		}
		if styleErr = e.validateStyleExtensions(request.Workspace, resource, renderStyle); styleErr != nil {
			return nil, styleErr
		}
		child := request
		child.Resource, child.Style, child.UseCache, child.Force = resource, renderStyle, false, false
		// Preserve transparency between members; JPEG is only an output encoding.
		child.Format = MediaTypePNG
		var data []byte
		if resource.Kind == workspace.ResourceGroup {
			data, err = e.renderLayerGroup(ctx, child, visiting, depth+1)
		} else {
			data, err = e.render(ctx, child)
		}
		if err != nil {
			return nil, err
		}
		img, _, decodeErr := image.Decode(bytes.NewReader(data))
		if decodeErr != nil {
			return nil, fmt.Errorf("decode group member tile: %w", decodeErr)
		}
		opacity := member.EffectiveOpacity()
		canvas.CompositeWithMode(img, member.Composite, opacity)
	}
	format, err := ParseTileFormat(request.Format)
	if err != nil {
		return nil, err
	}
	return e.mapGen.encodeImage(canvas.Image(), format)
}

// Persistent returns the durable cache manager for management and job APIs.
func (e *Engine) Persistent() *tilecache.Manager { return e.persistent }

func normalizeTileType(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func tileRenderConfigHash(cfg conf.Tiles) uint64 {
	// Version rendering semantics as well as configuration: old tiles may be
	// valid protobufs with incorrect coordinates or publication names.
	value := fmt.Sprintf("render-v2|%d|%d|%d|%d|%d", cfg.TileSize, cfg.MaxFeatures, cfg.MaxVertices, cfg.MaxTileBytes, cfg.StatementTimeoutMS)
	digest := sha256.Sum256([]byte(value))
	return binary.BigEndian.Uint64(digest[:8])
}

func effectiveRenderRevision(workspaceRevision int64, configHash uint64) int64 {
	if workspaceRevision <= 0 {
		workspaceRevision = 1
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%d", workspaceRevision, configHash)))
	value := binary.BigEndian.Uint64(digest[:8]) & uint64(^uint64(0)>>1)
	if value == 0 {
		return 1
	}
	return int64(value)
}
