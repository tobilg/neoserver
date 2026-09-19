// Package tilejobs runs durable, resumable persistent-cache maintenance jobs.
package tilejobs

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

type Manager struct {
	cfg               conf.PersistentCacheJobs
	tileCfg           conf.Tiles
	store             tilecache.JobStore
	registry          *workspace.Registry
	engine            *tiles.Engine
	persistent        *tilecache.Manager
	memory            *cache.Manager
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	notify            chan struct{}
	claimMu           sync.Mutex
	renderSlots       chan struct{}
	lifecycleMu       sync.RWMutex
	blockedWorkspaces map[string]bool
	blockedResources  map[string]map[string]bool
}

func NewManager(ctx context.Context, cfg conf.PersistentCacheJobs, tileCfg conf.Tiles, durable tilecache.JobStore, registry *workspace.Registry, engine *tiles.Engine, persistent *tilecache.Manager, memory *cache.Manager) (*Manager, error) {
	if durable == nil || registry == nil || engine == nil || persistent == nil {
		return nil, errors.New("tile jobs require durable store, workspace registry, tile engine, and persistent cache")
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 1
	}
	if cfg.MaxConcurrentJobs <= 0 {
		cfg.MaxConcurrentJobs = 1
	}
	if cfg.MaxConcurrentRenders <= 0 {
		cfg.MaxConcurrentRenders = 1
	}
	if err := durable.ResetInterruptedTileCacheJobs(ctx); err != nil {
		return nil, err
	}
	managerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m := &Manager{cfg: cfg, tileCfg: tileCfg, store: durable, registry: registry, engine: engine, persistent: persistent, memory: memory, ctx: managerCtx, cancel: cancel, notify: make(chan struct{}, 1), renderSlots: make(chan struct{}, cfg.MaxConcurrentRenders), blockedWorkspaces: make(map[string]bool), blockedResources: make(map[string]map[string]bool)}
	for range cfg.MaxConcurrentJobs {
		m.wg.Add(1)
		go m.worker()
	}
	return m, nil
}

