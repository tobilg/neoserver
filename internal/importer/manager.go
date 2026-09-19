// Package importer implements durable, bounded managed-vector import jobs.
package importer

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zip"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	duckdbsource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/datasource/geoparquet"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/datasource/vectorfile"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type Registry interface {
	GetByID(string) (*workspace.Workspace, bool)
	RefreshService(context.Context, string, string) error
}

type RollbackCoordinator interface {
	DeleteService(context.Context, string, string, bool) (*store.DeletionOperation, error)
	Get(context.Context, string) (*store.DeletionOperation, error)
	FindServiceDeletion(context.Context, string) (*store.DeletionOperation, error)
	Retry(context.Context, string) (*store.DeletionOperation, error)
}

type Manager struct {
	cfg       conf.Importer
	store     store.ImportStore
	registry  Registry
	rollback  RollbackCoordinator
	logger    *slog.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	notify    chan struct{}
	jobSlots  chan struct{}
	claimMu   sync.Mutex
	storageMu sync.Mutex
	active    map[string]bool // protected by claimMu
}

// ReconcileManagedPaths relocates catalog-owned import paths beneath the
// configured root before workspace data sources are opened. This makes a
// quiescent consistency set restorable at a different mount path.
func ReconcileManagedPaths(ctx context.Context, cfg conf.Importer, durable store.ImportStore) error {
	if err := reconcileRetainedSources(ctx, cfg, durable); err != nil {
		return err
	}
	assets, err := durable.ListManagedAssets(ctx)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		job, err := durable.GetImportJob(ctx, asset.ImportID)
		if err != nil {
			return err
		}
		stage := filepath.Join(cfg.Root, ".staging", job.ID+".duckdb")
		if strings.HasPrefix(filepath.Base(asset.Path), job.ID+".revision-") {
			stage = filepath.Join(cfg.Root, ".staging", filepath.Base(asset.Path))
		}
		final := filepath.Join(cfg.Root, job.WorkspaceID, job.ID+".duckdb")
		candidate := ""
		switch job.Status {
		case store.ImportPublished, store.ImportPublishing, store.ImportRollingBack:
			if regularFile(final) || (job.RollbackOperationID != "" && regularFile(final+".deleting-"+job.RollbackOperationID)) {
				candidate = final
			}
		}
		if candidate == "" && regularFile(stage) {
			candidate = stage
		}
		if candidate == "" && regularFile(final) {
			candidate = final
		}
		if candidate != "" && filepath.Clean(candidate) != filepath.Clean(asset.Path) {
			if err := durable.UpdateManagedAssetPath(ctx, job.ID, candidate); err != nil {
				return fmt.Errorf("relocate managed import %s: %w", job.ID, err)
			}
		}
	}
	return nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func New(ctx context.Context, cfg conf.Importer, durable store.ImportStore, registry Registry, rollback RollbackCoordinator, logger *slog.Logger) (*Manager, error) {
	if durable == nil || registry == nil {
		return nil, errors.New("importer requires durable store and workspace registry")
	}
	if err := os.MkdirAll(cfg.Root, 0700); err != nil {
		return nil, fmt.Errorf("create importer root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.Root, ".staging"), 0700); err != nil {
		return nil, fmt.Errorf("create importer staging root: %w", err)
	}
	if err := os.MkdirAll(cfg.TemporaryDirectory, 0700); err != nil {
		return nil, fmt.Errorf("create importer temporary directory: %w", err)
	}
	if err := durable.ResetInterruptedImportJobs(ctx); err != nil {
		return nil, fmt.Errorf("reset interrupted imports: %w", err)
	}
	managerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m := &Manager{cfg: cfg, store: durable, registry: registry, rollback: rollback, logger: logger, ctx: managerCtx, cancel: cancel,
		notify: make(chan struct{}, 1), jobSlots: make(chan struct{}, cfg.MaxConcurrentJobs), active: make(map[string]bool)}
	if err := m.recoverInterrupted(ctx); err != nil {
		cancel()
		return nil, err
	}
	if err := m.cleanupAbandonedRevisions(ctx); err != nil {
		cancel()
		return nil, err
	}
	m.logSourceUsage()
	for range cfg.WorkerCount {
		m.wg.Add(1)
		go m.worker()
	}
	return m, nil
}

func (m *Manager) Health(context.Context) error {
	if err := m.ctx.Err(); err != nil {
		return fmt.Errorf("import coordinator stopped: %w", err)
	}
	return nil
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

func (m *Manager) CreateURI(ctx context.Context, workspaceID, name, rawURI, createdBy string) (*store.ImportJob, error) {
	if _, ok := m.registry.GetByID(workspaceID); !ok {
		return nil, workspace.ErrWorkspaceNotFound
	}
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return nil, errors.New("invalid source_uri")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("source_uri must not contain credentials, query parameters, or fragments")
	}
	if parsed.Scheme != "" && !strings.EqualFold(parsed.Scheme, "https") {
		return nil, errors.New("managed imports support local allowlisted paths and HTTPS sources")
	}
	if err := pathpolicy.Check(rawURI); err != nil {
		return nil, err
	}
	if name == "" {
		name = sourceBaseName(parsed.Path)
	}
	job, err := m.store.CreateImportJob(ctx, store.CreateImportJobInput{WorkspaceID: workspaceID, Name: name, SourceKind: "uri",
		SourceLocator: sanitizedLocator(parsed), SourcePath: rawURI, SourceFilename: filepath.Base(parsed.Path), CreatedBy: createdBy})
	if err == nil {
		m.wake()
	}
	return job, err
}

func (m *Manager) CreateUpload(ctx context.Context, workspaceID, name, filename, createdBy string, source io.Reader) (*store.ImportJob, error) {
	m.storageMu.Lock()
	defer m.storageMu.Unlock()
	if _, ok := m.registry.GetByID(workspaceID); !ok {
		return nil, workspace.ErrWorkspaceNotFound
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" {
		return nil, errors.New("filename is required")
	}
	if !supportedSource(filename) && !strings.EqualFold(filepath.Ext(filename), ".zip") {
		return nil, errors.New("unsupported vector import format")
	}
	jobDir, err := os.MkdirTemp(m.cfg.TemporaryDirectory, "upload-*")
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(jobDir)
		}
	}()
	if err := os.Chmod(jobDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(jobDir, filename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	remaining, err := m.remainingSourceBytes()
	if err != nil {
		file.Close()
		return nil, err
	}
	limit := min(m.cfg.MaxUploadBytes, remaining)
	written, copyErr := io.Copy(file, io.LimitReader(source, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written > limit {
		return nil, fmt.Errorf("upload exceeds available retained-source budget or %d byte upload limit; publish or cancel unused imports", m.cfg.MaxUploadBytes)
	}
	if name == "" {
		name = sourceBaseName(filename)
	}
	job, err := m.store.CreateImportJob(ctx, store.CreateImportJobInput{WorkspaceID: workspaceID, Name: name, SourceKind: "upload",
		SourceLocator: filename, SourcePath: path, SourceRelativePath: filepath.Join(filepath.Base(jobDir), filename), SourceFilename: filename, CreatedBy: createdBy})
	if err != nil {
		return nil, err
	}
	cleanup = false
	m.logSourceUsage()
	m.wake()
	return job, nil
}

func (m *Manager) Get(ctx context.Context, id string) (*store.ImportJob, error) {
	return m.store.GetImportJob(ctx, id)
}
func (m *Manager) List(ctx context.Context, workspaceID string, limit int) ([]*store.ImportJob, error) {
	return m.store.ListImportJobs(ctx, workspaceID, limit)
}
func (m *Manager) ListPage(ctx context.Context, workspaceID string, options store.ImportListOptions) (*store.ImportJobPage, error) {
	return m.store.ListImportJobsPage(ctx, workspaceID, options)
}

func (m *Manager) SetPlan(ctx context.Context, id string, plan store.ImportPlan) (*store.ImportJob, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	job, err := m.store.GetImportJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Status != store.ImportAwaitingPlan && job.Status != store.ImportReadyToPublish && job.Status != store.ImportFailed {
		return nil, errors.New("import is not accepting a plan")
	}
	if job.ServiceID != "" || job.Phase == store.ImportPhaseRollback {
		return nil, errors.New("publication recovery must finish before changing the import plan")
	}
	if err = m.validatePlan(job, &plan); err != nil {
		return nil, err
	}
	if !regularFile(job.SourcePath) {
		return nil, errors.New("import source is no longer available; upload the dataset again")
	}
	status, phase, cancel, errorMessage := store.ImportQueued, store.ImportPhaseValidate, false, ""
	job, err = m.store.UpdateImportJob(ctx, id, store.ImportJobUpdate{Plan: &plan, Status: &status, Phase: &phase, CancelRequested: &cancel, ErrorMessage: &errorMessage, ClearCompletedAt: true})
	if err == nil {
		m.wake()
	}
	return job, err
}

func (m *Manager) Cancel(ctx context.Context, id string) error {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	job, err := m.store.GetImportJob(ctx, id)
	if err != nil {
		return err
	}
	if job.ServiceID != "" || job.Phase == store.ImportPhaseRollback {
		return errors.New("published imports must be removed through publication rollback")
	}
	err = m.store.RequestImportCancel(ctx, id)
	if err != nil {
		return err
	}
	if (job.Status != store.ImportRunning && job.Status != store.ImportCancelling) || (job.Status == store.ImportCancelling && !m.active[id]) {
		if err := m.cleanupCancelled(ctx, job); err != nil {
			return err
		}
	}
	m.wake()
	return nil
}

func (m *Manager) Retry(ctx context.Context, id string) (*store.ImportJob, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	job, err := m.store.GetImportJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Status != store.ImportFailed {
		return nil, errors.New("only failed imports can be retried")
	}
	if job.RetryCount >= m.cfg.MaxRetries {
		return nil, errors.New("import retry limit reached")
	}
	if job.Phase == store.ImportPhaseRollback && job.RollbackOperationID != "" {
		if m.rollback == nil {
			return nil, errors.New("catalog lifecycle is unavailable")
		}
		if _, err = m.rollback.Retry(ctx, job.RollbackOperationID); err != nil {
			return nil, err
		}
		retries, status, message := job.RetryCount+1, store.ImportRollingBack, ""
		job, err = m.store.UpdateImportJob(ctx, id, store.ImportJobUpdate{RetryCount: &retries, Status: &status, ErrorMessage: &message, ClearCompletedAt: true})
		if err == nil {
			m.watchRollback(job, job.RollbackOperationID)
		}
		return job, err
	}
	retries, status, cancel, message := job.RetryCount+1, store.ImportQueued, false, ""
	job, err = m.store.UpdateImportJob(ctx, id, store.ImportJobUpdate{RetryCount: &retries, Status: &status, CancelRequested: &cancel, ErrorMessage: &message})
	if err == nil {
		m.wake()
	}
	return job, err
}

func (m *Manager) Preview(ctx context.Context, id, layer string, limit int) ([]json.RawMessage, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	job, err := m.store.GetImportJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Status != store.ImportReadyToPublish && job.Status != store.ImportPublished {
		return nil, errors.New("preview is not ready")
	}
	asset, err := m.store.GetManagedAssetByImport(ctx, id)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > m.cfg.PreviewFeatures {
		limit = m.cfg.PreviewFeatures
	}
	ds, err := duckdbsource.New("preview-"+id, duckdbsource.Config{Path: asset.Path, ReadOnly: true, EncryptionKey: asset.EncryptionKey, Extensions: []string{"spatial"}})
	if err != nil {
		return nil, err
	}
	defer ds.Close()
	if layer == "" && job.Plan != nil && len(job.Plan.Layers) > 0 {
		layer = job.Plan.Layers[0].PublicID
	}
	return ds.Query(ctx, layer, datasource.QueryParams{Limit: limit})
}

// managedLayerExtents computes each published layer's envelope from the
// promoted managed database. Extents only improve previews and capabilities,
// so a failure is logged and the layer is published without one.
func (m *Manager) managedLayerExtents(ctx context.Context, id, path, key string, srids map[string]int) map[string]*store.SpatialExtent {
	extents := make(map[string]*store.SpatialExtent, len(srids))
	ds, err := duckdbsource.New("extent-"+id, duckdbsource.Config{Path: path, ReadOnly: true, EncryptionKey: key, Extensions: []string{"spatial"}, LayerSRIDs: srids})
	if err != nil {
		m.logger.Warn("managed layer extents unavailable", "import", id, "error", err)
		return extents
	}
	defer ds.Close()
	for layer := range srids {
		extent, err := ds.GetLayerExtent(ctx, layer)
		if err != nil || extent == nil {
			m.logger.Warn("managed layer extent unavailable", "import", id, "layer", layer, "error", err)
			continue
		}
		extents[layer] = &store.SpatialExtent{MinX: extent.MinX, MinY: extent.MinY, MaxX: extent.MaxX, MaxY: extent.MaxY, SRID: extent.SRID}
	}
	return extents
}

func (m *Manager) Publish(ctx context.Context, id string) (*store.ImportJob, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	job, err := m.store.GetImportJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Status != store.ImportReadyToPublish {
		return nil, errors.New("import is not ready to publish")
	}
	if job.Plan == nil {
		return nil, errors.New("import plan is missing")
	}
	status, phase := store.ImportPublishing, store.ImportPhasePublish
	if _, err = m.store.UpdateImportJob(ctx, id, store.ImportJobUpdate{Status: &status, Phase: &phase}); err != nil {
		return nil, err
	}
	job.Status, job.Phase = status, phase
	return m.finishPublish(ctx, job)
}

func (m *Manager) finishPublish(ctx context.Context, job *store.ImportJob) (*store.ImportJob, error) {
	id := job.ID
	asset, err := m.store.GetManagedAssetByImport(ctx, id)
	if err != nil {
		return nil, m.fail(ctx, job, err)
	}
	workspaceRoot := filepath.Join(m.cfg.Root, job.WorkspaceID)
	if err = os.MkdirAll(workspaceRoot, 0700); err != nil {
		return nil, m.fail(ctx, job, err)
	}
	finalPath := filepath.Join(workspaceRoot, job.ID+".duckdb")
	if asset.Path != finalPath {
		if err = os.Rename(asset.Path, finalPath); errors.Is(err, os.ErrNotExist) {
			if info, finalErr := os.Stat(finalPath); finalErr != nil || !info.Mode().IsRegular() {
				return nil, m.fail(ctx, job, fmt.Errorf("recover promoted managed database: %w", err))
			}
		} else if err != nil {
			return nil, m.fail(ctx, job, fmt.Errorf("promote managed database: %w", err))
		}
	}
	connection, _ := json.Marshal(store.DuckDBConnectionInfo{ManagedImportID: job.ID})
	srids := make(map[string]int, len(job.Plan.Layers))
	for _, item := range job.Plan.Layers {
		srids[item.PublicID] = item.TargetSRID
		if srids[item.PublicID] == 0 {
			srids[item.PublicID] = item.SourceSRID
		}
	}
	extents := m.managedLayerExtents(ctx, id, finalPath, asset.EncryptionKey, srids)
	layers := make([]store.CreateLayerInput, 0, len(job.Plan.Layers))
	for _, item := range job.Plan.Layers {
		layers = append(layers, store.CreateLayerInput{SourceLayer: item.PublicID, PublicID: item.PublicID, Title: item.Title, Description: item.Description, Enabled: item.Enabled, CRSDefault: srids[item.PublicID], Public: item.Public, AllowedRoles: item.AllowedRoles, NativeExtent: extents[item.PublicID]})
	}
	_, _, err = m.store.PublishImport(ctx, store.PublishImportInput{ImportID: id, WorkspaceID: job.WorkspaceID, ManagedPath: finalPath, EncryptionKey: asset.EncryptionKey,
		Service: store.CreateServiceInput{WorkspaceID: job.WorkspaceID, Name: job.Plan.ServiceName, Type: store.ServiceTypeDuckDB, ConnectionInfo: connection, Enabled: true}, Layers: layers})
	if err != nil {
		if asset.Path != finalPath {
			_ = os.Rename(finalPath, asset.Path)
		}
		return nil, m.fail(ctx, job, err)
	}
	published, _ := m.store.GetImportJob(ctx, id)
	if err := m.cleanupTemporarySource(job.SourcePath); err != nil {
		m.logger.Error("published import source cleanup failed", "import", id, "error", err)
	}
	if published != nil && published.ServiceID != "" {
		if err = m.registry.RefreshService(ctx, published.WorkspaceID, published.ServiceID); err != nil {
			m.logger.Error("published import runtime refresh failed", "import", id, "error", err)
			return published, nil
		}
	}
	return published, nil
}

func (m *Manager) Rollback(ctx context.Context, id string) (*store.ImportJob, error) {
	job, err := m.store.GetImportJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Status != store.ImportPublished {
		return nil, errors.New("only published imports can be rolled back")
	}
	if m.rollback == nil {
		return nil, errors.New("catalog lifecycle is unavailable")
	}
	status, phase := store.ImportRollingBack, store.ImportPhaseRollback
	job, err = m.store.UpdateImportJob(ctx, id, store.ImportJobUpdate{Status: &status, Phase: &phase})
	if err != nil {
		return nil, err
	}
	op, err := m.rollback.DeleteService(context.WithoutCancel(ctx), job.WorkspaceID, job.ServiceID, true)
	if err != nil {
		return nil, m.fail(ctx, job, err)
	}
	job, err = m.store.UpdateImportJob(ctx, id, store.ImportJobUpdate{RollbackOperationID: &op.ID})
	if err != nil {
		return nil, err
	}
	m.watchRollback(job, op.ID)
	return job, nil
}

func (m *Manager) recoverInterrupted(ctx context.Context) error {
	jobs, err := m.store.ListInterruptedImportJobs(ctx)
	if err != nil {
		return fmt.Errorf("list interrupted imports: %w", err)
	}
	for _, job := range jobs {
		switch job.Status {
		case store.ImportPublished:
			if err := m.cleanupTemporarySource(job.SourcePath); err != nil {
				m.logger.Error("resume published import source cleanup", "import", job.ID, "error", err)
			}
		case store.ImportCancelling, store.ImportCancelled:
			if err := m.cleanupCancelled(ctx, job); err != nil {
				return fmt.Errorf("resume import cancellation %s: %w", job.ID, err)
			}
		case store.ImportPublishing:
			if _, publishErr := m.finishPublish(ctx, job); publishErr != nil {
				m.logger.Error("resume interrupted import publication failed", "import", job.ID, "error", publishErr)
			}
		case store.ImportRollingBack:
			if recoverErr := m.recoverRollback(ctx, job); recoverErr != nil {
				m.logger.Error("resume interrupted import rollback failed", "import", job.ID, "error", recoverErr)
				_ = m.fail(context.WithoutCancel(ctx), job, recoverErr)
			}
		}
	}
	return nil
}

func (m *Manager) recoverRollback(ctx context.Context, job *store.ImportJob) error {
	if m.rollback == nil {
		return errors.New("catalog lifecycle is unavailable")
	}
	operationID := job.RollbackOperationID
	if operationID == "" {
		op, err := m.rollback.FindServiceDeletion(ctx, job.ServiceID)
		if errors.Is(err, store.ErrNotFound) {
			op, err = m.rollback.DeleteService(context.WithoutCancel(ctx), job.WorkspaceID, job.ServiceID, true)
		}
		if err != nil {
			return err
		}
		operationID = op.ID
		job, err = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{RollbackOperationID: &operationID})
		if err != nil {
			return err
		}
	}
	m.watchRollback(job, operationID)
	return nil
}

func (m *Manager) watchRollback(job *store.ImportJob, operationID string) {
	m.wg.Add(1)
	go m.finishRollback(job, operationID)
}

func (m *Manager) finishRollback(job *store.ImportJob, operationID string) {
	defer m.wg.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			op, err := m.rollback.Get(m.ctx, operationID)
			if err != nil {
				continue
			}
			if op.Status == store.DeletionCompleted {
				status, phase, now := store.ImportRolledBack, store.ImportPhaseRollback, time.Now().UTC()
				_, _ = m.store.UpdateImportJob(m.ctx, job.ID, store.ImportJobUpdate{Status: &status, Phase: &phase, CompletedAt: &now})
				return
			}
			if op.Status == store.DeletionFailed {
				_ = m.fail(m.ctx, job, errors.New(op.LastError))
				return
			}
		}
	}
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case m.jobSlots <- struct{}{}:
		case <-m.ctx.Done():
			return
		}
		job, err := m.claim(m.ctx)
		if err == nil && job != nil {
			m.run(job)
			<-m.jobSlots
			continue
		}
		<-m.jobSlots
		select {
		case <-m.ctx.Done():
			return
		case <-m.notify:
		case <-time.After(time.Second):
		}
	}
}
func (m *Manager) wake() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}
func (m *Manager) claim(ctx context.Context) (*store.ImportJob, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	jobs, err := m.store.ListRunnableImportJobs(ctx)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	job := jobs[0]
	status, now := store.ImportRunning, time.Now().UTC()
	job, err = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &status, StartedAt: &now})
	if err == nil {
		if m.active == nil {
			m.active = make(map[string]bool)
		}
		m.active[job.ID] = true
	}
	return job, err
}

