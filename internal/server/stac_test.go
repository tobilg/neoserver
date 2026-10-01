package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/store"
)

type stacRequest func(method, path, body string, principal *identity.Identity, headers map[string]string) *httptest.ResponseRecorder

type stacTestServer struct {
	srv      *Server
	request  stacRequest
	one, two *store.Workspace
	root     string
}

// newSTACTestServer starts a server with STAC enabled under /base and two
// workspaces, "one" and "two", whose catalogs are enabled and public.
func newSTACTestServer(t *testing.T) *stacTestServer {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	cfg, err := conf.Load("", false, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Store.Path = filepath.Join(root, "catalog.duckdb")
	cfg.Store.EncryptionKey = "abc123"
	cfg.STAC.Enabled = true
	cfg.STAC.DatabasePath = filepath.Join(root, "stac.duckdb")
	cfg.Cache.Enabled = false
	cfg.Audit.Enabled = false
	cfg.Auth.RequireHTTPS = false
	cfg.Server.DisableUI = true
	cfg.Server.BasePath = "/base"
	cfg.Server.UrlBase = "http://example.test"
	cfg.Datasource.AllowedPaths = []string{root + "/**"}
	catalog, _, err := store.Init(store.Config{Path: cfg.Store.Path, EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { catalog.Close() })
	one, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "one"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "two"})
	if err != nil {
		t.Fatal(err)
	}
	for _, ws := range []string{one.ID, two.ID} {
		if err = catalog.UpdateSTACSettings(ctx, ws, store.STACSettings{Enabled: true, Public: true}); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), catalog)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Shutdown(ctx) })
	request := func(method, path, body string, principal *identity.Identity, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		if principal != nil {
			r = r.WithContext(identity.WithIdentity(r.Context(), principal))
		}
		w := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(w, r)
		return w
	}
	return &stacTestServer{srv: srv, request: request, one: one, two: two, root: root}
}

func TestSTACWorkspaceHTTPAndLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := newSTACTestServer(t)
	srv, request, one, root := fixture.srv, fixture.request, fixture.one, fixture.root
	var err error
	admin := &identity.Identity{Roles: map[string]string{"*": "super_admin"}}
	c := staccatalog.Collection{Document: stacmodel.Collection("scenes", "Scenes", "Published scenes", "other", nil), Public: true}
	raw, _ := json.Marshal(c)
	response := request("POST", "/base/api/v1/workspaces/one/stac/collections", string(raw), admin, nil)
	if response.Code != 201 {
		t.Fatalf("create Collection: %d %s", response.Code, response.Body.String())
	}
	item := `{"type":"Feature","stac_version":"1.1.0","id":"one","collection":"scenes","geometry":{"type":"Point","coordinates":[7,52]},"bbox":[7,52,7,52],"properties":{"datetime":"2026-01-01T00:00:00Z"},"assets":{},"links":[{"rel":"collection","href":"https://example.org/scenes"}]}`
	response = request("POST", "/base/api/v1/workspaces/one/stac/collections/scenes/items", item, admin, nil)
	if response.Code != 200 {
		t.Fatalf("create Item: %d %s", response.Code, response.Body.String())
	}
	for _, path := range []string{"/base/stac", "/base/workspaces/two/stac/collections/scenes", "/base/workspaces/two/stac/collections/scenes/items/one"} {
		if w := request("GET", path, "", nil, nil); w.Code != 404 {
			t.Errorf("isolation %s = %d", path, w.Code)
		}
	}
	definition := request("GET", "/base/workspaces/one/stac/api", "", nil, map[string]string{"Accept": "application/vnd.oai.openapi+json;version=3.0"})
	if definition.Code != 200 || definition.Header().Get("Content-Type") != "application/vnd.oai.openapi+json;version=3.0" {
		t.Fatalf("OpenAPI media type: %d %s", definition.Code, definition.Header().Get("Content-Type"))
	}
	for _, path := range []string{"/", "/conformance", "/api", "/collections", "/collections/scenes", "/collections/scenes/items", "/collections/scenes/items/one", "/search"} {
		w := request("GET", "/base/workspaces/one/stac"+path+"?unknown=1", "", nil, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("unknown query parameter at %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/search?datetime=1985-04-12T23:20:50,52Z", "/collections/scenes/items?ids=one"} {
		w := request("GET", "/base/workspaces/one/stac"+path, "", nil, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid parameter at %s: %d", path, w.Code)
		}
	}
	for _, method := range []string{"GET", "POST"} {
		path := "/base/workspaces/one/stac/search"
		body := ""
		if method == "POST" {
			body = `{"bbox":[6,51,8,53]}`
		} else {
			path += "?bbox=6,51,8,53"
		}
		w := request(method, path, body, nil, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"numberReturned":1`) {
			t.Fatalf("search %s: %d %s", method, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "neoserver.invalid") || !strings.Contains(w.Body.String(), "http://example.test/base/workspaces/one/stac") {
			t.Fatalf("bad generated links: %s", w.Body.String())
		}
	}
	if w := request("POST", "/base/workspaces/one/stac/search", `{"bbox":[0,0,1,1],"intersects":{"type":"Point","coordinates":[0,0]}}`, nil, nil); w.Code != 400 {
		t.Errorf("bbox + intersects: %d", w.Code)
	}
	file := filepath.Join(root, "asset.tif")
	if err = os.WriteFile(file, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = srv.stac.Catalog.BindLocalAsset(ctx, one.ID, "", staccatalog.LocalAsset{CollectionID: "scenes", ItemID: "one", Key: "data", Path: file, MediaType: "image/tiff"}); err != nil {
		t.Fatal(err)
	}
	path := "/base/workspaces/one/stac/collections/scenes/items/one/assets/data"
	w := request("GET", path, "", nil, map[string]string{"Range": "bytes=2-4"})
	if w.Code != 206 || w.Body.String() != "234" {
		t.Fatalf("range %d %s", w.Code, w.Body.String())
	}
	w = request("HEAD", path, "", nil, nil)
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD %d %s", w.Code, w.Body.String())
	}
	if err = srv.registry.UpdateSTACSettings(ctx, one.ID, store.STACSettings{Enabled: true, Public: false}); err != nil {
		t.Fatal(err)
	}
	if w = request("GET", path, "", nil, nil); w.Code != 401 {
		t.Fatalf("private download %d", w.Code)
	}
	viewer := &identity.Identity{Roles: map[string]string{one.ID: "viewer"}}
	w = request("POST", "/base/workspaces/one/stac/search", `{}`, viewer, nil)
	if w.Code != 200 {
		t.Fatalf("viewer POST search %d %s", w.Code, w.Body.String())
	}
	plan, err := srv.lifecycle.PlanWorkspace(ctx, one.ID)
	if err != nil || plan.Auxiliary.STACCollections != 1 || plan.Auxiliary.STACItems != 1 {
		t.Fatalf("deletion inventory: %+v %v", plan, err)
	}
	if err = srv.stac.QuiesceAndDeleteLifecycle(ctx, one.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.stac.Catalog.GetCollection(ctx, one.ID, "scenes"); err != staccatalog.ErrNotFound {
		t.Fatalf("STAC cleanup: %v", err)
	}
	if _, err = os.Stat(file); err != nil {
		t.Fatalf("underlying data was deleted: %v", err)
	}
}

func TestSTACUploadLimitScope(t *testing.T) {
	for _, test := range []struct {
		path   string
		upload bool
	}{{"/base/api/v1/workspaces/demo/stac/imports", true}, {"/api/v1/workspaces/demo/stac/imports/x", false}, {"/workspaces/demo/stac/search", false}} {
		r := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader("12345"))
		handler := limitBody(3, 0, 10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(413)
			} else {
				w.WriteHeader(200)
			}
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if (w.Code == 200) != test.upload {
			t.Errorf("limit scope %s status %d", test.path, w.Code)
		}
	}
}
