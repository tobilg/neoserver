package ogc

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// ============================================================================
// Test helpers
// ============================================================================

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestConfig() conf.Config {
	return conf.Config{
		Metadata: conf.Metadata{
			Title: "Test Server",
		},
		Paging: conf.Paging{
			LimitDefault:   10,
			LimitMax:       100,
			CountTimeoutMS: 5000,
		},
		Server: conf.Server{
			BasePath: "",
			UrlBase:  "",
		},
	}
}

// withChiContext creates a context with chi route params
func withChiContext(ctx context.Context, params map[string]string) context.Context {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

// addTestLayer adds a service with a layer to a workspace
func addTestLayer(ws *workspace.Workspace, svcID, publicID string, ds datasource.DataSource) {
	svc := &workspace.Service{
		ID:         svcID,
		Name:       svcID,
		Enabled:    true,
		DataSource: ds,
	}
	svc.AddLayer(&workspace.Layer{
		ID:          publicID,
		PublicID:    publicID,
		Title:       "Test Layer",
		SourceLayer: "test",
		Enabled:     true,
		CRSDefault:  4326,
	})
	ws.AddService(svc)
}

func newTestWorkspace(name string, ogcEnabled bool) *workspace.Workspace {
	settings := &store.WorkspaceSettings{
		OGCAPI: store.OGCAPISettings{
			Enabled:  ogcEnabled,
			Public:   true, // Public for testing without auth
			Title:    "Test OGC API",
			Abstract: "Test workspace for OGC API",
		},
	}
	return &workspace.Workspace{
		ID:          "ws-" + name,
		Name:        name,
		Description: "Test workspace",
		Settings:    settings,
	}
}

func newTestHandler() *workspaceHandler {
	return &workspaceHandler{
		cfg:      newTestConfig(),
		logger:   newTestLogger(),
		registry: nil,
		cache:    nil,
	}
}

// ============================================================================
// parseSortBy Tests
// ============================================================================

func TestParseSortBy_Empty(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest("GET", "/items", nil)

	sortBy, err := h.parseSortBy(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sortBy) != 0 {
		t.Errorf("expected empty sortBy, got %d items", len(sortBy))
	}
}

func TestParseSortBy_SingleAscending(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest("GET", "/items?sortby=name", nil)

	sortBy, err := h.parseSortBy(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sortBy) != 1 {
		t.Fatalf("expected 1 sortBy field, got %d", len(sortBy))
	}
	if sortBy[0].Name != "name" {
		t.Errorf("expected field name 'name', got %q", sortBy[0].Name)
	}
	if sortBy[0].Desc {
		t.Error("expected ascending sort")
	}
}

func TestParseSortBy_SingleDescending(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest("GET", "/items?sortby=-created", nil)

	sortBy, err := h.parseSortBy(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sortBy) != 1 {
		t.Fatalf("expected 1 sortBy field, got %d", len(sortBy))
	}
	if sortBy[0].Name != "created" {
		t.Errorf("expected field name 'created', got %q", sortBy[0].Name)
	}
	if !sortBy[0].Desc {
		t.Error("expected descending sort")
	}
}

func TestParseSortBy_ExplicitAscending(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest("GET", "/items?sortby=+name", nil)

	sortBy, err := h.parseSortBy(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sortBy) != 1 {
		t.Fatalf("expected 1 sortBy field, got %d", len(sortBy))
	}
	if sortBy[0].Name != "name" {
		t.Errorf("expected field name 'name', got %q", sortBy[0].Name)
	}
	if sortBy[0].Desc {
		t.Error("expected ascending sort")
	}
}

func TestParseSortBy_Multiple(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest("GET", "/items?sortby=name,-created,+updated", nil)

	sortBy, err := h.parseSortBy(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sortBy) != 3 {
		t.Fatalf("expected 3 sortBy fields, got %d", len(sortBy))
	}

	// First: name, ascending
	if sortBy[0].Name != "name" || sortBy[0].Desc {
		t.Errorf("expected (name, asc), got (%q, desc=%v)", sortBy[0].Name, sortBy[0].Desc)
	}

	// Second: created, descending
	if sortBy[1].Name != "created" || !sortBy[1].Desc {
		t.Errorf("expected (created, desc), got (%q, desc=%v)", sortBy[1].Name, sortBy[1].Desc)
	}

	// Third: updated, ascending
	if sortBy[2].Name != "updated" || sortBy[2].Desc {
		t.Errorf("expected (updated, asc), got (%q, desc=%v)", sortBy[2].Name, sortBy[2].Desc)
	}
}

func TestParseSortBy_WithWhitespace(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest("GET", "/items?sortby=name,+", nil)

	sortBy, err := h.parseSortBy(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty parts should be skipped
	if len(sortBy) != 1 {
		t.Fatalf("expected 1 sortBy field, got %d", len(sortBy))
	}
}

// ============================================================================
// workspaceBaseURL Tests
// ============================================================================

func TestWorkspaceBaseURL_WithUrlBase(t *testing.T) {
	h := &workspaceHandler{
		cfg: conf.Config{
			Server: conf.Server{
				UrlBase:  "https://api.example.com",
				BasePath: "/v1",
			},
		},
	}
	req := httptest.NewRequest("GET", "/workspaces/test/ogc", nil)

	url := h.workspaceBaseURL(req, "test")
	expected := "https://api.example.com/v1/workspaces/test/ogc"
	if url != expected {
		t.Errorf("expected %q, got %q", expected, url)
	}
}

func TestWorkspaceBaseURL_WithTrailingSlash(t *testing.T) {
	h := &workspaceHandler{
		cfg: conf.Config{
			Server: conf.Server{
				UrlBase:  "https://api.example.com/",
				BasePath: "",
			},
		},
	}
	req := httptest.NewRequest("GET", "/workspaces/test/ogc", nil)

	url := h.workspaceBaseURL(req, "test")
	expected := "https://api.example.com/workspaces/test/ogc"
	if url != expected {
		t.Errorf("expected %q, got %q", expected, url)
	}
}

func TestWorkspaceBaseURL_NoUrlBase_HTTP(t *testing.T) {
	h := &workspaceHandler{
		cfg: conf.Config{
			Server: conf.Server{
				UrlBase:  "",
				BasePath: "",
			},
		},
	}
	req := httptest.NewRequest("GET", "/workspaces/test/ogc", nil)
	req.Host = "localhost:8080"

	url := h.workspaceBaseURL(req, "test")
	expected := "/workspaces/test/ogc"
	if url != expected {
		t.Errorf("expected %q, got %q", expected, url)
	}
}

func TestWorkspaceBaseURL_NoUrlBase_WithBasePath(t *testing.T) {
	h := &workspaceHandler{
		cfg: conf.Config{
			Server: conf.Server{
				UrlBase:  "",
				BasePath: "/api",
			},
		},
	}
	req := httptest.NewRequest("GET", "/api/workspaces/test/ogc", nil)
	req.Host = "example.com"

	url := h.workspaceBaseURL(req, "test")
	expected := "/api/workspaces/test/ogc"
	if url != expected {
		t.Errorf("expected %q, got %q", expected, url)
	}
}

// ============================================================================
// buildWorkspaceOpenAPI Tests
// ============================================================================

func TestBuildWorkspaceOpenAPI(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)

	doc := h.buildWorkspaceOpenAPI(ws)

	if doc.OpenAPI != "3.0.3" {
		t.Errorf("expected OpenAPI version 3.0.3, got %q", doc.OpenAPI)
	}
	if doc.Info.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %q", doc.Info.Version)
	}
	if doc.Paths.Value("/") == nil {
		t.Error("expected landing page path")
	}
	if doc.Paths.Value("/conformance") == nil {
		t.Error("expected conformance path")
	}
	if doc.Paths.Value("/collections") == nil {
		t.Error("expected collections path")
	}
	if doc.Paths.Value("/collections/{collectionId}/items") == nil || doc.Paths.Value("/collections/{collectionId}/queryables") == nil {
		t.Error("expected feature and queryables paths")
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode OpenAPI: %v", err)
	}
	loaded, err := openapi3.NewLoader().LoadFromData(encoded)
	if err != nil {
		t.Fatalf("load OpenAPI: %v", err)
	}
	if err := loaded.Validate(context.Background()); err != nil {
		t.Fatalf("OpenAPI document is invalid: %v", err)
	}
}

