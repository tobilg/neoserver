// Package cataloglifecycle coordinates referentially-complete publication
// deletion across the primary catalog, operational DuckDB stores, managed
// files, and process-local runtime state.
package cataloglifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/mosaiccatalog"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/wfs"
	"github.com/tobilg/neoserver/internal/workspace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Dependencies struct {
	Catalog        store.Store
	Deletions      store.CatalogDeletionStore
	Registry       *workspace.Registry
	Cache          *cache.Manager
	TileCache      TileCacheLifecycle
	TileJobs       TileJobLifecycle
	Mosaic         MosaicLifecycle
	Enforcer       *rbac.Enforcer
	WFSState       *wfs.RuntimeState
	StyleAssetRoot string
	Assets         AssetStore
	ManagedImports ManagedImportLifecycle
	Logger         *slog.Logger
}

type ManagedImportLifecycle interface {
	GetManagedAssetByService(context.Context, string) (*store.ManagedAsset, error)
	DeleteManagedAsset(context.Context, string) error
}

type TileCacheLifecycle interface {
	LifecycleStats(ctx context.Context, workspaceID string, resourceIDs []string) (entries, bytes, jobs int64, err error)
	PurgeLifecycle(ctx context.Context, workspaceID string, resourceIDs []string) (tilecache.DeleteResult, int64, error)
	LifecycleInventory(ctx context.Context) ([]tilecache.LifecycleOwner, error)
}

type TileJobLifecycle interface {
	QuiesceLifecycle(ctx context.Context, workspaceID string, resourceIDs []string) error
}

type MosaicLifecycle interface {
	LifecycleStats(ctx context.Context, workspaceID string, serviceIDs []string) (services, granules, jobs int64, err error)
	QuiesceAndDeleteLifecycle(ctx context.Context, workspaceID string, serviceIDs []string) error
	LifecycleInventory(ctx context.Context) ([]mosaiccatalog.LifecycleOwner, error)
}

type Coordinator struct {
	deps             Dependencies
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	running          map[string]bool
	wg               sync.WaitGroup
	integrityMu      sync.Mutex
	accepted         metric.Int64Counter
	completed        metric.Int64Counter
	failed           metric.Int64Counter
	tileBytesRemoved metric.Int64Counter
	duration         metric.Float64Histogram
}

func New(ctx context.Context, dependencies Dependencies) (*Coordinator, error) {
	if dependencies.Catalog == nil || dependencies.Deletions == nil || dependencies.Registry == nil {
		return nil, errors.New("catalog lifecycle requires catalog, deletion store, and workspace registry")
	}
	if dependencies.Logger == nil {
		dependencies.Logger = slog.Default()
	}
	if dependencies.StyleAssetRoot == "" {
		dependencies.StyleAssetRoot = "./data/style-assets"
	}
	if dependencies.Assets == nil {
		dependencies.Assets = NewFilesystemAssetStore(dependencies.StyleAssetRoot)
	}
	lifecycleContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	meter := otel.Meter("github.com/tobilg/neoserver/catalog-lifecycle")
	accepted, _ := meter.Int64Counter("neoserver.catalog.deletions.accepted")
	completed, _ := meter.Int64Counter("neoserver.catalog.deletions.completed")
	failed, _ := meter.Int64Counter("neoserver.catalog.deletions.failed")
	tileBytesRemoved, _ := meter.Int64Counter("neoserver.catalog.deletions.tile_bytes_removed")
	duration, _ := meter.Float64Histogram("neoserver.catalog.deletions.duration_seconds")
	return &Coordinator{deps: dependencies, ctx: lifecycleContext, cancel: cancel, running: make(map[string]bool),
		accepted: accepted, completed: completed, failed: failed, tileBytesRemoved: tileBytesRemoved, duration: duration}, nil
}

func (c *Coordinator) Close() {
	c.cancel()
	c.wg.Wait()
}

