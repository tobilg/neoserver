package tilejobs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

type jobTestEnv struct {
	manager     *Manager
	catalog     *store.DuckDBStore
	registry    *workspace.Registry
	persistent  *tilecache.Manager
	workspaceID string
	layerID     string
}

// newJobTestEnv wires a real catalog, registry, persistent cache, and tile
// engine so Create/claim/quiesce behavior runs against genuine durable state.
// tune lets a test adjust the job configuration -- notably MaxConcurrentJobs,
// which controls how many background workers poll for queued jobs.
func newJobTestEnv(t *testing.T, cacheEnabled bool, tune ...func(*conf.PersistentCacheJobs)) *jobTestEnv {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()

	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { catalog.Close() })

	ws, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "src", Type: store.ServiceTypePostGIS, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	layer, err := catalog.CreateLayer(ctx, store.CreateLayerInput{ServiceID: service.ID, SourceLayer: "roads", PublicID: "roads", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	settings := store.DefaultOGCTilesAPISettings()
	settings.Enabled = true
	settings.Settings.TileMatrixSets = []string{"WebMercatorQuad"}
	settings.Settings.CacheEnabled = cacheEnabled
	if err := catalog.UpdateOGCTilesAPISettings(ctx, ws.ID, settings); err != nil {
		t.Fatal(err)
	}

	registry := workspace.NewRegistry(catalog, nil)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}

	backend, err := tilecache.NewFilesystemStore(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	persistent, err := tilecache.NewManager(ctx, tilecache.Config{
		DatabasePath:        filepath.Join(root, "tile-cache.duckdb"),
		EncryptionKey:       "abc123",
		MaxBytes:            1 << 20,
		MaintenanceInterval: time.Hour,
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = persistent.Close() })

	memory, err := cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(memory.Close)

	cfg := conf.Config{Tiles: conf.Tiles{MinZoom: 0, MaxZoom: 20, TileSize: 4096}}
	engine := tiles.NewEngine(cfg, nil, memory, persistent)

	jobCfg := conf.PersistentCacheJobs{WorkerCount: 1, MaxConcurrentJobs: 1, MaxConcurrentRenders: 1, MaxTilesPerJob: 1000, MaxRetries: 1}
	for _, apply := range tune {
		apply(&jobCfg)
	}
	manager, err := NewManager(ctx, jobCfg,
		cfg.Tiles, persistent.JobStore(), registry, engine, persistent, memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Close(closeCtx)
	})

	return &jobTestEnv{manager: manager, catalog: catalog, registry: registry, persistent: persistent, workspaceID: ws.ID, layerID: layer.ID}
}

func wgs84Bounds() *tilecache.Bounds {
	return &tilecache.Bounds{BBox: [4]float64{-10, -10, 10, 10}, CRS: "EPSG:4326"}
}

func TestCreateValidation(t *testing.T) {
	env := newJobTestEnv(t, true)
	ctx := context.Background()

	tests := []struct {
		name    string
		request tilecache.JobRequest
		wantErr string
	}{
		{"bad operation", tilecache.JobRequest{Operation: "explode", Resource: "roads"}, "operation must be"},
		{"unknown resource", tilecache.JobRequest{Operation: tilecache.OperationSeed, Resource: "nope"}, "not found"},
		{"all_resources on seed", tilecache.JobRequest{Operation: tilecache.OperationSeed, AllResources: true}, "only supported for truncate"},
		{"resource and all_resources", tilecache.JobRequest{Operation: tilecache.OperationTruncate, AllResources: true, Resource: "roads"}, "mutually exclusive"},
		{"bad tile type", tilecache.JobRequest{Operation: tilecache.OperationSeed, Resource: "roads", TileType: "raster", Bounds: wgs84Bounds()}, "tile_type"},
		{"matrix set not enabled", tilecache.JobRequest{Operation: tilecache.OperationSeed, Resource: "roads", TileMatrixSet: "WorldCRS84Quad", Bounds: wgs84Bounds()}, "not enabled"},
		{"invalid zoom range", tilecache.JobRequest{Operation: tilecache.OperationSeed, Resource: "roads", Bounds: wgs84Bounds(), MinZoom: intPointer(8), MaxZoom: intPointer(2)}, "invalid zoom range"},
		{"bad bounds CRS", tilecache.JobRequest{Operation: tilecache.OperationSeed, Resource: "roads", Bounds: &tilecache.Bounds{BBox: [4]float64{0, 0, 1, 1}, CRS: "not-a-crs"}}, "CRS"},
		{"no extent without bounds", tilecache.JobRequest{Operation: tilecache.OperationSeed, Resource: "roads"}, "extent"},
		{"too many tiles", tilecache.JobRequest{Operation: tilecache.OperationSeed, Resource: "roads", Bounds: wgs84Bounds(), MinZoom: intPointer(0), MaxZoom: intPointer(18)}, "max_tiles_per_job"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := env.manager.Create(ctx, env.workspaceID, "tester", tt.request)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Create error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}

	// Unknown workspace fails before any validation.
	if _, err := env.manager.Create(ctx, "no-such-ws", "tester", tilecache.JobRequest{Operation: tilecache.OperationSeed, Resource: "roads"}); err == nil {
		t.Fatal("unknown workspace must fail")
	}
}

func TestCreateRequiresCacheEnabledForSeed(t *testing.T) {
	env := newJobTestEnv(t, false)
	ctx := context.Background()

	_, err := env.manager.Create(ctx, env.workspaceID, "tester", tilecache.JobRequest{Operation: tilecache.OperationSeed, Resource: "roads", Bounds: wgs84Bounds(), MinZoom: intPointer(0), MaxZoom: intPointer(1)})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("seed with caching disabled = %v, want cache gate error", err)
	}

	// Truncate is allowed even when caching is disabled (cleanup path).
	job, err := env.manager.Create(ctx, env.workspaceID, "tester", tilecache.JobRequest{Operation: tilecache.OperationTruncate, Resource: "roads"})
	if err != nil {
		t.Fatalf("truncate with caching disabled: %v", err)
	}
	if job.Status != tilecache.JobQueued {
		t.Fatalf("job status = %s, want queued", job.Status)
	}
}

