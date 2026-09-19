package wfs

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestCustomOperationGrantsPassOnlyTheirWFSGuard(t *testing.T) {
	e, err := rbac.NewEnforcerWithDefaults(rbac.NewMemoryAdapter())
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range []struct{ operation, action string }{{"Transaction", "write"}, {"LockFeature", "write"}, {"GetFeatureWithLock", "write"}, {"DropStoredQuery", "manage"}, {"CreateStoredQuery", "manage"}} {
		if err := e.AddOperationPolicy("custom", "ws", "wfs", policy.operation, policy.action); err != nil {
			t.Fatal(err)
		}
		ws := &workspace.Workspace{ID: "ws"}
		h := &workspaceHandler{logger: slog.Default()}
		r := httptest.NewRequest("GET", "/workspaces/ws/wfs?request="+policy.operation, nil)
		r = r.WithContext(workspace.WithWorkspace(identity.WithIdentity(r.Context(), &identity.Identity{Subject: "key", Roles: map[string]string{"ws": "custom"}}), ws))
		called := false
		handler := rbac.RequireServiceOperation(e, "wfs")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			if policy.action == "write" {
				if h.requireWrite(w, r, ws) {
					t.Error("custom write grant rejected")
				}
				if !h.requireAdmin(httptest.NewRecorder(), r, ws) {
					t.Error("write grant escalated to administration")
				}
			} else {
				if h.requireAdmin(w, r, ws) {
					t.Error("custom manage grant rejected")
				}
				if !h.requireWrite(httptest.NewRecorder(), r, ws) {
					t.Error("manage operation escalated to transaction")
				}
			}
			if !h.requireWrite(httptest.NewRecorder(), r, &workspace.Workspace{ID: "other"}) {
				t.Error("grant escaped workspace")
			}
		}))
		handler.ServeHTTP(httptest.NewRecorder(), r)
		if !called {
			t.Fatal("custom policy did not pass operation middleware")
		}
	}
}

func TestCapabilitiesEscapesWorkspaceMetadata(t *testing.T) {
	h := &workspaceHandler{cfg: conf.Config{
		Server: conf.Server{UrlBase: "https://example.test"},
		WFS:    conf.WFS{AppNamespacePrefix: "app", DefaultSRS: "urn:ogc:def:crs:EPSG::4326"},
	}}
	ws := &workspace.Workspace{
		ID:          "ws-1",
		Name:        `name</ows:Title><Injected value="1">`,
		Description: `description & <unsafe>`,
		Services:    map[string]*workspace.Service{},
		Settings:    &store.WorkspaceSettings{WFS: store.WFSSettings{Enabled: true, Public: true}},
	}
	w := httptest.NewRecorder()
	h.handleGetCapabilities(w, httptest.NewRequest(http.MethodGet, "/wfs", nil), ws)

	var document struct{ XMLName xml.Name }
	if err := xml.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatalf("capabilities are not well-formed XML: %v", err)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("<Injected")) {
		t.Fatal("workspace metadata was emitted as XML markup")
	}
}

func TestCapabilitiesDeclaresCITENamespaceOnce(t *testing.T) {
	h := &workspaceHandler{cfg: conf.Config{
		Server: conf.Server{UrlBase: "https://example.test"},
		WFS: conf.WFS{
			AppNamespace:       NSCite,
			AppNamespacePrefix: "cite",
			DefaultSRS:         "urn:ogc:def:crs:EPSG::4326",
		},
	}}
	ws := &workspace.Workspace{
		ID:       "ws-1",
		Name:     "demo",
		Services: map[string]*workspace.Service{},
		Settings: &store.WorkspaceSettings{WFS: store.WFSSettings{Enabled: true, Public: true}},
	}
	w := httptest.NewRecorder()
	h.handleGetCapabilities(w, httptest.NewRequest(http.MethodGet, "/wfs", nil), ws)

	body := w.Body.String()
	if got := bytes.Count([]byte(body), []byte(`xmlns:cite="`)); got != 1 {
		t.Fatalf("CITE namespace declaration count = %d, want 1\n%s", got, body)
	}
	var document struct{ XMLName xml.Name }
	if err := xml.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("capabilities are not well-formed XML: %v", err)
	}
}