// ============================================================================
// Handler Tests
// ============================================================================

func withWorkspace(ctx context.Context, ws *workspace.Workspace) context.Context {
	return workspace.WithWorkspace(ctx, ws)
}

func newACLTestWorkspace() *workspace.Workspace {
	ws := newTestWorkspace("acl", true)
	svc := &workspace.Service{ID: "svc", Name: "svc", Enabled: true}
	svc.AddLayer(&workspace.Layer{ID: "open", PublicID: "open", SourceLayer: "open", Enabled: true, CRSDefault: 4326})
	svc.AddLayer(&workspace.Layer{ID: "secret", PublicID: "secret", SourceLayer: "secret", Enabled: true, CRSDefault: 4326, AllowedRoles: []string{"admin"}})
	ws.AddService(svc)
	return ws
}

func TestCollection_RestrictedLayerHiddenFromAnonymous(t *testing.T) {
	h := newTestHandler()
	ws := newACLTestWorkspace()

	// Anonymous (public service) must not resolve a role-restricted collection.
	ctx := withWorkspace(withChiContext(context.Background(), map[string]string{"collectionId": "secret"}), ws)
	req := httptest.NewRequest("GET", "/collections/secret", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("restricted collection: got %d, want 404", rec.Code)
	}

	// The unrestricted collection is reachable.
	ctx2 := withWorkspace(withChiContext(context.Background(), map[string]string{"collectionId": "open"}), ws)
	req2 := httptest.NewRequest("GET", "/collections/open", nil).WithContext(ctx2)
	rec2 := httptest.NewRecorder()
	h.collection(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("open collection: got %d, want 200", rec2.Code)
	}
}