func (m *Manager) run(job *store.ImportJob) {
	defer func() { m.claimMu.Lock(); delete(m.active, job.ID); m.claimMu.Unlock() }()
	ctx, cancel := context.WithTimeout(m.ctx, time.Duration(m.cfg.TransformTimeoutSec)*time.Second)
	defer cancel()
	var err error
	if job.Discovery == nil {
		err = m.discover(ctx, job)
	} else if job.Plan != nil {
		err = m.transform(ctx, job)
	} else {
		status := store.ImportAwaitingPlan
		_, err = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &status})
	}
	if m.cancelled(context.WithoutCancel(ctx), job.ID) {
		err = context.Canceled
	}
	if err != nil {
		_ = m.fail(context.WithoutCancel(m.ctx), job, err)
	}
}

func (m *Manager) cancelled(ctx context.Context, jobID string) bool {
	job, err := m.store.GetImportJob(ctx, jobID)
	return err == nil && job.CancelRequested
}

func (m *Manager) discover(ctx context.Context, job *store.ImportJob) error {
	phase := store.ImportPhaseAcquire
	_, _ = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Phase: &phase})
	path := job.SourcePath
	var err error
	if job.SourceKind == "uri" {
		path, err = pathpolicy.Resolve(ctx, path)
		if err != nil {
			return err
		}
		if _, err := m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{SourcePath: &path}); err != nil {
			return err
		}
	}
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		archivePath := path
		path, err = m.extractArchive(ctx, job, path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(m.cfg.TemporaryDirectory, path)
		if err != nil {
			return err
		}
		if _, err := m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{SourcePath: &path, SourceRelativePath: &relative}); err != nil {
			return err
		}
		if err := m.cleanupTemporarySource(archivePath); err != nil {
			return err
		}
		m.logSourceUsage()
	}
	if info, statErr := os.Stat(path); statErr == nil {
		if info.Size() > m.cfg.MaxSourceBytes {
			return fmt.Errorf("source exceeds %d byte limit", m.cfg.MaxSourceBytes)
		}
		size := info.Size()
		_, _ = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{ProcessedBytes: &size})
	}
	if m.cancelled(ctx, job.ID) {
		return context.Canceled
	}
	phase = store.ImportPhaseDiscover
	_, _ = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Phase: &phase})
	ds, err := openSource(job.ID, path)
	if err != nil {
		return err
	}
	defer ds.Close()
	discovered, err := ds.DiscoverLayers(ctx)
	if err != nil {
		return err
	}
	if len(discovered) == 0 {
		return errors.New("source contains no vector layers")
	}
	if len(discovered) > m.cfg.MaxLayers {
		return fmt.Errorf("source has %d layers, exceeding limit %d", len(discovered), m.cfg.MaxLayers)
	}
	result := &store.ImportDiscovery{Layers: make([]store.ImportDiscoveredLayer, 0, len(discovered))}
	var total int64
	for _, layer := range discovered {
		if m.cancelled(ctx, job.ID) {
			return context.Canceled
		}
		info, infoErr := ds.GetLayerInfo(ctx, layer.Name)
		if infoErr != nil {
			return infoErr
		}
		count, countErr := ds.Count(ctx, layer.Name, datasource.QueryParams{})
		if countErr != nil {
			return countErr
		}
		total += int64(count)
		if total > m.cfg.MaxFeatures {
			return fmt.Errorf("source exceeds %d feature limit", m.cfg.MaxFeatures)
		}
		item := store.ImportDiscoveredLayer{Name: layer.Name, Title: layer.Title, Description: layer.Description, GeometryColumn: layer.GeometryColumn, GeometryType: layer.GeometryType, SRID: layer.SRID, IDColumn: layer.IDColumn, FeatureCount: int64(count)}
		if extentDS, ok := ds.(datasource.LayerExtentDataSource); ok {
			if extent, e := extentDS.GetLayerExtent(ctx, layer.Name); e == nil && extent != nil {
				item.NativeExtent = &store.SpatialExtent{MinX: extent.MinX, MinY: extent.MinY, MaxX: extent.MaxX, MaxY: extent.MaxY, SRID: extent.SRID}
			}
		}
		for _, property := range info.Properties {
			item.Properties = append(item.Properties, store.ImportProperty{Name: property.Name, Type: property.Type, JSONType: string(property.JSONType), Ordinal: property.Ordinal})
		}
		result.Layers = append(result.Layers, item)
	}
	totalLayers := len(result.Layers)
	status := store.ImportAwaitingPlan
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	if m.cancelled(ctx, job.ID) {
		return context.Canceled
	}
	_, err = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Discovery: result, Status: &status, TotalLayers: &totalLayers, ProcessedFeatures: &total})
	return err
}

