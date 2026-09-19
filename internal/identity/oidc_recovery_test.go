package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

func TestOIDCDiscoveryRecoversWithoutRebuildingMiddleware(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var available atomic.Bool
	var discoveryCalls atomic.Int32
	var issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			discoveryCalls.Add(1)
			if !available.Load() {
				http.Error(w, "unavailable", 503)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"ES256"}})
		} else {
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, Algorithm: "ES256", Use: "sig"}}})
		}
	}))
	defer provider.Close()
	issuer = provider.URL
	s := sessionTestStore(t)
	cfg := OIDCConfig{IssuerURL: issuer, ClientID: "console"}
	recovering := NewRecoveringOIDCValidator(s, cfg)
	app := Middleware(MiddlewareConfig{Store: s, OIDCConfig: &cfg, OIDCValidator: recovering, RequireAuth: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	if err := recovering.Health(context.Background()); err == nil {
		t.Fatal("unavailable provider reported ready")
	}
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, nil)
	payload, _ := json.Marshal(map[string]any{"iss": issuer, "aud": "console", "sub": "operator", "exp": time.Now().Add(time.Hour).Unix()})
	signed, _ := signer.Sign(payload)
	token, _ := signed.CompactSerialize()
	request := func() int {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w.Code
	}
	if code := request(); code != 503 {
		t.Fatalf("unavailable status=%d", code)
	}
	if discoveryCalls.Load() != 1 {
		t.Fatal("backoff was not respected")
	}
	available.Store(true)
	recovering.mu.Lock()
	recovering.nextAttempt = time.Time{}
	recovering.mu.Unlock()
	if code := request(); code != 204 {
		t.Fatalf("recovered status=%d", code)
	}
	if err := recovering.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if discoveryCalls.Load() != 2 {
		t.Fatal("healthy validator was not reused")
	}
}

func TestOIDCDiscoveryHonorsCancellation(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer provider.Close()
	v := NewRecoveringOIDCValidator(nil, OIDCConfig{IssuerURL: provider.URL, ClientID: "console"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := v.Health(ctx); err == nil {
		t.Fatal("stalled discovery reported ready")
	}
	if time.Since(start) > time.Second {
		t.Fatal("discovery ignored deadline")
	}
}