func TestCapabilitiesDeclareVersioningNotImplemented(t *testing.T) {
	h := &workspaceHandler{cfg: conf.Config{
		Server: conf.Server{UrlBase: "https://example.test"},
		WFS:    conf.WFS{AppNamespacePrefix: "app", DefaultSRS: "urn:ogc:def:crs:EPSG::4326"},
	}}
	ws := &workspace.Workspace{
		ID:       "ws-1",
		Name:     "demo",
		Services: map[string]*workspace.Service{},
		Settings: &store.WorkspaceSettings{WFS: store.WFSSettings{Enabled: true, Public: true}},
	}
	w := httptest.NewRecorder()
	h.handleGetCapabilities(w, httptest.NewRequest(http.MethodGet, "/wfs", nil), ws)

	body := w.Body.String()
	// Only version metadata is recorded today (no feature content history),
	// so both versioning conformance classes must be declared FALSE.
	for _, constraint := range []string{"ImplementsFeatureVersioning", "ImplementsVersionNav"} {
		idx := bytes.Index([]byte(body), []byte(constraint))
		if idx < 0 {
			t.Fatalf("constraint %s missing from capabilities", constraint)
		}
		window := body[idx:]
		if len(window) > 250 {
			window = window[:250]
		}
		if !bytes.Contains([]byte(window), []byte("<ows:DefaultValue>FALSE</ows:DefaultValue>")) {
			t.Errorf("constraint %s must declare FALSE, got:\n%s", constraint, window)
		}
	}
}

func TestGetServiceAndOperationFromXMLBody(t *testing.T) {
	h := &workspaceHandler{
		logger: slog.Default(),
		cfg:    conf.Config{},
	}

	tests := []struct {
		name          string
		body          string
		wantService   string
		wantVersion   string
		wantOperation string
	}{
		{
			name:          "GetFeature request",
			body:          `<wfs:GetFeature xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0"><wfs:Query typeNames="cities"/></wfs:GetFeature>`,
			wantService:   "WFS",
			wantVersion:   "2.0.0",
			wantOperation: "GETFEATURE",
		},
		{
			name:          "ListStoredQueries request",
			body:          `<wfs:ListStoredQueries xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0"/>`,
			wantService:   "WFS",
			wantVersion:   "2.0.0",
			wantOperation: "LISTSTOREDQUERIES",
		},
		{
			name:          "DescribeStoredQueries request",
			body:          `<wfs:DescribeStoredQueries xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0"/>`,
			wantService:   "WFS",
			wantVersion:   "2.0.0",
			wantOperation: "DESCRIBESTOREDQUERIES",
		},
		{
			name:          "DescribeFeatureType request",
			body:          `<wfs:DescribeFeatureType xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0"/>`,
			wantService:   "WFS",
			wantVersion:   "2.0.0",
			wantOperation: "DESCRIBEFEATURETYPE",
		},
		{
			name:          "GetCapabilities request",
			body:          `<wfs:GetCapabilities xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0"/>`,
			wantService:   "WFS",
			wantVersion:   "2.0.0",
			wantOperation: "GETCAPABILITIES",
		},
		{
			name:          "GetPropertyValue request",
			body:          `<wfs:GetPropertyValue xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0" valueReference="name"><wfs:Query typeNames="cities"/></wfs:GetPropertyValue>`,
			wantService:   "WFS",
			wantVersion:   "2.0.0",
			wantOperation: "GETPROPERTYVALUE",
		},
		{
			name:          "Self-closing element",
			body:          `<wfs:ListStoredQueries xmlns:wfs="http://www.opengis.net/wfs/2.0" handle="h1" service="WFS" version="2.0.0"/>`,
			wantService:   "WFS",
			wantVersion:   "2.0.0",
			wantOperation: "LISTSTOREDQUERIES",
		},
		{
			name:          "With count attribute",
			body:          `<wfs:GetFeature xmlns:wfs="http://www.opengis.net/wfs/2.0" count="10" service="WFS" version="2.0.0"><wfs:Query typeNames="cities"/></wfs:GetFeature>`,
			wantService:   "WFS",
			wantVersion:   "2.0.0",
			wantOperation: "GETFEATURE",
		},
		{
			name:          "Empty body",
			body:          ``,
			wantService:   "",
			wantVersion:   "",
			wantOperation: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request with body in context (simulating what handleWFS does)
			req := httptest.NewRequest("POST", "/wfs", bytes.NewReader([]byte(tt.body)))

			// Store body in context like handleWFS does
			bodyBytes := []byte(tt.body)
			if len(bodyBytes) > 0 {
				ctx := context.WithValue(req.Context(), bodyBytesKey, bodyBytes)
				req = req.WithContext(ctx)
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			gotService, gotVersion, gotOperation := h.getServiceAndOperationFromXMLBody(req)

			if gotService != tt.wantService {
				t.Errorf("service = %q, want %q", gotService, tt.wantService)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("version = %q, want %q", gotVersion, tt.wantVersion)
			}
			if gotOperation != tt.wantOperation {
				t.Errorf("operation = %q, want %q", gotOperation, tt.wantOperation)
			}
		})
	}
}

