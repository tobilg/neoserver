package mosaiccatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/airbusgeo/godal"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type Manager struct {
	cfg               conf.MosaicCatalog
	catalog           *catalog
	registry          *workspace.Registry
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	notify            chan struct{}
	claimMu           sync.Mutex
	serviceLocks      sync.Map
	startMu           sync.Mutex
	started           bool
	lifecycleMu       sync.RWMutex
	blockedWorkspaces map[string]bool
	blockedServices   map[string]bool
}

type LifecycleOwner struct {
	WorkspaceID string `json:"workspace_id"`
	ServiceID   string `json:"service_id"`
}

func Open(ctx context.Context, cfg conf.MosaicCatalog, encryptionKey string) (*Manager, error) {
	if !cfg.Enabled {
		return nil, ErrDisabled
	}
	index, err := openCatalog(cfg.DatabasePath, encryptionKey)
	if err != nil {
		return nil, err
	}
	if err := index.resetInterrupted(ctx); err != nil {
		index.db.Close()
		return nil, err
	}
	managerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	return &Manager{cfg: cfg, catalog: index, ctx: managerCtx, cancel: cancel, notify: make(chan struct{}, 1), blockedWorkspaces: make(map[string]bool), blockedServices: make(map[string]bool)}, nil
}

func (m *Manager) Start(registry *workspace.Registry) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if m.started {
		return nil
	}
	if registry == nil {
		return errors.New("mosaic catalog requires the workspace registry")
	}
	m.registry = registry
	workers := min(m.cfg.WorkerCount, m.cfg.MaxConcurrentJobs)
	if workers < 1 {
		workers = 1
	}
	for range workers {
		m.wg.Add(1)
		go m.worker()
	}
	m.started = true
	return nil
}

func (m *Manager) Close(ctx context.Context) error {
	m.cancel()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		return m.catalog.db.Close()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Health verifies both coordinator state and access to the durable catalog.
func (m *Manager) Health(ctx context.Context) error {
	m.startMu.Lock()
	started := m.started
	m.startMu.Unlock()
	if !started {
		return errors.New("mosaic catalog coordinator is not started")
	}
	if err := m.ctx.Err(); err != nil {
		return fmt.Errorf("mosaic catalog coordinator stopped: %w", err)
	}
	return m.catalog.db.PingContext(ctx)
}

func (m *Manager) ListRasterMosaicGranules(ctx context.Context, serviceID string) ([]store.RasterMosaicGranule, error) {
	return m.catalog.listRasterMosaicGranules(ctx, serviceID)
}

func (m *Manager) ListGranules(ctx context.Context, workspaceID, serviceID string, filter GranuleFilter) ([]*Granule, error) {
	if err := m.validateService(workspaceID, serviceID); err != nil {
		return nil, err
	}
	return m.catalog.listGranules(ctx, workspaceID, serviceID, filter)
}

func (m *Manager) GetGranule(ctx context.Context, workspaceID, serviceID, id string) (*Granule, error) {
	if err := m.validateService(workspaceID, serviceID); err != nil {
		return nil, err
	}
	return m.catalog.getGranule(ctx, workspaceID, serviceID, id)
}

func (m *Manager) DeleteGranule(ctx context.Context, workspaceID, serviceID, id string) error {
	if err := m.validateService(workspaceID, serviceID); err != nil {
		return err
	}
	unlock := m.lockService(serviceID)
	defer unlock()
	generation, previous, err := m.catalog.deleteGranule(ctx, workspaceID, serviceID, id)
	if err != nil {
		return err
	}
	if err := m.registry.RefreshService(ctx, workspaceID, serviceID); err != nil {
		if restoreErr := m.catalog.restoreGeneration(ctx, serviceID, generation, previous); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore previous mosaic generation: %w", restoreErr))
		}
		_ = m.catalog.discardGeneration(ctx, serviceID, generation)
		return err
	}
	_ = m.catalog.pruneInactiveGenerations(ctx, serviceID, generation)
	return nil
}

