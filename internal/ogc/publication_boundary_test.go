package ogc

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	ducksource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/workspace"
)

func boundarySource(t *testing.T) *ducksource.DataSource {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`LOAD spatial;
	CREATE TABLE records (id VARCHAR PRIMARY KEY, name VARCHAR, secret VARCHAR, geom GEOMETRY);
	INSERT INTO records VALUES ('1','published','hidden-column',ST_Point(7,51)),('2','excluded','hidden-row',ST_Point(8,52));
	CREATE TABLE restricted (id VARCHAR PRIMARY KEY, name VARCHAR, geom GEOMETRY);
	INSERT INTO restricted VALUES ('c','RESTRICTED_PAYLOAD',ST_Point(7,51));
	CREATE TABLE public_records (id VARCHAR PRIMARY KEY, name VARCHAR, geom GEOMETRY);
	INSERT INTO public_records VALUES ('b:c','public payload',ST_Point(8,52));`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	ds, err := ducksource.New("source", ducksource.Config{Path: path, ReadOnly: true, Extensions: []string{"spatial"}, SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ds.Close() })
	return ds
}

func TestSQLViewItemConfinesRowsAndProperties(t *testing.T) {
	ds := boundarySource(t)
	ws := newTestWorkspace("review", true)
	addTestLayer(ws, "source", "filtered", ds)
	layer := ws.Services["source"].Layers["filtered"]
	layer.IsSQLView = true
	layer.SQLViewConfig = &workspace.SQLViewConfig{SQL: "SELECT id, name, geom FROM records WHERE id='1'", GeometryColumn: "geom", SRID: 4326, IDColumn: "id", ReadOnly: true, Properties: []*workspace.SQLViewProperty{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}}
	h := newTestHandler()
	for _, source := range []string{"records", "_sql_view_"} {
		layer.SourceLayer = source
		for _, id := range []string{"1", "2", "' OR 1=1 --"} {
			req := httptest.NewRequest("GET", "http://example.test/items", nil)
			ctx := withChiContext(workspace.WithWorkspace(req.Context(), ws), map[string]string{"collectionId": "filtered", "featureId": id})
			w := httptest.NewRecorder()
			h.item(w, req.WithContext(ctx))
			want := 404
			if id == "1" {
				want = 200
			}
			if w.Code != want || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "hidden-") {
				t.Fatalf("source=%s id=%q: %d %s", source, id, w.Code, w.Body)
			}
		}
	}
	layer.SQLViewConfig = nil
	req := httptest.NewRequest("GET", "http://example.test/items", nil)
	ctx := withChiContext(workspace.WithWorkspace(req.Context(), ws), map[string]string{"collectionId": "filtered", "featureId": "1"})
	w := httptest.NewRecorder()
	h.item(w, req.WithContext(ctx))
	if w.Code == 200 {
		t.Fatal("invalid SQL view fell back to physical table")
	}
}

func TestItemCacheDoesNotCrossPublicationBoundary(t *testing.T) {
	ds := boundarySource(t)
	ws := newTestWorkspace("review", true)
	addTestLayer(ws, "secret", "a:b", ds)
	addTestLayer(ws, "public", "a", ds)
	secret := ws.Services["secret"].Layers["a:b"]
	secret.SourceLayer, secret.AllowedRoles = "restricted", []string{"admin"}
	ws.Services["public"].Layers["a"].SourceLayer = "public_records"
	h := newTestHandler()
	var err error
	h.cache, err = cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer h.cache.Close()
	run := func(collection, id string, admin bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "http://example.test/items", nil)
		ctx := withChiContext(workspace.WithWorkspace(req.Context(), ws), map[string]string{"collectionId": collection, "featureId": id})
		if admin {
			ctx = identity.WithIdentity(ctx, &identity.Identity{Roles: map[string]string{ws.ID: "admin"}})
		}
		w := httptest.NewRecorder()
		h.item(w, req.WithContext(ctx))
		return w
	}
	if w := run("a:b", "c", false); w.Code != 404 {
		t.Fatal("bad restriction fixture")
	}
	deadline := time.Now().Add(time.Second)
	for {
		w := run("a:b", "c", true)
		if w.Code != 200 {
			t.Fatalf("warm: %d %s", w.Code, w.Body)
		}
		if w.Header().Get("X-Cache") == "HIT" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cache not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	anon := run("a", "b:c", false)
	var result struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(anon.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if anon.Code != 200 || result.Properties["name"] != "public payload" {
		t.Fatalf("wrong publication: %d %s", anon.Code, anon.Body)
	}
}