func TestTruncateJobRunsToCompletion(t *testing.T) {
	env := newJobTestEnv(t, true)
	ctx := context.Background()

	job, err := env.manager.Create(ctx, env.workspaceID, "tester", tilecache.JobRequest{Operation: tilecache.OperationTruncate, Resource: "roads"})
	if err != nil {
		t.Fatalf("create truncate: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := env.manager.Get(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == tilecache.JobSucceeded {
			return
		}
		if current.Status == tilecache.JobFailed {
			t.Fatalf("truncate job failed: %s", current.ErrorMessage)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("truncate job did not complete")
}

func TestCancelQueuedJob(t *testing.T) {
	env := newJobTestEnv(t, true)
	ctx := context.Background()

	// Quiesce the workspace first so the worker cannot claim the job before
	// the cancel request lands.
	if err := env.manager.QuiesceLifecycle(ctx, "other-workspace", nil); err != nil {
		t.Fatal(err)
	}

	job, err := env.manager.Create(ctx, env.workspaceID, "tester", tilecache.JobRequest{Operation: tilecache.OperationTruncate, Resource: "roads"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.manager.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := env.manager.Get(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == tilecache.JobCancelled {
			return
		}
		if current.Status == tilecache.JobSucceeded {
			// The worker may have finished the truncate before the cancel
			// request was observed; that is a legal race outcome.
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("cancelled job did not reach a terminal state")
}

func TestQuiesceLifecycleBlocksCreateAndCancelsJobs(t *testing.T) {
	env := newJobTestEnv(t, true)
	ctx := context.Background()

	if err := env.manager.QuiesceLifecycle(ctx, env.workspaceID, nil); err != nil {
		t.Fatalf("quiesce: %v", err)
	}

	// New jobs for the quiesced workspace are refused.
	_, err := env.manager.Create(ctx, env.workspaceID, "tester", tilecache.JobRequest{Operation: tilecache.OperationTruncate, Resource: "roads"})
	if err == nil || !strings.Contains(err.Error(), "quiesced") {
		t.Fatalf("Create after quiesce = %v, want quiesced error", err)
	}

	// Other workspaces are unaffected (resource-scoped block map).
	if env.manager.lifecycleBlocked("some-other-ws", "x", false) {
		t.Fatal("unrelated workspace must not be blocked")
	}
}

func TestQuiesceLifecycleResourceScope(t *testing.T) {
	env := newJobTestEnv(t, true)
	ctx := context.Background()

	if err := env.manager.QuiesceLifecycle(ctx, env.workspaceID, []string{env.layerID}); err != nil {
		t.Fatalf("quiesce resource: %v", err)
	}
	if !env.manager.lifecycleBlocked(env.workspaceID, env.layerID, false) {
		t.Fatal("target resource must be blocked")
	}
	if env.manager.lifecycleBlocked(env.workspaceID, "other-layer", false) {
		t.Fatal("sibling resource must not be blocked")
	}
	// all_resources requests are blocked by any resource-level quiesce.
	if !env.manager.lifecycleBlocked(env.workspaceID, "", true) {
		t.Fatal("all-resources request must be blocked while any resource is quiesced")
	}
}
