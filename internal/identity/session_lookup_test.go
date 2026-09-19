package identity

import (
	"context"
	"github.com/tobilg/neoserver/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type noKeyEnumeration struct{ *store.DuckDBStore }

func (*noKeyEnumeration) ListAPIKeys(context.Context, *string) ([]*store.APIKey, error) {
	panic("session validation must not enumerate API keys")
}

func TestAPIKeySessionLookupAndImmediateRevocation(t *testing.T) {
	catalog := sessionTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	key, err := catalog.CreateAPIKey(ctx, store.CreateAPIKeyInput{RoleID: "admin", Name: "session"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = catalog.CreateBrowserSession(ctx, store.BrowserSession{TokenHash: HashSessionToken("session"), CSRFHash: "csrf", Subject: "test", AuthMethod: string(AuthMethodAPIKey), CredentialID: key.ID, Roles: map[string]string{"*": "admin"}, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	validator := NewSessionValidator(&noKeyEnumeration{catalog}, catalog, SessionConfig{IdleTimeout: time.Hour}, "", nil, nil)
	request := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session"})
	if id, err := validator.ValidateRequest(ctx, request); err != nil || id == nil || id.GetWorkspaceRole("any") != "admin" {
		t.Fatalf("session validation: %v %v", id, err)
	}
	if err := catalog.RevokeAPIKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if id, err := validator.ValidateRequest(ctx, request); err == nil || id != nil {
		t.Fatal("revoked key session accepted")
	}
}
