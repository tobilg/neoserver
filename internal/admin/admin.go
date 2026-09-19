// Package admin serves the embedded neoserver administration console.
package admin

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
)

// dist is populated by the Vite build. The placeholder is deliberately kept
// outside dist so a frontend build never overwrites a tracked file.
//
//go:embed all:dist all:placeholder
var assets embed.FS

type handler struct {
	dist          fs.FS
	index         []byte
	contentPolicy string
	// prefix is the mount path ("<base>/admin"). chi's Mount leaves r.URL.Path
	// as the full request path, so it must be trimmed before looking a file up
	// in the embedded FS.
	prefix string
}

// RegisterRoutes mounts the console without allowing its SPA fallback to
// shadow management or workspace protocol routes.
func RegisterRoutes(router chi.Router, cfg conf.Config) {
	base := strings.TrimSuffix(cfg.Server.BasePath, "/") + "/admin"
	router.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base+"/", http.StatusMovedPermanently)
	})
	dist, _ := fs.Sub(assets, "dist")
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		index, _ = assets.ReadFile("placeholder/index.html")
	}
	router.Mount("/admin/", &handler{
		dist:          dist,
		index:         index,
		contentPolicy: contentSecurityPolicy(cfg.Website.BasemapURL, cfg.Auth.OIDC.IssuerURL),
		prefix:        base,
	})
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// chi's Mount leaves r.URL.Path as the full request path, so drop the mount
	// prefix before resolving against the embedded FS. Without this, only paths
	// rewritten by the /assets/ rule below would ever resolve.
	requested := path.Clean("/" + r.URL.Path)
	requested = strings.TrimPrefix(strings.TrimPrefix(requested, h.prefix), "/")
	// Vite emits relative asset URLs. On a direct request to a nested SPA route,
	// the browser consequently requests e.g. /admin/w/demo/assets/app.js. Map
	// that URL back to the single embedded, content-hashed assets directory.
	if assetAt := strings.LastIndex(requested, "/assets/"); assetAt >= 0 {
		requested = requested[assetAt+1:]
	}
	if requested != "" && requested != "." {
		if info, err := fs.Stat(h.dist, requested); err == nil && !info.IsDir() {
			content, readErr := fs.ReadFile(h.dist, requested)
			if readErr != nil {
				http.Error(w, "asset unavailable", http.StatusInternalServerError)
				return
			}
			h.securityHeaders(w)
			if contentType := mime.TypeByExtension(path.Ext(requested)); contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			if strings.HasPrefix(requested, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			_, _ = w.Write(content)
			return
		}
	}
	h.securityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(h.index)
}

func (h *handler) securityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", h.contentPolicy)
}

func contentSecurityPolicy(basemap, oidcIssuer string) string {
	connect := "'self'"
	images := "'self' data: blob:"
	if parsed, err := url.Parse(basemap); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		origin := parsed.Scheme + "://" + parsed.Host
		connect += " " + origin
		images += " " + origin
	}
	if parsed, err := url.Parse(oidcIssuer); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		origin := parsed.Scheme + "://" + parsed.Host
		if !strings.Contains(connect, origin) {
			connect += " " + origin
		}
	}
	return "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src " + images +
		"; connect-src " + connect + "; worker-src 'self' blob:; font-src 'self' data:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"
}