func (m *Manager) CreateJob(ctx context.Context, workspaceID, serviceID, createdBy string, request HarvestRequest) (*Job, error) {
	if m.lifecycleBlocked(workspaceID, serviceID) {
		return nil, errors.New("mosaic lifecycle is quiesced for this service")
	}
	if err := m.validateService(workspaceID, serviceID); err != nil {
		return nil, err
	}
	if request.Mode == "" {
		request.Mode = HarvestSynchronize
	}
	if request.Mode != HarvestAppend && request.Mode != HarvestSynchronize {
		return nil, errors.New("mode must be append or synchronize")
	}
	if request.Pattern != "" {
		if _, err := doublestar.Match(request.Pattern, ""); err != nil {
			return nil, fmt.Errorf("invalid harvest pattern: %w", err)
		}
	}
	if int64(len(request.Granules)) > m.cfg.MaxGranulesPerJob {
		return nil, fmt.Errorf("harvest request exceeds %d granules", m.cfg.MaxGranulesPerJob)
	}
	job, err := m.catalog.createJob(ctx, workspaceID, serviceID, createdBy, request)
	if err == nil {
		m.wake()
	}
	return job, err
}

func (m *Manager) GetJob(ctx context.Context, workspaceID, serviceID, id string) (*Job, error) {
	job, err := m.catalog.getJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.WorkspaceID != workspaceID || job.ServiceID != serviceID {
		return nil, ErrNotFound
	}
	return job, nil
}

func (m *Manager) ListJobs(ctx context.Context, workspaceID, serviceID string, limit int) ([]*Job, error) {
	if err := m.validateService(workspaceID, serviceID); err != nil {
		return nil, err
	}
	return m.catalog.listJobs(ctx, workspaceID, serviceID, limit)
}

func (m *Manager) CancelJob(ctx context.Context, workspaceID, serviceID, id string) error {
	if _, err := m.GetJob(ctx, workspaceID, serviceID, id); err != nil {
		return err
	}
	err := m.catalog.requestCancel(ctx, id)
	m.wake()
	return err
}

