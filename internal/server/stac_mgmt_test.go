package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/store"
)

var stacSuperAdmin = &identity.Identity{Roles: map[string]string{"*": "super_admin"}}

func stacItemJSON(collection, id string) string {
	return `{"type":"Feature","stac_version":"1.1.0","id":"` + id + `","collection":"` + collection + `","geometry":{"type":"Point","coordinates":[7,52]},"bbox":[7,52,7,52],"properties":{"datetime":"2026-01-01T00:00:00Z"},"assets":{"data":{"href":"https://example.org/` + id + `.tif"}},"links":[]}`
}

func (f *stacTestServer) createCollection(t *testing.T, id string, public bool, roles []string, items ...string) {
	t.Helper()
	raw, _ := json.Marshal(staccatalog.Collection{Document: stacmodel.Collection(id, id, id, "other", nil), Public: public, AllowedRoles: roles})
	if w := f.request("POST", "/base/api/v1/workspaces/one/stac/collections", string(raw), stacSuperAdmin, nil); w.Code != 201 {
		t.Fatalf("create Collection %s: %d %s", id, w.Code, w.Body.String())
	}
	for _, item := range items {
		if w := f.request("POST", "/base/api/v1/workspaces/one/stac/collections/"+id+"/items", stacItemJSON(id, item), stacSuperAdmin, nil); w.Code != 200 {
			t.Fatalf("create Item %s: %d %s", item, w.Code, w.Body.String())
		}
	}
}

