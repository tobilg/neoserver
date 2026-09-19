package wms

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestGetMapAuthorizesBeforeWarmCacheAndNotModified(t *testing.T) {
	manager, err := cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	h, ws := rasterWMSFixture()
	h.cache = manager
	coverage := ws.Services["svc"].Coverages["elevation"]
	coverage.Public, coverage.AllowedRoles = false, []string{"admin"}
	endpoint := "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=elevation&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180&WIDTH=32&HEIGHT=16&FORMAT=image/png"
	run := func(roles map[string]string, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, endpoint, nil)
		ctx := workspace.WithWorkspace(r.Context(), ws)
		if roles != nil {
			ctx = identity.WithIdentity(ctx, &identity.Identity{Roles: roles})
		}
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		h.handleWMS(w, r.WithContext(ctx))
		return w
	}
	if w := run(nil, ""); w.Code != 404 {
		t.Fatalf("cold anonymous: %d", w.Code)
	}
	admin := run(map[string]string{ws.ID: "admin"}, "")
	if admin.Code != 200 {
		t.Fatalf("admin: %d %s", admin.Code, admin.Body.String())
	}
	if admin.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("restricted image marked public")
	}
	for _, roles := range []map[string]string{nil, {"unrelated": "admin"}, {ws.ID: "viewer"}} {
		for _, etag := range []string{"", admin.Header().Get("ETag")} {
			if w := run(roles, etag); w.Code != 404 {
				t.Fatalf("warm denied caller: %d", w.Code)
			}
		}
	}
	coverage.AllowedRoles = []string{"other"}
	if w := run(map[string]string{ws.ID: "admin"}, admin.Header().Get("ETag")); w.Code != 404 {
		t.Fatalf("revoked: %d", w.Code)
	}
}