func (m *Manager) transform(ctx context.Context, job *store.ImportJob) error {
	if err := m.validatePlan(job, job.Plan); err != nil {
		return err
	}
	phase := store.ImportPhaseTransform
	processed := 0
	features := int64(0)
	_, _ = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Phase: &phase, ProcessedLayers: &processed, ProcessedFeatures: &features})
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return err
	}
	key := hex.EncodeToString(keyBytes)
	stagePath := filepath.Join(m.cfg.Root, ".staging", job.ID+".revision-"+uuid.NewString()+".duckdb")
	selected := false
	defer func() {
		if !selected {
			_ = removeOwnedFile(stagePath)
			_ = removeOwnedFile(stagePath + ".wal")
		}
	}()
	db, err := openImportDatabase(stagePath, key)
	if err != nil {
		return err
	}
	defer db.Close()
	discovered := make(map[string]store.ImportDiscoveredLayer)
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA managed.__neoserver; CREATE TABLE managed.__neoserver.layers (name VARCHAR PRIMARY KEY, srid INTEGER NOT NULL)"); err != nil {
		return err
	}
	for _, item := range job.Discovery.Layers {
		discovered[item.Name] = item
	}
	for _, plan := range job.Plan.Layers {
		if m.cancelled(ctx, job.ID) {
			return context.Canceled
		}
		source := discovered[plan.SourceLayer]
		if strings.EqualFold(filepath.Ext(job.SourcePath), ".parquet") {
			var geometryType string
			err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT typeof((SELECT %s FROM read_parquet('%s') LIMIT 1))", quoteIdent(source.GeometryColumn), quoteLiteralValue(job.SourcePath))).Scan(&geometryType)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("inspect Parquet geometry: %w", err)
			}
			// Properties retain the physical representation for SQL generation.
			source.Properties = append(source.Properties, store.ImportProperty{Name: source.GeometryColumn, Type: geometryType})
		}
		query, idColumn, err := buildTransformSQL(job.SourcePath, source, plan)
		if err != nil {
			return err
		}
		if _, err = db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("transform layer %q: %w", plan.SourceLayer, err)
		}
		var total, nonNull, distinct int64
		if err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*),count(%s),count(DISTINCT %s) FROM managed.%s", quoteIdent(idColumn), quoteIdent(idColumn), quoteIdent(plan.PublicID))).Scan(&total, &nonNull, &distinct); err != nil {
			return err
		}
		if total != nonNull || total != distinct {
			return fmt.Errorf("id column %q for layer %q must be non-null and unique", idColumn, plan.PublicID)
		}
		if _, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE managed.%s ADD PRIMARY KEY (%s)", quoteIdent(plan.PublicID), quoteIdent(idColumn))); err != nil {
			return fmt.Errorf("add primary key: %w", err)
		}
		if _, err = db.ExecContext(ctx, "INSERT INTO managed.__neoserver.layers VALUES (?, ?)", plan.PublicID, plan.TargetSRID); err != nil {
			return err
		}
		features += total
		processed++
		_, _ = m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{ProcessedLayers: &processed, ProcessedFeatures: &features})
	}
	if _, err = db.ExecContext(ctx, "CHECKPOINT managed"); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	m.claimMu.Lock()
	defer m.claimMu.Unlock()
	if m.cancelled(ctx, job.ID) {
		return context.Canceled
	}
	previous, previousErr := m.store.GetManagedAssetByImport(ctx, job.ID)
	if previousErr != nil && !errors.Is(previousErr, store.ErrNotFound) {
		return previousErr
	}
	if err := m.store.CompleteImportTransform(ctx, store.ManagedAsset{ImportID: job.ID, WorkspaceID: job.WorkspaceID, Path: stagePath, EncryptionKey: key}); err != nil {
		return err
	}
	selected = true
	if previous != nil {
		if err := removeOwnedFile(previous.Path); err != nil {
			m.logger.Error("remove superseded import revision", "import", job.ID, "error", err)
		}
	}
	return nil
}

