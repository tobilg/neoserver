package mgmt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/stylegraphics"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failingSessionWrites struct{ *store.DuckDBStore }

func (s failingSessionWrites) RevokeBrowserSession(context.Context, string, time.Time) error {
	return errors.New("simulated storage write failure")
}
func (s failingSessionWrites) RotateBrowserSession(context.Context, string, string, string, string, time.Time, time.Time) error {
	return errors.New("simulated storage write failure")
}

func TestSessionWriteFailuresAreRetryable(t *testing.T) {
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	cfg := testConfig()
	cfg.Auth.Users = map[string]string{"operator": "pw"}
	cfg.Auth.Session.IdleTimeoutSec = 3600
	now := time.Now().UTC()
	_, err = catalog.CreateBrowserSession(context.Background(), store.BrowserSession{TokenHash: identity.HashSessionToken("token"), CSRFHash: identity.HashSessionToken("csrf"), Subject: "operator", AuthMethod: "basic", CredentialFingerprint: identity.CredentialFingerprint("operator", "pw"), Roles: map[string]string{"*": "super_admin"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	broken := failingSessionWrites{catalog}
	h := &handler{store: broken, cfg: cfg}
	router := identity.Middleware(identity.MiddlewareConfig{Store: broken, Session: &identity.SessionConfig{}, BasicAuthUsers: cfg.Auth.Users})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/logout":
			h.logout(w, r)
		case "/api/v1/auth/refresh":
			h.refreshSession(w, r)
		default:
			if principal, ok := identity.FromContext(r.Context()); !ok || principal.SessionID == "" {
				w.WriteHeader(401)
			} else {
				w.WriteHeader(200)
			}
		}
	}))
	for _, path := range []string{"/api/v1/auth/refresh", "/api/v1/auth/logout", "/api/v1/auth/me"} {
		method := "POST"
		if strings.HasSuffix(path, "me") {
			method = "GET"
		}
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "token"})
		r.Header.Set("X-CSRF-Token", "csrf")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		expected := http.StatusServiceUnavailable
		if method == "GET" {
			expected = http.StatusOK
		}
		if w.Code != expected || len(w.Result().Cookies()) != 0 {
			t.Fatalf("%s: status=%d body=%s cookies=%v", path, w.Code, w.Body, w.Result().Cookies())
		}
	}
	// Recovery uses the retained cookie to retry durable revocation.
	h.store = catalog
	r := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "token"})
	r.Header.Set("X-CSRF-Token", "csrf")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 204 || len(w.Result().Cookies()) != 2 {
		t.Fatalf("retry logout: %d %s", w.Code, w.Body)
	}
	r = httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "token"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("revoked cookie replay succeeded: %d", w.Code)
	}
}

type delayedAssetCommit struct {
	*store.DuckDBStore
	first, release chan struct{}
	redHash        string
}

func (s *delayedAssetCommit) UpsertStyleAsset(ctx context.Context, input store.UpsertStyleAssetInput) (*store.StyleAsset, error) {
	if input.SHA256 == s.redHash {
		close(s.first)
		<-s.release
	}
	return s.DuckDBStore.UpsertStyleAsset(ctx, input)
}
func TestConcurrentAssetCommitsKeepPayloadIdentity(t *testing.T) {
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	ws, err := catalog.CreateWorkspace(context.Background(), store.CreateWorkspaceInput{Name: "review"})
	if err != nil {
		t.Fatal(err)
	}
	red := `<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2" fill="red"/></svg>`
	blue := strings.ReplaceAll(red, "red", "blue")
	sum := sha256.Sum256([]byte(red))
	delayed := &delayedAssetCommit{catalog, make(chan struct{}), make(chan struct{}), hex.EncodeToString(sum[:])}
	cfg := testConfig()
	cfg.WMS.StyleAssetPath = filepath.Join(root, "assets")
	h := &handler{store: delayed, cfg: cfg}
	r := chi.NewRouter()
	r.Put("/{workspace}/{asset}", h.putStyleAsset)
	run := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/review/marker.svg", strings.NewReader(body))
		req.Header.Set("Content-Type", "image/svg+xml")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- run(red) }()
	select {
	case <-delayed.first:
	case w := <-done:
		t.Fatalf("first request did not reach store: %d %s", w.Code, w.Body)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	second := run(blue)
	close(delayed.release)
	first := <-done
	asset, err := catalog.GetStyleAsset(context.Background(), ws.ID, "marker.svg")
	if err != nil {
		t.Fatal(err)
	}
	body, err := stylegraphics.ReadAssetObject(filepath.Join(cfg.WMS.StyleAssetPath, ws.ID), "marker.svg", asset.SHA256, 1024)
	if err != nil {
		t.Fatal(err)
	}
	actual := sha256.Sum256(body)
	if first.Code != 201 || second.Code != 201 || asset.SHA256 != hex.EncodeToString(actual[:]) || asset.SizeBytes != int64(len(body)) {
		t.Fatalf("inconsistent asset: first=%d second=%d asset=%+v bytes=%s", first.Code, second.Code, asset, body)
	}
	for _, payload := range []string{red, blue} {
		sum := sha256.Sum256([]byte(payload))
		if _, err := stylegraphics.ReadAssetObject(filepath.Join(cfg.WMS.StyleAssetPath, ws.ID), "marker.svg", hex.EncodeToString(sum[:]), 1024); err != nil {
			t.Fatalf("in-flight snapshot payload lost: %v", err)
		}
	}
}
