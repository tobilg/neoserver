package tiles

import (
	"bytes"
	"encoding/json"
	"image"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func datasetWorkspace() (*workspace.Workspace, *fakeDataSource) {
	ws := newTestWorkspace()
	ds := &fakeDataSource{wkbResult: []datasource.RenderFeature{{Geometry: pointWKB(0, 0)}}}
	addTestLayer(ws, "roads", ds)
	ws.Groups = map[string]*workspace.LayerGroup{
		"base": {ID: "base-id", PublicID: "base", Enabled: true, Public: true, Members: []store.LayerGroupMember{{Resource: "roads"}}},
		"map":  {ID: "map-id", PublicID: "map", Title: "Curated map", Enabled: true, Public: true, Members: []store.LayerGroupMember{{Resource: "base"}, {Resource: "roads"}}},
	}
	ws.Settings.OGCTilesAPI.Settings.DatasetMapLayerGroupID = "map-id"
	return ws, ds
}

func TestDatasetMapDiscoveryAndAvailability(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*workspace.Workspace)
		available bool
	}{
		{"configured", func(*workspace.Workspace) {}, true},
		{"unselected", func(ws *workspace.Workspace) { ws.Settings.OGCTilesAPI.Settings.DatasetMapLayerGroupID = "" }, false},
		{"missing", func(ws *workspace.Workspace) { ws.Settings.OGCTilesAPI.Settings.DatasetMapLayerGroupID = "unknown" }, false},
		{"disabled group", func(ws *workspace.Workspace) { ws.Groups["map"].Enabled = false }, false},
		{"disabled member", func(ws *workspace.Workspace) { layer, _ := ws.GetLayer("roads"); layer.Enabled = false }, false},
		{"restricted member", func(ws *workspace.Workspace) {
			layer, _ := ws.GetLayer("roads")
			layer.AllowedRoles = []string{"admin"}
		}, false},
		{"restricted nested group", func(ws *workspace.Workspace) {
			ws.Groups["base"].Public = false
			ws.Groups["base"].AllowedRoles = []string{"admin"}
		}, false},
		{"map disabled", func(ws *workspace.Workspace) { ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled = false }, false},
		{"no grids", func(ws *workspace.Workspace) { ws.Settings.OGCTilesAPI.Settings.TileMatrixSets = []string{"missing"} }, false},
		{"no formats", func(ws *workspace.Workspace) { ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, _ := datasetWorkspace()
			tc.mutate(ws)
			h := newTestHandler(newTestConfig())
			for name, handler := range map[string]http.HandlerFunc{"landing": h.landing, "conformance": h.conformance, "api": h.api} {
				rec := httptest.NewRecorder()
				handler(rec, tileRequest(ws, "/", nil))
				if rec.Code != 200 {
					t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
				}
				marker := "/map/tiles"
				if name == "conformance" {
					marker = "/conf/dataset-tilesets"
				}
				if name == "api" {
					marker = "datasetMap.getTile"
				}
				if strings.Contains(rec.Body.String(), marker) != tc.available {
					t.Fatalf("%s availability: %s", name, rec.Body)
				}
			}
			rec := httptest.NewRecorder()
			h.collectionMapTilesets(rec, tileRequest(ws, "/map/tiles", nil))
			if (rec.Code == 200) != tc.available {
				t.Fatalf("list availability: %d %s", rec.Code, rec.Body)
			}
			if !tc.available {
				return
			}
			var list TileSetList
			if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
				t.Fatal(err)
			}
			if len(list.TileSets) != 2 {
				t.Fatalf("tilesets: %+v", list)
			}
			for _, set := range list.TileSets {
				if set.Title != "Curated map" || set.DataType != DataTypeMap || set.CRS == "" || set.TileMatrixSetURI == "" {
					t.Fatalf("metadata: %+v", set)
				}
				for _, link := range set.Links {
					if strings.Contains(link.Href, "/collections/") {
						t.Fatal("dataset link points to collection")
					}
					if link.Rel == "item" && !link.Templated {
						t.Fatal("unmarked template")
					}
				}
			}
			data, _ := json.Marshal(h.buildWorkspaceOpenAPI(ws, ""))
			doc, err := openapi3.NewLoader().LoadFromData(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := doc.Validate(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDatasetMapAdvertisedLimits(t *testing.T) {
	ws, _ := datasetWorkspace()
	cfg := newTestConfig()
	cfg.Tiles.MinZoom, cfg.Tiles.MaxZoom = 1, 2
	h := newTestHandler(cfg)
	listResponse := httptest.NewRecorder()
	h.collectionMapTilesets(listResponse, tileRequest(ws, "/map/tiles", nil))
	var list TileSetList
	if err := json.Unmarshal(listResponse.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, set := range list.TileSets {
		if len(set.TileMatrixSetLimits) != 2 || set.TileMatrixSetLimits[0].TileMatrixID != "1" || set.TileMatrixSetLimits[1].TileMatrixID != "2" {
			t.Fatalf("limits ignore configured zoom range: %+v", set)
		}
		metadata := httptest.NewRecorder()
		h.collectionMapTileset(metadata, tileRequest(ws, "/map/tiles/"+set.TileMatrixSetID, map[string]string{"tileMatrixSetId": set.TileMatrixSetID}))
		var detail TileSetMetadata
		if err := json.Unmarshal(metadata.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(detail.TileMatrixSetLimits, set.TileMatrixSetLimits) {
			t.Fatal("list and detail disagree on limits")
		}
		for _, limit := range set.TileMatrixSetLimits {
			for _, tc := range []struct{ row, col, status int }{
				{limit.MinTileRow, limit.MinTileCol, 200},
				{limit.MaxTileRow, limit.MaxTileCol, 200},
				{limit.MaxTileRow + 1, limit.MinTileCol, 400},
				{limit.MinTileRow, limit.MaxTileCol + 1, 400},
			} {
				rec := httptest.NewRecorder()
				h.getMapTile(rec, tileRequest(ws, "/map/tiles", vectorTileParams("", set.TileMatrixSetID, limit.TileMatrixID, strconv.Itoa(tc.row), strconv.Itoa(tc.col))))
				if rec.Code != tc.status {
					t.Fatalf("%s matrix %s row %d col %d: got %d, want %d", set.TileMatrixSetID, limit.TileMatrixID, tc.row, tc.col, rec.Code, tc.status)
				}
			}
		}
	}
}

func TestDatasetMapFormatsAndCacheAccess(t *testing.T) {
	mgr, err := cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	ws, ds := datasetWorkspace()
	ws.Settings.OGCTilesAPI.Settings.CacheEnabled = true
	h := newTestHandler(newTestConfig())
	h.cache = mgr
	request := func(collection, format string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.getMapTile(rec, tileRequest(ws, "/map/tiles?f="+format, vectorTileParams(collection, TMSWebMercatorQuad, "0", "0", "0")))
		return rec
	}
	for _, format := range []string{"png", "jpeg", "webp"} {
		dataset := request("", format)
		collection := request("map", format)
		if dataset.Code != 200 || collection.Code != 200 {
			t.Fatalf("%s: %d %s %d %s", format, dataset.Code, dataset.Body, collection.Code, collection.Body)
		}
		if !bytes.Equal(dataset.Body.Bytes(), collection.Body.Bytes()) {
			t.Fatal("dataset differs from group tile")
		}
		img, _, err := image.Decode(bytes.NewReader(dataset.Body.Bytes()))
		if err != nil || img.Bounds().Dx() != 256 {
			t.Fatalf("decode %s: %v", format, err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for request("map", "png").Header().Get("X-Cache") != "HIT" {
		if time.Now().After(deadline) {
			t.Fatal("cache not populated")
		}
		time.Sleep(10 * time.Millisecond)
	}
	before := atomic.LoadInt64(&ds.queryCalls)
	if got := request("", "png"); got.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("dataset did not reuse collection cache: %s", got.Header())
	}
	if atomic.LoadInt64(&ds.queryCalls) != before {
		t.Fatal("cached dataset rendered again")
	}
	layer, _ := ws.GetLayer("roads")
	layer.AllowedRoles = []string{"admin"}
	if got := request("", "png"); got.Code != 404 {
		t.Fatalf("restricted cached tile leaked: %d", got.Code)
	}
	rec := httptest.NewRecorder()
	r := tileRequest(ws, "/map/tiles", vectorTileParams("", TMSWebMercatorQuad, "0", "0", "0"))
	r = r.WithContext(withIdentity(r.Context(), "admin", map[string]string{ws.ID: "admin"}))
	h.getMapTile(rec, r)
	if rec.Code != 200 {
		t.Fatalf("authorized request: %d %s", rec.Code, rec.Body)
	}
	layer.AllowedRoles = nil
	layer.TileCacheGeneration++
	if got := request("", "png"); got.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("child mutation did not invalidate: %s", got.Header())
	}
	ws.Groups["map"].PublicID = "renamed"
	ws.Groups["renamed"] = ws.Groups["map"]
	delete(ws.Groups, "map")
	if got := request("", "png"); got.Code != 200 {
		t.Fatalf("rename broke UUID selection: %d %s", got.Code, got.Body)
	}
}

func TestDatasetMapRoutesAndValidation(t *testing.T) {
	ws, _ := datasetWorkspace()
	cfg := newTestConfig()
	cfg.Server.UrlBase = "http://example.test"
	cfg.Server.BasePath = "/prefix"
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(withWorkspace(r.Context(), ws)))
		})
	})
	RegisterWorkspaceRoutes(router, WorkspaceDependencies{Config: cfg, Logger: newTestLogger()})
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/map/tiles", 200}, {"/map/tiles/WorldCRS84Quad", 200}, {"/map/tiles/WebMercatorQuad/2/1/2", 200},
		{"/map/tiles?collections=roads", 400}, {"/map/tiles/WebMercatorQuad/0/0/0?collections=roads", 400},
		{"/map/tiles/unknown", 404}, {"/map/tiles/unknown/0/0/0", 404}, {"/map/tiles/WebMercatorQuad/x/0/0", 400},
		{"/map/tiles/WebMercatorQuad/0/2/0", 400}, {"/map/tiles/WebMercatorQuad/0/0/0?f=mvt", 400},
		{"/map/tiles/WebMercatorQuad/0/0/0?style=missing", 400},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
			if rec.Code != tc.status {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			if tc.path == "/map/tiles" && !strings.Contains(rec.Body.String(), "/prefix/workspaces/demo/ogc-tiles/map/tiles/") {
				t.Fatalf("base path missing: %s", rec.Body)
			}
		})
	}
	ws.Settings.OGCTilesAPI.Settings.TileMatrixSets = []string{TMSWorldCRS84Quad}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/map/tiles/WebMercatorQuad/0/0/0", nil))
	if rec.Code != 404 {
		t.Fatalf("disabled grid: %d", rec.Code)
	}
	ws.Settings.OGCTilesAPI.Public = false
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/map/tiles", nil))
	if rec.Code != 401 {
		t.Fatalf("private workspace: %d", rec.Code)
	}
}