func (m *Manager) validatePlan(job *store.ImportJob, plan *store.ImportPlan) error {
	if job.Discovery == nil {
		return errors.New("discovery is not complete")
	}
	if plan == nil || !safeName(plan.ServiceName) {
		return errors.New("service_name must be a safe non-empty identifier")
	}
	if len(plan.Layers) == 0 || len(plan.Layers) > m.cfg.MaxLayers {
		return errors.New("import plan must select a bounded number of layers")
	}
	sources := map[string]store.ImportDiscoveredLayer{}
	for _, item := range job.Discovery.Layers {
		sources[item.Name] = item
	}
	publicIDs := map[string]bool{}
	for i := range plan.Layers {
		item := &plan.Layers[i]
		source, ok := sources[item.SourceLayer]
		if !ok {
			return fmt.Errorf("unknown source layer %q", item.SourceLayer)
		}
		if !safeName(item.PublicID) || publicIDs[item.PublicID] {
			return fmt.Errorf("invalid or duplicate public_id %q", item.PublicID)
		}
		publicIDs[item.PublicID] = true
		if item.GeometryColumn == "" {
			item.GeometryColumn = source.GeometryColumn
		}
		if item.TargetGeometry == "" {
			item.TargetGeometry = item.GeometryColumn
		}
		if item.SourceSRID == 0 {
			item.SourceSRID = source.SRID
		}
		if item.TargetSRID == 0 {
			item.TargetSRID = item.SourceSRID
		}
		if item.SourceSRID <= 0 || item.TargetSRID <= 0 {
			return fmt.Errorf("layer %q requires valid source and target CRS", item.SourceLayer)
		}
		properties := map[string]bool{}
		for _, p := range source.Properties {
			properties[p.Name] = true
		}
		targets := map[string]bool{}
		for _, field := range item.Fields {
			if !properties[field.Source] {
				return fmt.Errorf("unknown field %q", field.Source)
			}
			if field.Include {
				target := field.Target
				if target == "" {
					target = field.Source
				}
				if !safeName(target) || targets[target] {
					return fmt.Errorf("invalid or duplicate target field %q", target)
				}
				targets[target] = true
				if _, ok := castType(field.Type); !ok {
					return fmt.Errorf("unsupported target type %q", field.Type)
				}
			}
		}
	}
	return nil
}

