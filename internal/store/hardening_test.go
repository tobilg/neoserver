package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

// TestValidateToken_RejectsNonES256Alg verifies that the JWT algorithm is pinned to
// ES256 — a token advertising a different algorithm (e.g. "none") is rejected before
// any signature processing.
func TestValidateToken_RejectsNonES256Alg(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(
		`{"iss":"neoserver","sub":"attacker","role":"super_admin","exp":%d}`,
		time.Now().Add(time.Hour).Unix(),
	)))
	token := header + "." + payload + "." // empty signature

	if _, err := store.ValidateToken(ctx, token); err == nil {
		t.Fatal("expected token with alg=none to be rejected")
	}
}

// TestValidateToken_ValidES256 is a positive control: a properly signed ES256 token
// still validates.
func TestValidateToken_ValidES256(t *testing.T) {
	store, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	signingKey, err := store.GetActiveSigningKey(ctx)
	if err != nil {
		t.Fatalf("GetActiveSigningKey: %v", err)
	}
	token, err := store.CreateToken(signingKey, "user", "viewer", "ws-1", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if _, err := store.ValidateToken(ctx, token); err != nil {
		t.Fatalf("expected valid ES256 token to validate, got %v", err)
	}
}