func (m *Manager) Create(ctx context.Context, workspaceID, createdBy string, request tilecache.JobRequest) (*tilecache.Job, error) {
	if m.lifecycleBlocked(workspaceID, request.ResourceID, request.AllResources) {
		return nil, errors.New("tile cache lifecycle is quiesced for this resource")
	}
	ws, release, ok := m.registry.AcquireByID(workspaceID)
	defer release()
	if !ok {
		return nil, workspace.ErrWorkspaceNotFound
	}
	if request.Operation != tilecache.OperationSeed && request.Operation != tilecache.OperationReseed && request.Operation != tilecache.OperationTruncate {
		return nil, errors.New("operation must be seed, reseed, or truncate")
	}
	if ws.Settings == nil {
		return nil, errors.New("workspace settings are unavailable")
	}
	if request.Operation != tilecache.OperationTruncate && !ws.Settings.OGCTilesAPI.Settings.CacheEnabled {
		return nil, errors.New("persistent tile caching is disabled for this workspace")
	}
	if request.AllResources && request.Operation != tilecache.OperationTruncate {
		return nil, errors.New("all_resources is only supported for truncate jobs")
	}
	if request.AllResources && request.Resource != "" {
		return nil, errors.New("resource and all_resources are mutually exclusive")
	}

	var resource *workspace.PublishedResource
	if !request.AllResources {
		resource = resolveResource(ws, firstNonEmpty(request.Resource, request.ResourceID))
		if resource == nil {
			return nil, errors.New("resource was not found")
		}
		request.Resource = resource.PublicID()
		request.ResourceID, request.Generation, request.ResourceKind = resourceIdentity(resource)
		if m.lifecycleBlocked(workspaceID, request.ResourceID, false) {
			return nil, errors.New("tile cache lifecycle is quiesced for this resource")
		}
	}
	if request.Operation == tilecache.OperationTruncate && request.Bounds == nil {
		job, err := m.store.CreateTileCacheJob(ctx, tilecache.CreateJobInput{WorkspaceID: workspaceID, Request: request, CreatedBy: createdBy})
		if err == nil {
			m.wake()
		}
		return job, err
	}
	if request.AllResources {
		return nil, errors.New("bounded all-resource truncation is not supported")
	}
	if request.TileType == "" {
		request.TileType = "map"
	}
	if request.TileType != "map" && request.TileType != "vector" {
		return nil, errors.New("tile_type must be map or vector")
	}
	if request.TileType == "vector" && resource.Kind != workspace.ResourceFeature {
		return nil, errors.New("vector tiles require a feature layer")
	}
	if request.TileMatrixSet == "" {
		if len(ws.Settings.OGCTilesAPI.Settings.TileMatrixSets) == 0 {
			return nil, errors.New("workspace has no enabled tile matrix sets")
		}
		request.TileMatrixSet = ws.Settings.OGCTilesAPI.Settings.TileMatrixSets[0]
	}
	if !contains(ws.Settings.OGCTilesAPI.Settings.TileMatrixSets, request.TileMatrixSet) {
		return nil, errors.New("tile matrix set is not enabled")
	}
	if request.Format == "" {
		if request.TileType == "vector" {
			request.Format = tiles.MediaTypeMVT
		} else {
			request.Format = tiles.MediaTypePNG
		}
	}
	minZoom, maxZoom := m.zoomRange(request)
	if minZoom < 0 || maxZoom < minZoom || maxZoom > 24 {
		return nil, errors.New("invalid zoom range")
	}
	request.MinZoom, request.MaxZoom = intPointer(minZoom), intPointer(maxZoom)

	bounds, err := jobBounds(resource, request.Bounds, request.TileMatrixSet)
	if err != nil {
		return nil, err
	}
	request.Bounds = bounds
	chunks, total, err := buildChunks(request.TileMatrixSet, minZoom, maxZoom, bounds.BBox)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, errors.New("job bounds do not intersect the tile matrix set")
	}
	if m.cfg.MaxTilesPerJob > 0 && total > m.cfg.MaxTilesPerJob {
		return nil, fmt.Errorf("job contains %d tiles, exceeding max_tiles_per_job %d", total, m.cfg.MaxTilesPerJob)
	}
	// Resolve one representative key now so invalid format/style combinations
	// fail synchronously rather than after the job is accepted.
	first := chunks[0]
	if _, err := m.engine.ResolveIdentity(tiles.EngineRequest{Workspace: ws, Resource: resource, TileType: request.TileType, MatrixSet: request.TileMatrixSet, Zoom: first.Zoom, Column: first.MinCol, Row: first.MinRow, Format: request.Format, Style: request.Style}); err != nil {
		return nil, err
	}
	job, err := m.store.CreateTileCacheJob(ctx, tilecache.CreateJobInput{WorkspaceID: workspaceID, Request: request, TotalTiles: total, CreatedBy: createdBy})
	if err != nil {
		return nil, err
	}
	if err := m.store.ReplaceTileCacheJobChunks(ctx, job.ID, chunks); err != nil {
		status, message, now := tilecache.JobFailed, err.Error(), time.Now().UTC()
		_, _ = m.store.UpdateTileCacheJob(ctx, job.ID, tilecache.JobUpdate{Status: &status, ErrorMessage: &message, CompletedAt: &now})
		return nil, err
	}
	m.wake()
	return job, nil
}

func (m *Manager) Get(ctx context.Context, id string) (*tilecache.Job, error) {
	return m.store.GetTileCacheJob(ctx, id)
}

func (m *Manager) List(ctx context.Context, workspaceID string, limit int) ([]*tilecache.Job, error) {
	return m.store.ListTileCacheJobs(ctx, workspaceID, limit)
}

func (m *Manager) Cancel(ctx context.Context, id string) error {
	err := m.store.RequestTileCacheJobCancel(ctx, id)
	m.wake()
	return err
}

func (m *Manager) Close(ctx context.Context) error {
	m.cancel()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Health reports whether the durable job coordinator is still running and
// its backing persistent cache remains writable.
func (m *Manager) Health(_ context.Context) error {
	if err := m.ctx.Err(); err != nil {
		return fmt.Errorf("tile job coordinator stopped: %w", err)
	}
	if !m.persistent.Writable() {
		return tilecache.ErrOwnershipLost
	}
	return nil
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		job, err := m.claimNext(m.ctx)
		if err == nil && job != nil {
			m.run(job)
			continue
		}
		select {
		case <-m.ctx.Done():
			return
		case <-m.notify:
		case <-time.After(time.Second):
		}
	}
}

