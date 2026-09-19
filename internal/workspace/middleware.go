package workspace

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Middleware creates HTTP middleware that loads the workspace from the URL parameter
// and adds it to the request context.
func Middleware(registry *Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workspaceName := chi.URLParam(r, "workspaceId")
			if workspaceName == "" {
				http.Error(w, "workspace not specified", http.StatusBadRequest)
				return
			}

			ws, release, ok := registry.Acquire(workspaceName)
			if !ok {
				http.NotFound(w, r)
				return
			}
			defer release()

			ctx := WithWorkspace(r.Context(), ws)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireWorkspace is middleware that ensures a workspace is present in the context.
func RequireWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := FromContext(r.Context())
		if !ok {
			http.Error(w, "workspace not found", http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LoadLayer is middleware that loads a layer from the URL parameter into the context.
func LoadLayer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, ok := FromContext(r.Context())
		if !ok {
			http.Error(w, "workspace not found", http.StatusInternalServerError)
			return
		}

		layerID := chi.URLParam(r, "layer")
		if layerID == "" {
			layerID = chi.URLParam(r, "collectionId")
		}
		if layerID == "" {
			next.ServeHTTP(w, r)
			return
		}

		layer, svc := ws.GetLayer(layerID)
		if layer == nil {
			http.NotFound(w, r)
			return
		}

		ctx := WithLayer(r.Context(), layer)
		ctx = WithService(ctx, svc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