func TestDatasetMapCompositionPreservesEarlierMembers(t *testing.T) {
	ws, first := datasetWorkspace()
	first.wkbResult = []datasource.RenderFeature{{Geometry: pointWKB(-10018754.1713946, 0)}}
	second := &fakeDataSource{wkbResult: []datasource.RenderFeature{{Geometry: pointWKB(10018754.1713946, 0)}}}
	svc := &workspace.Service{ID: "second", Name: "second", Enabled: true, DataSource: second}
	svc.AddLayer(&workspace.Layer{ID: "labels", PublicID: "labels", SourceLayer: "labels", Enabled: true, CRSDefault: 4326})
	ws.AddService(svc)
	ws.Groups["map"].Members[1].Resource = "labels"
	h := newTestHandler(newTestConfig())
	for _, format := range []string{"png", "jpeg", "webp"} {
		rec := httptest.NewRecorder()
		h.getMapTile(rec, tileRequest(ws, "/map/tiles?f="+format, vectorTileParams("", TMSWebMercatorQuad, "0", "0", "0")))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", format, rec.Code, rec.Body)
		}
		img, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range []int{64, 192} {
			r, g, b, a := img.At(x, 128).RGBA()
			// The default point fill is blue. Both the nested first member and
			// the final member must survive compositing in every output format.
			if r > 30000 || g < 20000 || g > 50000 || b < 40000 || a < 60000 {
				t.Fatalf("%s lost member at %d: RGBA %d %d %d %d", format, x, r, g, b, a)
			}
		}
	}
}