func (m *Manager) claimNext(ctx context.Context) (*tilecache.Job, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	job, err := m.store.ClaimNextTileCacheJob(ctx)
	if err != nil || job == nil {
		return job, err
	}
	if m.lifecycleBlocked(job.WorkspaceID, job.Request.ResourceID, job.Request.AllResources) {
		status, now := tilecache.JobCancelled, time.Now().UTC()
		_, err := m.store.UpdateTileCacheJob(ctx, job.ID, tilecache.JobUpdate{Status: &status, CompletedAt: &now})
		return nil, err
	}
	return job, nil
}

// QuiesceLifecycle prevents new jobs for a deletion scope, cancels matching
// queued/running jobs, and waits until no matching job can still write tiles.
// An empty resourceIDs slice quiesces the whole workspace.
func (m *Manager) QuiesceLifecycle(ctx context.Context, workspaceID string, resourceIDs []string) error {
	m.lifecycleMu.Lock()
	if len(resourceIDs) == 0 {
		m.blockedWorkspaces[workspaceID] = true
	} else {
		if m.blockedResources[workspaceID] == nil {
			m.blockedResources[workspaceID] = make(map[string]bool)
		}
		for _, resourceID := range resourceIDs {
			m.blockedResources[workspaceID][resourceID] = true
		}
	}
	m.lifecycleMu.Unlock()
	m.wake()

	if err := m.persistent.CancelLifecycleJobs(ctx, workspaceID, resourceIDs); err != nil {
		return err
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		active, err := m.persistent.ActiveLifecycleJobs(ctx, workspaceID, resourceIDs)
		if err != nil {
			return err
		}
		if active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) lifecycleBlocked(workspaceID, resourceID string, allResources bool) bool {
	m.lifecycleMu.RLock()
	defer m.lifecycleMu.RUnlock()
	if m.blockedWorkspaces[workspaceID] {
		return true
	}
	resources := m.blockedResources[workspaceID]
	return resources != nil && (allResources || resources[resourceID])
}

func (m *Manager) run(job *tilecache.Job) {
	ws, release, ok := m.registry.AcquireByID(job.WorkspaceID)
	defer release()
	if !ok {
		m.finishFailed(job, "workspace no longer exists")
		return
	}
	var resource *workspace.PublishedResource
	if !job.Request.AllResources {
		resource = resolveResource(ws, firstNonEmpty(job.Request.ResourceID, job.Request.Resource))
		if resource == nil {
			m.finishFailed(job, "resource no longer exists")
			return
		}
	}
	chunks, err := m.store.ListTileCacheJobChunks(m.ctx, job.ID)
	if err != nil {
		m.finishFailed(job, err.Error())
		return
	}
	if len(chunks) == 0 && (job.Request.Operation != tilecache.OperationTruncate || job.Request.Bounds != nil) {
		m.finishFailed(job, "job has no durable tile chunks")
		return
	}
	if job.Request.Operation == tilecache.OperationTruncate && job.Request.Bounds == nil && len(chunks) == 0 {
		selector := tilecache.Selector{WorkspaceID: job.WorkspaceID}
		if resource != nil {
			selector.ResourceID = job.Request.ResourceID
		}
		selector.TileType, selector.MatrixSet, selector.Format = job.Request.TileType, job.Request.TileMatrixSet, job.Request.Format
		selector.MinZoom, selector.MaxZoom = job.Request.MinZoom, job.Request.MaxZoom
		if resource != nil && job.Request.Style != "" {
			selector.StyleName, err = m.engine.ResolveStyleName(ws, resource, job.Request.Style)
			if err != nil {
				m.finishFailed(job, err.Error())
				return
			}
		}
		result, deleteErr := m.persistent.Delete(m.ctx, selector)
		if deleteErr != nil {
			m.finishFailed(job, deleteErr.Error())
			return
		}
		if m.memory != nil {
			m.memory.InvalidateTiles(job.WorkspaceID)
		}
		processed, bytesDeleted, now, status := result.Entries, result.Bytes, time.Now().UTC(), tilecache.JobSucceeded
		_, _ = m.store.UpdateTileCacheJob(m.ctx, job.ID, tilecache.JobUpdate{Status: &status, TotalTiles: &processed, ProcessedTiles: &processed, SucceededTiles: &processed, BytesDeleted: &bytesDeleted, CompletedAt: &now})
		return
	}

	state := &jobState{job: job}
	queue := make(chan tilecache.JobChunk)
	var workers sync.WaitGroup
	workerCount := min(m.cfg.WorkerCount, max(1, len(chunks)))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for chunk := range queue {
				m.runChunk(state, ws, resource, &chunk)
			}
		}()
	}
	for _, chunk := range chunks {
		if chunk.Status == "succeeded" {
			continue
		}
		queue <- chunk
	}
	close(queue)
	workers.Wait()
	if job.Request.Operation == tilecache.OperationTruncate && m.memory != nil {
		// A bounded truncate deletes exact durable keys, but the same payloads may
		// still be present in the process-local L1 cache.
		m.memory.InvalidateTiles(job.WorkspaceID)
	}

	current, _ := m.store.GetTileCacheJob(m.ctx, job.ID)
	now := time.Now().UTC()
	if state.interrupted.Load() {
		return
	}
	if state.cancelled.Load() || (current != nil && current.CancelRequested) {
		status := tilecache.JobCancelled
		_, _ = m.store.UpdateTileCacheJob(context.WithoutCancel(m.ctx), job.ID, tilecache.JobUpdate{Status: &status, CompletedAt: &now})
		return
	}
	status := tilecache.JobSucceeded
	message := ""
	if state.failed.Load() > 0 {
		status, message = tilecache.JobFailed, "one or more tiles failed"
	}
	_, _ = m.store.UpdateTileCacheJob(context.WithoutCancel(m.ctx), job.ID, state.update(status, message, &now))
}