func TestCollections_ListingOmitsRestrictedLayer(t *testing.T) {
	h := newTestHandler()
	ws := newACLTestWorkspace()

	ctx := withWorkspace(context.Background(), ws)
	req := httptest.NewRequest("GET", "/collections", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.collections(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("collections: got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "\"open\"") {
		t.Fatalf("expected open layer in listing, got: %s", body)
	}
	if strings.Contains(body, "\"secret\"") {
		t.Fatalf("restricted layer must not appear in anonymous listing, got: %s", body)
	}
}

func TestLanding_NoWorkspaceInContext(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	h.landing(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestLanding_OGCDisabled(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.landing(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestLanding_Success(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "localhost:8080"
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.landing(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp LandingPage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Title != "Test OGC API" {
		t.Errorf("expected title 'Test OGC API', got %q", resp.Title)
	}
	if len(resp.Links) != 5 {
		t.Errorf("expected 5 links, got %d", len(resp.Links))
	}
}

func TestLanding_FallbackTitle(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	ws.Settings.OGCAPI.Title = ""
	ws.Settings.OGCAPI.Abstract = ""

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "localhost"
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.landing(w, req)

	var resp LandingPage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Should fall back to workspace name
	if resp.Title != ws.Name {
		t.Errorf("expected title %q, got %q", ws.Name, resp.Title)
	}
}

func TestConformance_NoWorkspaceInContext(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/conformance", nil)

	h.conformance(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestConformance_OGCDisabled(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/conformance", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.conformance(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestConformance_Success(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/conformance", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.conformance(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Conformance
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.ConformsTo) != 10 {
		t.Errorf("expected 10 conformance URIs, got %d", len(resp.ConformsTo))
	}

	// Verify core conformance
	hasCore := false
	for _, uri := range resp.ConformsTo {
		if uri == "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core" {
			hasCore = true
			break
		}
	}
	if !hasCore {
		t.Error("expected core conformance URI")
	}
}

func TestCollections_NoWorkspaceInContext(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections", nil)

	h.collections(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestCollections_OGCDisabled(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.collections(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestCollections_EmptyWorkspace(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections", nil)
	req.Host = "localhost"
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.collections(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Collections
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.Collections) != 0 {
		t.Errorf("expected empty collections, got %d", len(resp.Collections))
	}
}

func TestCollection_NoWorkspaceInContext(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/test", nil)

	h.collection(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestCollection_OGCDisabled(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/test", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.collection(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestCollection_NotFound(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/nonexistent", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "nonexistent"})
	req = req.WithContext(ctx)

	h.collection(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestItems_NoWorkspaceInContext(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/test/items", nil)

	h.items(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestItems_OGCDisabled(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/test/items", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.items(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestItems_CollectionNotFound(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/nonexistent/items", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "nonexistent"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestItems_InvalidLimit(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", &mockDataSource{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items?limit=abc", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestItems_NegativeLimit(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", &mockDataSource{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items?limit=-1", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestItems_InvalidOffset(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", &mockDataSource{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items?offset=abc", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestItems_NegativeOffset(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", &mockDataSource{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items?offset=-5", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestItems_InvalidBBox(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", &mockDataSource{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items?bbox=invalid", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestItems_AntimeridianBBox(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	ds := &mockDataSource{}
	addTestLayer(ws, "svc1", "testlayer", ds)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items?bbox=177,65,-177,70", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if ds.lastParams.BBox == nil || ds.lastParams.BBox.MinX != 177 || ds.lastParams.BBox.MaxX != -177 {
		t.Fatalf("query bbox = %+v", ds.lastParams.BBox)
	}
}

func TestItems_InvalidCRS(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", &mockDataSource{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items?crs=invalid", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestItems_InvalidBBoxCRS(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", &mockDataSource{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items?bbox-crs=invalid", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestItems_InvalidFilterCRS(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", &mockDataSource{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items?filter=name='test'&filter-crs=invalid", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestItems_DataSourceNotAvailable(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", nil) // nil DataSource

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer"})
	req = req.WithContext(ctx)

	h.items(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestItem_NoWorkspaceInContext(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/test/items/1", nil)

	h.item(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestItem_OGCDisabled(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/test/items/1", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.item(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestItem_CollectionNotFound(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/nonexistent/items/1", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "nonexistent", "featureId": "1"})
	req = req.WithContext(ctx)

	h.item(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestItem_DataSourceNotAvailable(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items/1", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer", "featureId": "1"})
	req = req.WithContext(ctx)

	h.item(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestItem_InvalidCRS(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc1", "testlayer", &mockDataSource{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/collections/testlayer/items/1?crs=invalid", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "testlayer", "featureId": "1"})
	req = req.WithContext(ctx)

	h.item(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestApi_NoWorkspaceInContext(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api", nil)

	h.api(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestApi_OGCDisabled(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.api(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestApi_Success(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api", nil)
	req.Host = "localhost"
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.api(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	doc, err := openapi3.NewLoader().LoadFromData(w.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if doc.OpenAPI != "3.0.3" {
		t.Errorf("expected OpenAPI 3.0.3, got %q", doc.OpenAPI)
	}
}

func TestApiHTML_NoWorkspaceInContext(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api.html", nil)

	h.apiHTML(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestApiHTML_OGCDisabled(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api.html", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.apiHTML(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestItems_LookaheadPagingDateTimeAndCRS84Default(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	ds := &mockDataSource{features: []json.RawMessage{
		json.RawMessage(`{"type":"Feature","id":1,"geometry":null,"properties":{"name":"a"}}`),
		json.RawMessage(`{"type":"Feature","id":2,"geometry":null,"properties":{"name":"b"}}`),
		json.RawMessage(`{"type":"Feature","id":3,"geometry":null,"properties":{"name":"c"}}`),
	}}
	addTestLayer(ws, "svc", "events", ds)
	layer, _ := ws.GetLayer("events")
	layer.CRSDefault = 3857
	layer.Dimensions = []*workspace.Dimension{{Name: "time", Units: "ISO8601", SourceProperty: "time", EndProperty: "time_end"}}

	req := httptest.NewRequest("GET", "/collections/events/items?limit=2&offset=4&datetime=2020-01-01T00%3A00%3A00Z%2F2020-01-02T00%3A00%3A00Z&filter=name%3D%27a%27&filter-lang=cql2-text", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "events"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.items(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("items status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Crs"); got != "<"+query.CRS84URI+">" {
		t.Fatalf("Content-Crs = %q", got)
	}
	var body struct {
		NumberReturned int    `json:"numberReturned"`
		NumberMatched  *int   `json:"numberMatched"`
		TimeStamp      string `json:"timeStamp"`
		Features       []any  `json:"features"`
		Links          []Link `json:"links"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.NumberReturned != 2 || len(body.Features) != 2 {
		t.Fatalf("returned=%d features=%d", body.NumberReturned, len(body.Features))
	}
	if body.NumberMatched == nil || *body.NumberMatched != 3 || body.TimeStamp == "" {
		t.Fatalf("matched=%v timestamp=%q", body.NumberMatched, body.TimeStamp)
	}
	var next string
	for _, link := range body.Links {
		if link.Rel == "next" {
			next = link.Href
		}
	}
	if !strings.Contains(next, "limit=2") || !strings.Contains(next, "offset=6") || !strings.Contains(next, "datetime=") || !strings.Contains(next, "filter=") {
		t.Fatalf("next link did not preserve the query: %q", next)
	}
	if ds.lastParams.Limit != 3 || ds.lastParams.Offset != 4 || ds.lastParams.OutputSRID != 4326 {
		t.Fatalf("query params = %+v", ds.lastParams)
	}
	if ds.lastCountParams.Limit != 0 || ds.lastCountParams.Offset != 0 || ds.lastCountParams.OutputSRID != 0 {
		t.Fatalf("count params = %+v", ds.lastCountParams)
	}
	combinedFilter := ds.lastParams.WithDateTimeFilter().Filter
	if ds.lastParams.DateTime == nil || !strings.Contains(combinedFilter, `"time" IS NULL`) || !strings.Contains(combinedFilter, `name='a'`) {
		t.Fatalf("datetime/filter not combined: %+v", ds.lastParams)
	}
}

func TestCollectionExtent_CombinesFreshSpatialAndTemporalMetadata(t *testing.T) {
	layer := &workspace.Layer{
		NativeExtent: &store.SpatialExtent{MinX: 7, MinY: 50, MaxX: 8, MaxY: 51, SRID: 4326},
		Dimensions:   []*workspace.Dimension{{Name: "time", SourceProperty: "observed_at", Extent: "2020-01-01T00:00:00Z/2020-12-31T23:59:59Z"}},
	}
	extent := collectionExtent(layer)
	if extent == nil || extent.Spatial == nil || extent.Temporal == nil {
		t.Fatalf("combined extent = %+v", extent)
	}
	if got := extent.Spatial.BBox[0]; len(got) != 4 || got[0] != 7 || got[3] != 51 {
		t.Fatalf("spatial bbox = %v", got)
	}
	if extent.Spatial.CRS != query.CRS84URI {
		t.Fatalf("spatial CRS = %q", extent.Spatial.CRS)
	}

	layer.NativeExtent.Stale = true
	if got := collectionExtent(layer); got == nil || got.Spatial != nil || got.Temporal == nil {
		t.Fatalf("stale spatial extent must be omitted without dropping temporal metadata: %+v", got)
	}
}

func TestItems_CountTimeoutOmitsNumberMatchedWithoutFailingPage(t *testing.T) {
	h := newTestHandler()
	h.cfg.Paging.CountTimeoutMS = 1
	ws := newTestWorkspace("test", true)
	ds := &mockDataSource{
		features: []json.RawMessage{json.RawMessage(`{"type":"Feature","id":1,"geometry":null,"properties":{}}`)},
		countFunc: func(ctx context.Context, _ datasource.QueryParams) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		},
	}
	addTestLayer(ws, "svc", "slow-count", ds)
	req := httptest.NewRequest("GET", "/collections/slow-count/items", nil)
	ctx := withChiContext(withWorkspace(req.Context(), ws), map[string]string{"collectionId": "slow-count"})
	rec := httptest.NewRecorder()
	h.items(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["numberMatched"]; exists {
		t.Fatalf("timed-out count must be omitted: %s", rec.Body.String())
	}
	if body["numberReturned"] != float64(1) || body["timeStamp"] == nil {
		t.Fatalf("page metadata = %v", body)
	}
}

func TestItems_DateTimeWithoutBindingLeavesCollectionUntimed(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	ds := &mockDataSource{}
	addTestLayer(ws, "svc", "places", ds)
	req := httptest.NewRequest("GET", "/collections/places/items?datetime=2020-01-01T00%3A00%3A00Z", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "places"})
	rec := httptest.NewRecorder()
	h.items(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ds.lastParams.Filter != "" || ds.lastParams.DateTime == nil {
		t.Fatalf("untimed collection should not be filtered: %+v", ds.lastParams)
	}
}

func TestItems_InvalidCoreAndFilterParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"unknown", "vendor=true"},
		{"datetime", "datetime=not-a-time"},
		{"filter-lang", "filter=name%3D%27a%27&filter-lang=cql2-json"},
		{"filter-property", "filter=missing%3D1"},
		{"unadvertised-crs", "crs=EPSG%3A25832"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newTestHandler()
			ws := newTestWorkspace("test", true)
			addTestLayer(ws, "svc", "places", &mockDataSource{})
			req := httptest.NewRequest("GET", "/collections/places/items?"+test.query, nil)
			ctx := withWorkspace(req.Context(), ws)
			ctx = withChiContext(ctx, map[string]string{"collectionId": "places"})
			rec := httptest.NewRecorder()
			h.items(rec, req.WithContext(ctx))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestQueryables_DescribesPropertiesAndGeometry(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)
	addTestLayer(ws, "svc", "places", &mockDataSource{})
	req := httptest.NewRequest("GET", "/collections/places/queryables", nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, map[string]string{"collectionId": "places"})
	rec := httptest.NewRecorder()
	h.queryables(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != queryablesType {
		t.Fatalf("Content-Type=%q", got)
	}
	var schema Queryables
	if err := json.Unmarshal(rec.Body.Bytes(), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Schema != "https://json-schema.org/draft/2020-12/schema" || schema.AdditionalProperties {
		t.Fatalf("unexpected queryables schema: %+v", schema)
	}
	if schema.Properties["time"]["format"] != "date-time" || schema.Properties["geom"]["format"] != "geometry-Point" {
		t.Fatalf("unexpected property mappings: %+v", schema.Properties)
	}
}

func TestParseDateTimeIntervals(t *testing.T) {
	dimension := &workspace.Dimension{Name: "time", SourceProperty: "start", EndProperty: "end"}
	selection, err := parseDateTime("../2020-01-02T00:00:00Z", dimension)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Start != nil || selection.End == nil || !strings.Contains(dateTimeCQL(selection), `"start" <=`) {
		t.Fatalf("unexpected open interval: %+v %s", selection, dateTimeCQL(selection))
	}
	if _, err := parseDateTime("../..", dimension); err == nil {
		t.Fatal("expected fully open interval to be rejected")
	}
	dateSelection, err := parseDateTime("2020-01-01", dimension)
	if err != nil || dateSelection.Instant || dateSelection.Start == nil || dateSelection.End == nil || dateSelection.Start.Day() != 1 || dateSelection.End.Day() != 1 {
		t.Fatalf("unexpected date selection: %+v err=%v", dateSelection, err)
	}
	durationSelection, err := parseDateTime("2020-01-01/P2D", dimension)
	if err != nil || durationSelection.End == nil || durationSelection.End.Day() != 3 {
		t.Fatalf("unexpected duration selection: %+v err=%v", durationSelection, err)
	}
}

func TestApiHTML_Success(t *testing.T) {
	h := newTestHandler()
	ws := newTestWorkspace("test", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api.html", nil)
	req = req.WithContext(withWorkspace(req.Context(), ws))

	h.apiHTML(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected text/html content type, got %q", ct)
	}
	if !contains(w.Body.String(), "swagger-ui") {
		t.Error("expected Swagger UI HTML")
	}
}

// ============================================================================
// WorkspaceDependencies Tests
// ============================================================================

func TestWorkspaceDependencies_Fields(t *testing.T) {
	deps := WorkspaceDependencies{
		Config: newTestConfig(),
		Logger: newTestLogger(),
	}

	if deps.Config.Metadata.Title != "Test Server" {
		t.Errorf("expected title 'Test Server', got %q", deps.Config.Metadata.Title)
	}
}

// ============================================================================
// RegisterWorkspaceRoutes Tests
// ============================================================================

func TestRegisterWorkspaceRoutes(t *testing.T) {
	r := chi.NewRouter()
	deps := WorkspaceDependencies{
		Config: newTestConfig(),
		Logger: newTestLogger(),
	}

	// Should not panic
	RegisterWorkspaceRoutes(r, deps)

	// Verify routes are registered (by walking the routes)
	routeCount := 0
	chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		routeCount++
		return nil
	})

	if routeCount != 9 {
		t.Errorf("expected 9 routes, got %d", routeCount)
	}
}

// ============================================================================
// Mock DataSource for testing
// ============================================================================

type mockDataSource struct {
	features        []json.RawMessage
	lastParams      datasource.QueryParams
	lastCountParams datasource.QueryParams
	info            *datasource.LayerInfo
	countFunc       func(context.Context, datasource.QueryParams) (int, error)
}

func (m *mockDataSource) Type() store.ServiceType {
	return store.ServiceTypePostGIS
}

func (m *mockDataSource) ID() string {
	return "mock-ds"
}

func (m *mockDataSource) DiscoverLayers(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	return nil, nil
}

func (m *mockDataSource) Query(ctx context.Context, layer string, params datasource.QueryParams) ([]json.RawMessage, error) {
	m.lastParams = params
	return append([]json.RawMessage(nil), m.features...), nil
}

func (m *mockDataSource) QueryWKB(ctx context.Context, layer string, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	return nil, nil
}

func (m *mockDataSource) QueryByID(ctx context.Context, layer, featureID string, outputSRID int) (json.RawMessage, bool, error) {
	return nil, false, nil
}

func (m *mockDataSource) Count(ctx context.Context, layer string, params datasource.QueryParams) (int, error) {
	m.lastCountParams = params
	if m.countFunc != nil {
		return m.countFunc(ctx, params)
	}
	return len(m.features), nil
}

func (m *mockDataSource) GetLayerInfo(ctx context.Context, layer string) (*datasource.LayerInfo, error) {
	if m.info != nil {
		return m.info, nil
	}
	return &datasource.LayerInfo{
		Name: "public.test", Schema: "public", GeometryColumn: "geom", GeometryType: "POINT", SRID: 4326, IDColumn: "id",
		Properties: []datasource.PropertyInfo{
			{Name: "id", Type: "integer", JSONType: datasource.JSONTypeInteger},
			{Name: "name", Type: "varchar", JSONType: datasource.JSONTypeString},
			{Name: "time", Type: "timestamptz", JSONType: datasource.JSONTypeString},
			{Name: "time_end", Type: "timestamptz", JSONType: datasource.JSONTypeString},
		},
		PGTypes: map[string]string{"id": "int4", "name": "text", "time": "timestamptz", "time_end": "timestamptz"},
	}, nil
}

func (m *mockDataSource) Health(ctx context.Context) error {
	return nil
}

func (m *mockDataSource) Close() error {
	return nil
}

// ============================================================================
// SimpleOpenAPI Tests
// ============================================================================

// Legacy serialization fixtures are retained to guard the JSON shape consumed
// by older clients; production OpenAPI generation uses kin-openapi.
type SimpleOpenAPI struct {
	OpenAPI string                 `json:"openapi"`
	Info    OpenAPIInfo            `json:"info"`
	Servers []*ServerInfo          `json:"servers,omitempty"`
	Paths   map[string]interface{} `json:"paths"`
}

type OpenAPIInfo struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

type ServerInfo struct {
	URL string `json:"url"`
}

func TestSimpleOpenAPI_JSONSerialization(t *testing.T) {
	doc := SimpleOpenAPI{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       "Test API",
			Description: "A test API",
			Version:     "1.0.0",
		},
		Servers: []*ServerInfo{{URL: "http://localhost"}},
		Paths:   map[string]interface{}{"/": "test"},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("failed to marshal SimpleOpenAPI: %v", err)
	}

	var decoded SimpleOpenAPI
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal SimpleOpenAPI: %v", err)
	}

	if decoded.OpenAPI != "3.0.3" {
		t.Errorf("expected OpenAPI=3.0.3, got %q", decoded.OpenAPI)
	}
	if decoded.Info.Title != "Test API" {
		t.Errorf("expected title 'Test API', got %q", decoded.Info.Title)
	}
}

func TestServerInfo_JSONSerialization(t *testing.T) {
	info := ServerInfo{URL: "http://localhost:8080"}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal ServerInfo: %v", err)
	}

	var decoded ServerInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ServerInfo: %v", err)
	}

	if decoded.URL != "http://localhost:8080" {
		t.Errorf("expected URL 'http://localhost:8080', got %q", decoded.URL)
	}
}

func TestOpenAPIInfo_OmitEmpty(t *testing.T) {
	info := OpenAPIInfo{
		Title:   "Test",
		Version: "1.0.0",
		// Description is empty
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal OpenAPIInfo: %v", err)
	}

	s := string(data)
	if contains(s, "description") {
		t.Error("expected description to be omitted when empty")
	}
}

// ============================================================================
// requireAuth (workspace authorization) tests — Fix #1
// ============================================================================

func TestRequireAuth_WorkspaceAuthorization(t *testing.T) {
	h := &workspaceHandler{cfg: newTestConfig(), logger: newTestLogger()}
	ws := &workspace.Workspace{ID: "ws-1", Name: "ws-1"}

	newReq := func(id *identity.Identity) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		if id != nil {
			r = r.WithContext(identity.WithIdentity(r.Context(), id))
		}
		return r
	}

	t.Run("public service allows anonymous", func(t *testing.T) {
		w := httptest.NewRecorder()
		if h.requireAuth(w, newReq(nil), ws, true) {
			t.Error("public service should not require auth")
		}
	})

	t.Run("no identity -> 401", func(t *testing.T) {
		w := httptest.NewRecorder()
		if !h.requireAuth(w, newReq(nil), ws, false) {
			t.Fatal("expected request to be denied")
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("identity without workspace access -> 403", func(t *testing.T) {
		other := &identity.Identity{Subject: "u", Roles: map[string]string{"ws-2": "viewer"}}
		w := httptest.NewRecorder()
		if !h.requireAuth(w, newReq(other), ws, false) {
			t.Fatal("expected cross-workspace request to be denied")
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})

	t.Run("identity with workspace access -> allowed", func(t *testing.T) {
		member := &identity.Identity{Subject: "u", Roles: map[string]string{"ws-1": "viewer"}}
		w := httptest.NewRecorder()
		if h.requireAuth(w, newReq(member), ws, false) {
			t.Error("workspace member should be allowed")
		}
	})

	t.Run("super admin -> allowed", func(t *testing.T) {
		admin := &identity.Identity{Subject: "root", Roles: map[string]string{"*": "super_admin"}}
		w := httptest.NewRecorder()
		if h.requireAuth(w, newReq(admin), ws, false) {
			t.Error("super admin should be allowed")
		}
	})
}
