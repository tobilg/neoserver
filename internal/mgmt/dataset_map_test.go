package mgmt

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestWorkspaceMapSettingsContract(t *testing.T) {
	ctx := context.Background()
	config := store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"}
	catalog, _, err := store.Init(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { catalog.Close() }()
	ws, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "source", Type: store.ServiceTypePostGIS, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.CreateLayer(ctx, store.CreateLayerInput{ServiceID: service.ID, SourceLayer: "roads", PublicID: "roads", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	group, err := catalog.CreateLayerGroup(ctx, store.CreateLayerGroupInput{WorkspaceID: ws.ID, PublicID: "map", Members: []store.LayerGroupMember{{Resource: "roads"}}})
	if err != nil {
		t.Fatal(err)
	}
	registry := workspace.NewRegistry(catalog, nil)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}
	h := &handler{store: catalog, registry: registry, logger: slog.Default()}
	router := chi.NewRouter()
	router.Get("/{workspace}", h.getOGCTilesAPISettings)
	router.Put("/{workspace}", h.updateOGCTilesAPISettings)
	router.Post("/{workspace}/groups", h.createLayerGroup)
	router.Put("/{workspace}/groups/{group}", h.updateLayerGroup)
	created := httptest.NewRecorder()
	router.ServeHTTP(created, httptest.NewRequest("POST", "/demo/groups", strings.NewReader(`{"public_id":"second","members":[{"resource":"roads"}]}`)))
	var saved map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &saved); err != nil || created.Code != 201 || saved["id"] == nil || saved["workspace_id"] != ws.ID || saved["created_at"] == nil {
		t.Fatalf("create contract: %d %s %v", created.Code, created.Body, err)
	}
	updated := httptest.NewRecorder()
	router.ServeHTTP(updated, httptest.NewRequest("PUT", "/demo/groups/"+saved["id"].(string), strings.NewReader(`{"title":"Second map"}`)))
	if err := json.Unmarshal(updated.Body.Bytes(), &saved); err != nil || updated.Code != 200 || saved["title"] != "Second map" {
		t.Fatalf("update contract: %d %s %v", updated.Code, updated.Body, err)
	}

	update := func(selection *string, want int) {
		t.Helper()
		body := map[string]any{"enabled": true, "public": true, "settings": map[string]any{"vector_tiles": map[string]any{"enabled": false}, "map_tiles": map[string]any{"enabled": true, "formats": []string{"image/png"}}, "tile_matrix_sets": []string{"WebMercatorQuad"}, "cache_enabled": true}}
		if selection != nil {
			body["settings"].(map[string]any)["dataset_map_layer_group_id"] = *selection
		}
		data, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("PUT", "/demo", strings.NewReader(string(data))))
		if rec.Code != want {
			t.Fatalf("PUT status %d: %s", rec.Code, rec.Body)
		}
	}
	assertSelection := func(want string) {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/demo", nil))
		var response OGCTilesAPISettingsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || rec.Code != 200 || response.Settings.DatasetMapLayerGroupID == nil || *response.Settings.DatasetMapLayerGroupID != want {
			t.Fatalf("GET: %d %s %v", rec.Code, rec.Body, err)
		}
		runtime, ok := registry.Get("demo")
		if !ok || runtime.Settings.OGCTilesAPI.Settings.DatasetMapLayerGroupID != want {
			t.Fatal("runtime selection differs")
		}
	}
	assertSelection("")
	update(&group.ID, 200)
	assertSelection(group.ID)
	update(nil, 200)
	assertSelection(group.ID)
	invalid := "missing"
	update(&invalid, 400)
	assertSelection(group.ID)
	// The selected group is disabled; configuring it before publication is allowed.
	if err := catalog.Close(); err != nil {
		t.Fatal(err)
	}
	catalog, err = store.Open(config)
	if err != nil {
		t.Fatal(err)
	}
	registry = workspace.NewRegistry(catalog, nil)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}
	h.store = catalog
	h.registry = registry
	assertSelection(group.ID)
	empty := ""
	update(&empty, 200)
	assertSelection("")
}