type jobState struct {
	job          *tilecache.Job
	processed    atomic.Int64
	succeeded    atomic.Int64
	skipped      atomic.Int64
	failed       atomic.Int64
	bytesWritten atomic.Int64
	bytesDeleted atomic.Int64
	cancelled    atomic.Bool
	interrupted  atomic.Bool
	updateMu     sync.Mutex
}

func (s *jobState) update(status tilecache.JobStatus, message string, completed *time.Time) tilecache.JobUpdate {
	processed := s.job.ProcessedTiles + s.processed.Load()
	succeeded := s.job.SucceededTiles + s.succeeded.Load()
	skipped := s.job.SkippedTiles + s.skipped.Load()
	failed := s.job.FailedTiles + s.failed.Load()
	bytesWritten := s.job.BytesWritten + s.bytesWritten.Load()
	bytesDeleted := s.job.BytesDeleted + s.bytesDeleted.Load()
	return tilecache.JobUpdate{Status: &status, ProcessedTiles: &processed, SucceededTiles: &succeeded, SkippedTiles: &skipped, FailedTiles: &failed, BytesWritten: &bytesWritten, BytesDeleted: &bytesDeleted, ErrorMessage: &message, CompletedAt: completed}
}

func (m *Manager) runChunk(state *jobState, ws *workspace.Workspace, resource *workspace.PublishedResource, chunk *tilecache.JobChunk) {
	chunk.Status = "running"
	_ = m.store.UpdateTileCacheJobChunk(m.ctx, *chunk)
	width := int64(chunk.MaxCol - chunk.MinCol + 1)
	total := width * int64(chunk.MaxRow-chunk.MinRow+1)
	for chunk.NextOffset < total {
		if m.ctx.Err() != nil {
			state.interrupted.Store(true)
			chunk.Status = "queued"
			_ = m.store.UpdateTileCacheJobChunk(context.WithoutCancel(m.ctx), *chunk)
			return
		}
		if m.cancelRequested(state.job.ID) {
			state.cancelled.Store(true)
			chunk.Status = "cancelled"
			_ = m.store.UpdateTileCacheJobChunk(context.WithoutCancel(m.ctx), *chunk)
			return
		}
		offset := chunk.NextOffset
		column := chunk.MinCol + int(offset%width)
		row := chunk.MinRow + int(offset/width)
		bytes, skipped, err := m.processTile(ws, resource, state.job.Request, chunk.Zoom, column, row)
		chunk.NextOffset++
		state.processed.Add(1)
		if err != nil {
			state.failed.Add(1)
			chunk.Attempts++
			chunk.LastError = err.Error()
		} else if skipped {
			state.skipped.Add(1)
		} else {
			state.succeeded.Add(1)
			if state.job.Request.Operation == tilecache.OperationTruncate {
				state.bytesDeleted.Add(bytes)
			} else {
				state.bytesWritten.Add(bytes)
			}
		}
		_ = m.store.UpdateTileCacheJobChunk(context.WithoutCancel(m.ctx), *chunk)
		state.updateMu.Lock()
		status := tilecache.JobRunning
		_, _ = m.store.UpdateTileCacheJob(context.WithoutCancel(m.ctx), state.job.ID, state.update(status, "", nil))
		state.updateMu.Unlock()
	}
	chunk.Status = "succeeded"
	_ = m.store.UpdateTileCacheJobChunk(context.WithoutCancel(m.ctx), *chunk)
}

