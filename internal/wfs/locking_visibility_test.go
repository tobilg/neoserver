package wfs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/workspace"
)

type lockVisibilitySource struct {
	*pagingContractSource
	metadata int
}

func (s *lockVisibilitySource) GetLayerInfo(ctx context.Context, name string) (*datasource.LayerInfo, error) {
	s.metadata++
	return s.pagingContractSource.GetLayerInfo(ctx, name)
}

func TestLockingRequiresPublicationVisibilityBeforeSourceAccess(t *testing.T) {
	for _, operation := range []string{"LockFeature", "GetFeatureWithLock"} {
		for _, method := range []string{"GET", "POST", "GET-stored", "POST-stored"} {
			for _, restriction := range []string{"role", "custom writer", "disabled layer", "disabled service", "SQL view", "invalid SQL view", "allowed"} {
				t.Run(operation+"/"+method+"/"+restriction, func(t *testing.T) {
					h, ws, base := pagingFixture()
					ds := &lockVisibilitySource{pagingContractSource: base}
					ws.Services["source"].DataSource = ds
					h.state = NewRuntimeState(h.cfg.WFS, nil, h.logger)
					defer h.state.Close()
					layer := ws.Services["source"].Layers["roads"]
					layer.PublicID = "public.roads"
					delete(ws.Services["source"].Layers, "roads")
					ws.Services["source"].Layers[layer.PublicID] = layer
					switch restriction {
					case "role", "custom writer":
						layer.Public = false
						layer.AllowedRoles = []string{"admin"}
					case "disabled layer":
						layer.Enabled = false
					case "disabled service":
						ws.Services["source"].Enabled = false
					case "SQL view":
						layer.IsSQLView = true
						layer.SQLViewConfig = &workspace.SQLViewConfig{ReadOnly: true}
					case "invalid SQL view":
						layer.IsSQLView = true
					}
					target := "http://example.test/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=" + operation + "&TYPENAMES=public.roads&COUNT=1"
					body := ""
					if method == "GET-stored" {
						target = "http://example.test/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=" + operation + "&STOREDQUERY_ID=urn:ogc:def:query:OGC-WFS::GetFeatureById&ID=app:public.roads.1"
					}
					if strings.HasPrefix(method, "POST") {
						target = "http://example.test/wfs"
						query := `<Query typeNames="public.roads"/>`
						if method == "POST-stored" {
							query = `<StoredQuery id="urn:ogc:def:query:OGC-WFS::GetFeatureById"><Parameter name="ID">app:public.roads.1</Parameter></StoredQuery>`
						}
						body = `<` + operation + ` service="WFS" version="2.0.0" count="1">` + query + `</` + operation + `>`
					}
					r := httptest.NewRequest(strings.Split(method, "-")[0], target, strings.NewReader(body))
					ctx := workspace.WithWorkspace(r.Context(), ws)
					ctx = identity.WithIdentity(ctx, &identity.Identity{Subject: "writer", Roles: map[string]string{ws.ID: "editor"}})
					enforcer, err := rbac.NewEnforcerWithDefaults(rbac.NewMemoryAdapter())
					if err != nil {
						t.Fatal(err)
					}
					if restriction == "custom writer" {
						ctx = identity.WithIdentity(ctx, &identity.Identity{Subject: "custom", Roles: map[string]string{ws.ID: "custom"}})
						if err := enforcer.AddOperationPolicy("custom", ws.ID, "wfs", operation, rbac.ActionWrite); err != nil {
							t.Fatal(err)
						}
					}
					w := httptest.NewRecorder()
					rbac.RequireServiceOperation(enforcer, "wfs")(http.HandlerFunc(h.handleWFS)).ServeHTTP(w, r.WithContext(ctx))
					if restriction == "allowed" {
						if w.Code != 200 || len(base.queries) != 1 {
							t.Fatalf("allowed: %d %s", w.Code, w.Body)
						}
					} else if w.Code == 200 || len(base.queries) != 0 || ds.metadata != 0 {
						t.Fatalf("read before authorization: %d %s queries=%d metadata=%d", w.Code, w.Body, len(base.queries), ds.metadata)
					}
					if restriction != "allowed" && h.state.Locks.IsFeatureLocked(ws.ID, layer.PublicID, "1") {
						t.Fatal("denied caller acquired lock")
					}
				})
			}
		}
	}
}

func TestLockingPreflightsEveryQueryAndRechecksChangedVisibility(t *testing.T) {
	h, ws, base := pagingFixture()
	ds := &lockVisibilitySource{pagingContractSource: base}
	ws.Services["source"].DataSource = ds
	h.state = NewRuntimeState(h.cfg.WFS, nil, h.logger)
	defer h.state.Close()
	ws.Services["source"].Layers["private"] = &workspace.Layer{ID: "private", PublicID: "private", SourceLayer: "roads", Enabled: true, AllowedRoles: []string{"admin"}}
	e, err := rbac.NewEnforcerWithDefaults(rbac.NewMemoryAdapter())
	if err != nil {
		t.Fatal(err)
	}
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/wfs", strings.NewReader(body))
		ctx := workspace.WithWorkspace(r.Context(), ws)
		ctx = identity.WithIdentity(ctx, &identity.Identity{Subject: "writer", Roles: map[string]string{ws.ID: "editor"}})
		w := httptest.NewRecorder()
		rbac.RequireServiceOperation(e, "wfs")(http.HandlerFunc(h.handleWFS)).ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	w := call(`<LockFeature service="WFS" version="2.0.0"><Query typeNames="roads"/><Query typeNames="private"/></LockFeature>`)
	if w.Code == 200 || ds.metadata != 0 || len(base.queries) != 0 || h.state.Locks.IsFeatureLocked(ws.ID, "roads", "1") {
		t.Fatalf("multi-query preflight: %d %s", w.Code, w.Body)
	}
	body := `<GetFeatureWithLock service="WFS" version="2.0.0"><Query typeNames="roads"/></GetFeatureWithLock>`
	w = call(body)
	if w.Code != 200 {
		t.Fatalf("initial lock: %d %s", w.Code, w.Body)
	}
	layer := ws.Services["source"].Layers["roads"]
	layer.Public = false
	layer.AllowedRoles = []string{"admin"}
	base.queries = nil
	ds.metadata = 0
	w = call(body)
	if w.Code == 200 || ds.metadata != 0 || len(base.queries) != 0 {
		t.Fatalf("revoked visibility: %d %s", w.Code, w.Body)
	}
}
