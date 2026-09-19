package cache

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/identity"
)

// SetHTTPHeaders applies validators and freshness metadata for a response body.
func SetHTTPHeaders(w http.ResponseWriter, r *http.Request, public bool, ttl time.Duration, body []byte) string {
	w.Header().Add("Vary", "Authorization, X-API-Key, Cookie")
	// A public service may return a role-dependent representation. Never let
	// shared caches reuse responses obtained with any kind of credential.
	principal, _ := identity.FromContext(r.Context())
	public = public && principal == nil && r.Header.Get("Authorization") == "" &&
		r.Header.Get("X-API-Key") == "" && r.Header.Get("Cookie") == "" && r.URL.Query().Get("apikey") == ""
	sum := sha256.Sum256(body)
	etag := fmt.Sprintf(`"%x"`, sum[:])
	w.Header().Set("ETag", etag)
	if public {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", max(0, int(ttl.Seconds()))))
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	return etag
}

func IsNotModified(r *http.Request, etag string) bool { return r.Header.Get("If-None-Match") == etag }

// RequestCachePolicy respects an explicit origin refresh. no-cache may refill
// the cache; no-store must neither reuse nor store this request's response.
func RequestCachePolicy(r *http.Request) (read, write bool) {
	read, write = true, true
	for _, header := range r.Header.Values("Cache-Control") {
		for _, directive := range strings.Split(header, ",") {
			key, value, _ := strings.Cut(strings.ToLower(strings.TrimSpace(directive)), "=")
			switch strings.TrimSpace(key) {
			case "no-store":
				return false, false
			case "no-cache":
				read = false
			case "max-age":
				if strings.Trim(strings.TrimSpace(value), `"`) == "0" {
					read = false
				}
			}
		}
	}
	if r.Header.Get("Cache-Control") == "" && strings.EqualFold(strings.TrimSpace(r.Header.Get("Pragma")), "no-cache") {
		read = false
	}
	return read, write
}
