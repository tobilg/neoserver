package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/tobilg/neoserver/internal/protocolrequest"
)

// slowBody streams chunks for longer than the server WriteTimeout.
func slowBody(w http.ResponseWriter, _ *http.Request) {
	flusher := w.(http.Flusher)
	for i := 0; i < 6; i++ {
		_, _ = io.WriteString(w, strings.Repeat("x", 1024))
		flusher.Flush()
		time.Sleep(100 * time.Millisecond)
	}
	_, _ = io.WriteString(w, "END")
}

func TestExportWriteDeadlineOutlivesServerWriteTimeout(t *testing.T) {
	const serverWriteTimeout = 300 * time.Millisecond
	for _, otel := range []bool{false, true} {
		r := chi.NewRouter()
		r.Use(protocolrequest.Middleware(""))
		r.Use(exportWriteDeadline(5*time.Second, serverWriteTimeout))
		r.Use(middleware.Compress(5))
		r.Get("/workspaces/demo/wfs", slowBody)
		r.Get("/workspaces/demo/ogc/collections/c/items", slowBody)
		r.Get("/workspaces/demo/ogc/collections", slowBody)

		var handler http.Handler = r
		if otel {
			handler = otelhttp.NewHandler(handler, "test")
		}
		srv := httptest.NewUnstartedServer(handler)
		srv.Config.WriteTimeout = serverWriteTimeout
		srv.Start()

		for path, wantComplete := range map[string]bool{
			"/workspaces/demo/wfs?SERVICE=WFS&REQUEST=GetFeature&TYPENAMES=a": true,
			"/workspaces/demo/ogc/collections/c/items":                        true,
			"/workspaces/demo/wfs?SERVICE=WFS&REQUEST=GetCapabilities":        false,
			"/workspaces/demo/ogc/collections":                                false,
		} {
			resp, err := srv.Client().Get(srv.URL + path)
			complete := false
			if err == nil {
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				complete = readErr == nil && strings.HasSuffix(string(body), "END")
			}
			if complete != wantComplete {
				t.Errorf("otel=%v %s: complete=%v, want %v", otel, path, complete, wantComplete)
			}
		}
		srv.Close()
	}
}
