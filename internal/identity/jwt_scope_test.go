package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

func TestJWTRolesAreWorkspaceScoped(t *testing.T) {
	ctx := context.Background()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	key, err := catalog.GetActiveSigningKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	validator := NewJWTValidator(catalog)

	scoped, err := catalog.CreateToken(key, "alice", "editor", "ws-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	id, err := validator.Validate(ctx, scoped)
	if err != nil {
		t.Fatal(err)
	}
	if id.Roles["ws-1"] != "editor" || len(id.Roles) != 1 {
		t.Fatalf("scoped editor roles = %v", id.Roles)
	}

	global, err := catalog.CreateToken(key, "root", "super_admin", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if id, err = validator.Validate(ctx, global); err != nil || id.Roles["*"] != "super_admin" {
		t.Fatalf("super_admin roles = %+v %v", id, err)
	}

	// Tokens minted before workspace scoping granted their role everywhere.
	for name, payload := range map[string]jwtPayload{
		"legacy unscoped editor": {Iss: "neoserver", Sub: "legacy", Role: "editor"},
		"wildcard editor":        {Iss: "neoserver", Sub: "wild", Role: "viewer", Workspace: "*"},
		"scoped super_admin":     {Iss: "neoserver", Sub: "root", Role: "super_admin", Workspace: "ws-1"},
	} {
		payload.Exp = time.Now().Add(time.Hour).Unix()
		token := signTestJWT(t, key, payload)
		if id, err := validator.Validate(ctx, token); err == nil || id != nil {
			t.Errorf("%s: accepted with roles %+v", name, id)
		}
	}
}

func signTestJWT(t *testing.T, key *store.SigningKey, payload jwtPayload) string {
	t.Helper()
	private, err := x509.ParseECPrivateKey(key.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	header, _ := json.Marshal(jwtHeader{Alg: "ES256", Typ: "JWT"})
	body, _ := json.Marshal(payload)
	message := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	digest := sha256.Sum256([]byte(message))
	r, s, err := ecdsa.Sign(rand.Reader, private, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return message + "." + base64.RawURLEncoding.EncodeToString(signature)
}
