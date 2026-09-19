package store

import (
	"context"
	"testing"
	"time"
)

func TestSessionV25MigrationPreservesExistingSession(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC()
	before, err := s.CreateBrowserSession(ctx, BrowserSession{Subject: "operator", AuthMethod: "jwt", TokenHash: "old-token", CSRFHash: "old-csrf", IdleExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`CREATE TEMP TABLE session_backup AS SELECT * EXCLUDE(previous_token_hash,previous_csrf_hash,previous_valid_until) FROM browser_sessions;
DROP TABLE browser_sessions;` + migrationV21SQL + `INSERT INTO browser_sessions SELECT * FROM session_backup; DROP TABLE session_backup;`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.runMigrations(24); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetBrowserSessionByTokenHash(ctx, "old-token")
	if err != nil || after.ID != before.ID || after.CSRFHash != "old-csrf" || after.PreviousTokenHash != "" {
		t.Fatalf("migration changed session: %+v %v", after, err)
	}
	if err := s.RotateBrowserSession(ctx, after.ID, "old-token", "new-token", "new-csrf", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}
