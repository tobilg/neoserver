package wms

import (
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/workspace"
)

// NormalizeQuery returns a copy of the query values with all parameter names uppercased.
// WMS 1.3.0 requires parameter names to be case-insensitive.
func NormalizeQuery(r *http.Request) url.Values {
	q := make(url.Values)
	for k, v := range r.URL.Query() {
		q[strings.ToUpper(k)] = v
	}
	return q
}

// WorkspaceDependencies holds dependencies for workspace-scoped WMS handlers.
type WorkspaceDependencies struct {
	Config   conf.Config
	Logger   *slog.Logger
	Registry *workspace.Registry
	Cache    *cache.Manager
}

// workspaceHandler handles WMS requests for a specific workspace.
type workspaceHandler struct {
	cfg         conf.Config
	logger      *slog.Logger
	registry    *workspace.Registry
	cache       *cache.Manager
	renderSlots chan struct{}
	styleMu     sync.RWMutex
	styleCache  map[string]cachedStyle
}

type cachedStyle struct {
	style   *sld.Style
	expires time.Time
}

// requireAuth enforces authentication and workspace authorization for non-public
// services. For a non-public workspace it requires a valid identity that has access
// to this specific workspace (or is a super admin). It writes the appropriate error
// response (401 or 403) and returns true when the request must be rejected.
func (h *workspaceHandler) requireAuth(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, isPublic bool) bool {
	if isPublic {
		return false
	}
	if h.cfg.Auth.RequireHTTPS && !identity.IsSecureTransport(r.Context()) {
		w.Header().Set("Upgrade", "TLS/1.2, HTTP/1.1")
		http.Error(w, "HTTPS required", http.StatusUpgradeRequired)
		return true
	}
	id, ok := identity.FromContext(r.Context())
	if !ok || id == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return true
	}
	if !id.HasWorkspaceAccess(ws.ID) {
		http.Error(w, "no access to this workspace", http.StatusForbidden)
		return true
	}
	return false
}

// workspaceRole returns the caller's role for the given workspace, or "" for an
// anonymous request (only possible on a public service). Used to filter layers
// by per-layer read access (Layer.VisibleToRole).
func workspaceRole(r *http.Request, workspaceID string) string {
	if id, ok := identity.FromContext(r.Context()); ok && id != nil {
		return id.GetWorkspaceRole(workspaceID)
	}
	return ""
}

// writeError writes a sanitized WMS exception. Typed *RequestError values are
// surfaced to the client; any other (internal) error is logged server-side and the
// client receives a generic message so internal details are not disclosed.
func (h *workspaceHandler) writeError(w http.ResponseWriter, err error) {
	exc := ExceptionFromError(err)
	if _, ok := err.(*RequestError); !ok {
		h.logger.Error("wms request failed", "err", err)
	}
	WriteException(w, exc.Code, exc.Text)
}

// RegisterWorkspaceRoutes registers WMS routes for workspace-scoped access.
// The workspace must be loaded into the context before these routes are called.
func RegisterWorkspaceRoutes(r chi.Router, deps WorkspaceDependencies) {
	renderer.ConfigureFontPaths(deps.Config.WMS.FontPaths)
	maxRenders := deps.Config.WMS.MaxConcurrentRenders
	if maxRenders <= 0 {
		maxRenders = runtime.GOMAXPROCS(0) / 2
		if maxRenders < 1 {
			maxRenders = 1
		}
		if maxRenders > 4 {
			maxRenders = 4
		}
	}
	h := &workspaceHandler{
		cfg:         deps.Config,
		logger:      deps.Logger,
		registry:    deps.Registry,
		cache:       deps.Cache,
		renderSlots: make(chan struct{}, maxRenders),
		styleCache:  make(map[string]cachedStyle),
	}

	// WMS uses a single endpoint with query parameters
	r.Get("/", h.handleWMS)
	r.Post("/", h.handleWMS)
}

// handleWMS handles all WMS requests for a workspace.
func (h *workspaceHandler) handleWMS(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		WriteException(w, ExceptionInvalidParameterValue, "workspace not found in context")
		return
	}

	// Check if WMS is enabled for this workspace
	if ws.Settings == nil || !ws.Settings.WMS.Enabled {
		WriteException(w, ExceptionOperationNotSupported, "WMS is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.WMS.Public) {
		return
	}

	// Get request type (uppercase for case-insensitive comparison per WMS spec)
	q := NormalizeQuery(r)
	request := q.Get("REQUEST")

	switch request {
	case RequestGetCapabilities:
		h.handleGetCapabilities(w, r, ws)
	case RequestGetMap:
		h.handleGetMap(w, r, ws)
	case RequestGetFeatureInfo:
		h.handleGetFeatureInfo(w, r, ws)
	case RequestGetLegendGraphic:
		h.handleGetLegendGraphic(w, r, ws)
	case RequestDescribeLayer:
		h.handleDescribeLayer(w, r, ws)
	case "":
		WriteException(w, ExceptionMissingParameterValue, "REQUEST parameter is required")
	default:
		WriteException(w, ExceptionOperationNotSupported, "Unsupported REQUEST: "+request)
	}
}

func (h *workspaceHandler) workspaceBaseURL(r *http.Request, wsName string) string {
	return strings.TrimRight(h.cfg.Server.UrlBase, "/") + h.cfg.Server.BasePath + "/workspaces/" + url.PathEscape(wsName) + "/wms"
}
