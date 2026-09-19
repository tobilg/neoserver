package identity

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
)

func TestSignedOIDCBrowserAndAccessTokenPoliciesAreSeparate(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator := &OIDCValidator{store: sessionTestStore(t), config: OIDCConfig{RequiredScopes: []string{"maps:read"}}, verifier: oidc.NewVerifier("https://issuer.example", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: "console", SupportedSigningAlgs: []string{"ES256"}})}
	for _, scope := range []any{nil, 7, "", "maps:write", "maps:read"} {
		claims := map[string]any{"iss": "https://issuer.example", "aud": "console", "sub": "operator", "exp": time.Now().Add(time.Hour).Unix()}
		if scope != nil {
			claims["scope"] = scope
		}
		payload, _ := json.Marshal(claims)
		signed, err := signer.Sign(payload)
		if err != nil {
			t.Fatal(err)
		}
		token, err := signed.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := validator.ValidateIDToken(context.Background(), token); err != nil {
			t.Fatalf("valid ID token rejected: %v", err)
		}
		_, err = validator.Validate(context.Background(), token)
		if (err == nil) != (scope == "maps:read") {
			t.Fatalf("scope %v API result=%v", scope, err)
		}
	}
}
