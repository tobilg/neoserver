package store

import (
	"context"
	"testing"
	"time"
)

func TestBrowserSessionLifecycle(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	created, err := catalog.CreateBrowserSession(ctx, BrowserSession{
		TokenHash: "token-one", CSRFHash: "csrf-one", Subject: "ada", AuthMethod: "password",
		Roles: map[string]string{"demo": "admin"}, Claims: map[string]any{"groups": []string{"gis-admins"}},
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(12 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.GetBrowserSessionByTokenHash(ctx, "token-one")
	if err != nil || loaded.Subject != "ada" || loaded.Roles["demo"] != "admin" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if err = catalog.RotateBrowserSession(ctx, created.ID, created.TokenHash, "token-two", "csrf-two", now.Add(time.Minute), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if previous, err := catalog.GetBrowserSessionByTokenHash(ctx, "token-one"); err != nil || previous.PreviousTokenHash != "token-one" {
		t.Fatalf("in-flight token overlap missing: %v", err)
	}
	if err = catalog.RevokeBrowserSession(ctx, created.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	deleted, err := catalog.DeleteExpiredBrowserSessions(ctx, now.Add(3*time.Minute))
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
}

// A clock with nanosecond resolution (Linux) writes a timestamp the TIMESTAMP
// column cannot hold, so the session returned by the write used to disagree
// with the row a later read returns. macOS hides this: its clock is already
// microsecond-granular.
func TestCreateBrowserSessionReturnsStoredPrecision(t *testing.T) {
	catalog, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(274 * time.Nanosecond)
	created, err := catalog.CreateBrowserSession(ctx, BrowserSession{
		TokenHash: "token-precision", CSRFHash: "csrf-precision", Subject: "ada", AuthMethod: "password",
		Roles:     map[string]string{"demo": "admin"},
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(12 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := catalog.GetBrowserSessionByTokenHash(ctx, "token-precision")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		name             string
		returned, stored time.Time
	}{
		{"created_at", created.CreatedAt, stored.CreatedAt},
		{"last_seen_at", created.LastSeenAt, stored.LastSeenAt},
		{"idle_expires_at", created.IdleExpiresAt, stored.IdleExpiresAt},
		{"expires_at", created.ExpiresAt, stored.ExpiresAt},
	} {
		if !field.returned.Equal(field.stored) {
			t.Errorf("%s: returned %s, stored %s", field.name, field.returned, field.stored)
		}
	}
}
