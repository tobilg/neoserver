package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
)

func TestConsoleFallbackAndHeaders(t *testing.T) {
	router := chi.NewRouter()
	RegisterRoutes(router, conf.Config{Server: conf.Server{BasePath: "/base"}})
	request := httptest.NewRequest(http.MethodGet, "/admin/w/demo/layers", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "neoserver Console") {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "__NEOSERVER_BASE__") || !strings.Contains(recorder.Body.String(), `href="./"`) {
		t.Fatalf("Vite relative base was not preserved: %q", recorder.Body.String())
	}
	if recorder.Header().Get("Content-Security-Policy") == "" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("missing console security/cache headers: %#v", recorder.Header())
	}
}

func TestNestedConsoleRouteServesRelativeViteAsset(t *testing.T) {
	h := &handler{dist: fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte(`<script type="module" src="./assets/app-abc.js"></script>`)},
		"assets/app-abc.js": &fstest.MapFile{Data: []byte(`document.body.dataset.ready = "true"`)},
	}}
	assetRequest := httptest.NewRequest(http.MethodGet, "/admin/w/demo/assets/app-abc.js", nil)
	assetRecorder := httptest.NewRecorder()
	h.ServeHTTP(assetRecorder, assetRequest)
	if assetRecorder.Code != http.StatusOK || !strings.Contains(assetRecorder.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("nested asset status=%d cache=%q", assetRecorder.Code, assetRecorder.Header().Get("Cache-Control"))
	}
	if !strings.Contains(assetRecorder.Body.String(), "dataset.ready") {
		t.Fatalf("nested asset body=%q", assetRecorder.Body.String())
	}
}

// Files outside the Vite `assets/` directory -- the Swagger UI runtime the
// management API docs load, for one -- must resolve too. chi's Mount leaves
// r.URL.Path as the full path, so a handler that forgets to trim the mount
// prefix serves index.html for them and the consuming page silently breaks.
func TestEmbeddedFileOutsideAssetsDirectoryIsServed(t *testing.T) {
	for _, prefix := range []string{"", "/base"} {
		h := &handler{
			prefix: prefix + "/admin",
			index:  []byte("<!doctype html><title>console</title>"),
			dist: fstest.MapFS{
				"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")},
				"vendor/swagger/swagger-ui.css": &fstest.MapFile{
					Data: []byte(".swagger-ui{color:red}"),
				},
			},
		}
		request := httptest.NewRequest(
			http.MethodGet, prefix+"/admin/vendor/swagger/swagger-ui.css", nil)
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)

		if body := recorder.Body.String(); !strings.Contains(body, ".swagger-ui") {
			t.Fatalf("prefix %q: served %q instead of the embedded file", prefix, body)
		}
		if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "css") {
			t.Fatalf("prefix %q: Content-Type = %q, want text/css", prefix, contentType)
		}
		// Only content-hashed Vite output may be cached immutably.
		if strings.Contains(recorder.Header().Get("Cache-Control"), "immutable") {
			t.Fatalf("prefix %q: non-hashed asset must not be immutable", prefix)
		}
	}
}

func TestConsoleDoesNotShadowOtherRoutes(t *testing.T) {
	router := chi.NewRouter()
	RegisterRoutes(router, conf.Config{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", recorder.Code)
	}
}
