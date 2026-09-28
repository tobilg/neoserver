package identity

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestBasicAuthGuessingIsThrottledOnProtocolEndpoints(t *testing.T) {
	middleware := Middleware(MiddlewareConfig{Store: newMockStore(), BasicAuthUsers: map[string]string{"ada": "correct"}, DefaultRole: "viewer"})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	attempt := func(password string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/workspaces/demo/wms?SERVICE=WMS&REQUEST=GetCapabilities", nil)
		request.RemoteAddr = "198.51.100.1:1234"
		request.SetBasicAuth("ada", password)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	for range clientFailureThreshold {
		if code := attempt("guess").Code; code != http.StatusUnauthorized {
			t.Fatalf("wrong password: %d", code)
		}
	}
	blocked := attempt("correct")
	if blocked.Code != http.StatusTooManyRequests || blocked.Header().Get("Retry-After") == "" {
		t.Fatalf("throttled client: %d Retry-After=%q", blocked.Code, blocked.Header().Get("Retry-After"))
	}
}

func TestStaleSessionCookieReadsPublicProtocolEndpointsAnonymously(t *testing.T) {
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	middleware := Middleware(MiddlewareConfig{Store: catalog, Session: &SessionConfig{}})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); ok {
			t.Error("a stale session produced an identity")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		method, target string
		want           int
	}{
		{http.MethodGet, "/workspaces/demo/wms?SERVICE=WMS&REQUEST=GetMap", http.StatusNoContent},
		{http.MethodGet, "/workspaces/demo/ogc/collections/roads/items", http.StatusNoContent},
		{http.MethodGet, "/api/v1/workspaces", http.StatusUnauthorized},
		{http.MethodGet, "/workspaces/demo/wfs?SERVICE=WFS&REQUEST=LockFeature", http.StatusUnauthorized},
		{http.MethodPost, "/workspaces/demo/wfs", http.StatusUnauthorized},
	} {
		request := httptest.NewRequest(tc.method, tc.target, nil)
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "expired-session"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.target, response.Code, tc.want)
		}
	}
}
