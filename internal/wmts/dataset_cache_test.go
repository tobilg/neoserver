package wmts

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestDatasetMapReusesWMTSGroupCache(t *testing.T) {
	h, ws := testHandler()
	memory, err := cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	h.engine = tiles.NewEngine(h.cfg, h.logger, memory, nil)
	ws.Groups = map[string]*workspace.LayerGroup{"map": {ID: "map-id", PublicID: "map", Enabled: true, Public: true, Members: []store.LayerGroupMember{{Resource: "elevation"}}}}
	ws.Settings.OGCTilesAPI.Settings.DatasetMapLayerGroupID = "map-id"
	ws.Settings.OGCTilesAPI.Settings.CacheEnabled = true
	getWMTS := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.kvp(rec, wmtsRequest(ws, "http://example.test/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0&LAYER=map&STYLE=default&FORMAT=image%2Fpng&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=0&TILEROW=0&TILECOL=0"))
		return rec
	}
	warmed := getWMTS()
	if warmed.Code != 200 {
		t.Fatalf("WMTS: %d %s", warmed.Code, warmed.Body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for getWMTS().Header().Get("X-Cache") != "HIT" {
		if time.Now().After(deadline) {
			t.Fatal("WMTS cache not admitted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	router := chi.NewRouter()
	tiles.RegisterWorkspaceRoutes(router, tiles.WorkspaceDependencies{Config: h.cfg, Logger: h.logger, Engine: h.engine, Cache: memory})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, wmtsRequest(ws, "/map/tiles/WebMercatorQuad/0/0/0"))
	if rec.Code != 200 || rec.Header().Get("X-Cache") != "HIT" || !bytes.Equal(rec.Body.Bytes(), warmed.Body.Bytes()) {
		t.Fatalf("dataset did not reuse WMTS group tile: %d %s", rec.Code, rec.Header())
	}
}