func (m *Manager) processTile(ws *workspace.Workspace, resource *workspace.PublishedResource, request tilecache.JobRequest, zoom, column, row int) (int64, bool, error) {
	engineRequest := tiles.EngineRequest{Workspace: ws, Resource: resource, TileType: request.TileType, MatrixSet: request.TileMatrixSet, Zoom: zoom, Column: column, Row: row, Format: request.Format, Style: request.Style, UseCache: true, Force: true}
	identity, err := m.engine.ResolveIdentity(engineRequest)
	if err != nil {
		return 0, false, err
	}
	if request.Operation == tilecache.OperationSeed {
		exists, err := m.persistent.Exists(m.ctx, identity)
		if err != nil || exists {
			return 0, exists, err
		}
	}
	if request.Operation == tilecache.OperationTruncate {
		result, err := m.persistent.DeleteIdentity(m.ctx, identity)
		return result.Bytes, result.Entries == 0, err
	}
	var lastErr error
	for attempt := 0; attempt <= m.cfg.MaxRetries; attempt++ {
		select {
		case m.renderSlots <- struct{}{}:
		case <-m.ctx.Done():
			return 0, false, m.ctx.Err()
		}
		result, renderErr := m.engine.Fetch(m.ctx, engineRequest)
		<-m.renderSlots
		if renderErr == nil {
			exists, existsErr := m.persistent.Exists(m.ctx, identity)
			if existsErr == nil && exists {
				return int64(len(result.Data)), false, nil
			}
			if existsErr != nil {
				renderErr = existsErr
			} else {
				renderErr = errors.New("tile was not persisted; check cache quotas")
			}
		}
		lastErr = renderErr
	}
	return 0, false, lastErr
}

func (m *Manager) cancelRequested(jobID string) bool {
	job, err := m.store.GetTileCacheJob(m.ctx, jobID)
	return err == nil && job.CancelRequested
}

func (m *Manager) finishFailed(job *tilecache.Job, message string) {
	status, now := tilecache.JobFailed, time.Now().UTC()
	_, _ = m.store.UpdateTileCacheJob(context.WithoutCancel(m.ctx), job.ID, tilecache.JobUpdate{Status: &status, ErrorMessage: &message, CompletedAt: &now})
}

func (m *Manager) wake() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *Manager) zoomRange(request tilecache.JobRequest) (int, int) {
	minimum, maximum := m.tileCfg.MinZoom, m.tileCfg.MaxZoom
	if request.MinZoom != nil {
		minimum = *request.MinZoom
	}
	if request.MaxZoom != nil {
		maximum = *request.MaxZoom
	}
	return minimum, maximum
}

func resolveResource(ws *workspace.Workspace, identifier string) *workspace.PublishedResource {
	if resource := ws.GetResource(identifier); resource != nil {
		return resource
	}
	for _, resource := range ws.VisibleResources("super_admin") {
		id, _, _ := resourceIdentity(resource)
		if id == identifier {
			return resource
		}
	}
	return nil
}

