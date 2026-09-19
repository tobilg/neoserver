package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

func sessionTestStore(t *testing.T) *store.DuckDBStore {
	t.Helper()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close() })
	return catalog
}

func createPasswordSession(t *testing.T, catalog *store.DuckDBStore, token, csrf string, now time.Time) *store.BrowserSession {
	t.Helper()
	session, err := catalog.CreateBrowserSession(context.Background(), store.BrowserSession{
		TokenHash: HashSessionToken(token), CSRFHash: HashSessionToken(csrf), Subject: "ada", AuthMethod: "password",
		Roles: map[string]string{"workspace": "admin"}, CredentialFingerprint: CredentialFingerprint("ada", "correct"),
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(12 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestSessionValidatorCSRFAndExpiry(t *testing.T) {
	catalog := sessionTestStore(t)
	now := time.Now().UTC()
	createPasswordSession(t, catalog, "session-token", "csrf-token", now)
	validator := NewSessionValidator(catalog, catalog, SessionConfig{IdleTimeout: time.Hour}, "", map[string]string{"ada": "correct"}, nil)

	get := httptest.NewRequest(http.MethodGet, "/workspaces/demo/ogc", nil)
	get.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-token"})
	principal, err := validator.ValidateRequest(get.Context(), get)
	if err != nil || principal == nil || principal.Subject != "ada" {
		t.Fatalf("principal=%+v err=%v", principal, err)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
	post.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-token"})
	if _, err = validator.ValidateRequest(post.Context(), post); err == nil {
		t.Fatal("unsafe cookie request without CSRF token was accepted")
	} else if authErr, ok := err.(*AuthError); !ok || authErr.StatusCode != http.StatusForbidden {
		t.Fatalf("CSRF error=%T %+v", err, err)
	}
	post.Header.Set("X-CSRF-Token", "csrf-token")
	if _, err = validator.ValidateRequest(post.Context(), post); err != nil {
		t.Fatalf("valid CSRF token rejected: %v", err)
	}
}

func TestSessionValidatorPassiveRequestsDoNotExtendIdleExpiry(t *testing.T) {
	catalog := sessionTestStore(t)
	created := createPasswordSession(t, catalog, "session-token", "csrf-token", time.Now().UTC().Add(-2*time.Minute))
	validator := NewSessionValidator(catalog, catalog, SessionConfig{IdleTimeout: time.Hour}, "", map[string]string{"ada": "correct"}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-token"})
	principal, err := validator.ValidateRequest(request.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !principal.SessionIdleExpiresAt.Equal(created.IdleExpiresAt) {
		t.Fatalf("passive request extended idle expiry: got %s, previous %s", principal.SessionIdleExpiresAt, created.IdleExpiresAt)
	}
	persisted, err := catalog.GetBrowserSessionByTokenHash(request.Context(), HashSessionToken("session-token"))
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.IdleExpiresAt.Equal(principal.SessionIdleExpiresAt) {
		t.Fatalf("reported idle expiry %s differs from persisted %s", principal.SessionIdleExpiresAt, persisted.IdleExpiresAt)
	}
}

func TestPassiveSessionCannotBeRevivedAfterIdleExpiry(t *testing.T) {
	catalog := sessionTestStore(t)
	createPasswordSession(t, catalog, "expired", "csrf", time.Now().UTC().Add(-2*time.Hour))
	validator := NewSessionValidator(catalog, catalog, SessionConfig{IdleTimeout: time.Hour}, "", map[string]string{"ada": "correct"}, nil)
	for _, method := range []string{"GET", "POST"} {
		r := httptest.NewRequest(method, "/api/v1/auth/refresh", nil)
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "expired"})
		r.Header.Set("X-CSRF-Token", "csrf")
		if _, err := validator.ValidateRequest(r.Context(), r); err == nil {
			t.Fatal("expired session was revived")
		}
	}
}

func TestSessionCSRFProtectsKVPWriteOperations(t *testing.T) {
	catalog := sessionTestStore(t)
	createPasswordSession(t, catalog, "session-token", "csrf-token", time.Now().UTC())
	validator := NewSessionValidator(catalog, catalog, SessionConfig{IdleTimeout: time.Hour}, "", map[string]string{"ada": "correct"}, nil)
	for _, operation := range []string{"DropStoredQuery", "CreateStoredQuery", "LockFeature", "GetFeatureWithLock", "Transaction"} {
		for _, csrf := range []string{"", "wrong", "csrf-token"} {
			r := httptest.NewRequest("GET", "/maps/workspaces/demo/wfs?service=WFS&request="+operation, nil)
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-token"})
			r.Header.Set("X-CSRF-Token", csrf)
			_, err := validator.ValidateRequest(r.Context(), r)
			if (err == nil) != (csrf == "csrf-token") {
				t.Fatalf("operation=%s csrf=%q error=%v", operation, csrf, err)
			}
		}
	}
}

func TestMiddlewareExplicitCredentialDoesNotFallBackToSession(t *testing.T) {
	catalog := sessionTestStore(t)
	createPasswordSession(t, catalog, "session-token", "csrf-token", time.Now().UTC())
	nextCalled := false
	handler := Middleware(MiddlewareConfig{Store: catalog, RequireAuth: true, BasicAuthUsers: map[string]string{"ada": "correct"},
		Session: &SessionConfig{IdleTimeout: time.Hour}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-token"})
	request.Header.Set("X-API-Key", "nsk_invalid")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || nextCalled {
		t.Fatalf("status=%d nextCalled=%v", recorder.Code, nextCalled)
	}
}

func TestRevokedAPIKeyInvalidatesBrowserSession(t *testing.T) {
	catalog := sessionTestStore(t)
	ctx := context.Background()
	workspace, err := catalog.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := catalog.CreateAPIKey(ctx, store.CreateAPIKeyInput{OwnerName: "client", WorkspaceID: &workspace.ID, RoleID: "viewer", Name: "browser"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = catalog.CreateBrowserSession(ctx, store.BrowserSession{
		TokenHash: HashSessionToken("session-token"), CSRFHash: HashSessionToken("csrf-token"), Subject: "client",
		AuthMethod: string(AuthMethodAPIKey), CredentialID: created.ID, Roles: map[string]string{workspace.ID: "viewer"},
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(12 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator := NewSessionValidator(catalog, catalog, SessionConfig{IdleTimeout: time.Hour}, "", nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-token"})
	if _, err = validator.ValidateRequest(ctx, request); err != nil {
		t.Fatalf("session rejected before revocation: %v", err)
	}
	if err = catalog.RevokeAPIKey(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = validator.ValidateRequest(ctx, request); err == nil {
		t.Fatal("revoked API key session remained valid")
	}
}

func TestConfiguredRoleChangesApplyToExistingBrowserSessions(t *testing.T) {
	for _, method := range []string{"password", "static_apikey"} {
		t.Run(method, func(t *testing.T) {
			catalog := sessionTestStore(t)
			now := time.Now().UTC()
			fingerprint := CredentialFingerprint("ada", "correct")
			if method == "static_apikey" {
				fingerprint = CredentialFingerprint("static", "correct")
			}
			_, err := catalog.CreateBrowserSession(context.Background(), store.BrowserSession{TokenHash: HashSessionToken("old"), Subject: "ada", AuthMethod: method, Roles: map[string]string{"*": "super_admin"}, CredentialFingerprint: fingerprint, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			for _, role := range []string{"viewer", "admin"} {
				h := Middleware(MiddlewareConfig{Store: catalog, RequireAuth: true, DefaultRole: role, StaticAPIKey: "correct", BasicAuthUsers: map[string]string{"ada": "correct"}, Session: &SessionConfig{IdleTimeout: time.Hour}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					id, _ := FromContext(r.Context())
					_, _ = w.Write([]byte(id.Roles["*"]))
				}))
				for _, cookie := range []bool{true, false} {
					r := httptest.NewRequest("GET", "/", nil)
					if cookie {
						r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "old"})
					} else if method == "password" {
						r.SetBasicAuth("ada", "correct")
					} else {
						r.Header.Set("X-API-Key", "correct")
					}
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if w.Code != 200 || w.Body.String() != role {
						t.Fatalf("cookie=%v status=%d role=%s", cookie, w.Code, w.Body.String())
					}
				}
			}
		})
	}
}

func TestStaleSessionCookieDoesNotBlockSignInBootstrap(t *testing.T) {
	catalog := sessionTestStore(t)
	middleware := Middleware(MiddlewareConfig{Store: catalog, Session: &SessionConfig{IdleTimeout: time.Hour}})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); ok {
			t.Errorf("%s: stale cookie produced an identity", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for path, want := range map[string]int{
		"/api/v1/console/config":          http.StatusNoContent,
		"/base/api/v1/console/config":     http.StatusNoContent,
		"/api/v1/auth/login":              http.StatusNoContent,
		"/api/v1/auth/me":                 http.StatusUnauthorized,
		"/workspaces/demo/console/config": http.StatusUnauthorized,
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "from-before-a-restart"})
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != want {
			t.Errorf("%s: status %d, want %d", path, recorder.Code, want)
		}
	}
}
