package cache

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/identity"
)

func TestCredentialedPublicServiceResponsesCannotEnterSharedCache(t *testing.T) {
	for _, header := range []string{"Authorization", "X-API-Key", "Cookie", "context", "query", "anonymous"} {
		t.Run(header, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/collections", nil)
			switch header {
			case "context":
				r = r.WithContext(identity.WithIdentity(r.Context(), &identity.Identity{Roles: map[string]string{"*": "super_admin"}}))
			case "query":
				r.URL.RawQuery = "apikey=test"
			case "anonymous":
			default:
				r.Header.Set(header, "test")
			}
			w := httptest.NewRecorder()
			etag := SetHTTPHeaders(w, r, true, time.Minute, []byte("representation"))
			want := "private, no-store"
			if header == "anonymous" {
				want = "public, max-age=60"
			}
			if w.Header().Get("Cache-Control") != want {
				t.Fatalf("got %q", w.Header().Get("Cache-Control"))
			}
			r.Header.Set("If-None-Match", etag)
			if !IsNotModified(r, etag) {
				t.Fatal("validator lost")
			}
		})
	}
}