func TestDatasetMapCustomGridAndParameters(t *testing.T) {
	ws, ds := datasetWorkspace()
	grid := getWebMercatorQuad()
	grid.ID = "DatasetGrid"
	grid.URI = ""
	if err := ReplaceCustomTileMatrixSets([]*TileMatrixSetDefinition{grid}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ReplaceCustomTileMatrixSets(nil) })
	ws.Settings.OGCTilesAPI.Settings.TileMatrixSets = []string{grid.ID}
	layer, _ := ws.GetLayer("roads")
	layer.Dimensions = []*workspace.Dimension{{Name: "time", SourceProperty: "observed"}, {Name: "elevation", SourceProperty: "height"}}
	h := newTestHandler(newTestConfig())
	rec := httptest.NewRecorder()
	h.getMapTile(rec, tileRequest(ws, "/map/tiles?datetime=2020-01-01&elevation=10", vectorTileParams("", grid.ID, "2", "1", "2")))
	if rec.Code != 200 {
		t.Fatalf("custom map: %d %s", rec.Code, rec.Body)
	}
	params := ds.params()
	if params.BBox == nil || math.Abs(params.BBox.MinX) > 1e-6 || math.Abs(params.BBox.MinY) > 1e-6 || math.Abs(params.BBox.MaxX-90) > 1e-6 || params.BBox.MaxY < 60 || params.BBox.MaxY > 70 || !strings.Contains(params.Filter, "observed = 2020-01-01") || !strings.Contains(params.Filter, "height = 10") {
		t.Fatalf("coordinates or dimensions lost: %+v", params)
	}
	ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats = []string{MediaTypeJPEG}
	rec = httptest.NewRecorder()
	h.getMapTile(rec, tileRequest(ws, "/map/tiles", vectorTileParams("", grid.ID, "0", "0", "0")))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != MediaTypeJPEG {
		t.Fatalf("format default: %d %s", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	h.getMapTile(rec, tileRequest(ws, "/map/tiles?f=png", vectorTileParams("", grid.ID, "0", "0", "0")))
	if rec.Code != 400 {
		t.Fatalf("disabled format: %d", rec.Code)
	}
}

func TestDatasetMapStyleAndSelectionChangeIdentity(t *testing.T) {
	ws, _ := datasetWorkspace()
	ws.AddStyle(&workspace.Style{Name: "paint", SLDBody: validSLD})
	ws.Groups["map"].DefaultStyle = "paint"
	engine := NewEngine(newTestConfig(), newTestLogger(), nil, nil)
	resolve := func() string {
		t.Helper()
		id, err := engine.ResolveIdentity(EngineRequest{Workspace: ws, Resource: datasetMapResource(ws, ""), TileType: "map", MatrixSet: TMSWebMercatorQuad, Format: MediaTypePNG})
		if err != nil {
			t.Fatal(err)
		}
		return id.CanonicalKey()
	}
	first := resolve()
	ws.AddStyle(&workspace.Style{Name: "paint", SLDBody: strings.ReplaceAll(validSLD, "#ff0000", "#00ff00")})
	second := resolve()
	if first == second {
		t.Fatal("style update reused old image identity")
	}
	copy := *ws.Groups["map"]
	copy.ID = "replacement"
	copy.PublicID = "replacement"
	ws.Groups[copy.PublicID] = &copy
	ws.Settings.OGCTilesAPI.Settings.DatasetMapLayerGroupID = copy.ID
	if resolve() == second {
		t.Fatal("selection update reused old group identity")
	}
}
