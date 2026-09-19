package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagementErrorsAreScoped(t *testing.T) {
	for _, status := range []int{400, 401, 403, 413, 426, 429, 503} {
		handler := ManagementErrorScope("/api/v1")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { HTTPError(w, r, "test error", status) }))
		for _, path := range []string{"/api/v1/workspaces", "/workspaces/demo/wms"} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != status {
				t.Fatal(w.Code)
			}
			if path == "/api/v1/workspaces" {
				var body struct {
					Code    int
					Message string
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != status || body.Message != "test error" {
					t.Fatalf("bad contract %s", w.Body.String())
				}
			}
			if path == "/workspaces/demo/wms" && w.Body.String() != "test error\n" {
				t.Fatal("protocol error format changed")
			}
		}
	}
}