func resourceIdentity(resource *workspace.PublishedResource) (string, int64, string) {
	if resource.Layer != nil {
		return resource.Layer.ID, resource.Layer.TileCacheGeneration, string(workspace.ResourceFeature)
	}
	if resource.Coverage != nil {
		return resource.Coverage.ID, resource.Coverage.TileCacheGeneration, string(workspace.ResourceCoverage)
	}
	if resource.Group != nil {
		return resource.Group.ID, resource.Group.TileCacheGeneration, string(workspace.ResourceGroup)
	}
	return "", 0, ""
}

func jobBounds(resource *workspace.PublishedResource, requested *tilecache.Bounds, matrixSet string) (*tilecache.Bounds, error) {
	targetSRID := tiles.GetTMSSRID(matrixSet)
	if targetSRID == 0 {
		return nil, errors.New("unknown tile matrix set")
	}
	var bbox [4]float64
	var sourceSRID int
	if requested != nil {
		bbox = requested.BBox
		sourceSRID = parseSRID(requested.CRS)
		if sourceSRID == 0 {
			return nil, errors.New("bounds CRS must be EPSG:4326 or EPSG:3857")
		}
	} else {
		var extent *store.SpatialExtent
		if resource.Layer != nil {
			extent = resource.Layer.NativeExtent
		} else if resource.Coverage != nil {
			extent = resource.Coverage.NativeExtent
		} else if resource.Group != nil {
			extent = resource.Group.NativeExtent
		}
		if extent == nil || extent.Stale {
			return nil, errors.New("resource extent is unavailable; provide bounds explicitly")
		}
		bbox, sourceSRID = [4]float64{extent.MinX, extent.MinY, extent.MaxX, extent.MaxY}, extent.SRID
	}
	if bbox[0] >= bbox[2] || bbox[1] >= bbox[3] {
		return nil, errors.New("bounds must have increasing coordinates")
	}
	transformed, err := transformBounds(bbox, sourceSRID, targetSRID)
	if err != nil {
		return nil, err
	}
	return &tilecache.Bounds{BBox: transformed, CRS: fmt.Sprintf("EPSG:%d", targetSRID)}, nil
}

func transformBounds(bbox [4]float64, source, target int) ([4]float64, error) {
	if source == target {
		return bbox, nil
	}
	if source == 4326 && target == 3857 {
		minX, minY := lonLatToMercator(bbox[0], bbox[1])
		maxX, maxY := lonLatToMercator(bbox[2], bbox[3])
		return [4]float64{minX, minY, maxX, maxY}, nil
	}
	if source == 3857 && target == 4326 {
		minX, minY := mercatorToLonLat(bbox[0], bbox[1])
		maxX, maxY := mercatorToLonLat(bbox[2], bbox[3])
		return [4]float64{minX, minY, maxX, maxY}, nil
	}
	if source <= 0 || target <= 0 {
		return [4]float64{}, errors.New("bounds transformation requires valid EPSG codes")
	}
	sourceRef, err := godal.NewSpatialRefFromEPSG(source)
	if err != nil {
		return [4]float64{}, fmt.Errorf("open source EPSG:%d: %w", source, err)
	}
	defer sourceRef.Close()
	targetRef, err := godal.NewSpatialRefFromEPSG(target)
	if err != nil {
		return [4]float64{}, fmt.Errorf("open target EPSG:%d: %w", target, err)
	}
	defer targetRef.Close()
	transform, err := godal.NewTransform(sourceRef, targetRef)
	if err != nil {
		return [4]float64{}, fmt.Errorf("create bounds transformation: %w", err)
	}
	defer transform.Close()

	// Densify all four edges so the transformed envelope also covers curved
	// projected bounds instead of considering only their corners.
	const segments = 16
	xs, ys := make([]float64, 0, (segments+1)*4), make([]float64, 0, (segments+1)*4)
	for index := 0; index <= segments; index++ {
		ratio := float64(index) / segments
		x := bbox[0] + ratio*(bbox[2]-bbox[0])
		y := bbox[1] + ratio*(bbox[3]-bbox[1])
		xs = append(xs, x, x, bbox[0], bbox[2])
		ys = append(ys, bbox[1], bbox[3], y, y)
	}
	success := make([]bool, len(xs))
	if err := transform.TransformEx(xs, ys, nil, success); err != nil {
		return [4]float64{}, fmt.Errorf("transform bounds: %w", err)
	}
	result := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for index, ok := range success {
		if !ok || math.IsNaN(xs[index]) || math.IsNaN(ys[index]) || math.IsInf(xs[index], 0) || math.IsInf(ys[index], 0) {
			return [4]float64{}, fmt.Errorf("cannot transform bounds from EPSG:%d to EPSG:%d", source, target)
		}
		result[0], result[1] = math.Min(result[0], xs[index]), math.Min(result[1], ys[index])
		result[2], result[3] = math.Max(result[2], xs[index]), math.Max(result[3], ys[index])
	}
	return result, nil
}

