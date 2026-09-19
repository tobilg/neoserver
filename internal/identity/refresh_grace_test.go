package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionPreviousTokenExpiresWithoutExtendingGrace(t *testing.T) {
	catalog := sessionTestStore(t)
	now := time.Now().UTC()
	session := createPasswordSession(t, catalog, "old", "old-csrf", now.Add(-time.Hour))
	// Fix idle expiry before rotating; the created fixture would expire at now.
	if err := catalog.TouchBrowserSession(context.Background(), session.ID, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := catalog.RotateBrowserSession(context.Background(), session.ID, HashSessionToken("old"), HashSessionToken("new"), HashSessionToken("new-csrf"), now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	validator := NewSessionValidator(catalog, catalog, SessionConfig{}, "", map[string]string{"ada": "correct"}, nil)
	for _, tc := range []struct {
		token    string
		accepted bool
	}{{"old", false}, {"new", true}} {
		r := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: tc.token})
		principal, err := validator.ValidateRequest(r.Context(), r)
		if (err == nil && principal != nil) != tc.accepted {
			t.Fatalf("%s: %v %v", tc.token, principal, err)
		}
	}
}
