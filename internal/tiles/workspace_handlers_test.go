package tiles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/paulmach/orb/encoding/mvt"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

// --- Route wiring ---

func TestRegisterWorkspaceRoutes_Wiring(t *testing.T) {
	r := chi.NewRouter()
	RegisterWorkspaceRoutes(r, WorkspaceDependencies{Config: newTestConfig(), Logger: newTestLogger()})

	var routes []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(routes) != 17 {
		t.Errorf("expected 17 registered routes, got %d: %v", len(routes), routes)
	}
	for _, want := range []string{
		"GET /api",
		"GET /collections/{collectionId}/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}",
		"GET /collections/{collectionId}/map/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}",
		"GET /collections/{collectionId}/tilejson.json",
	} {
		found := false
		for _, route := range routes {
			if route == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing route %q in %v", want, routes)
		}
	}
}

// --- Metadata handlers ---

func TestLanding(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	ws.Settings.OGCTilesAPI.Title = "Custom Title"

	rec := httptest.NewRecorder()
	h.landing(rec, tileRequest(ws, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var lp LandingPage
	if err := json.Unmarshal(rec.Body.Bytes(), &lp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if lp.Title != "Custom Title" {
		t.Errorf("title = %q, want settings override", lp.Title)
	}
	if len(lp.Links) != 5 {
		t.Errorf("expected 5 links, got %+v", lp.Links)
	}
	if !strings.Contains(lp.Links[0].Href, "/workspaces/demo/ogc-tiles") {
		t.Errorf("links must be workspace-scoped: %+v", lp.Links[0])
	}
}

func TestAPI_ReturnsValidOpenAPIForEveryTilesRoute(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	rec := httptest.NewRecorder()
	h.api(rec, tileRequest(ws, "/api", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != MediaTypeOpenAPI {
		t.Fatalf("Content-Type = %q", got)
	}
	doc, err := openapi3.NewLoader().LoadFromData(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("load OpenAPI: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
	seenOperationIDs := make(map[string]string)
	for _, path := range []string{
		"/", "/conformance", "/api", "/tileMatrixSets", "/tileMatrixSets/{tileMatrixSetId}",
		"/collections", "/collections/{collectionId}", "/collections/{collectionId}/tiles",
		"/collections/{collectionId}/tiles/{tileMatrixSetId}",
		"/collections/{collectionId}/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}",
		"/collections/{collectionId}/map/tiles", "/collections/{collectionId}/map/tiles/{tileMatrixSetId}",
		"/collections/{collectionId}/map/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}",
		"/collections/{collectionId}/tilejson.json",
	} {
		item := doc.Paths.Value(path)
		if item == nil || item.Get == nil || item.Get.OperationID == "" {
			t.Errorf("path %s is missing a GET operationId", path)
			continue
		}
		if previous, exists := seenOperationIDs[item.Get.OperationID]; exists {
			t.Errorf("operationId %q is shared by %s and %s", item.Get.OperationID, previous, path)
		}
		seenOperationIDs[item.Get.OperationID] = path
	}
	if path := seenOperationIDs["collectionMap.getTile"]; !strings.Contains(path, "/map/tiles/") {
		t.Fatalf("standard *.getTile operationId is not bound to the map tile route: %v", seenOperationIDs)
	}
}

func TestLanding_NoWorkspaceInContext(t *testing.T) {
	h := newTestHandler(newTestConfig())
	rec := httptest.NewRecorder()
	h.landing(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when workspace missing", rec.Code)
	}
}

func TestAllWorkspaceHandlersTreatMissingContextAsServerError(t *testing.T) {
	h := newTestHandler(newTestConfig())
	handlers := map[string]http.HandlerFunc{
		"landing": h.landing, "conformance": h.conformance, "api": h.api, "tile matrix sets": h.tileMatrixSets,
		"tile matrix set": h.tileMatrixSet, "collections": h.collections, "collection": h.collection,
		"vector tilesets": h.collectionTilesets, "vector tileset": h.collectionTileset, "vector tile": h.getVectorTile,
		"map tilesets": h.collectionMapTilesets, "map tileset": h.collectionMapTileset, "map tile": h.getMapTile,
		"tilejson": h.tileJSON,
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if body := decodeErrBody(recorder); body["type"] != "ServerError" {
				t.Fatalf("error = %+v", body)
			}
		})
	}
}

func TestConformance(t *testing.T) {
	h := newTestHandler(newTestConfig())
	rec := httptest.NewRecorder()
	h.conformance(rec, tileRequest(newTestWorkspace(), "/conformance", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var c Conformance
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, uri := range c.ConformsTo {
		if strings.HasSuffix(uri, "/conf/core") {
			found = true
		}
	}
	if !found {
		t.Errorf("conformance must declare core: %v", c.ConformsTo)
	}
}

func TestTileMatrixSets(t *testing.T) {
	h := newTestHandler(newTestConfig())
	rec := httptest.NewRecorder()
	h.tileMatrixSets(rec, tileRequest(newTestWorkspace(), "/tileMatrixSets", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp TileMatrixSetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.TileMatrixSets) != 2 {
		t.Errorf("expected 2 tile matrix sets, got %+v", resp.TileMatrixSets)
	}
	if len(resp.TileMatrixSets[0].Links) == 0 {
		t.Errorf("items must carry self links: %+v", resp.TileMatrixSets[0])
	}
}

func TestTileMatrixSet_KnownAndUnknown(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()

	rec := httptest.NewRecorder()
	h.tileMatrixSet(rec, tileRequest(ws, "/tileMatrixSets/WebMercatorQuad", map[string]string{"tileMatrixSetId": TMSWebMercatorQuad}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var def TileMatrixSetDefinition
	if err := json.Unmarshal(rec.Body.Bytes(), &def); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(def.TileMatrices) != 25 {
		t.Errorf("expected 25 tile matrices, got %d", len(def.TileMatrices))
	}

	rec = httptest.NewRecorder()
	h.tileMatrixSet(rec, tileRequest(ws, "/tileMatrixSets/Nope", map[string]string{"tileMatrixSetId": "Nope"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown TMS", rec.Code)
	}
}

func TestCollections_FiltersRestrictedLayers(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "open", nil)
	secret := addTestLayer(ws, "secret", nil)
	secret.AllowedRoles = []string{"admin"}

	rec := httptest.NewRecorder()
	h.collections(rec, tileRequest(ws, "/collections", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp CollectionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Collections) != 1 || resp.Collections[0].ID != "open" {
		t.Fatalf("anonymous listing must hide restricted layers, got %+v", resp.Collections)
	}
}

func TestCollection(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", nil)

	rec := httptest.NewRecorder()
	h.collection(rec, tileRequest(ws, "/collections/roads", map[string]string{"collectionId": "roads"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var col Collection
	if err := json.Unmarshal(rec.Body.Bytes(), &col); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if col.ID != "roads" {
		t.Errorf("collection ID = %q", col.ID)
	}
	// Vector and map tiles enabled → vector, tilejson, and map links plus self.
	if len(col.Links) != 4 {
		t.Errorf("expected 4 links, got %+v", col.Links)
	}

	rec = httptest.NewRecorder()
	h.collection(rec, tileRequest(ws, "/collections/nope", map[string]string{"collectionId": "nope"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown collection", rec.Code)
	}
}

func TestCoverageCollectionIsAdditiveMapOnly(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", nil)
	addTestCoverage(ws, "elevation")
	rec := httptest.NewRecorder()
	h.collections(rec, tileRequest(ws, "/collections", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list CollectionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Collections) != 2 {
		t.Fatalf("expected feature and coverage collections, got %+v", list.Collections)
	}
	rec = httptest.NewRecorder()
	h.collection(rec, tileRequest(ws, "/collections/elevation", map[string]string{"collectionId": "elevation"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("coverage collection status=%d", rec.Code)
	}
	var col Collection
	_ = json.Unmarshal(rec.Body.Bytes(), &col)
	for _, link := range col.Links {
		if strings.Contains(link.Rel, "tilesets-vector") {
			t.Fatalf("coverage advertised vector tiles: %+v", col.Links)
		}
	}
	rec = httptest.NewRecorder()
	h.collectionTilesets(rec, tileRequest(ws, "/collections/elevation/tiles", map[string]string{"collectionId": "elevation"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("coverage vector tiles status=%d want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.collectionMapTilesets(rec, tileRequest(ws, "/collections/elevation/map/tiles", map[string]string{"collectionId": "elevation"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("coverage map tilesets status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sets TileSetList
	_ = json.Unmarshal(rec.Body.Bytes(), &sets)
	if len(sets.TileSets) == 0 || sets.TileSets[0].DataType != DataTypeMap {
		t.Fatalf("unexpected coverage map metadata: %+v", sets)
	}
	rec = httptest.NewRecorder()
	h.tileJSON(rec, tileRequest(ws, "/collections/elevation/tilejson.json", map[string]string{"collectionId": "elevation"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("coverage tilejson status=%d", rec.Code)
	}
	var tj TileJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &tj)
	if len(tj.VectorLayers) != 0 || len(tj.Tiles) == 0 || !strings.Contains(tj.Tiles[0], "/map/tiles/") {
		t.Fatalf("unexpected coverage TileJSON: %+v", tj)
	}
}

func TestCollection_RestrictedIs404ForAnonymous(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	secret := addTestLayer(ws, "secret", nil)
	secret.AllowedRoles = []string{"admin"}

	rec := httptest.NewRecorder()
	h.collection(rec, tileRequest(ws, "/collections/secret", map[string]string{"collectionId": "secret"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for restricted layer without role", rec.Code)
	}

	// With the allowed role in context the layer is served.
	req := tileRequest(ws, "/collections/secret", map[string]string{"collectionId": "secret"})
	req = req.WithContext(withIdentity(req.Context(), "u1", map[string]string{ws.ID: "admin"}))
	rec = httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for admin", rec.Code)
	}
}

func TestCollectionTilesets(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", nil)

	rec := httptest.NewRecorder()
	h.collectionTilesets(rec, tileRequest(ws, "/collections/roads/tiles", map[string]string{"collectionId": "roads"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var list TileSetList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// One tileset per configured TMS.
	if len(list.TileSets) != 2 {
		t.Fatalf("expected 2 tilesets, got %+v", list.TileSets)
	}
	if list.TileSets[0].DataType != DataTypeVector {
		t.Errorf("dataType = %q, want vector", list.TileSets[0].DataType)
	}
	for _, tileset := range list.TileSets {
		assertOGCTileItemTemplates(t, tileset.Links)
	}
}

func TestCollectionTileset(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", nil)

	rec := httptest.NewRecorder()
	h.collectionTileset(rec, tileRequest(ws, "/collections/roads/tiles/WebMercatorQuad", map[string]string{
		"collectionId": "roads", "tileMatrixSetId": TMSWebMercatorQuad,
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var ts TileSetMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &ts); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ts.TileMatrixSetID != TMSWebMercatorQuad || ts.CRS != CRS3857URI {
		t.Errorf("unexpected tileset identity: %+v", ts)
	}
	assertOGCTileItemTemplates(t, ts.Links)

	rec = httptest.NewRecorder()
	h.collectionTileset(rec, tileRequest(ws, "/collections/roads/tiles/Nope", map[string]string{
		"collectionId": "roads", "tileMatrixSetId": "Nope",
	}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown TMS", rec.Code)
	}
}

func TestCollectionMapTilesets(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", nil)

	rec := httptest.NewRecorder()
	h.collectionMapTilesets(rec, tileRequest(ws, "/collections/roads/map/tiles", map[string]string{"collectionId": "roads"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var list TileSetList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.TileSets) != 2 || list.TileSets[0].DataType != DataTypeMap {
		t.Fatalf("expected 2 map tilesets, got %+v", list.TileSets)
	}
	// PNG, JPEG, and WebP item links plus self and the tiling scheme.
	if len(list.TileSets[0].Links) != 5 {
		t.Errorf("expected self + tiling scheme + 3 format item links, got %+v", list.TileSets[0].Links)
	}
	for _, tileset := range list.TileSets {
		assertOGCTileItemTemplates(t, tileset.Links)
		for _, link := range tileset.Links {
			if link.Rel == "item" && !strings.Contains(link.Href, "?f=") {
				t.Errorf("map item link is missing format parameter: %+v", link)
			}
		}
	}
}

func TestCollectionMapTileset(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", nil)

	rec := httptest.NewRecorder()
	h.collectionMapTileset(rec, tileRequest(ws, "/collections/roads/map/tiles/WorldCRS84Quad", map[string]string{
		"collectionId": "roads", "tileMatrixSetId": TMSWorldCRS84Quad,
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var ts TileSetMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &ts); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ts.DataType != DataTypeMap || ts.CRS != CRS4326URI {
		t.Errorf("unexpected tileset: %+v", ts)
	}
	assertOGCTileItemTemplates(t, ts.Links)
}

func assertOGCTileItemTemplates(t *testing.T, links []Link) {
	t.Helper()
	foundItem := false
	for _, link := range links {
		if link.Rel != "item" {
			continue
		}
		foundItem = true
		for _, placeholder := range []string{"{tileMatrix}", "{tileRow}", "{tileCol}"} {
			if !strings.Contains(link.Href, placeholder) {
				t.Errorf("OGC tile item link %q is missing %s", link.Href, placeholder)
			}
		}
		for _, legacy := range []string{"{z}", "{y}", "{x}"} {
			if strings.Contains(link.Href, legacy) {
				t.Errorf("OGC tile item link %q contains TileJSON placeholder %s", link.Href, legacy)
			}
		}
	}
	if !foundItem {
		t.Error("tileset contains no item link")
	}
}

func TestTileJSONHandler(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{})

	rec := httptest.NewRecorder()
	h.tileJSON(rec, tileRequest(ws, "/collections/roads/tilejson.json", map[string]string{"collectionId": "roads"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var tj TileJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &tj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tj.TileJSON != "3.0.0" || len(tj.Tiles) != 1 {
		t.Errorf("unexpected tilejson: %+v", tj)
	}
	if !strings.Contains(tj.Tiles[0], "/workspaces/demo/ogc-tiles/collections/roads/tiles/") {
		t.Errorf("tile URL = %q", tj.Tiles[0])
	}
	for _, placeholder := range []string{"{z}", "{y}", "{x}"} {
		if !strings.Contains(tj.Tiles[0], placeholder) {
			t.Errorf("TileJSON URL %q is missing %s", tj.Tiles[0], placeholder)
		}
	}

	rec = httptest.NewRecorder()
	h.tileJSON(rec, tileRequest(ws, "/collections/nope/tilejson.json", map[string]string{"collectionId": "nope"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// --- Guard chain (representative handler: landing / getVectorTile) ---

func TestGuard_TilesDisabled(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	ws.Settings.OGCTilesAPI.Enabled = false

	rec := httptest.NewRecorder()
	h.landing(rec, tileRequest(ws, "/", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when tiles disabled", rec.Code)
	}
	if body := decodeErrBody(rec); body["type"] != "ServiceDisabled" {
		t.Errorf("error type = %q, want ServiceDisabled", body["type"])
	}
}

func TestGuard_NilSettings(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := &workspace.Workspace{ID: "ws1", Name: "demo"} // Settings nil

	rec := httptest.NewRecorder()
	h.conformance(rec, tileRequest(ws, "/conformance", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for nil settings", rec.Code)
	}
}

func TestGuard_VectorTilesDisabled(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	ws.Settings.OGCTilesAPI.Settings.VectorTiles.Enabled = false
	addTestLayer(ws, "roads", &fakeDataSource{})

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/collections/roads/tiles/WebMercatorQuad/0/0/0", map[string]string{
		"collectionId": "roads", "tileMatrixSetId": TMSWebMercatorQuad, "tileMatrix": "0", "tileRow": "0", "tileCol": "0",
	}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when vector tiles disabled", rec.Code)
	}
}

func TestGuard_MapTilesDisabled(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled = false
	addTestLayer(ws, "roads", &fakeDataSource{})

	rec := httptest.NewRecorder()
	h.getMapTile(rec, tileRequest(ws, "/collections/roads/map/tiles/WebMercatorQuad/0/0/0", map[string]string{
		"collectionId": "roads", "tileMatrixSetId": TMSWebMercatorQuad, "tileMatrix": "0", "tileRow": "0", "tileCol": "0",
	}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when map tiles disabled", rec.Code)
	}
}

func TestRequireAuth_NonPublicWorkspace(t *testing.T) {
	cfg := newTestConfig()
	cfg.Auth.RequireHTTPS = false
	h := newTestHandler(cfg)
	ws := newTestWorkspace()
	ws.Settings.OGCTilesAPI.Public = false

	// No identity → 401.
	rec := httptest.NewRecorder()
	h.landing(rec, tileRequest(ws, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without identity", rec.Code)
	}

	// Identity without access to this workspace → 403.
	req := tileRequest(ws, "/", nil)
	req = req.WithContext(withIdentity(req.Context(), "u1", map[string]string{"other-ws": "admin"}))
	rec = httptest.NewRecorder()
	h.landing(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for cross-workspace identity", rec.Code)
	}

	// Workspace member → 200.
	req = tileRequest(ws, "/", nil)
	req = req.WithContext(withIdentity(req.Context(), "u1", map[string]string{ws.ID: "viewer"}))
	rec = httptest.NewRecorder()
	h.landing(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for workspace member", rec.Code)
	}

	// Global super_admin → 200.
	req = tileRequest(ws, "/", nil)
	req = req.WithContext(withIdentity(req.Context(), "root", map[string]string{"*": "super_admin"}))
	rec = httptest.NewRecorder()
	h.landing(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for super_admin", rec.Code)
	}
}

func TestRequireAuth_HTTPSRequired(t *testing.T) {
	cfg := newTestConfig()
	cfg.Auth.RequireHTTPS = true
	h := newTestHandler(cfg)
	ws := newTestWorkspace()
	ws.Settings.OGCTilesAPI.Public = false

	// No transport-security marker in context → treated as insecure → 426.
	req := tileRequest(ws, "/", nil)
	req = req.WithContext(withIdentity(req.Context(), "u1", map[string]string{ws.ID: "viewer"}))
	rec := httptest.NewRecorder()
	h.landing(rec, req)
	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want 426 on insecure transport", rec.Code)
	}
	if rec.Header().Get("Upgrade") == "" {
		t.Errorf("426 response must carry an Upgrade header")
	}

	// Public workspaces bypass the transport check entirely.
	ws.Settings.OGCTilesAPI.Public = true
	rec = httptest.NewRecorder()
	h.landing(rec, tileRequest(ws, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for public workspace", rec.Code)
	}
}

// --- getVectorTile ---

func vectorTileParams(collection, tms, matrix, row, column string) map[string]string {
	return map[string]string{
		"collectionId": collection, "tileMatrixSetId": tms,
		"tileMatrix": matrix, "tileRow": row, "tileCol": column,
	}
}

func TestGetVectorTile_InvalidCoords(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{})

	tests := []struct {
		name, matrix, row, column, parameter string
	}{
		{"non-numeric tileMatrix", "abc", "0", "0", "tileMatrix"},
		{"non-numeric tileRow", "0", "abc", "0", "tileRow"},
		{"non-numeric tileCol", "0", "0", "abc", "tileCol"},
		{"tileCol out of range", "1", "0", "2", ""},
		{"negative tileMatrix", "-1", "0", "0", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, tt.matrix, tt.row, tt.column)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if body := decodeErrBody(rec); body["type"] != "InvalidParameter" {
				t.Errorf("error type = %q, want InvalidParameter", body["type"])
			} else if tt.parameter != "" && !strings.Contains(body["detail"], tt.parameter) {
				t.Errorf("detail = %q, want parameter %q", body["detail"], tt.parameter)
			}
		})
	}
}

func TestGetVectorTile_ZoomOutsideConfiguredRange(t *testing.T) {
	cfg := newTestConfig()
	cfg.Tiles.MinZoom = 5
	cfg.Tiles.MaxZoom = 10
	h := newTestHandler(cfg)
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{})

	for _, z := range []string{"4", "11"} {
		rec := httptest.NewRecorder()
		h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, z, "0", "0")))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("z=%s: status = %d, want 400", z, rec.Code)
		}
		if body := decodeErrBody(rec); !strings.Contains(body["detail"], "zoom level outside configured range") {
			t.Errorf("z=%s: detail = %q", z, body["detail"])
		}
	}
}

func TestGetVectorTile_UnknownCollection(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{})

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("nope", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetVectorTile_RestrictedLayerIs404(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	secret := addTestLayer(ws, "secret", &fakeDataSource{})
	secret.AllowedRoles = []string{"admin"}

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("secret", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for restricted layer", rec.Code)
	}
}

func TestGetVectorTile_NilDataSource(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", nil)

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for nil datasource", rec.Code)
	}
}

func TestGetVectorTile_LayerInfoError(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{layerInfoErr: errors.New("boom")})

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on layer info error", rec.Code)
	}
}

func TestGetVectorTile_GenerationError(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{queryErr: errors.New("db down")})

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on generation error", rec.Code)
	}
}

func TestGetVectorTile_HappyPath(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{queryResult: []json.RawMessage{pointFeature(1, 0, 0)}})

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != MediaTypeMVT {
		t.Errorf("Content-Type = %q, want %q", ct, MediaTypeMVT)
	}
	if xc := rec.Header().Get("X-Cache"); xc != "MISS" {
		t.Errorf("X-Cache = %q, want MISS", xc)
	}
	layers, err := mvt.Unmarshal(rec.Body.Bytes())
	if err != nil || len(layers) != 1 || len(layers[0].Features) != 1 {
		t.Fatalf("body must be a decodable MVT with 1 feature, err=%v layers=%v", err, layers)
	}
}

func TestGetVectorTile_RenderQueueFull(t *testing.T) {
	h := newTestHandler(newTestConfig())
	h.queueTimeout = 20 * time.Millisecond
	h.renderSlots <- struct{}{} // occupy the single render slot
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{})

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when render queue is full", rec.Code)
	}
	if body := decodeErrBody(rec); body["type"] != "ServerBusy" {
		t.Errorf("error type = %q, want ServerBusy", body["type"])
	}
}

func TestGetVectorTile_SQLViewPath(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	ds := &fakeSQLViewDataSource{sqlViewRows: []json.RawMessage{pointFeature(1, 0, 0)}}
	layer := addTestLayer(ws, "view", ds)
	layer.IsSQLView = true
	layer.SQLViewConfig = &workspace.SQLViewConfig{
		SQL:            "SELECT * FROM t",
		GeometryColumn: "geom",
		SRID:           4326,
		Properties:     []*workspace.SQLViewProperty{{Name: "n", Type: "integer"}},
	}

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("view", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := mvt.Unmarshal(rec.Body.Bytes()); err != nil {
		t.Fatalf("body must decode as MVT: %v", err)
	}
}

func TestGetVectorTile_CacheMissThenHit(t *testing.T) {
	mgr, err := cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatalf("cache manager: %v", err)
	}
	defer mgr.Close()

	h := newTestHandler(newTestConfig())
	h.cache = mgr
	ws := newTestWorkspace()
	ws.Settings.OGCTilesAPI.Settings.CacheEnabled = true
	ds := &fakeDataSource{queryResult: []json.RawMessage{pointFeature(1, 0, 0)}}
	addTestLayer(ws, "roads", ds)

	rec := httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusOK || rec.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("first request: status = %d, X-Cache = %q", rec.Code, rec.Header().Get("X-Cache"))
	}

	// Ristretto admits entries asynchronously; poll until the tile is cached.
	key := cache.MVTTileKey(ws.ID, "roads", TMSWebMercatorQuad, 0, 0, 0)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := mgr.GetTile(key); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tile was never admitted to the cache")
		}
		time.Sleep(10 * time.Millisecond)
	}

	rec = httptest.NewRecorder()
	h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusOK || rec.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("second request: status = %d, X-Cache = %q, want HIT", rec.Code, rec.Header().Get("X-Cache"))
	}
	if got := atomic.LoadInt64(&ds.queryCalls); got != 1 {
		t.Errorf("datasource queried %d times, want 1 (second request served from cache)", got)
	}
}

// --- getMapTile ---

func TestGetMapTile_HappyPath(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{wkbResult: []datasource.RenderFeature{{Geometry: pointWKB(0, 0)}}})

	rec := httptest.NewRecorder()
	h.getMapTile(rec, tileRequest(ws, "/map/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != MediaTypePNG {
		t.Errorf("Content-Type = %q, want PNG", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "\x89PNG") {
		t.Errorf("body is not a PNG")
	}
}

func TestGetMapTile_UnknownFormat(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{})

	rec := httptest.NewRecorder()
	h.getMapTile(rec, tileRequest(ws, "/map/tiles?f=gif", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown format", rec.Code)
	}
}

func TestGetMapTile_FormatNotEnabled(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats = []string{MediaTypePNG}
	addTestLayer(ws, "roads", &fakeDataSource{})

	rec := httptest.NewRecorder()
	h.getMapTile(rec, tileRequest(ws, "/map/tiles?f=webp", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for disabled format", rec.Code)
	}
	if body := decodeErrBody(rec); !strings.Contains(body["detail"], "not enabled") {
		t.Errorf("detail = %q", body["detail"])
	}
}

func TestGetMapTile_DefaultsToFirstEnabledFormat(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	// PNG not enabled; the first enabled format (WebP) becomes the default.
	ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats = []string{MediaTypeWEBP}
	addTestLayer(ws, "roads", &fakeDataSource{wkbResult: []datasource.RenderFeature{{Geometry: pointWKB(0, 0)}}})

	rec := httptest.NewRecorder()
	h.getMapTile(rec, tileRequest(ws, "/map/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != MediaTypeWEBP {
		t.Errorf("Content-Type = %q, want WebP fallback", ct)
	}
}

func TestGetMapTile_StyleValidation(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	ws.AddStyle(&workspace.Style{Name: "good", SLDBody: validSLD})
	ws.AddStyle(&workspace.Style{Name: "broken", SLDBody: "<not-sld"})
	addTestLayer(ws, "roads", &fakeDataSource{wkbResult: []datasource.RenderFeature{{Geometry: pointWKB(0, 0)}}})

	tests := []struct {
		name       string
		style      string
		wantStatus int
		wantDetail string
	}{
		{"invalid name", "../evil", http.StatusBadRequest, "invalid style name"},
		{"not found", "missing", http.StatusBadRequest, "style not found"},
		{"unparseable body", "broken", http.StatusBadRequest, "style is invalid"},
		{"valid style", "good", http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.getMapTile(rec, tileRequest(ws, "/map/tiles?style="+tt.style, vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantDetail != "" {
				if body := decodeErrBody(rec); !strings.Contains(body["detail"], tt.wantDetail) {
					t.Errorf("detail = %q, want containing %q", body["detail"], tt.wantDetail)
				}
			}
		})
	}
}

func TestGetMapTile_RenderQueueFull(t *testing.T) {
	h := newTestHandler(newTestConfig())
	h.queueTimeout = 20 * time.Millisecond
	h.renderSlots <- struct{}{}
	ws := newTestWorkspace()
	addTestLayer(ws, "roads", &fakeDataSource{})

	rec := httptest.NewRecorder()
	h.getMapTile(rec, tileRequest(ws, "/map/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// --- Concurrency ---

func TestAcquireRender(t *testing.T) {
	h := newTestHandler(newTestConfig())

	release, ok := h.acquireRender(context.Background())
	if !ok {
		t.Fatal("expected slot acquisition to succeed")
	}
	// Slot is now full; a second acquire times out.
	h.queueTimeout = 20 * time.Millisecond
	if _, ok := h.acquireRender(context.Background()); ok {
		t.Fatal("expected acquisition to fail while slot is held")
	}
	release()
	// Released slot can be re-acquired.
	if _, ok := h.acquireRender(context.Background()); !ok {
		t.Fatal("expected acquisition to succeed after release")
	}
}

func TestAcquireRender_ContextCancel(t *testing.T) {
	h := newTestHandler(newTestConfig())
	h.queueTimeout = 5 * time.Second // long timeout so cancellation wins
	h.renderSlots <- struct{}{}      // block the slot

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if _, ok := h.acquireRender(ctx); ok {
		t.Fatal("expected acquisition to fail on context cancel")
	}
	if time.Since(start) > time.Second {
		t.Fatal("context cancellation did not interrupt the wait")
	}
}

func TestGetVectorTile_SingleflightCoalesces(t *testing.T) {
	h := newTestHandler(newTestConfig())
	ws := newTestWorkspace()
	ds := &fakeDataSource{
		queryResult: []json.RawMessage{pointFeature(1, 0, 0)},
		queryDelay:  300 * time.Millisecond,
	}
	addTestLayer(ws, "roads", ds)

	const n = 5
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i > 0 {
				time.Sleep(50 * time.Millisecond) // join while the first render is in flight
			}
			rec := httptest.NewRecorder()
			h.getVectorTile(rec, tileRequest(ws, "/tiles", vectorTileParams("roads", TMSWebMercatorQuad, "0", "0", "0")))
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("request %d: status = %d", i, code)
		}
	}
	if got := atomic.LoadInt64(&ds.queryCalls); got != 1 {
		t.Errorf("datasource queried %d times, want 1 (singleflight coalescing)", got)
	}
}

// --- tileLayerInfo ---

func TestTileLayerInfo_PassThrough(t *testing.T) {
	want := testLayerInfo("roads")
	ds := &fakeDataSource{layerInfo: want}
	layer := &workspace.Layer{SourceLayer: "roads"}

	info, config, err := tileLayerInfo(context.Background(), ds, layer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != want || config != nil {
		t.Errorf("non-SQL-view must pass layer info through with nil config")
	}

	ds.layerInfoErr = errors.New("boom")
	if _, _, err := tileLayerInfo(context.Background(), ds, layer); err == nil {
		t.Fatal("expected error propagation")
	}
}

func TestTileLayerInfo_SQLView(t *testing.T) {
	layer := &workspace.Layer{
		PublicID:  "view",
		IsSQLView: true,
		SQLViewConfig: &workspace.SQLViewConfig{
			SQL:            "SELECT * FROM t",
			GeometryColumn: "geom",
			GeometryType:   "Point",
			SRID:           3857,
			IDColumn:       "fid",
			Properties: []*workspace.SQLViewProperty{
				{Name: "a", Type: "integer"},
				{Name: "b", Type: "number"},
				{Name: "c", Type: "boolean"},
				{Name: "d", Type: "string"},
				{Name: "e", Type: "anything"},
			},
		},
	}

	info, config, err := tileLayerInfo(context.Background(), &fakeSQLViewDataSource{}, layer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config == nil || config.SQL != "SELECT * FROM t" || config.SRID != 3857 {
		t.Fatalf("unexpected config: %+v", config)
	}
	if info.Name != "view" || info.GeometryColumn != "geom" || info.IDColumn != "fid" {
		t.Errorf("unexpected info: %+v", info)
	}
	wantTypes := map[string]datasource.JSONType{
		"a": datasource.JSONTypeInteger,
		"b": datasource.JSONTypeNumber,
		"c": datasource.JSONTypeBoolean,
		"d": datasource.JSONTypeString,
		"e": datasource.JSONTypeString, // unknown types default to string
	}
	for _, prop := range info.Properties {
		if prop.JSONType != wantTypes[prop.Name] {
			t.Errorf("property %q JSONType = %q, want %q", prop.Name, prop.JSONType, wantTypes[prop.Name])
		}
	}
}

func TestTileLayerInfo_SQLViewUnsupportedDataSource(t *testing.T) {
	layer := &workspace.Layer{
		IsSQLView:     true,
		SQLViewConfig: &workspace.SQLViewConfig{SQL: "SELECT 1"},
	}
	_, _, err := tileLayerInfo(context.Background(), &fakeDataSource{}, layer)
	if err == nil || !strings.Contains(err.Error(), "does not support SQL views") {
		t.Fatalf("expected SQL view support error, got %v", err)
	}
}

// --- small helpers ---

func TestContainsString(t *testing.T) {
	if !containsString([]string{"a", "b"}, "b") {
		t.Error("expected true for present value")
	}
	if containsString([]string{"a", "b"}, "c") {
		t.Error("expected false for absent value")
	}
	if containsString(nil, "a") {
		t.Error("expected false for nil slice")
	}
}

func TestMapTileItemLinks(t *testing.T) {
	links := mapTileItemLinks("http://base", "roads", TMSWebMercatorQuad, []string{
		MediaTypePNG, MediaTypeJPEG, MediaTypeWEBP, "application/bogus",
	})
	if len(links) != 3 {
		t.Fatalf("unknown formats must be skipped, got %+v", links)
	}
	wantParams := []string{"f=png", "f=jpeg", "f=webp"}
	for i, link := range links {
		if !strings.Contains(link.Href, wantParams[i]) {
			t.Errorf("link %d = %q, want containing %q", i, link.Href, wantParams[i])
		}
		if link.Rel != "item" {
			t.Errorf("link rel = %q, want item", link.Rel)
		}
	}
}

func TestURLPathEscape(t *testing.T) {
	tests := map[string]string{
		"plain": "plain", "with space": "with%20space", "a/b": "a%2Fb", "100%": "100%25",
		"query?": "query%3F", "fragment#": "fragment%23", "café": "caf%C3%A9",
	}
	for input, expected := range tests {
		got := urlPathEscape(input)
		if got != expected {
			t.Errorf("urlPathEscape(%q) = %q, want %q", input, got, expected)
		}
		decoded, err := url.PathUnescape(got)
		if err != nil || decoded != input {
			t.Errorf("round trip %q = %q, %v", input, decoded, err)
		}
	}
}

func TestWorkspaceBaseURL(t *testing.T) {
	cfg := newTestConfig()
	cfg.Server.UrlBase = "http://example.com/"
	cfg.Server.BasePath = "/geo"
	h := newTestHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := h.workspaceBaseURL(req, "my ws"); got != "http://example.com/geo/workspaces/my%20ws/ogc-tiles" {
		t.Errorf("workspaceBaseURL = %q", got)
	}
	if got := h.getBaseURL(req); got != "http://example.com/geo" {
		t.Errorf("getBaseURL = %q", got)
	}
}
