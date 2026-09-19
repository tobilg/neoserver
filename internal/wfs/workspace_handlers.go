package wfs

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/protocolrequest"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

// bodyBytesKey is the context key for storing request body bytes
const bodyBytesKey contextKey = "wfs-body-bytes"

// GetBodyBytes retrieves the stored body bytes from context, or reads from r.Body if not stored.
// This ensures the body can be read multiple times across different handlers.
func GetBodyBytes(r *http.Request) ([]byte, error) {
	if body, ok := r.Context().Value(bodyBytesKey).([]byte); ok {
		return body, nil
	}
	if r.Body == nil {
		return nil, nil
	}
	return io.ReadAll(r.Body)
}

// WorkspaceDependencies holds dependencies for workspace-scoped WFS handlers.
type WorkspaceDependencies struct {
	Config   conf.Config
	Logger   *slog.Logger
	Registry *workspace.Registry
	Cache    *cache.Manager
	Store    store.Store
	State    *RuntimeState
}

// workspaceHandler handles WFS requests for a specific workspace.
type workspaceHandler struct {
	cfg      conf.Config
	logger   *slog.Logger
	registry *workspace.Registry
	cache    *cache.Manager
	store    store.Store
	state    *RuntimeState
	exports  chan struct{}
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

func (h *workspaceHandler) requireWrite(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) bool {
	id, ok := identity.FromContext(r.Context())
	if !ok || id == nil {
		writeException(w, http.StatusUnauthorized, ExceptionAuthorizationFailed, "", "authentication required")
		return true
	}
	if !id.CanWrite(ws.ID) && !rbac.HasOperationGrant(r, ws.ID, "wfs", rbac.ActionWrite) {
		writeException(w, http.StatusForbidden, ExceptionAuthorizationFailed, "", "write access required")
		return true
	}
	return false
}

func (h *workspaceHandler) requireAdmin(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) bool {
	id, ok := identity.FromContext(r.Context())
	if !ok || id == nil {
		writeException(w, http.StatusUnauthorized, ExceptionAuthorizationFailed, "", "authentication required")
		return true
	}
	if !id.IsAdmin(ws.ID) && !rbac.HasOperationGrant(r, ws.ID, "wfs", rbac.ActionManage) {
		writeException(w, http.StatusForbidden, ExceptionAuthorizationFailed, "", "admin access required")
		return true
	}
	return false
}

// withAnonymousMutationIdentity grants the narrowly scoped, deployment-level
// anonymous mutation mode. It is intentionally effective only for public WFS
// workspaces while global authentication is disabled. A real identity is never
// replaced, and the synthetic principal exists only in this WFS request context.
func (h *workspaceHandler) withAnonymousMutationIdentity(r *http.Request, ws *workspace.Workspace, operation string) *http.Request {
	if !h.cfg.WFS.AllowAnonymousMutations || h.cfg.Auth.Enabled || ws.Settings == nil || !ws.Settings.WFS.Public {
		return r
	}
	if id, ok := identity.FromContext(r.Context()); ok && id != nil {
		return r
	}
	switch operation {
	case "TRANSACTION", "CREATESTOREDQUERY", "DROPSTOREDQUERY", "LOCKFEATURE", "GETFEATUREWITHLOCK":
		id := &identity.Identity{
			Subject:    "wfs-anonymous-mutation",
			AuthMethod: identity.AuthMethodAnonymous,
			Roles:      map[string]string{ws.ID: "admin"},
		}
		return r.WithContext(identity.WithIdentity(r.Context(), id))
	default:
		return r
	}
}

// workspaceRole returns the caller's role for the given workspace, or "" for an
// anonymous request (only possible on a public service). Used to filter feature
// types by per-layer read access (Layer.VisibleToRole).
func workspaceRole(r *http.Request, workspaceID string) string {
	if id, ok := identity.FromContext(r.Context()); ok && id != nil {
		return id.GetWorkspaceRole(workspaceID)
	}
	return ""
}

// writeInternalError logs the underlying error server-side and returns a generic
// OWS exception to the client, avoiding disclosure of internal/database details.
func (h *workspaceHandler) writeInternalError(w http.ResponseWriter, publicMessage string, err error) {
	h.logger.Error("wfs request failed", "detail", publicMessage, "err", err)
	WriteException(w, ExceptionNoApplicableCode, "", publicMessage)
}

// RegisterWorkspaceRoutes registers WFS routes for workspace-scoped access.
// The workspace must be loaded into the context before these routes are called.
func RegisterWorkspaceRoutes(r chi.Router, deps WorkspaceDependencies) {
	state := deps.State
	if state == nil {
		persistence, _ := deps.Store.(RuntimePersistence)
		state = NewRuntimeState(deps.Config.WFS, persistence, deps.Logger)
	}
	h := &workspaceHandler{
		cfg:      deps.Config,
		logger:   deps.Logger,
		registry: deps.Registry,
		cache:    deps.Cache,
		store:    deps.Store,
		state:    state,
		exports:  make(chan struct{}, max(1, deps.Config.WFS.MaxConcurrentExports)),
	}

	// WFS uses a single endpoint with query parameters
	r.Get("/", h.handleWFS)
	r.Post("/", h.handleWFS)
}

// handleWFS handles all WFS requests for a workspace.
func (h *workspaceHandler) handleWFS(w http.ResponseWriter, r *http.Request) {
	r = protocolrequest.Prepare(r, "wfs")
	operation := protocolrequest.Get(r)
	if operation.Err != nil {
		WriteException(w, ExceptionOperationParsingFailed, "request", operation.Err.Error())
		return
	}
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		WriteException(w, ExceptionNoApplicableCode, "", "workspace not found in context")
		return
	}

	// Check if WFS is enabled for this workspace
	if ws.Settings == nil || !ws.Settings.WFS.Enabled {
		WriteException(w, ExceptionOperationNotSupported, "", "WFS is not enabled for this workspace")
		return
	}

	// Check authentication if not public
	if h.requireAuth(w, r, ws, ws.Settings.WFS.Public) {
		return
	}

	// For POST requests, read and store body bytes in context so they can be read multiple times
	if r.Method == http.MethodPost && r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err == nil && len(bodyBytes) > 0 {
			// Store body in context for downstream handlers
			ctx := context.WithValue(r.Context(), bodyBytesKey, bodyBytes)
			r = r.WithContext(ctx)
			// Also restore body for any code that reads directly
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			h.logger.Debug("WFS POST body stored in context", "bodyLen", len(bodyBytes))
		} else if err != nil {
			h.logger.Error("WFS POST body read error", "error", err)
		}
	}

	// Dispatch the exact operation already used for authorization and auditing.
	service, request, version := operation.ServiceParameter, operation.Name, operation.Version

	h.logger.Debug("WFS request routing", "method", r.Method, "service", service, "request", request)

	// Validate SERVICE (only after we've tried both sources)
	if service != "" && service != "WFS" {
		WriteException(w, ExceptionInvalidParameterValue, "SERVICE", "SERVICE must be WFS")
		return
	}

	// SERVICE parameter is required for GetCapabilities per WFS 2.0 spec (ISO 19142:7.5)
	if request == "GETCAPABILITIES" && service == "" {
		WriteException(w, ExceptionMissingParameterValue, "SERVICE", "SERVICE parameter is required for GetCapabilities")
		return
	}
	if request != "GETCAPABILITIES" && version != "" && version != "2.0.0" && version != "2.0.2" {
		WriteException(w, ExceptionVersionNegotiationFailed, "VERSION", "Server supports WFS 2.0.0 and 2.0.2")
		return
	}

	r = h.withAnonymousMutationIdentity(r, ws, request)

	switch request {
	case "GETCAPABILITIES":
		h.handleGetCapabilities(w, r, ws)
	case "DESCRIBEFEATURETYPE":
		h.handleDescribeFeatureType(w, r, ws)
	case "GETFEATURE":
		h.handleGetFeature(w, r, ws)
	case "GETPROPERTYVALUE":
		h.handleGetPropertyValue(w, r, ws)
	case "LISTSTOREDQUERIES":
		h.handleListStoredQueries(w, r, ws)
	case "DESCRIBESTOREDQUERIES":
		h.handleDescribeStoredQueries(w, r, ws)
	case "TRANSACTION":
		if h.requireWrite(w, r, ws) {
			return
		}
		h.handleTransaction(w, r, ws)
	case "CREATESTOREDQUERY":
		if h.requireAdmin(w, r, ws) {
			return
		}
		h.handleCreateStoredQuery(w, r, ws)
	case "DROPSTOREDQUERY":
		if h.requireAdmin(w, r, ws) {
			return
		}
		h.handleDropStoredQuery(w, r, ws)
	case "LOCKFEATURE":
		if h.requireWrite(w, r, ws) {
			return
		}
		h.handleLockFeature(w, r, ws)
	case "GETFEATUREWITHLOCK":
		if h.requireWrite(w, r, ws) {
			return
		}
		h.handleGetFeatureWithLock(w, r, ws)
	case "":
		WriteException(w, ExceptionMissingParameterValue, "REQUEST", "REQUEST parameter is required")
	default:
		WriteException(w, ExceptionOperationNotSupported, "REQUEST", "Unsupported REQUEST: "+request)
	}
}