func (m *Manager) extractArchive(ctx context.Context, job *store.ImportJob, path string) (string, error) {
	m.storageMu.Lock()
	defer m.storageMu.Unlock()
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > m.cfg.MaxArchiveFiles {
		return "", fmt.Errorf("archive exceeds %d file limit", m.cfg.MaxArchiveFiles)
	}
	root := filepath.Join(m.cfg.TemporaryDirectory, "extract-"+job.ID)
	_ = os.RemoveAll(root)
	if err = os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	var expanded int64
	remaining, err := m.remainingSourceBytes()
	if err != nil {
		return "", err
	}
	expandedLimit := min(m.cfg.MaxExpandedBytes, remaining)
	var candidates []string
	for _, entry := range reader.File {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		clean := filepath.Clean(entry.Name)
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", errors.New("archive contains unsafe path")
		}
		destination := filepath.Join(root, clean)
		if entry.FileInfo().IsDir() {
			if err = os.MkdirAll(destination, 0700); err != nil {
				return "", err
			}
			continue
		}
		remaining := expandedLimit - expanded
		if remaining < 0 || entry.UncompressedSize64 > uint64(remaining) {
			return "", fmt.Errorf("archive exceeds available retained-source budget or %d expanded-byte limit", m.cfg.MaxExpandedBytes)
		}
		if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return "", err
		}
		source, err := entry.Open()
		if err != nil {
			return "", err
		}
		target, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			source.Close()
			return "", err
		}
		copyLimit := remaining
		if copyLimit < math.MaxInt64 {
			copyLimit++
		}
		written, copyErr := io.Copy(target, io.LimitReader(source, copyLimit))
		closeErr := target.Close()
		source.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if written > remaining {
			return "", fmt.Errorf("archive exceeds available retained-source budget or %d expanded-byte limit", m.cfg.MaxExpandedBytes)
		}
		expanded += written
		if closeErr != nil {
			return "", closeErr
		}
		if supportedSource(destination) {
			candidates = append(candidates, destination)
		}
	}
	sort.Strings(candidates)
	for _, candidate := range candidates {
		if strings.EqualFold(filepath.Ext(candidate), ".shp") {
			cleanup = false
			return candidate, nil
		}
	}
	if len(candidates) == 1 {
		cleanup = false
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", errors.New("archive contains no supported vector source")
	}
	return "", errors.New("archive contains multiple vector sources; package one dataset per upload")
}

