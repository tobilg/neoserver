package wfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

type cacheTransactionSource struct {
	datasource.DataSource
	committed int
}

func (*cacheTransactionSource) Health(context.Context) error { return nil }
func (*cacheTransactionSource) Close() error                 { return nil }
func (*cacheTransactionSource) Type() store.ServiceType      { return store.ServiceTypeDuckDB }
func (s *cacheTransactionSource) AtomicWrite(ctx context.Context, fn func(datasource.FeatureWriter) error) error {
	if err := fn(s); err != nil {
		return err
	}
	s.committed++
	return nil
}
func (*cacheTransactionSource) Insert(context.Context, string, []datasource.FeatureData) ([]string, error) {
	return []string{"1"}, nil
}
func (*cacheTransactionSource) Update(context.Context, string, map[string]interface{}, string, []interface{}) (int, error) {
	return 1, nil
}
func (*cacheTransactionSource) Delete(context.Context, string, string, []interface{}) (int, error) {
	return 1, nil
}
func (*cacheTransactionSource) UpdateReturning(context.Context, string, map[string]interface{}, string, []interface{}) ([]string, error) {
	return []string{"1"}, nil
}
func (*cacheTransactionSource) DeleteReturning(context.Context, string, string, []interface{}) ([]string, error) {
	return []string{"1"}, nil
}
func (*cacheTransactionSource) Replace(context.Context, string, datasource.FeatureData, string, []interface{}) ([]string, error) {
	return []string{"1"}, nil
}
func (*cacheTransactionSource) GetLayerInfo(context.Context, string) (*datasource.LayerInfo, error) {
	return &datasource.LayerInfo{Name: "roads", GeometryColumn: "geom", GeometryType: "POINT", SRID: 4326, IDColumn: "id"}, nil
}
func (s *cacheTransactionSource) Query(context.Context, string, datasource.QueryParams) ([]json.RawMessage, error) {
	return []json.RawMessage{json.RawMessage(fmt.Sprintf(`{"type":"Feature","geometry":{"type":"Point","coordinates":[0,0]},"properties":{"revision":%d}}`, s.committed))}, nil
}
func (*cacheTransactionSource) QueryWKB(context.Context, string, datasource.QueryParams) ([]datasource.RenderFeature, error) {
	return nil, nil
}

func TestWFSTransactionsInvalidateDurableVectorMapAndGroupTilesAcrossReload(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	source := &cacheTransactionSource{}
	registry := workspace.NewRegistry(catalog, func(*store.Service) (datasource.DataSource, error) { return source, nil })
	defer registry.Close()
	ws, err := registry.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "writes"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := registry.CreateService(ctx, store.CreateServiceInput{WorkspaceID: ws.ID, Name: "source", Type: store.ServiceTypeDuckDB, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.CreateLayer(ctx, ws.ID, svc.ID, store.CreateLayerInput{ServiceID: svc.ID, PublicID: "roads", SourceLayer: "roads", Enabled: true, Public: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.CreateLayerGroup(ctx, ws.ID, store.CreateLayerGroupInput{WorkspaceID: ws.ID, PublicID: "base", Enabled: true, Public: true, Members: []store.LayerGroupMember{{Resource: "roads"}}}, 8)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := tilecache.NewFilesystemStore(filepath.Join(root, "tiles"))
	if err != nil {
		t.Fatal(err)
	}
	persistent, err := tilecache.NewManager(ctx, tilecache.Config{DatabasePath: filepath.Join(root, "cache.db"), EncryptionKey: "abc123", MaxBytes: 8 << 20, MaintenanceInterval: time.Hour}, backend)
	if err != nil {
		t.Fatal(err)
	}
	defer persistent.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := conf.Config{Tiles: conf.Tiles{Enabled: true, TileSize: 4096, MinZoom: 0, MaxZoom: 22, MaxFeatures: 1000, MaxVertices: 10000, MaxTileBytes: 1 << 20}}
	engine := tiles.NewEngine(cfg, logger, nil, persistent)
	h := &workspaceHandler{registry: registry, logger: logger}
	for _, operation := range []string{`<wfs:Insert><roads/></wfs:Insert>`, `<wfs:Update typeName="roads"/>`, `<wfs:Delete typeName="roads"><fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0"><fes:ResourceId rid="roads.1"/></fes:Filter></wfs:Delete>`} {
		current, release, ok := registry.AcquireByID(ws.ID)
		if !ok {
			t.Fatal("missing workspace")
		}
		var requests []tiles.EngineRequest
		for _, spec := range []struct{ resource, kind, format string }{{"roads", "vector", tiles.MediaTypeMVT}, {"roads", "map", tiles.MediaTypePNG}, {"base", "map", tiles.MediaTypePNG}} {
			request := tiles.EngineRequest{Workspace: current, Resource: current.GetResource(spec.resource), TileType: spec.kind, MatrixSet: tiles.TMSWebMercatorQuad, Format: spec.format, UseCache: true}
			id, err := engine.ResolveIdentity(request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := persistent.Put(ctx, id, []byte("synthetic-pre-edit-tile"), tilecache.Policy{}); err != nil {
				t.Fatal(err)
			}
			warm, err := engine.Fetch(ctx, request)
			if err != nil || warm.CacheTier != "persistent" {
				t.Fatalf("warm=%+v err=%v", warm, err)
			}
			requests = append(requests, request)
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/wfs", strings.NewReader(`<wfs:Transaction xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0">`+operation+`</wfs:Transaction>`))
		h.handleTransaction(response, request, current)
		if response.Code != 200 || strings.Contains(response.Body.String(), "ExceptionReport") {
			t.Fatalf("transaction failed: %s", response.Body.String())
		}
		for _, request := range requests {
			fresh, err := engine.Fetch(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.CacheStatus == "HIT" || string(fresh.Data) == "synthetic-pre-edit-tile" {
				t.Fatalf("stale %s %s: %+v", request.Resource.PublicID(), request.TileType, fresh)
			}
		}
		release()
		if err := registry.Load(ctx); err != nil {
			t.Fatal(err)
		}
		loaded, done, _ := registry.AcquireByID(ws.ID)
		for _, request := range requests {
			request.Workspace = loaded
			request.Resource = loaded.GetResource(request.Resource.PublicID())
			fresh, err := engine.Fetch(ctx, request)
			if err != nil || string(fresh.Data) == "synthetic-pre-edit-tile" {
				t.Fatalf("stale after reload: %+v %v", fresh, err)
			}
		}
		done()
	}
	if source.committed != 3 {
		t.Fatalf("commits=%d", source.committed)
	}
}