func TestSTACCORSAllowsRangeReads(t *testing.T) {
	f := newSTACTestServer(t)
	f.createCollection(t, "scenes", true, nil, "one")
	file := filepath.Join(f.root, "asset.tif")
	if err := os.WriteFile(file, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.stac.Catalog.BindLocalAsset(context.Background(), f.one.ID, "", staccatalog.LocalAsset{CollectionID: "scenes", ItemID: "one", Key: "data", Path: file, MediaType: "image/tiff"}); err != nil {
		t.Fatal(err)
	}
	path := "/base/workspaces/one/stac/collections/scenes/items/one/assets/data"
	preflight := f.request("OPTIONS", path, "", nil, map[string]string{"Origin": "https://browser.example", "Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "range"})
	if preflight.Code != http.StatusOK || !strings.Contains(strings.ToLower(preflight.Header().Get("Access-Control-Allow-Headers")), "range") {
		t.Fatalf("Range preflight: %d %v", preflight.Code, preflight.Header())
	}
	w := f.request("GET", path, "", nil, map[string]string{"Origin": "https://browser.example", "Range": "bytes=2-4"})
	if w.Code != http.StatusPartialContent || w.Body.String() != "234" {
		t.Fatalf("cross-origin range: %d %s", w.Code, w.Body.String())
	}
	exposed := strings.ToLower(w.Header().Get("Access-Control-Expose-Headers"))
	for _, header := range []string{"content-range", "accept-ranges", "etag"} {
		if !strings.Contains(exposed, header) {
			t.Errorf("%s not exposed: %q", header, exposed)
		}
	}
}

func TestSTACRoleRestrictedCollectionsOverHTTP(t *testing.T) {
	f := newSTACTestServer(t)
	f.createCollection(t, "open", true, nil, "public-item")
	f.createCollection(t, "restricted", false, []string{"editor"}, "secret")
	viewer := &identity.Identity{Roles: map[string]string{f.one.ID: "viewer"}}
	editor := &identity.Identity{Roles: map[string]string{f.one.ID: "editor"}}
	outsider := &identity.Identity{Roles: map[string]string{f.two.ID: "editor"}}
	base := "/base/workspaces/one/stac"
	hidden := func(label string, principal *identity.Identity) {
		t.Helper()
		if w := f.request("GET", base+"/collections", "", principal, nil); w.Code != 200 || strings.Contains(w.Body.String(), "restricted") || !strings.Contains(w.Body.String(), `"open"`) {
			t.Errorf("%s Collection listing: %d %s", label, w.Code, w.Body.String())
		}
		if w := f.request("GET", base+"/", "", principal, nil); strings.Contains(w.Body.String(), "collections/restricted") {
			t.Errorf("%s landing page links restricted Collection", label)
		}
		for _, path := range []string{"/collections/restricted", "/collections/restricted/items", "/collections/restricted/items/secret"} {
			if w := f.request("GET", base+path, "", principal, nil); w.Code != 404 {
				t.Errorf("%s %s = %d", label, path, w.Code)
			}
		}
		for _, query := range []string{"ids=secret", "collections=restricted", ""} {
			w := f.request("GET", base+"/search?"+query, "", principal, nil)
			// The self link echoes the query, so inspect only the returned Items.
			if w.Code != 200 || strings.Contains(w.Body.String(), `"id":"secret"`) {
				t.Errorf("%s search %q: %d %s", label, query, w.Code, w.Body.String())
			}
		}
		if w := f.request("POST", base+"/search", `{"ids":["secret"]}`, principal, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"numberReturned":0`) {
			t.Errorf("%s POST search: %d %s", label, w.Code, w.Body.String())
		}
	}
	hidden("anonymous", nil)
	hidden("viewer", viewer)
	if err := f.srv.registry.UpdateSTACSettings(context.Background(), f.one.ID, store.STACSettings{Enabled: true, Public: false}); err != nil {
		t.Fatal(err)
	}
	if w := f.request("GET", base+"/collections", "", nil, nil); w.Code != 401 {
		t.Errorf("anonymous private catalog: %d", w.Code)
	}
	if w := f.request("GET", base+"/collections", "", outsider, nil); w.Code != 403 {
		t.Errorf("member of another workspace: %d %s", w.Code, w.Body.String())
	}
	hidden("private viewer", viewer)
	if w := f.request("GET", base+"/collections/restricted/items/secret", "", editor, nil); w.Code != 200 {
		t.Errorf("editor Item: %d %s", w.Code, w.Body.String())
	}
	if w := f.request("POST", base+"/search", `{"ids":["secret"]}`, editor, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"numberReturned":1`) {
		t.Errorf("editor search: %d %s", w.Code, w.Body.String())
	}
}

func TestSTACManagementHandlers(t *testing.T) {
	f := newSTACTestServer(t)
	admin := &identity.Identity{Roles: map[string]string{f.one.ID: "admin"}}
	viewer := &identity.Identity{Roles: map[string]string{f.one.ID: "viewer"}}
	api := "/base/api/v1/workspaces/one"
	decode := func(t *testing.T, body string) map[string]any {
		t.Helper()
		var v map[string]any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		return v
	}

	t.Run("authorization", func(t *testing.T) {
		if w := f.request("GET", api+"/settings/stac", "", nil, nil); w.Code != 401 {
			t.Errorf("anonymous settings: %d", w.Code)
		}
		if w := f.request("GET", api+"/stac/collections", "", viewer, nil); w.Code != 403 {
			t.Errorf("viewer management: %d", w.Code)
		}
		w := f.request("PUT", api+"/settings/stac", `{"enabled":true,"public":true,"title":"Scenes"}`, admin, nil)
		if w.Code != 200 {
			t.Fatalf("admin settings: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("GET", api+"/settings/stac", "", admin, nil); w.Code != 200 || decode(t, w.Body.String())["title"] != "Scenes" {
			t.Errorf("settings round trip: %d %s", w.Code, w.Body.String())
		}
	})

	f.createCollection(t, "scenes", true, nil)
	t.Run("collections", func(t *testing.T) {
		w := f.request("GET", api+"/stac/collections/scenes", "", admin, nil)
		if w.Code != 200 {
			t.Fatalf("get Collection: %d %s", w.Code, w.Body.String())
		}
		var c staccatalog.Collection
		if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		c.Document["title"] = "Renamed"
		raw, _ := json.Marshal(c)
		if w = f.request("PUT", api+"/stac/collections/scenes", string(raw), admin, nil); w.Code != 200 {
			t.Fatalf("update Collection: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("PUT", api+"/stac/collections/scenes", string(raw), admin, nil); w.Code != 409 {
			t.Errorf("stale revision: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("PUT", api+"/stac/collections/other", string(raw), admin, nil); w.Code != 400 {
			t.Errorf("changed Collection id: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("POST", api+"/stac/collections", string(raw), admin, nil); w.Code != 409 {
			t.Errorf("duplicate Collection: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("DELETE", api+"/stac/collections/missing", "", admin, nil); w.Code != 404 {
			t.Errorf("delete missing Collection: %d %s", w.Code, w.Body.String())
		}
	})

	upload := func(t *testing.T, collection, body string) *staccatalog.Job {
		t.Helper()
		w := f.request("POST", api+"/stac/imports?collection_id="+collection, body, admin, map[string]string{"Content-Type": "application/x-ndjson"})
		if w.Code != 201 {
			t.Fatalf("import: %d %s", w.Code, w.Body.String())
		}
		var j staccatalog.Job
		if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
			t.Fatal(err)
		}
		if j.Status != "ready" {
			t.Fatalf("import status %q", j.Status)
		}
		return &j
	}
	t.Run("imports", func(t *testing.T) {
		j := upload(t, "scenes", stacItemJSON("scenes", "a")+"\n"+stacItemJSON("scenes", "b")+"\n")
		w := f.request("GET", api+"/stac/imports/"+j.ID+"/preview", "", admin, nil)
		if w.Code != 200 || len(decode(t, w.Body.String())["items"].([]any)) != 2 {
			t.Fatalf("preview: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("GET", api+"/stac/collections/scenes/items/a", "", admin, nil); w.Code != 404 {
			t.Errorf("unpublished Item visible: %d", w.Code)
		}
		if w = f.request("POST", api+"/stac/imports/"+j.ID+"/publish", `{"upsert":false}`, admin, nil); w.Code != 200 {
			t.Fatalf("publish: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("GET", api+"/stac/collections/scenes/items", "", admin, nil); w.Code != 200 || len(decode(t, w.Body.String())["items"].([]any)) != 2 {
			t.Errorf("published Items: %d %s", w.Code, w.Body.String())
		}
		duplicate := upload(t, "scenes", stacItemJSON("scenes", "a"))
		if w = f.request("POST", api+"/stac/imports/"+duplicate.ID+"/publish", `{"upsert":false}`, admin, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "upsert") {
			t.Errorf("duplicate without upsert: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("POST", api+"/stac/imports/"+duplicate.ID+"/publish", `{"upsert":true}`, admin, nil); w.Code != 200 {
			t.Errorf("upsert: %d %s", w.Code, w.Body.String())
		}
		cancelled := upload(t, "scenes", stacItemJSON("scenes", "c"))
		if w = f.request("DELETE", api+"/stac/imports/"+cancelled.ID, "", admin, nil); w.Code != 204 {
			t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("POST", api+"/stac/imports/"+cancelled.ID+"/publish", `{}`, admin, nil); w.Code != 400 {
			t.Errorf("publish cancelled: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("POST", api+"/stac/imports?collection_id=missing", stacItemJSON("missing", "x"), admin, nil); w.Code != 404 {
			t.Errorf("import into missing Collection: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("POST", api+"/stac/imports?collection_id=scenes", `{"type":"Feature"`, admin, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "job_id") {
			t.Errorf("malformed import: %d %s", w.Code, w.Body.String())
		}
		if w = f.request("GET", api+"/stac/jobs/unknown", "", admin, nil); w.Code != 404 {
			t.Errorf("unknown job: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("bindings", func(t *testing.T) {
		f.createCollection(t, "linked", true, nil)
		body := `{"collection_id":"linked","revision":1,"binding":{"service_id":"missing","resource_id":"missing","resource_kind":"layer","mode":"dataset"}}`
		if w := f.request("POST", api+"/stac/bindings", body, admin, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "missing or disabled") {
			t.Errorf("binding to missing source: %d %s", w.Code, w.Body.String())
		}
		if w := f.request("POST", api+"/stac/bindings/unknown/refresh", "", admin, nil); w.Code != 404 {
			t.Errorf("refresh unknown binding: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("local assets", func(t *testing.T) {
		file := filepath.Join(f.root, "scene.tif")
		database := filepath.Join(f.root, "copy.duckdb")
		for _, path := range []string{file, database} {
			if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		bind := func(principal *identity.Identity, path string, authorized bool) (int, string) {
			raw, _ := json.Marshal(map[string]any{"collection_id": "scenes", "item_id": "a", "key": "data", "path": path, "media_type": "image/tiff", "whole_file_authorized": authorized})
			w := f.request("POST", api+"/stac/assets", string(raw), principal, nil)
			return w.Code, w.Body.String()
		}
		if code, body := bind(stacSuperAdmin, file, false); code != 400 {
			t.Errorf("unauthorized whole file: %d %s", code, body)
		}
		if code, body := bind(admin, file, true); code != 403 {
			t.Errorf("workspace admin arbitrary file: %d %s", code, body)
		}
		if code, body := bind(stacSuperAdmin, database, true); code != 400 {
			t.Errorf("database container: %d %s", code, body)
		}
		if code, body := bind(stacSuperAdmin, file, true); code != 201 {
			t.Fatalf("super admin binding: %d %s", code, body)
		}
		if w := f.request("GET", "/base/workspaces/one/stac/collections/scenes/items/a/assets/data", "", nil, nil); w.Code != 200 || w.Body.String() != "data" {
			t.Errorf("download: %d %s", w.Code, w.Body.String())
		}
		if w := f.request("DELETE", api+"/stac/assets?collection_id=scenes&item_id=a&key=data", "", admin, nil); w.Code != 204 {
			t.Errorf("unbind: %d %s", w.Code, w.Body.String())
		}
		if w := f.request("GET", "/base/workspaces/one/stac/collections/scenes/items/a/assets/data", "", nil, nil); w.Code != 404 {
			t.Errorf("unbound download: %d", w.Code)
		}
	})
}