func lonLatToMercator(lon, lat float64) (float64, float64) {
	lat = min(85.0511287798066, max(-85.0511287798066, lat))
	return lon * tiles.WebMercatorOriginY / 180, math.Log(math.Tan((90+lat)*math.Pi/360)) * tiles.WebMercatorOriginY / math.Pi
}

func mercatorToLonLat(x, y float64) (float64, float64) {
	return x / tiles.WebMercatorOriginY * 180, (2*math.Atan(math.Exp(y/tiles.WebMercatorOriginY*math.Pi)) - math.Pi/2) * 180 / math.Pi
}

func buildChunks(matrixSet string, minZoom, maxZoom int, requested [4]float64) ([]tilecache.JobChunk, int64, error) {
	definition, err := tiles.GetTileMatrixSetDefinition(matrixSet)
	if err != nil {
		return nil, 0, err
	}
	world := definition.BoundingBox
	bbox := [4]float64{max(requested[0], world.LowerCorner[0]), max(requested[1], world.LowerCorner[1]), min(requested[2], world.UpperCorner[0]), min(requested[3], world.UpperCorner[1])}
	if bbox[0] >= bbox[2] || bbox[1] >= bbox[3] {
		return nil, 0, nil
	}
	var chunks []tilecache.JobChunk
	var total int64
	for zoom := minZoom; zoom <= maxZoom; zoom++ {
		matrix := definition.TileMatrices[zoom]
		spanX, spanY := matrix.CellSize*float64(matrix.TileWidth), matrix.CellSize*float64(matrix.TileHeight)
		minCol := int(math.Floor((bbox[0] - matrix.PointOfOrigin[0]) / spanX))
		maxCol := int(math.Floor((math.Nextafter(bbox[2], math.Inf(-1)) - matrix.PointOfOrigin[0]) / spanX))
		minRow := int(math.Floor((matrix.PointOfOrigin[1] - bbox[3]) / spanY))
		maxRow := int(math.Floor((matrix.PointOfOrigin[1] - math.Nextafter(bbox[1], math.Inf(1))) / spanY))
		minCol, maxCol = max(0, minCol), min(matrix.MatrixWidth-1, maxCol)
		minRow, maxRow = max(0, minRow), min(matrix.MatrixHeight-1, maxRow)
		if minCol > maxCol || minRow > maxRow {
			continue
		}
		width, height := maxCol-minCol+1, maxRow-minRow+1
		total += int64(width) * int64(height)
		columnBlock := min(width, 32)
		rowBlock := max(1, 1024/columnBlock)
		for row := minRow; row <= maxRow; row += rowBlock {
			for column := minCol; column <= maxCol; column += columnBlock {
				chunks = append(chunks, tilecache.JobChunk{Zoom: zoom, MinCol: column, MaxCol: min(maxCol, column+columnBlock-1), MinRow: row, MaxRow: min(maxRow, row+rowBlock-1), Status: "queued"})
			}
		}
	}
	return chunks, total, nil
}

func parseSRID(value string) int {
	value = strings.TrimSpace(strings.ToUpper(value))
	if strings.Contains(value, "CRS84") {
		return 4326
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ':' || r == '/' })
	for index := len(parts) - 1; index >= 0; index-- {
		if number, err := strconv.Atoi(parts[index]); err == nil {
			return number
		}
	}
	return 0
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func intPointer(value int) *int { return &value }