// Health reports whether the durable deletion coordinator can still accept
// and recover work. Individual failed operations remain retryable and do not
// make the coordinator itself unhealthy.
func (c *Coordinator) Health(_ context.Context) error {
	if err := c.ctx.Err(); err != nil {
		return fmt.Errorf("catalog lifecycle coordinator stopped: %w", err)
	}
	return nil
}

func (c *Coordinator) PlanWorkspace(ctx context.Context, workspaceID string) (*store.DeletionPlan, error) {
	plan, err := c.deps.Deletions.PlanWorkspaceDeletion(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if err := c.enrich(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (c *Coordinator) PlanService(ctx context.Context, workspaceID, serviceID string) (*store.DeletionPlan, error) {
	plan, err := c.deps.Deletions.PlanServiceDeletion(ctx, workspaceID, serviceID)
	if err != nil {
		return nil, err
	}
	if err := c.enrich(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (c *Coordinator) DeleteWorkspace(ctx context.Context, workspaceID string, recursive bool) (*store.DeletionOperation, error) {
	plan, err := c.PlanWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if !recursive {
		if plan.HasDependencies() {
			return nil, &store.DeletionConflictError{Plan: *plan}
		}
		if err := c.deps.Catalog.DeleteWorkspace(ctx, workspaceID); err != nil {
			return nil, err
		}
		c.clearRuntime(*plan)
		return nil, nil
	}
	return c.begin(ctx, *plan)
}

func (c *Coordinator) DeleteService(ctx context.Context, workspaceID, serviceID string, recursive bool) (*store.DeletionOperation, error) {
	plan, err := c.PlanService(ctx, workspaceID, serviceID)
	if err != nil {
		return nil, err
	}
	if len(plan.Blockers) > 0 {
		return nil, store.ErrDatasetMapInUse
	}
	if !recursive {
		if plan.HasDependencies() {
			return nil, &store.DeletionConflictError{Plan: *plan}
		}
		if err := c.deps.Catalog.DeleteService(ctx, serviceID); err != nil {
			return nil, err
		}
		c.clearRuntime(*plan)
		return nil, nil
	}
	return c.begin(ctx, *plan)
}

func (c *Coordinator) Get(ctx context.Context, operationID string) (*store.DeletionOperation, error) {
	return c.deps.Deletions.GetCatalogDeletion(ctx, operationID)
}

func (c *Coordinator) FindServiceDeletion(ctx context.Context, serviceID string) (*store.DeletionOperation, error) {
	return c.deps.Deletions.FindCatalogDeletionByTarget(ctx, store.DeletionScopeService, serviceID)
}

func (c *Coordinator) Retry(ctx context.Context, operationID string) (*store.DeletionOperation, error) {
	op, err := c.deps.Deletions.GetCatalogDeletion(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if op.Status == store.DeletionCompleted {
		return op, nil
	}
	if err := c.deps.Deletions.UpdateCatalogDeletion(ctx, op.ID, store.DeletionRunning, op.Phase, ""); err != nil {
		return nil, err
	}
	op.Status, op.LastError = store.DeletionRunning, ""
	c.start(op)
	return op, nil
}

// Recover resumes all tombstoned operations. Targets remain filtered out of
// catalog listings until completion, including while this method is running.
func (c *Coordinator) Recover(ctx context.Context) error {
	operations, err := c.deps.Deletions.ListPendingCatalogDeletions(ctx)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		c.quiesceRuntime(operation.Plan)
		c.start(operation)
	}
	return nil
}

func (c *Coordinator) begin(ctx context.Context, plan store.DeletionPlan) (*store.DeletionOperation, error) {
	op, _, err := c.deps.Deletions.BeginCatalogDeletion(ctx, plan)
	if err != nil {
		return nil, err
	}
	// Quiesce synchronously before returning 202: callers must never receive an
	// accepted operation while the old publication can still serve requests.
	c.quiesceRuntime(op.Plan)
	if err := c.deps.Deletions.UpdateCatalogDeletion(ctx, op.ID, store.DeletionRunning, store.DeletionPhaseTombstoned, ""); err != nil {
		return nil, err
	}
	op.Status, op.Phase = store.DeletionRunning, store.DeletionPhaseTombstoned
	c.accepted.Add(ctx, 1, metric.WithAttributes(attribute.String("scope", string(plan.Scope))))
	c.start(op)
	return op, nil
}

func (c *Coordinator) start(operation *store.DeletionOperation) {
	c.mu.Lock()
	if c.running[operation.ID] {
		c.mu.Unlock()
		return
	}
	c.running[operation.ID] = true
	c.wg.Add(1)
	c.mu.Unlock()
	go func() {
		started := time.Now()
		attributes := metric.WithAttributes(attribute.String("scope", string(operation.Scope)))
		defer c.wg.Done()
		defer func() {
			c.mu.Lock()
			delete(c.running, operation.ID)
			c.mu.Unlock()
		}()
		if err := c.execute(c.ctx, operation); err != nil {
			current, getErr := c.deps.Deletions.GetCatalogDeletion(context.WithoutCancel(c.ctx), operation.ID)
			phase := operation.Phase
			if getErr == nil {
				phase = current.Phase
			}
			message := err.Error()
			if len(message) > 2048 {
				message = message[:2048]
			}
			_ = c.deps.Deletions.UpdateCatalogDeletion(context.WithoutCancel(c.ctx), operation.ID, store.DeletionFailed, phase, message)
			// Publish retryability atomically with the durable failed status. The
			// deferred delete remains as a harmless success-path cleanup.
			c.mu.Lock()
			delete(c.running, operation.ID)
			c.mu.Unlock()
			c.deps.Logger.Error("catalog deletion failed", "operation", operation.ID, "scope", operation.Scope, "target", operation.TargetID, "phase", phase, "error", err)
			c.failed.Add(context.WithoutCancel(c.ctx), 1, attributes)
			c.duration.Record(context.WithoutCancel(c.ctx), time.Since(started).Seconds(), attributes)
			return
		}
		c.completed.Add(context.WithoutCancel(c.ctx), 1, attributes)
		c.duration.Record(context.WithoutCancel(c.ctx), time.Since(started).Seconds(), attributes)
	}()
}

func (c *Coordinator) execute(ctx context.Context, operation *store.DeletionOperation) error {
	plan := operation.Plan
	c.quiesceRuntime(plan)
	resourceIDs := deletionResourceIDs(plan)
	if c.deps.TileJobs != nil {
		if err := c.deps.TileJobs.QuiesceLifecycle(ctx, plan.WorkspaceID, resourceIDsForScope(plan, resourceIDs)); err != nil {
			return fmt.Errorf("quiesce tile cache jobs: %w", err)
		}
	}
	if err := c.deps.Deletions.UpdateCatalogDeletion(ctx, operation.ID, store.DeletionRunning, store.DeletionPhaseJobsQuiesced, ""); err != nil {
		return err
	}
	serviceIDs := deletionServiceIDs(plan)
	if c.deps.Mosaic != nil {
		if err := c.deps.Mosaic.QuiesceAndDeleteLifecycle(ctx, plan.WorkspaceID, servicesForScope(plan, serviceIDs)); err != nil {
			return fmt.Errorf("remove mosaic lifecycle state: %w", err)
		}
	}
	if c.deps.TileCache != nil {
		deleted, _, err := c.deps.TileCache.PurgeLifecycle(ctx, plan.WorkspaceID, resourceIDsForScope(plan, resourceIDs))
		if err != nil {
			return fmt.Errorf("remove tile cache lifecycle state: %w", err)
		}
		c.tileBytesRemoved.Add(ctx, deleted.Bytes, metric.WithAttributes(attribute.String("scope", string(plan.Scope))))
	}
	if plan.Scope == store.DeletionScopeWorkspace {
		if err := c.deps.Assets.Stage(operation.ID, plan.WorkspaceID); err != nil {
			return fmt.Errorf("stage managed style assets: %w", err)
		}
	}
	catalogCommitted := operation.Phase == store.DeletionPhaseCatalogCommitted || operation.Phase == store.DeletionPhaseRuntimeCleared
	managedAssets, err := c.stageManagedImports(ctx, operation.ID, serviceIDs, catalogCommitted)
	if err != nil {
		return err
	}
	if err := c.deps.Deletions.UpdateCatalogDeletion(ctx, operation.ID, store.DeletionRunning, store.DeletionPhaseAuxiliaryRemoved, ""); err != nil {
		return err
	}
	if err := c.deps.Deletions.CommitCatalogDeletion(ctx, operation.ID); err != nil {
		c.restoreManagedImports(operation.ID, managedAssets)
		return fmt.Errorf("commit catalog deletion: %w", err)
	}
	if err := c.removeManagedImports(ctx, operation.ID, managedAssets); err != nil {
		return err
	}
	if plan.Scope == store.DeletionScopeWorkspace {
		if err := c.deps.Assets.RemoveStaged(operation.ID); err != nil {
			return fmt.Errorf("remove staged style assets: %w", err)
		}
	}
	c.clearRuntime(plan)
	if err := c.deps.Deletions.UpdateCatalogDeletion(ctx, operation.ID, store.DeletionRunning, store.DeletionPhaseRuntimeCleared, ""); err != nil {
		return err
	}
	if err := c.deps.Deletions.UpdateCatalogDeletion(ctx, operation.ID, store.DeletionCompleted, store.DeletionPhaseCompleted, ""); err != nil {
		return err
	}
	c.deps.Logger.Info("catalog deletion completed", "operation", operation.ID, "scope", operation.Scope, "target", operation.TargetID)
	return nil
}

func (c *Coordinator) stageManagedImports(ctx context.Context, operationID string, serviceIDs []string, catalogCommitted bool) ([]*store.ManagedAsset, error) {
	if c.deps.ManagedImports == nil {
		return nil, nil
	}
	var assets []*store.ManagedAsset
	for _, serviceID := range serviceIDs {
		asset, err := c.deps.ManagedImports.GetManagedAssetByService(ctx, serviceID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect managed import asset: %w", err)
		}
		staged := asset.Path + ".deleting-" + operationID
		if _, err = os.Stat(staged); errors.Is(err, os.ErrNotExist) {
			if err = os.Rename(asset.Path, staged); errors.Is(err, os.ErrNotExist) && catalogCommitted {
				// A prior attempt removed the file after committing the catalog but
				// failed before deleting the managed-asset metadata. The retry only
				// needs to finish that metadata cleanup.
				assets = append(assets, asset)
				continue
			} else if err != nil {
				c.restoreManagedImports(operationID, assets)
				return nil, fmt.Errorf("stage managed import asset: %w", err)
			}
		} else if err != nil {
			c.restoreManagedImports(operationID, assets)
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (c *Coordinator) restoreManagedImports(operationID string, assets []*store.ManagedAsset) {
	for _, asset := range assets {
		staged := asset.Path + ".deleting-" + operationID
		if err := os.Rename(staged, asset.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			c.deps.Logger.Error("restore staged managed import failed", "path", asset.Path, "error", err)
		}
	}
}

func (c *Coordinator) removeManagedImports(ctx context.Context, operationID string, assets []*store.ManagedAsset) error {
	for _, asset := range assets {
		staged := asset.Path + ".deleting-" + operationID
		if err := os.Remove(staged); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove managed import asset: %w", err)
		}
		if err := c.deps.ManagedImports.DeleteManagedAsset(ctx, asset.ImportID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("remove managed import metadata: %w", err)
		}
	}
	return nil
}

func (c *Coordinator) enrich(ctx context.Context, plan *store.DeletionPlan) error {
	resourceIDs := deletionResourceIDs(*plan)
	if c.deps.TileCache != nil {
		entries, bytes, jobs, err := c.deps.TileCache.LifecycleStats(ctx, plan.WorkspaceID, resourceIDsForScope(*plan, resourceIDs))
		if err != nil {
			return fmt.Errorf("inspect persistent tile cache: %w", err)
		}
		plan.Auxiliary.TileEntries, plan.Auxiliary.TileBytes, plan.Auxiliary.TileJobs = entries, bytes, jobs
	}
	if c.deps.Mosaic != nil {
		services, granules, jobs, err := c.deps.Mosaic.LifecycleStats(ctx, plan.WorkspaceID, servicesForScope(*plan, deletionServiceIDs(*plan)))
		if err != nil {
			return fmt.Errorf("inspect mosaic catalog: %w", err)
		}
		plan.Auxiliary.MosaicServices, plan.Auxiliary.MosaicGranules, plan.Auxiliary.MosaicJobs = services, granules, jobs
	}
	if c.deps.ManagedImports != nil {
		for _, serviceID := range deletionServiceIDs(*plan) {
			if _, err := c.deps.ManagedImports.GetManagedAssetByService(ctx, serviceID); err == nil {
				plan.Auxiliary.ManagedAssets++
			} else if !errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("inspect managed import asset: %w", err)
			}
		}
	}
	if plan.Scope == store.DeletionScopeWorkspace {
		count, err := c.deps.Assets.Count(plan.WorkspaceID)
		if err != nil {
			return err
		}
		plan.Auxiliary.ManagedAssets += count
	}
	return nil
}

func (c *Coordinator) quiesceRuntime(plan store.DeletionPlan) {
	if plan.Scope == store.DeletionScopeWorkspace {
		c.deps.Registry.Remove(plan.WorkspaceID)
		return
	}
	if err := c.deps.Registry.QuiesceService(plan.WorkspaceID, plan.Target.ID); err != nil &&
		!errors.Is(err, workspace.ErrWorkspaceNotFound) && !errors.Is(err, workspace.ErrServiceNotFound) {
		c.deps.Logger.Warn("failed to quiesce service runtime", "workspace", plan.WorkspaceID, "service", plan.Target.ID, "error", err)
	}
}

func (c *Coordinator) clearRuntime(plan store.DeletionPlan) {
	c.quiesceRuntime(plan)
	if c.deps.WFSState != nil {
		c.deps.WFSState.ClearWorkspace(plan.WorkspaceID)
	}
	if c.deps.Cache != nil {
		c.deps.Cache.InvalidateWorkspace(plan.WorkspaceID)
	}
	if c.deps.Enforcer != nil {
		if plan.Scope == store.DeletionScopeWorkspace {
			if err := c.deps.Enforcer.RemoveAllWorkspacePolicies(plan.WorkspaceID); err != nil {
				c.deps.Logger.Warn("remove workspace RBAC policies", "workspace", plan.WorkspaceID, "error", err)
			}
		} else if err := c.deps.Enforcer.RemoveResourcePolicies(plan.WorkspaceID, deletionResourceNames(plan)); err != nil {
			c.deps.Logger.Warn("remove resource RBAC policies", "workspace", plan.WorkspaceID, "error", err)
		}
	}
}

func deletionResourceIDs(plan store.DeletionPlan) []string {
	refs := append(append(append([]store.DeletionRef{}, plan.Layers...), plan.Coverages...), plan.LayerGroups...)
	result := make([]string, 0, len(refs))
	for _, ref := range refs {
		result = append(result, ref.ID)
	}
	return result
}

func deletionResourceNames(plan store.DeletionPlan) []string {
	refs := append(append(append([]store.DeletionRef{}, plan.Layers...), plan.Coverages...), plan.LayerGroups...)
	result := make([]string, 0, len(refs))
	for _, ref := range refs {
		result = append(result, ref.Name)
	}
	return result
}

func deletionServiceIDs(plan store.DeletionPlan) []string {
	if plan.Scope == store.DeletionScopeService {
		return []string{plan.Target.ID}
	}
	result := make([]string, 0, len(plan.Services))
	for _, ref := range plan.Services {
		result = append(result, ref.ID)
	}
	return result
}

func resourceIDsForScope(plan store.DeletionPlan, ids []string) []string {
	if plan.Scope == store.DeletionScopeWorkspace {
		return nil
	}
	return ids
}

func servicesForScope(plan store.DeletionPlan, ids []string) []string {
	if plan.Scope == store.DeletionScopeWorkspace {
		return nil
	}
	return ids
}