// getServiceAndOperationFromXMLBody extracts service, version, and operation from the POST body's root XML element.
// It uses the body bytes stored in context by handleWFS.
// For WFS 2.0 XML POST requests, these attributes are on the root element (e.g., <wfs:GetFeature service="WFS" version="2.0.0">).
func (h *workspaceHandler) getServiceAndOperationFromXMLBody(r *http.Request) (service, version, operation string) {
	// Get body bytes from context (stored by handleWFS)
	body, err := GetBodyBytes(r)
	if err != nil {
		h.logger.Debug("WFS XML body read error", "error", err)
		return "", "", ""
	}
	if len(body) == 0 {
		h.logger.Debug("WFS XML body is empty")
		return "", "", ""
	}

	h.logger.Debug("WFS XML body parsing", "bodyPreview", string(body[:min(200, len(body))]))

	// Parse just enough to get the root element name and attributes
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			h.logger.Debug("WFS XML parse error", "error", err)
			return "", "", ""
		}
		if startElem, ok := token.(xml.StartElement); ok {
			// Extract operation from local name
			operation = strings.ToUpper(startElem.Name.Local)

			// Extract service and version from attributes
			for _, attr := range startElem.Attr {
				switch strings.ToLower(attr.Name.Local) {
				case "service":
					service = strings.ToUpper(attr.Value)
				case "version":
					version = attr.Value
				}
			}
			h.logger.Debug("WFS XML parsed", "operation", operation, "service", service, "version", version)
			return service, version, operation
		}
	}
}

