package mgmt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
)

func TestConsoleConfigLoginMethodsAndSecretSafety(t *testing.T) {
	tests := []struct {
		name, method string
		enabled      bool
		users        map[string]string
		issuer       string
		clientID     string
		wantPassword bool
		wantOIDC     bool
	}{
		{name: "tokens only", method: "none"},
		{name: "basic enabled", enabled: true, method: "basic", users: map[string]string{"ada": "secret-password"}, wantPassword: true},
		{name: "basic without users", enabled: true, method: "basic"},
		{name: "oidc independent", issuer: "https://idp.example", clientID: "console", wantOIDC: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := conf.Config{Server: conf.Server{BasePath: "/geo"}, Auth: conf.Auth{Enabled: test.enabled, Method: test.method,
				ApiKey: "static-api-secret", Users: test.users, OIDC: conf.OIDCConfig{IssuerURL: test.issuer, ClientID: test.clientID, BrowserLoginEnabled: true}},
				Store: conf.Store{EncryptionKey: "catalog-secret"}}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "https://gis.example/geo/api/v1/console/config", nil)
			(&handler{cfg: cfg}).consoleConfig(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d", recorder.Code)
			}
			var body struct {
				Auth struct {
					Password bool `json:"password_login"`
					Token    bool `json:"token_login"`
					OIDC     struct {
						Enabled bool `json:"enabled"`
					} `json:"oidc"`
				} `json:"auth"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Auth.Password != test.wantPassword || !body.Auth.Token || body.Auth.OIDC.Enabled != test.wantOIDC {
				t.Fatalf("methods=%+v", body.Auth)
			}
			for _, secret := range []string{"secret-password", "static-api-secret", "catalog-secret"} {
				if strings.Contains(recorder.Body.String(), secret) {
					t.Fatalf("console config exposed %q", secret)
				}
			}
			if test.wantOIDC && !strings.Contains(recorder.Body.String(), `https://gis.example/geo/admin/auth/callback`) {
				t.Fatalf("wrong redirect URI: %s", recorder.Body.String())
			}
		})
	}
}

func TestLoginRateLimiterBackoff(t *testing.T) {
	limiter := newLoginRateLimiter()
	now := time.Now()
	for attempt := 1; attempt <= 4; attempt++ {
		if delay := limiter.fail("client", now); delay > 0 {
			t.Fatalf("attempt %d unexpectedly blocked for %s", attempt, delay)
		}
	}
	if delay := limiter.fail("client", now); delay < 29*time.Second || delay > 31*time.Second {
		t.Fatalf("fifth failure delay=%s, want about 30s", delay)
	}
	if delay := limiter.allowed("client", now); delay < 29*time.Second {
		t.Fatalf("allowed delay=%s", delay)
	}
	limiter.success("client")
	if delay := limiter.allowed("client", now); delay != 0 {
		t.Fatalf("successful login did not reset limiter: %s", delay)
	}
}

func TestBrowserScopesMergeRequiredScopes(t *testing.T) {
	got := browserScopes([]string{"openid", "profile"}, []string{"email", "profile"})
	want := []string{"openid", "profile", "email"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("browserScopes() = %v, want %v", got, want)
	}
}