func TestWFSWriteOperationsRequireEditor(t *testing.T) {
	h := &workspaceHandler{logger: slog.Default()}
	ws := &workspace.Workspace{
		ID: "ws-1",
		Settings: &store.WorkspaceSettings{WFS: store.WFSSettings{
			Enabled: true,
			Public:  true,
		}},
	}

	tests := []struct {
		name       string
		identity   *identity.Identity
		wantStatus int
	}{
		{name: "anonymous", wantStatus: http.StatusUnauthorized},
		{name: "viewer", identity: &identity.Identity{Subject: "viewer", Roles: map[string]string{"ws-1": "viewer"}}, wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/wfs?SERVICE=WFS&REQUEST=Transaction", nil)
			ctx := workspace.WithWorkspace(req.Context(), ws)
			if tt.identity != nil {
				ctx = identity.WithIdentity(ctx, tt.identity)
			}
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()
			h.handleWFS(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestAnonymousMutationIdentity(t *testing.T) {
	ws := &workspace.Workspace{
		ID: "ws-1",
		Settings: &store.WorkspaceSettings{WFS: store.WFSSettings{
			Enabled: true,
			Public:  true,
		}},
	}
	operationTests := []struct {
		operation string
		wantAdmin bool
	}{
		{operation: "GETFEATURE", wantAdmin: false},
		{operation: "TRANSACTION", wantAdmin: true},
		{operation: "LOCKFEATURE", wantAdmin: true},
		{operation: "GETFEATUREWITHLOCK", wantAdmin: true},
		{operation: "CREATESTOREDQUERY", wantAdmin: true},
		{operation: "DROPSTOREDQUERY", wantAdmin: true},
	}
	for _, tt := range operationTests {
		t.Run(tt.operation, func(t *testing.T) {
			h := &workspaceHandler{cfg: conf.Config{WFS: conf.WFS{AllowAnonymousMutations: true}}}
			req := h.withAnonymousMutationIdentity(httptest.NewRequest(http.MethodPost, "/wfs", nil), ws, tt.operation)
			id, ok := identity.FromContext(req.Context())
			if tt.wantAdmin {
				if !ok || id == nil || !id.IsAdmin(ws.ID) {
					t.Fatalf("operation %s did not receive the anonymous admin identity", tt.operation)
				}
				if got := lockOwner(req); got != "anonymous:wfs-anonymous-mutation" {
					t.Fatalf("lock owner = %q", got)
				}
			} else if ok && id != nil {
				t.Fatalf("read operation received mutation identity: %#v", id)
			}
		})
	}

	t.Run("disabled", func(t *testing.T) {
		h := &workspaceHandler{}
		req := h.withAnonymousMutationIdentity(httptest.NewRequest(http.MethodPost, "/wfs", nil), ws, "TRANSACTION")
		if id, ok := identity.FromContext(req.Context()); ok && id != nil {
			t.Fatalf("disabled mode injected identity: %#v", id)
		}
	})

	t.Run("private workspace", func(t *testing.T) {
		h := &workspaceHandler{cfg: conf.Config{WFS: conf.WFS{AllowAnonymousMutations: true}}}
		private := &workspace.Workspace{
			ID:       ws.ID,
			Settings: &store.WorkspaceSettings{WFS: store.WFSSettings{Enabled: true, Public: false}},
		}
		req := h.withAnonymousMutationIdentity(httptest.NewRequest(http.MethodPost, "/wfs", nil), private, "TRANSACTION")
		if id, ok := identity.FromContext(req.Context()); ok && id != nil {
			t.Fatalf("private workspace injected identity: %#v", id)
		}
	})

	t.Run("preserves authenticated identity", func(t *testing.T) {
		h := &workspaceHandler{cfg: conf.Config{WFS: conf.WFS{AllowAnonymousMutations: true}}}
		original := &identity.Identity{Subject: "editor", Roles: map[string]string{ws.ID: "editor"}}
		req := httptest.NewRequest(http.MethodPost, "/wfs", nil)
		req = req.WithContext(identity.WithIdentity(req.Context(), original))
		req = h.withAnonymousMutationIdentity(req, ws, "TRANSACTION")
		got, _ := identity.FromContext(req.Context())
		if got != original {
			t.Fatal("authenticated identity was replaced")
		}
	})
}

func TestXMLBodyTakesPrecedenceOverQueryParam(t *testing.T) {
	// This test verifies that for POST requests, the XML body operation
	// takes precedence over query parameters.
	// This is important because CITE tests add ?request=GetCapabilities to all URLs.
	h := &workspaceHandler{
		logger: slog.Default(),
		cfg:    conf.Config{},
	}

	tests := []struct {
		name          string
		queryRequest  string // REQUEST query param
		body          string
		wantOperation string
	}{
		{
			name:          "XML body overrides query param",
			queryRequest:  "GetCapabilities",
			body:          `<wfs:ListStoredQueries xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0"/>`,
			wantOperation: "LISTSTOREDQUERIES",
		},
		{
			name:          "GetFeature body with GetCapabilities query",
			queryRequest:  "GetCapabilities",
			body:          `<wfs:GetFeature xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0"><wfs:Query typeNames="cities"/></wfs:GetFeature>`,
			wantOperation: "GETFEATURE",
		},
		{
			name:          "Empty body uses query param",
			queryRequest:  "GetCapabilities",
			body:          ``,
			wantOperation: "", // Empty body returns empty operation from XML extraction
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request with query param
			req := httptest.NewRequest("POST", "/wfs?request="+tt.queryRequest, bytes.NewReader([]byte(tt.body)))

			// Store body in context like handleWFS does
			bodyBytes := []byte(tt.body)
			if len(bodyBytes) > 0 {
				ctx := context.WithValue(req.Context(), bodyBytesKey, bodyBytes)
				req = req.WithContext(ctx)
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			_, _, gotOperation := h.getServiceAndOperationFromXMLBody(req)

			if gotOperation != tt.wantOperation {
				t.Errorf("operation = %q, want %q", gotOperation, tt.wantOperation)
			}
		})
	}
}

func TestGetBodyBytes(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() *http.Request
		wantBody  string
		wantErr   bool
	}{
		{
			name: "body in context",
			setupFunc: func() *http.Request {
				req := httptest.NewRequest("POST", "/wfs", nil)
				ctx := context.WithValue(req.Context(), bodyBytesKey, []byte("test body"))
				return req.WithContext(ctx)
			},
			wantBody: "test body",
			wantErr:  false,
		},
		{
			name: "body from request",
			setupFunc: func() *http.Request {
				return httptest.NewRequest("POST", "/wfs", bytes.NewReader([]byte("test body")))
			},
			wantBody: "test body",
			wantErr:  false,
		},
		{
			name: "nil body",
			setupFunc: func() *http.Request {
				req := httptest.NewRequest("POST", "/wfs", nil)
				req.Body = nil
				return req
			},
			wantBody: "",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupFunc()
			body, err := GetBodyBytes(req)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetBodyBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(body) != tt.wantBody {
				t.Errorf("GetBodyBytes() = %q, want %q", string(body), tt.wantBody)
			}
		})
	}
}
