package wfs

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type authorizationQueryStore struct {
	store.Store
	definition *store.WFSStoredQuery
}

func (s *authorizationQueryStore) GetWFSStoredQuery(context.Context, string, string) (*store.WFSStoredQuery, error) {
	return s.definition, nil
}

type authorizationItemSource struct{ *pagingContractSource }

func (s *authorizationItemSource) QueryByID(ctx context.Context, layer, id string, srid int) (json.RawMessage, bool, error) {
	rows, err := s.Query(ctx, layer, datasource.QueryParams{})
	return rows[0], true, err
}

func TestStoredQueriesAlwaysResolveCurrentAuthorization(t *testing.T) {
	for _, query := range []string{"urn:ogc:def:query:OGC-WFS::GetFeatureById", "custom"} {
		for _, method := range []string{"GET", "POST"} {
			t.Run(query+"/"+method, func(t *testing.T) {
				h, ws, source := pagingFixture()
				ws.Services["source"].DataSource = &authorizationItemSource{source}
				layer := ws.Services["source"].Layers["roads"]
				layer.Public, layer.AllowedRoles = false, []string{"admin"}
				queryStore := &authorizationQueryStore{definition: &store.WFSStoredQuery{QueryExpression: `<Query typeNames="roads"/>`}}
				h.store = queryStore
				var err error
				h.cache, err = cache.NewManager(cache.DefaultConfig())
				if err != nil {
					t.Fatal(err)
				}
				defer h.cache.Close()
				run := func(role string) *httptest.ResponseRecorder {
					q := url.Values{"SERVICE": {"WFS"}, "REQUEST": {"GetFeature"}, "STOREDQUERY_ID": {query}, "ID": {"roads.1"}}
					target, body := "http://example.test/wfs?"+q.Encode(), ""
					if method == "POST" {
						target, body = "http://example.test/wfs", `<GetFeature service="WFS" version="2.0.0"><StoredQuery id="`+query+`"><Parameter name="ID">roads.1</Parameter></StoredQuery></GetFeature>`
					}
					r := httptest.NewRequest(method, target, strings.NewReader(body))
					ctx := identity.WithIdentity(r.Context(), &identity.Identity{Roles: map[string]string{ws.ID: role}})
					w := httptest.NewRecorder()
					h.handleGetFeature(w, r.WithContext(ctx), ws)
					return w
				}
				for _, role := range []string{"", "admin", "admin", "", "viewer"} {
					w := run(role)
					if (w.Code == 200) != (role == "admin") {
						t.Fatalf("role %q: %d %s", role, w.Code, w.Body)
					}
					if w.Header().Get("X-Cache") == "HIT" {
						t.Fatal("stored query used shared response cache")
					}
				}
				layer.AllowedRoles = []string{"super_admin"}
				if w := run("admin"); w.Code == 200 {
					t.Fatal("revoked access returned a stored-query result")
				}
				layer.AllowedRoles = []string{"admin"}
				if query == "custom" {
					queryStore.definition.QueryExpression = `<Query typeNames="unpublished"/>`
					if w := run("admin"); w.Code == 200 {
						t.Fatal("old query definition was served")
					}
				}
				ws.Services["source"].Enabled = false
				if w := run("admin"); w.Code == 200 {
					t.Fatal("disabled service returned a stored-query result")
				}
			})
		}
	}
}

func TestFeatureCacheChecksCurrentLayerAccess(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		t.Run(method, func(t *testing.T) {
			h, ws, _ := pagingFixture()
			layer := ws.Services["source"].Layers["roads"]
			layer.Public, layer.AllowedRoles = false, []string{"admin"}
			var err error
			h.cache, err = cache.NewManager(cache.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer h.cache.Close()
			run := func(role string) *httptest.ResponseRecorder {
				target, body := "http://example.test/wfs?SERVICE=WFS&REQUEST=GetFeature&TYPENAMES=roads&COUNT=1", ""
				if method == "POST" {
					target, body = "http://example.test/wfs", `<GetFeature service="WFS" version="2.0.0" count="1"><Query typeNames="roads"/></GetFeature>`
				}
				req := httptest.NewRequest(method, target, strings.NewReader(body))
				ctx := workspace.WithWorkspace(req.Context(), ws)
				if role != "" {
					ctx = identity.WithIdentity(ctx, &identity.Identity{Subject: "test", Roles: map[string]string{ws.ID: role}})
					req.Header.Set("Authorization", "Bearer fixture")
				}
				w := httptest.NewRecorder()
				h.handleGetFeature(w, req.WithContext(ctx), ws)
				return w
			}
			if w := run(""); w.Code == 200 {
				t.Fatal("cold anonymous request allowed")
			}
			deadline := time.Now().Add(time.Second)
			for {
				w := run("admin")
				if w.Code != 200 {
					t.Fatalf("admin: %d %s", w.Code, w.Body)
				}
				if w.Header().Get("X-Cache") == "HIT" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("cache never admitted response")
				}
				time.Sleep(time.Millisecond)
			}
			for _, role := range []string{"", "viewer"} {
				if w := run(role); w.Code == 200 || strings.Contains(w.Body.String(), "control") {
					t.Fatalf("cached disclosure to %q: %d %s", role, w.Code, w.Body)
				}
			}
			layer.AllowedRoles = []string{"super_admin"}
			if w := run("admin"); w.Code == 200 {
				t.Fatal("permission revocation ignored")
			}
			layer.AllowedRoles, layer.Enabled = []string{"admin"}, false
			if w := run("admin"); w.Code == 200 {
				t.Fatal("disabled layer served")
			}
			layer.Enabled, ws.Services["source"].Enabled = true, false
			if w := run("admin"); w.Code == 200 {
				t.Fatal("disabled service served")
			}
		})
	}
}

type blockedFeatureSource struct {
	*pagingContractSource
	started, release chan struct{}
}

func (s *blockedFeatureSource) Query(ctx context.Context, layer string, p datasource.QueryParams) ([]json.RawMessage, error) {
	close(s.started)
	<-s.release
	return s.pagingContractSource.Query(ctx, layer, p)
}

func TestUnauthorizedRequestCannotJoinPrivilegedCacheFill(t *testing.T) {
	h, ws, source := pagingFixture()
	blocked := &blockedFeatureSource{source, make(chan struct{}), make(chan struct{})}
	ws.Services["source"].DataSource = blocked
	layer := ws.Services["source"].Layers["roads"]
	layer.Public, layer.AllowedRoles = false, []string{"admin"}
	var err error
	h.cache, err = cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer h.cache.Close()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("GET", "http://example.test/wfs?SERVICE=WFS&REQUEST=GetFeature&TYPENAMES=roads", nil)
		ctx := identity.WithIdentity(req.Context(), &identity.Identity{Roles: map[string]string{ws.ID: "admin"}})
		w := httptest.NewRecorder()
		h.handleGetFeature(w, req.WithContext(ctx), ws)
		done <- w
	}()
	<-blocked.started
	anonymous := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		h.handleGetFeature(w, httptest.NewRequest("GET", "http://example.test/wfs?SERVICE=WFS&REQUEST=GetFeature&TYPENAMES=roads", nil), ws)
		anonymous <- w
	}()
	select {
	case w := <-anonymous:
		if w.Code == 200 {
			t.Error("unauthorized fill shared")
		}
	case <-time.After(time.Second):
		t.Error("unauthorized caller joined pending load")
	}
	close(blocked.release)
	if w := <-done; w.Code != 200 {
		t.Fatalf("admin failed: %s", w.Body)
	}
}
