package httputil

import (
	"context"
	"net/http"
	"strings"
)

type managementErrorsKey struct{}

// ManagementErrors scopes JSON errors to the management API. Protocol error
// formats (including WMS/WFS XML) are left to their existing handlers.
func ManagementErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), managementErrorsKey{}, true)))
	})
}

// ManagementErrorScope runs before authentication/transport/body limits. path
// is the router-local management prefix, after any configured base path strip.
func ManagementErrorScope(path string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		scoped := ManagementErrors(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == path || strings.HasPrefix(r.URL.Path, path+"/") {
				scoped.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func HTTPError(w http.ResponseWriter, r *http.Request, message string, status int) {
	if scoped, _ := r.Context().Value(managementErrorsKey{}).(bool); scoped {
		WriteJSON(w, status, map[string]any{"code": status, "message": message})
		return
	}
	http.Error(w, message, status)
}