func (m *Manager) validateService(workspaceID, serviceID string) error {
	if m.registry == nil {
		return errors.New("mosaic catalog is not started")
	}
	ws, ok := m.registry.GetByID(workspaceID)
	if !ok {
		return workspace.ErrWorkspaceNotFound
	}
	service := ws.ResolveService(serviceID)
	if service == nil {
		return workspace.ErrServiceNotFound
	}
	if service.Type != store.ServiceTypeRasterMosaic {
		return ErrServiceNotMosaic
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

func (m *Manager) claimNext(ctx context.Context) (*Job, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	rows, err := m.catalog.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM mosaic_harvest_jobs queued
		WHERE queued.status='queued' AND queued.cancel_requested=false
		AND NOT EXISTS (SELECT 1 FROM mosaic_harvest_jobs active WHERE active.service_id=queued.service_id AND active.status IN ('running','cancelling'))
		ORDER BY queued.created_at LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	job, err := scanJob(rows)
	if err != nil {
		return nil, err
	}
	if m.lifecycleBlocked(job.WorkspaceID, job.ServiceID) {
		_ = m.catalog.requestCancel(ctx, job.ID)
		return nil, nil
	}
	return m.markRunning(ctx, job)
}

// LifecycleStats reports operational mosaic state owned by the deletion
// scope. An empty serviceIDs slice selects the complete workspace.
func (m *Manager) LifecycleStats(ctx context.Context, workspaceID string, serviceIDs []string) (services, granules, jobs int64, err error) {
	condition, args := mosaicLifecycleCondition(workspaceID, serviceIDs)
	if err = m.catalog.db.QueryRowContext(ctx, `SELECT count(*) FROM mosaic_services WHERE `+condition, args...).Scan(&services); err != nil {
		return 0, 0, 0, err
	}
	if err = m.catalog.db.QueryRowContext(ctx, `SELECT count(*) FROM mosaic_granules WHERE `+condition, args...).Scan(&granules); err != nil {
		return 0, 0, 0, err
	}
	if err = m.catalog.db.QueryRowContext(ctx, `SELECT count(*) FROM mosaic_harvest_jobs WHERE `+condition, args...).Scan(&jobs); err != nil {
		return 0, 0, 0, err
	}
	return services, granules, jobs, nil
}

// QuiesceAndDeleteLifecycle cancels and drains harvest work and then removes
// only catalog/index state. Source raster files are never touched.
func (m *Manager) QuiesceAndDeleteLifecycle(ctx context.Context, workspaceID string, serviceIDs []string) error {
	m.lifecycleMu.Lock()
	if len(serviceIDs) == 0 {
		m.blockedWorkspaces[workspaceID] = true
	} else {
		for _, serviceID := range serviceIDs {
			m.blockedServices[serviceID] = true
		}
	}
	m.lifecycleMu.Unlock()
	// Serialize with claimNext so a queued row cannot be concurrently promoted
	// to running while it is being cancelled and removed.
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	condition, args := mosaicLifecycleCondition(workspaceID, serviceIDs)

	// Serialize with any worker completing its final service generation.
	ids := append([]string(nil), serviceIDs...)
	if len(ids) == 0 {
		rows, err := m.catalog.db.QueryContext(ctx, `SELECT DISTINCT service_id FROM (
			SELECT service_id,workspace_id FROM mosaic_services UNION ALL
			SELECT service_id,workspace_id FROM mosaic_granules UNION ALL
			SELECT service_id,workspace_id FROM mosaic_harvest_jobs) WHERE workspace_id=? ORDER BY service_id`, workspaceID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	slices.Sort(ids)
	unlocks := make([]func(), 0, len(ids))
	for _, id := range ids {
		unlocks = append(unlocks, m.lockService(id))
	}
	defer func() {
		for index := len(unlocks) - 1; index >= 0; index-- {
			unlocks[index]()
		}
	}()
	// A worker can be claimed just before the lifecycle block is installed.
	// Holding every affected service lock proves that such a worker has either
	// finished or reached the blocked check in run(). It is now safe to finish
	// cancellation without polling job status.
	if _, err := m.catalog.db.ExecContext(ctx, `UPDATE mosaic_harvest_jobs SET status='cancelled',
		cancel_requested=true,completed_at=coalesce(completed_at,current_timestamp),updated_at=current_timestamp
		WHERE `+condition+` AND status IN ('queued','running','cancelling')`, args...); err != nil {
		return err
	}
	m.catalog.writeMu.Lock()
	defer m.catalog.writeMu.Unlock()
	return m.deleteLifecycleRows(ctx, condition, args)
}

func (m *Manager) deleteLifecycleRows(ctx context.Context, condition string, args []any) error {
	// DuckDB can report an optimistic tuple-deletion conflict when a harvest
	// worker committed its final status immediately before this transaction.
	// All service and claim locks are held here, so retrying starts from a stable
	// snapshot and remains idempotent.
	for attempt := 0; attempt < 5; attempt++ {
		tx, err := m.catalog.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		failed := false
		for _, table := range []string{"mosaic_harvest_jobs", "mosaic_granules", "mosaic_services"} {
			if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+condition, args...); err != nil {
				failed = true
				break
			}
		}
		if !failed {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if err == nil {
			return nil
		}
		_ = tx.Rollback()
		if !strings.Contains(strings.ToLower(err.Error()), "conflict on tuple deletion") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 5 * time.Millisecond):
		}
	}
	return errors.New("mosaic lifecycle deletion remained conflicted after retries")
}

func (m *Manager) lifecycleBlocked(workspaceID, serviceID string) bool {
	m.lifecycleMu.RLock()
	defer m.lifecycleMu.RUnlock()
	return m.blockedWorkspaces[workspaceID] || m.blockedServices[serviceID]
}

func mosaicLifecycleCondition(workspaceID string, serviceIDs []string) (string, []any) {
	if len(serviceIDs) == 0 {
		return "workspace_id=?", []any{workspaceID}
	}
	marks := make([]string, len(serviceIDs))
	args := make([]any, 0, len(serviceIDs)+1)
	args = append(args, workspaceID)
	for index, id := range serviceIDs {
		marks[index] = "?"
		args = append(args, id)
	}
	return "workspace_id=? AND service_id IN (" + strings.Join(marks, ",") + ")", args
}

func (m *Manager) LifecycleInventory(ctx context.Context) ([]LifecycleOwner, error) {
	rows, err := m.catalog.db.QueryContext(ctx, `SELECT DISTINCT workspace_id,service_id FROM (
		SELECT workspace_id,service_id FROM mosaic_services UNION ALL
		SELECT workspace_id,service_id FROM mosaic_granules UNION ALL
		SELECT workspace_id,service_id FROM mosaic_harvest_jobs) ORDER BY workspace_id,service_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []LifecycleOwner
	for rows.Next() {
		var owner LifecycleOwner
		if err := rows.Scan(&owner.WorkspaceID, &owner.ServiceID); err != nil {
			return nil, err
		}
		result = append(result, owner)
	}
	return result, rows.Err()
}

func (m *Manager) markRunning(ctx context.Context, job *Job) (*Job, error) {
	now := time.Now().UTC()
	result, err := m.catalog.db.ExecContext(ctx, `UPDATE mosaic_harvest_jobs SET status='running',started_at=?,updated_at=? WHERE id=? AND status='queued' AND cancel_requested=false`, now, now, job.ID)
	if err != nil {
		return nil, err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return nil, nil
	}
	job.Status, job.StartedAt, job.UpdatedAt = JobRunning, &now, now
	return job, nil
}

func (m *Manager) run(job *Job) {
	unlock := m.lockService(job.ServiceID)
	defer unlock()
	if m.lifecycleBlocked(job.WorkspaceID, job.ServiceID) {
		m.cancelJob(context.WithoutCancel(m.ctx), job)
		return
	}
	ctx := m.ctx
	candidates, err := m.harvestCandidates(ctx, job)
	if err != nil {
		m.failJob(ctx, job, err)
		return
	}
	job.TotalGranules = int64(len(candidates))
	if job.TotalGranules == 0 {
		m.failJob(ctx, job, errors.New("harvest found no granules"))
		return
	}
	if job.TotalGranules > m.cfg.MaxGranulesPerJob {
		m.failJob(ctx, job, fmt.Errorf("harvest contains %d granules, exceeding limit %d", job.TotalGranules, m.cfg.MaxGranulesPerJob))
		return
	}
	preserve := job.Request.Mode == HarvestAppend
	generation, err := m.catalog.beginGeneration(ctx, job.WorkspaceID, job.ServiceID, preserve)
	if err != nil {
		m.failJob(ctx, job, err)
		return
	}
	job.Generation = generation
	if err := m.catalog.updateJob(ctx, job); err != nil {
		_ = m.catalog.discardGeneration(ctx, job.ServiceID, generation)
		m.failJob(ctx, job, err)
		return
	}
	batchSize := max(1, m.cfg.BatchSize)
	var reference *Granule
	if preserve {
		items, _ := m.catalog.listGranules(ctx, job.WorkspaceID, job.ServiceID, GranuleFilter{Limit: 1})
		if len(items) > 0 {
			reference = items[0]
		}
	}
	for _, candidate := range candidates {
		if m.cancelled(ctx, job) {
			_ = m.catalog.discardGeneration(ctx, job.ServiceID, generation)
			m.cancelJob(ctx, job)
			return
		}
		var item *Granule
		var inspectErr error
		for attempt := 0; attempt <= m.cfg.MaxRetries; attempt++ {
			item, inspectErr = inspectGranule(ctx, job.WorkspaceID, job.ServiceID, generation, candidate)
			if inspectErr == nil {
				break
			}
		}
		job.Processed++
		if inspectErr == nil {
			inspectErr = compatibleGranule(reference, item)
		}
		if inspectErr != nil {
			job.Failed++
			job.ErrorMessage = inspectErr.Error()
		} else if inspectErr = m.catalog.upsertGranule(ctx, item); inspectErr != nil {
			job.Failed++
			job.ErrorMessage = inspectErr.Error()
		} else {
			job.Succeeded++
			if reference == nil {
				reference = item
			}
		}
		if job.Processed%int64(batchSize) == 0 || job.Processed == job.TotalGranules {
			if err := m.catalog.updateJob(ctx, job); err != nil {
				_ = m.catalog.discardGeneration(ctx, job.ServiceID, generation)
				m.failJob(ctx, job, err)
				return
			}
		}
		if inspectErr != nil {
			_ = m.catalog.discardGeneration(ctx, job.ServiceID, generation)
			m.failJob(ctx, job, inspectErr)
			return
		}
	}
	previous, err := m.catalog.activateJobGeneration(ctx, job.WorkspaceID, job.ServiceID, generation, job.ID)
	if err != nil {
		_ = m.catalog.discardGeneration(ctx, job.ServiceID, generation)
		if errors.Is(err, errHarvestCancelled) {
			m.cancelJob(ctx, job)
			return
		}
		m.failJob(ctx, job, err)
		return
	}
	if err := m.registry.RefreshService(ctx, job.WorkspaceID, job.ServiceID); err != nil {
		if restoreErr := m.catalog.restoreGeneration(ctx, job.ServiceID, generation, previous); restoreErr != nil {
			m.failJob(ctx, job, errors.Join(fmt.Errorf("activate harvested mosaic: %w", err), fmt.Errorf("restore previous mosaic generation: %w", restoreErr)))
			return
		}
		_ = m.catalog.discardGeneration(ctx, job.ServiceID, generation)
		m.failJob(ctx, job, fmt.Errorf("activate harvested mosaic: %w", err))
		return
	}
	_ = m.catalog.pruneInactiveGenerations(ctx, job.ServiceID, generation)
	now := time.Now().UTC()
	job.Status, job.CompletedAt, job.ErrorMessage = JobSucceeded, &now, ""
	if err := m.catalog.updateJob(ctx, job); err != nil {
		slog.Error("persist mosaic harvest completion", "job", job.ID, "error", err)
	}
}

func (m *Manager) harvestCandidates(ctx context.Context, job *Job) ([]HarvestGranule, error) {
	ws, ok := m.registry.GetByID(job.WorkspaceID)
	if !ok {
		return nil, workspace.ErrWorkspaceNotFound
	}
	service := ws.ResolveService(job.ServiceID)
	if service == nil {
		return nil, workspace.ErrServiceNotFound
	}
	var config store.RasterMosaicConnectionInfo
	if err := json.Unmarshal(service.ConnectionInfo, &config); err != nil {
		return nil, err
	}
	items := append([]HarvestGranule(nil), job.Request.Granules...)
	if len(job.Request.Granules) == 0 {
		for _, item := range config.Granules {
			items = append(items, HarvestGranule{Path: item.Path, Time: item.Time, Elevation: item.Elevation, Priority: item.Priority})
		}
	}
	directory := firstNonEmpty(job.Request.Directory, config.Directory)
	pattern := firstNonEmpty(job.Request.Pattern, config.Pattern, "*.tif")
	if directory != "" {
		if err := pathpolicy.Check(filepath.Join(directory, pattern)); err != nil {
			return nil, err
		}
		matches, err := doublestar.FilepathGlob(filepath.Join(directory, pattern))
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			items = append(items, HarvestGranule{Path: match})
		}
	}
	seen := map[string]bool{}
	result := items[:0]
	for _, item := range items {
		item.Path = strings.TrimSpace(item.Path)
		if item.Path == "" || seen[item.Path] {
			continue
		}
		if err := pathpolicy.Check(item.Path); err != nil {
			return nil, err
		}
		seen[item.Path] = true
		result = append(result, item)
	}
	slices.SortFunc(result, func(a, b HarvestGranule) int { return strings.Compare(a.Path, b.Path) })
	return result, nil
}

func inspectGranule(ctx context.Context, workspaceID, serviceID string, generation int64, candidate HarvestGranule) (*Granule, error) {
	if candidate.Time != "" {
		instant, err := time.Parse(time.RFC3339Nano, candidate.Time)
		if err != nil {
			return nil, fmt.Errorf("granule time must be RFC 3339: %w", err)
		}
		candidate.Time = instant.UTC().Format(time.RFC3339Nano)
	}
	if candidate.Elevation != nil && (math.IsNaN(*candidate.Elevation) || math.IsInf(*candidate.Elevation, 0)) {
		return nil, errors.New("granule elevation must be finite")
	}
	resolved, err := pathpolicy.Resolve(ctx, candidate.Path)
	if err != nil {
		return nil, err
	}
	openPath := resolved
	if strings.HasPrefix(strings.ToLower(openPath), "s3://") {
		openPath = "/vsis3/" + strings.TrimPrefix(openPath, "s3://")
	}
	dataset, err := godal.Open(openPath, godal.RasterOnly(), godal.Drivers("GTiff"))
	if err != nil {
		return nil, fmt.Errorf("open granule %s: %w", candidate.Path, err)
	}
	defer dataset.Close()
	structure := dataset.Structure()
	if structure.SizeX < 1 || structure.SizeY < 1 || structure.NBands < 1 {
		return nil, errors.New("granule is empty")
	}
	transform, err := dataset.GeoTransform()
	if err != nil || transform[1] == 0 || transform[5] == 0 || transform[2] != 0 || transform[4] != 0 {
		return nil, errors.New("granule must be an axis-aligned georeferenced grid")
	}
	ref := dataset.SpatialRef()
	if ref == nil {
		return nil, errors.New("granule CRS is required")
	}
	if ref.AuthorityCode("") == "" {
		_ = ref.AutoIdentifyEPSG()
	}
	authority, code := ref.AuthorityName(""), ref.AuthorityCode("")
	if authority == "" || code == "" {
		return nil, errors.New("granule CRS authority is required")
	}
	srid, _ := strconv.Atoi(code)
	x2 := transform[0] + float64(structure.SizeX)*transform[1]
	y2 := transform[3] + float64(structure.SizeY)*transform[5]
	bbox := [4]float64{math.Min(transform[0], x2), math.Min(transform[3], y2), math.Max(transform[0], x2), math.Max(transform[3], y2)}
	item := &Granule{WorkspaceID: workspaceID, ServiceID: serviceID, Generation: generation, SourceURI: candidate.Path,
		CRS: "http://www.opengis.net/def/crs/" + authority + "/0/" + code, SRID: srid, BBox: bbox,
		Width: structure.SizeX, Height: structure.SizeY, BandCount: structure.NBands,
		DataType: dataset.Bands()[0].Structure().DataType.String(), ResolutionX: transform[1], ResolutionY: transform[5],
		Time: candidate.Time, Elevation: candidate.Elevation, Priority: candidate.Priority, FootprintWKB: footprintWKB(bbox)}
	if stat, statErr := os.Stat(resolved); statErr == nil {
		modified := stat.ModTime().UTC()
		item.SizeBytes, item.ModifiedAt = stat.Size(), &modified
	}
	return item, nil
}

func compatibleGranule(reference, item *Granule) error {
	if reference == nil || item == nil {
		return nil
	}
	if reference.SRID != item.SRID || reference.BandCount != item.BandCount || reference.DataType != item.DataType {
		return fmt.Errorf("granule %s is incompatible with the mosaic CRS/band schema", item.SourceURI)
	}
	return nil
}

func (m *Manager) cancelled(ctx context.Context, job *Job) bool {
	if m.lifecycleBlocked(job.WorkspaceID, job.ServiceID) {
		return true
	}
	current, err := m.catalog.getJob(ctx, job.ID)
	return err != nil || current.CancelRequested
}

func (m *Manager) failJob(ctx context.Context, job *Job, err error) {
	if current, readErr := m.catalog.getJob(ctx, job.ID); readErr == nil && current.CancelRequested {
		m.cancelJob(ctx, job)
		return
	}
	now := time.Now().UTC()
	job.Status, job.CompletedAt, job.ErrorMessage = JobFailed, &now, err.Error()
	if persistErr := m.catalog.updateJob(ctx, job); persistErr != nil {
		slog.Error("persist mosaic harvest failure", "job", job.ID, "error", persistErr)
	}
}

func (m *Manager) cancelJob(ctx context.Context, job *Job) {
	now := time.Now().UTC()
	job.Status, job.CompletedAt, job.CancelRequested = JobCancelled, &now, true
	if err := m.catalog.updateJob(ctx, job); err != nil {
		slog.Error("persist mosaic harvest cancellation", "job", job.ID, "error", err)
	}
}

func (m *Manager) wake() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *Manager) lockService(serviceID string) func() {
	value, _ := m.serviceLocks.LoadOrStore(serviceID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
