package mgmt

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/cataloglifecycle"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestWorkspaceDeletionContract(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	workspaceRecord, _ := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "demo"})
	service, _ := catalog.CreateService(ctx, store.CreateServiceInput{WorkspaceID: workspaceRecord.ID, Name: "source", Type: store.ServiceTypePostGIS, Enabled: true})
	_, _ = catalog.CreateLayer(ctx, store.CreateLayerInput{ServiceID: service.ID, SourceLayer: "roads", PublicID: "roads", Enabled: true})
	registry := workspace.NewRegistry(catalog, nil)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}
	cacheManager, _ := cache.NewManager(cache.DefaultConfig())
	defer cacheManager.Close()
	enforcer, _ := rbac.NewEnforcerWithDefaults(rbac.NewMemoryAdapter())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lifecycle, err := cataloglifecycle.New(ctx, cataloglifecycle.Dependencies{Catalog: catalog, Deletions: catalog,
		Registry: registry, Cache: cacheManager, Enforcer: enforcer, StyleAssetRoot: filepath.Join(root, "assets"), Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close()
	h := &handler{cfg: testConfig(), store: catalog, registry: registry, cache: cacheManager, enforcer: enforcer, logger: logger, lifecycle: lifecycle}

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/demo", nil)
	request = request.WithContext(withChiContext(request.Context(), map[string]string{"workspace": "demo"}))
	response := httptest.NewRecorder()
	h.deleteWorkspace(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("non-recursive status = %d, body %s", response.Code, response.Body.String())
	}
	var conflict deletionConflictResponse
	if err := json.NewDecoder(response.Body).Decode(&conflict); err != nil || !conflict.RecurseRequired || len(conflict.Plan.Layers) != 1 {
		t.Fatalf("conflict = %+v, %v", conflict, err)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/demo?recurse=true", nil)
	request = request.WithContext(withChiContext(request.Context(), map[string]string{"workspace": "demo"}))
	response = httptest.NewRecorder()
	h.deleteWorkspace(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("recursive status = %d, body %s", response.Code, response.Body.String())
	}
	if location := response.Header().Get("Location"); !strings.Contains(location, "/api/v1/deletions/") {
		t.Fatalf("Location = %q", location)
	}
	var operation store.DeletionOperation
	if err := json.NewDecoder(response.Body).Decode(&operation); err != nil || operation.ID == "" {
		t.Fatalf("operation = %+v, %v", operation, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := lifecycle.Get(ctx, operation.ID)
		if err == nil && current.Status == store.DeletionCompleted {
			for _, principalWorkspace := range []string{workspaceRecord.ID, "unrelated"} {
				for _, retry := range []bool{false, true} {
					if retry && principalWorkspace == workspaceRecord.ID {
						continue
					}
					r := httptest.NewRequest("GET", "/api/v1/deletions/"+operation.ID, nil)
					ctx := withChiContext(r.Context(), map[string]string{"operation": operation.ID})
					ctx = identity.WithIdentity(ctx, &identity.Identity{Roles: map[string]string{principalWorkspace: "admin"}})
					w := httptest.NewRecorder()
					if retry {
						h.retryDeletionOperation(w, r.WithContext(ctx))
					} else {
						h.getDeletionOperation(w, r.WithContext(ctx))
					}
					want := 200
					if principalWorkspace != workspaceRecord.ID {
						want = 403
					}
					if w.Code != want {
						t.Fatalf("deletion authorization: %d %s", w.Code, w.Body)
					}
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("recursive workspace deletion did not complete")
}