func (h *workspaceHandler) handleDescribeFeatureType(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()

	// Parse request
	req, err := ParseDescribeFeatureTypeRequest(r)
	if err != nil {
		if reqErr, ok := err.(*RequestError); ok {
			WriteException(w, reqErr.Code, reqErr.Locator, reqErr.Message)
		} else {
			WriteException(w, ExceptionNoApplicableCode, "", err.Error())
		}
		return
	}

	// Collect layer info for schema generation
	var layers []*layerSchemaInfo

	// If no type names specified, describe all layers the caller may read
	if len(req.TypeNames) == 0 {
		for _, layer := range ws.VisibleLayers(workspaceRole(r, ws.ID)) {
			_, service := ws.GetLayer(layer.PublicID)
			if service == nil || service.DataSource == nil {
				continue
			}

			layerInfo, err := service.DataSource.GetLayerInfo(ctx, layer.SourceLayer)
			if err != nil {
				continue
			}

			layers = append(layers, &layerSchemaInfo{
				TypeName: publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix),
				Info:     layerInfo,
			})
		}
	} else {
		// Describe only requested type names
		for _, typeName := range req.TypeNames {
			// A layer the caller may not read is treated as an unknown type name.
			layer, service := resolveFeatureLayer(ws, typeName, h.cfg.WFS.AppNamespacePrefix)
			if layer == nil || service == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
				WriteException(w, ExceptionInvalidParameterValue, "typeNames", fmt.Sprintf("Unknown type name: %s", typeName))
				return
			}

			if service.DataSource == nil {
				WriteException(w, ExceptionNoApplicableCode, "", fmt.Sprintf("Service not available for layer: %s", layer.PublicID))
				return
			}

			layerInfo, err := service.DataSource.GetLayerInfo(ctx, layer.SourceLayer)
			if err != nil {
				h.writeInternalError(w, "Failed to get layer info", err)
				return
			}

			layers = append(layers, &layerSchemaInfo{
				TypeName: typeName,
				Info:     layerInfo,
			})
		}
	}

	// Generate and write schema
	for _, layer := range layers {
		if _, err := propertyXMLNames(layer.Info); err != nil {
			WriteException(w, ExceptionNoApplicableCode, "", err.Error())
			return
		}
	}
	schema := GenerateSchema(layers, h.cfg.WFS.AppNamespace, h.cfg.WFS.AppNamespacePrefix)
	WriteSchema(w, schema)
}

func (h *workspaceHandler) workspaceBaseURL(r *http.Request, wsName string) string {
	return strings.TrimRight(h.cfg.Server.UrlBase, "/") + h.cfg.Server.BasePath + "/workspaces/" + url.PathEscape(wsName) + "/wfs"
}
