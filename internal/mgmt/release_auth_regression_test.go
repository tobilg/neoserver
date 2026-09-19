package mgmt

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/audit"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
)

func TestReleaseAuthenticationRefreshAndAudit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	cfg := testConfig()
	cfg.Auth.Enabled, cfg.Auth.Method, cfg.Auth.DefaultRole = true, "basic", "super_admin"
	cfg.Auth.Users = map[string]string{"operator": "test-password"}
	cfg.Auth.Session.TTLSec, cfg.Auth.Session.IdleTimeoutSec = 7200, 3600
	manager, err := audit.Open(ctx, conf.Audit{Enabled: true, DatabasePath: filepath.Join(root, "audit.db"), RetentionDays: 90}, "abc123", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(ctx)
	h := &handler{cfg: cfg, store: catalog, loginRate: newLoginRateLimiter()}
	auth := identity.Middleware(identity.MiddlewareConfig{Store: catalog, Session: &identity.SessionConfig{}, BasicAuthUsers: cfg.Auth.Users})
	router := manager.Middleware(auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			h.login(w, r)
		case "/api/v1/auth/refresh":
			h.refreshSession(w, r)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})))
	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"method":"password","username":"operator","password":"test-password"}`)))
	if login.Code != 201 {
		t.Fatalf("login %d: %s", login.Code, login.Body)
	}
	oldCookies := login.Result().Cookies()
	request := func(method, path string, cookies []*http.Cookie, csrf bool) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
			if csrf && cookie.Name == identity.CSRFCookieName {
				r.Header.Set("X-CSRF-Token", cookie.Value)
			}
		}
		return r
	}
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, request("POST", "/api/v1/workspaces", oldCookies, false))
	if denied.Code != 403 {
		t.Fatalf("missing CSRF: %d", denied.Code)
	}
	foreign := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"foreign","sub":"untrusted"}`)) + ".AA"
	for _, credential := range []string{"Bearer " + foreign, "Unknown credentials"} {
		r := request("GET", "/public", oldCookies, false)
		r.Header.Set("Authorization", credential)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, r)
		if response.Code != 401 {
			t.Fatalf("unhandled explicit credential downgraded: %d", response.Code)
		}
	}
	// Barrier after validation deterministically recreates the original race.
	const count = 8
	var ready, finished sync.WaitGroup
	ready.Add(count)
	finished.Add(count)
	responses := make([]*httptest.ResponseRecorder, count)
	concurrent := manager.Middleware(auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ready.Done()
		ready.Wait()
		h.refreshSession(w, r)
	})))
	for i := range count {
		go func() {
			defer finished.Done()
			responses[i] = httptest.NewRecorder()
			concurrent.ServeHTTP(responses[i], request("POST", "/api/v1/auth/refresh", oldCookies, true))
		}()
	}
	finished.Wait()
	winners := 0
	var newCookies []*http.Cookie
	for _, response := range responses {
		if response.Code == 200 {
			winners++
			newCookies = response.Result().Cookies()
		} else if response.Code != 409 {
			t.Fatalf("refresh: %d %s", response.Code, response.Body)
		}
	}
	if winners != 1 {
		t.Fatalf("successful rotations = %d", winners)
	}
	for _, cookies := range [][]*http.Cookie{oldCookies, newCookies} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request("POST", "/api/v1/workspaces", cookies, true))
		if response.Code != 200 {
			t.Fatalf("in-flight/current session invalid: %d", response.Code)
		}
	}
	coalesced := httptest.NewRecorder()
	router.ServeHTTP(coalesced, request("POST", "/api/v1/auth/refresh", newCookies, true))
	if coalesced.Code != 200 || len(coalesced.Result().Cookies()) != 0 {
		t.Fatalf("refresh overlap rotated again: %d", coalesced.Code)
	}
	events, err := manager.List(ctx, audit.Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	foundLogin, found401, found403 := false, false, false
	for _, event := range events {
		if event.Operation == "login" {
			foundLogin = event.Principal == "operator" && event.AuthMethod == "basic" && event.Security
		}
		if event.Status == 401 {
			found401 = true
			if event.Principal != "" {
				t.Fatalf("unverified attribution: %+v", event)
			}
		}
		if event.Status == 403 {
			found403 = event.Security
		}
	}
	if !foundLogin || !found401 || !found403 {
		t.Fatalf("audit outcomes missing: %+v", events)
	}
	for _, cookie := range newCookies {
		if cookie.Name == identity.SessionCookieName {
			session, err := catalog.GetBrowserSessionByTokenHash(ctx, identity.HashSessionToken(cookie.Value))
			if err != nil {
				t.Fatal(err)
			}
			if err := catalog.RevokeBrowserSession(ctx, session.ID, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, cookies := range [][]*http.Cookie{oldCookies, newCookies} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request("GET", "/public", cookies, false))
		if response.Code != 401 {
			t.Fatalf("revocation did not revoke both generations: %d", response.Code)
		}
	}
}
