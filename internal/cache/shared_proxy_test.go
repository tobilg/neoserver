package cache

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A real HTTP reverse proxy with a small shared response cache. The origin's
// Vary controls the lookup key; private/no-store responses never enter it.
func TestSharedProxyDoesNotReusePrivilegedRepresentation(t *testing.T) {
	var revoked atomic.Bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		body := []byte("public")
		if key == "admin" && revoked.Load() {
			w.WriteHeader(401)
			return
		}
		if key == "admin" {
			body = []byte("restricted")
		}
		SetHTTPHeaders(w, r, true, time.Minute, body)
		_, _ = w.Write(body)
	}))
	defer origin.Close()
	endpoint, _ := url.Parse(origin.URL)
	proxy := httputil.NewSingleHostReverseProxy(endpoint)
	type entry struct {
		header http.Header
		body   string
	}
	entries := map[string]entry{}
	vary := []string{}
	var mu sync.Mutex
	cacheKey := func(r *http.Request) string {
		key := r.URL.RequestURI()
		for _, name := range vary {
			key += "|" + name + "=" + r.Header.Get(name)
		}
		return key
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if !strings.Contains(response.Header.Get("Cache-Control"), "public,") {
			return nil
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return err
		}
		response.Body = io.NopCloser(strings.NewReader(string(body)))
		mu.Lock()
		defer mu.Unlock()
		vary = nil
		for _, line := range response.Header.Values("Vary") {
			for _, name := range strings.Split(line, ",") {
				vary = append(vary, strings.TrimSpace(name))
			}
		}
		entries[cacheKey(response.Request)] = entry{response.Header.Clone(), string(body)}
		return nil
	}
	shared := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cached, ok := entries[cacheKey(r)]
		mu.Unlock()
		if ok {
			for k, v := range cached.header {
				w.Header()[k] = v
			}
			_, _ = io.WriteString(w, cached.body)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	defer shared.Close()
	for _, tc := range []struct {
		key, body string
		status    int
		revoke    bool
	}{
		{"admin", "restricted", 200, false}, {"", "public", 200, false}, {"viewer", "public", 200, false},
		{"admin", "restricted", 200, false}, {"admin", "", 401, true}, {"", "public", 200, true},
	} {
		revoked.Store(tc.revoke)
		r, _ := http.NewRequest("GET", shared.URL+"/collections", nil)
		r.Header.Set("X-API-Key", tc.key)
		response, err := shared.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != tc.status || string(body) != tc.body {
			t.Fatalf("%q: %d %q", tc.key, response.StatusCode, body)
		}
	}
}
