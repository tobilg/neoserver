package identity

import (
	"net/http/httptest"
	"testing"
)

func TestSTACStaleSessionReadFallback(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		read         bool
	}{{"POST", "/workspaces/demo/stac/search", true}, {"POST", "/workspaces/demo/stac/collections", false}, {"POST", "/api/v1/workspaces/demo/stac/imports", false}, {"DELETE", "/workspaces/demo/stac/search", false}} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if got := staleSessionMayReadAnonymously(req); got != tc.read {
			t.Errorf("%s %s anonymous fallback=%t", tc.method, tc.path, got)
		}
	}
	// Existing valid-session CSRF enforcement deliberately remains method-based.
	if !isUnsafeMethod("POST") {
		t.Fatal("POST must retain browser-session CSRF checks")
	}
}