func (m *Manager) markCancelled(ctx context.Context, job *store.ImportJob) error {
	if err := m.cleanupCancelled(context.WithoutCancel(ctx), job); err != nil {
		return err
	}
	return context.Canceled
}
func (m *Manager) fail(ctx context.Context, job *store.ImportJob, cause error) error {
	if m.cancelled(ctx, job.ID) {
		if err := m.markCancelled(ctx, job); !errors.Is(err, context.Canceled) {
			m.logger.Error("import cancellation cleanup failed; retry cancellation or restart", "import", job.ID, "error", err)
		}
		return cause
	}
	status, now, message := store.ImportFailed, time.Now().UTC(), sanitizeError(cause.Error())
	_, updateErr := m.store.UpdateImportJob(ctx, job.ID, store.ImportJobUpdate{Status: &status, CompletedAt: &now, ErrorMessage: &message})
	if errors.Is(updateErr, context.Canceled) {
		return m.markCancelled(ctx, job)
	}
	return cause
}

func openSource(id, path string) (datasource.DataSource, error) {
	if strings.EqualFold(filepath.Ext(path), ".parquet") {
		return geoparquet.New(id, geoparquet.Config{Path: path})
	}
	return vectorfile.New(id, vectorfile.Config{Path: path})
}
func openImportDatabase(path, key string) (*sql.DB, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.Exec("INSTALL spatial; LOAD spatial"); err != nil {
		db.Close()
		return nil, err
	}
	// The target is local, but an import source may use S3/HTTPS.
	_, _ = db.Exec("INSTALL httpfs; LOAD httpfs")
	statement := fmt.Sprintf("ATTACH '%s' AS managed (ENCRYPTION_KEY '%s')", quoteLiteralValue(path), quoteLiteralValue(key))
	if _, err = db.Exec(statement); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (m *Manager) cleanupTemporarySource(path string) error {
	root, err := filepath.Abs(m.cfg.TemporaryDirectory)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil
	}
	first := strings.Split(relative, string(filepath.Separator))[0]
	if strings.HasPrefix(first, "upload-") || strings.HasPrefix(first, "extract-") {
		return os.RemoveAll(filepath.Join(root, first))
	}
	return nil
}

func removeOwnedFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func buildTransformSQL(path string, source store.ImportDiscoveredLayer, plan store.ImportLayerPlan) (string, string, error) {
	read := fmt.Sprintf("ST_Read('%s'", quoteLiteralValue(path))
	if strings.EqualFold(filepath.Ext(path), ".parquet") {
		read = fmt.Sprintf("read_parquet('%s')", quoteLiteralValue(path))
	} else {
		if source.Name != "" && strings.EqualFold(filepath.Ext(path), ".gpkg") {
			read += fmt.Sprintf(", layer='%s'", quoteLiteralValue(source.Name))
		}
		read += vectorfile.ReadPolicySQL + ")"
	}
	var fields []string
	targetNames := map[string]bool{}
	if len(plan.Fields) == 0 {
		for _, property := range source.Properties {
			if property.Name == source.GeometryColumn {
				continue
			}
			fields = append(fields, "src."+quoteIdent(property.Name))
			targetNames[property.Name] = true
		}
	} else {
		for _, field := range plan.Fields {
			if !field.Include {
				continue
			}
			target := field.Target
			if target == "" {
				target = field.Source
			}
			expression := "src." + quoteIdent(field.Source)
			if sqlType, ok := castType(field.Type); ok && sqlType != "" {
				expression = "CAST(" + expression + " AS " + sqlType + ")"
			}
			fields = append(fields, expression+" AS "+quoteIdent(target))
			targetNames[target] = true
		}
	}
	geom := "src." + quoteIdent(source.GeometryColumn)
	if strings.EqualFold(filepath.Ext(path), ".parquet") {
		var native bool
		for _, property := range source.Properties {
			if property.Name == source.GeometryColumn && strings.HasPrefix(strings.ToUpper(property.Type), "GEOMETRY") {
				native = true
			}
		}
		if !native {
			geom = "ST_GeomFromWKB(" + geom + ")"
		}
	}
	if plan.SourceSRID != plan.TargetSRID {
		geom = fmt.Sprintf("ST_Transform(%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", geom, plan.SourceSRID, plan.TargetSRID)
	}
	fields = append(fields, geom+" AS "+quoteIdent(plan.TargetGeometry))
	idColumn := plan.IDColumn
	if idColumn == "" {
		idColumn = "__neoserver_id"
		fields = append([]string{"row_number() OVER ()::BIGINT AS " + quoteIdent(idColumn)}, fields...)
	} else if !targetNames[idColumn] {
		return "", "", fmt.Errorf("id_column %q is not an included target field", idColumn)
	}
	return fmt.Sprintf("CREATE TABLE managed.%s AS SELECT %s FROM %s AS src", quoteIdent(plan.PublicID), strings.Join(fields, ","), read), idColumn, nil
}

func castType(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", true
	case "string", "varchar":
		return "VARCHAR", true
	case "integer", "bigint":
		return "BIGINT", true
	case "number", "double":
		return "DOUBLE", true
	case "boolean":
		return "BOOLEAN", true
	case "date":
		return "DATE", true
	case "timestamp":
		return "TIMESTAMP", true
	default:
		return "", false
	}
}
func quoteIdent(value string) string        { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func quoteLiteralValue(value string) string { return strings.ReplaceAll(value, "'", "''") }
func supportedSource(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gpkg", ".geojson", ".json", ".fgb", ".parquet", ".shp":
		return true
	default:
		return false
	}
}
func safeName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r == '_' || r == '-' || r == ':' || r == '.' || r > 127 || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
func sourceBaseName(value string) string {
	base := filepath.Base(value)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
func sanitizedLocator(value *url.URL) string {
	clone := *value
	clone.User = nil
	clone.RawQuery = ""
	clone.Fragment = ""
	return clone.String()
}
func sanitizeError(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	if len(value) > 1024 {
		value = value[:1024]
	}
	return value
}
