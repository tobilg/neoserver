package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/importer"
	"github.com/tobilg/neoserver/internal/rbac"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func releaseWorkflowFixture(t *testing.T) (*handler, http.Handler, string, string) {
	t.Helper()
	root := t.TempDir()
	ctx := context.Background()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.db"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { catalog.Close() })
	registry := workspace.NewRegistry(catalog, nil)
	t.Cleanup(func() { registry.Close() })
	ws, err := registry.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "workflow"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := catalog.CreateAPIKey(ctx, store.CreateAPIKeyInput{WorkspaceID: &ws.ID, RoleID: "admin", Name: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	enforcer, err := rbac.NewEnforcerWithDefaults(rbac.NewStoreAdapter(catalog))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := conf.Config{Importer: conf.Importer{Enabled: true, Root: filepath.Join(root, "imports"), TemporaryDirectory: filepath.Join(root, "staging"), MaxUploadBytes: 1 << 20, MaxConcurrentJobs: 1, WorkerCount: 1, ShutdownTimeoutSec: 2, UploadTimeoutSec: 10, UploadIdleTimeoutSec: 5}}
	imports, err := importer.New(ctx, cfg.Importer, catalog, registry, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { imports.Close(context.Background()) })
	h := &handler{store: catalog, registry: registry, logger: logger, cfg: cfg, importer: imports}
	router := chi.NewRouter()
	router.Use(identity.Middleware(identity.MiddlewareConfig{Store: catalog, RequireAuth: true}))
	RegisterRoutes(router, Dependencies{Store: catalog, Registry: registry, Enforcer: enforcer, Logger: logger, Config: cfg, Importer: imports})
	return h, router, ws.ID, key.Key
}

func workflowRequest(router http.Handler, key, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestDuplicateClaimMappingReturnsConflict(t *testing.T) {
	_, router, _, key := releaseWorkflowFixture(t)
	body := `{"claim_name":"realm_access.roles","claim_value":"gis-admins","role_id":"admin"}`
	for _, status := range []int{http.StatusCreated, http.StatusConflict} {
		w := workflowRequest(router, key, http.MethodPost, "/workspaces/workflow/claim-mappings", body)
		if w.Code != status {
			t.Fatalf("expected %d, got %d: %s", status, w.Code, w.Body.String())
		}
	}
}

func TestServiceNamesAndManagedBindingHTTPContract(t *testing.T) {
	h, router, ws, key := releaseWorkflowFixture(t)
	svc, err := h.registry.CreateService(context.Background(), store.CreateServiceInput{WorkspaceID: ws, Name: "source", Type: store.ServiceTypePostGIS, ConnectionInfo: json.RawMessage(`{"host":"old","password":"retained"}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{svc.Name, svc.ID} {
		w := workflowRequest(router, key, "PUT", "/workspaces/workflow/services/"+id, `{"connection_info":{"host":"new"}}`)
		if w.Code != 200 {
			t.Fatalf("alias %s: %d %s", id, w.Code, w.Body.String())
		}
		stored, err := h.store.GetService(context.Background(), svc.ID)
		if err != nil || !bytes.Contains(stored.ConnectionInfo, []byte("retained")) {
			t.Fatal("secret retention failed")
		}
	}
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/services", `{"name":"foreign","type":"duckdb","connection_info":{"managed_import_id":"victim-id"}}`},
		{"POST", "/services/test-connection", `{"type":"duckdb","connection_info":{"managed_import_id":"victim-id"}}`},
		{"PUT", "/services/" + svc.ID, `{"connection_info":{"managed_import_id":"victim-id"}}`},
		{"POST", "/services/" + svc.ID + "/test-connection", `{"connection_info":{"managed_import_id":"victim-id"}}`},
	} {
		w := workflowRequest(router, key, tc.method, "/workspaces/workflow"+tc.path, tc.body)
		if w.Code != 400 {
			t.Fatalf("binding accepted: %s %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestUploadedDisguisedVRTFailsBeforePublication(t *testing.T) {
	h, router, _, key := releaseWorkflowFixture(t)
	var requests atomic.Int64
	sentinel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/geo+json")
		_, _ = io.WriteString(w, `{"type":"FeatureCollection","name":"fixture","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"marker":"denied"}}]}`)
	}))
	defer sentinel.Close()
	t.Cleanup(func() {
		if requests.Load() != 0 {
			t.Error("uploaded wrapper contacted a private-network source")
		}
	})
	// An unsupported wrapper must fail, including when disguised as an
	// otherwise supported upload extension.
	body := `<OGRVRTDataSource><OGRVRTLayer name="proxy"><SrcDataSource>` + sentinel.URL + `/fixture.geojson</SrcDataSource><SrcLayer>fixture</SrcLayer></OGRVRTLayer></OGRVRTDataSource>`
	var buffer bytes.Buffer
	multipartWriter := multipart.NewWriter(&buffer)
	file, err := multipartWriter.CreateFormFile("file", "proxy.geojson")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	multipartWriter.Close()
	r := httptest.NewRequest("POST", "/workspaces/workflow/imports", &buffer)
	r.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	r.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("upload=%d %s", w.Code, w.Body.String())
	}
	var job store.ImportJob
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		stored, err := h.importer.Get(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == store.ImportFailed {
			if stored.Discovery != nil && len(stored.Discovery.Layers) != 0 {
				t.Fatal("indirect layers discovered")
			}
			return
		}
		if stored.Status == store.ImportAwaitingPlan {
			t.Fatal("VRT upload reached publication planning")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("unsafe upload did not reach failed status")
}

func TestAlternativeStyleFormatsBindThroughCommonCompiler(t *testing.T) {
	h, router, ws, key := releaseWorkflowFixture(t)
	for _, tc := range []struct{ format, body string }{
		{"css", `* { fill: #ff0000; }`},
		{"ysld", "feature-styles:\n- rules:\n  - symbolizers:\n    - line:\n        color: '#ff0000'\n        width: 3\n"},
		{"mapbox", `{"version":8,"sources":{},"layers":[{"id":"areas","type":"fill","paint":{"fill-color":"#ff0000"}}]}`},
	} {
		payload, _ := json.Marshal(map[string]string{"name": tc.format, "format": tc.format, "body": tc.body})
		w := workflowRequest(router, key, "POST", "/workspaces/workflow/styles", string(payload))
		if w.Code != 201 {
			t.Fatalf("%s create: %d %s", tc.format, w.Code, w.Body.String())
		}
		if err := h.validateStyleBindings(context.Background(), ws, tc.format, nil, false); err != nil {
			t.Fatalf("%s bind: %v", tc.format, err)
		}
		if err := h.validateGroupStyleBindings(context.Background(), ws, tc.format, nil); err != nil {
			t.Fatalf("%s group bind: %v", tc.format, err)
		}
	}
}

func TestStyleWritesRejectContentAfterSLDRoot(t *testing.T) {
	_, router, _, key := releaseWorkflowFixture(t)
	const valid = `<StyledLayerDescriptor version="1.0.0"><NamedLayer><Name>layer</Name><UserStyle><FeatureTypeStyle><Rule><PolygonSymbolizer><Fill><CssParameter name="fill">#5fa8cc</CssParameter></Fill></PolygonSymbolizer></Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`
	const broken = valid + "\n<broken"
	payload, _ := json.Marshal(map[string]string{"name": "trailing", "format": "sld_1.0.0", "body": broken})
	if w := workflowRequest(router, key, "POST", "/workspaces/workflow/styles", string(payload)); w.Code != 400 {
		t.Fatalf("create with trailing content: %d %s", w.Code, w.Body.String())
	}
	payload, _ = json.Marshal(map[string]string{"name": "trailing", "format": "sld_1.0.0", "body": valid})
	if w := workflowRequest(router, key, "POST", "/workspaces/workflow/styles", string(payload)); w.Code != 201 {
		t.Fatalf("create valid style: %d %s", w.Code, w.Body.String())
	}
	payload, _ = json.Marshal(map[string]string{"body": broken})
	w := workflowRequest(router, key, "PUT", "/workspaces/workflow/styles/trailing", string(payload))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "after the root element") {
		t.Fatalf("update with trailing content: %d %s", w.Code, w.Body.String())
	}
	w = workflowRequest(router, key, "GET", "/workspaces/workflow/styles/trailing", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":true`) || strings.Contains(w.Body.String(), "broken") {
		t.Fatalf("stored style changed: %d %s", w.Code, w.Body.String())
	}
}

func TestCreatedWorkspaceReportsTimestampsAndRoleErrorsListRoles(t *testing.T) {
	h, router, _, key := releaseWorkflowFixture(t)
	recorder := httptest.NewRecorder()
	h.createWorkspace(recorder, httptest.NewRequest(http.MethodPost, "/workspaces", strings.NewReader(`{"name":"fresh"}`)))
	var created WorkspaceResponse
	if recorder.Code != http.StatusCreated || json.Unmarshal(recorder.Body.Bytes(), &created) != nil {
		t.Fatalf("create workspace: %d %s", recorder.Code, recorder.Body.String())
	}
	if created.CreatedAt.IsZero() || time.Since(created.CreatedAt) > time.Minute {
		t.Fatalf("created_at = %s", created.CreatedAt)
	}
	w := workflowRequest(router, key, "POST", "/workspaces/workflow/apikeys", `{"name":"client","role":"viewer"}`)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "one of: admin, editor, super_admin, viewer") {
		t.Fatalf("missing role_id: %d %s", w.Code, w.Body.String())
	}
}

func TestDataSourceOpenFailuresAreClientErrors(t *testing.T) {
	err := errors.Join(&workspace.DataSourceError{Err: errors.New(`path "/x" is not permitted by the datasource allowlist`)}, nil)
	detail, ok := dataSourceErrorDetail(err)
	if !ok || !strings.Contains(detail, "not permitted by the datasource allowlist") {
		t.Fatalf("detail=%q ok=%v", detail, ok)
	}
	if _, ok := dataSourceErrorDetail(errors.New("disk full")); ok {
		t.Fatal("unrelated errors must stay internal")
	}
}

func TestWorkspaceNamesMustBeURLSafe(t *testing.T) {
	h, _, _, _ := releaseWorkflowFixture(t)
	for name, want := range map[string]int{
		"My Demo":        http.StatusBadRequest,
		"-leading":       http.StatusBadRequest,
		"a/b":            http.StatusBadRequest,
		"demo-2026_v1.0": http.StatusCreated,
	} {
		body, _ := json.Marshal(map[string]string{"name": name})
		recorder := httptest.NewRecorder()
		h.createWorkspace(recorder, httptest.NewRequest(http.MethodPost, "/workspaces", bytes.NewReader(body)))
		if recorder.Code != want {
			t.Errorf("%q: status %d, want %d (%s)", name, recorder.Code, want, recorder.Body.String())
		}
	}
}

func TestSessionListItemsExplainConsoleAccess(t *testing.T) {
	denied := newSessionListItem(&store.BrowserSession{AuthMethod: "oidc", Roles: map[string]string{"ws": "viewer"},
		Claims: map[string]any{"iss": "https://idp", "sub": "u1", "email": "u@example.org", "realm_access.roles": []any{"gis-nobody"}, "exp": 1}})
	if denied.ConsoleAccess {
		t.Fatal("viewer session reported console access")
	}
	claims, _ := denied.PresentedClaims["claims"].(map[string]any)
	if denied.PresentedClaims["iss"] != "https://idp" || claims["realm_access.roles"] == nil || claims["exp"] != nil {
		t.Fatalf("presented claims = %+v", denied.PresentedClaims)
	}
	for _, session := range []*store.BrowserSession{
		{AuthMethod: "password", GlobalRole: "super_admin"},
		{AuthMethod: "jwt", Roles: map[string]string{"*": "admin"}},
		{AuthMethod: "oidc", Roles: map[string]string{"ws": "admin"}},
	} {
		if item := newSessionListItem(session); !item.ConsoleAccess {
			t.Errorf("%+v: expected console access", session)
		}
	}
	if item := newSessionListItem(&store.BrowserSession{AuthMethod: "password"}); item.PresentedClaims != nil {
		t.Fatal("non-OIDC sessions must not present claims")
	}
}

func TestRevokedAPIKeysCanBeDeletedPermanently(t *testing.T) {
	h, router, wsID, key := releaseWorkflowFixture(t)
	ctx := context.Background()
	created, err := h.store.CreateAPIKey(ctx, store.CreateAPIKeyInput{WorkspaceID: &wsID, RoleID: "viewer", Name: "client"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := h.store.(*store.DuckDBStore).CreateBrowserSession(ctx, store.BrowserSession{
		TokenHash: "client-session", CSRFHash: "csrf", Subject: "client", AuthMethod: "apikey", CredentialID: created.ID,
		Roles: map[string]string{wsID: "viewer"}, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	permanent := "/workspaces/workflow/apikeys/" + created.ID + "/permanent"

	if w := workflowRequest(router, key, http.MethodDelete, permanent, ""); w.Code != http.StatusConflict {
		t.Fatalf("deleting an active key: %d %s", w.Code, w.Body.String())
	}
	if w := workflowRequest(router, key, http.MethodDelete, "/workspaces/workflow/apikeys/"+created.ID, ""); w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	if w := workflowRequest(router, key, http.MethodDelete, permanent, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete revoked key: %d %s", w.Code, w.Body.String())
	}

	list := workflowRequest(router, key, http.MethodGet, "/workspaces/workflow/apikeys", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), created.ID) {
		t.Fatalf("deleted key still listed: %d %s", list.Code, list.Body.String())
	}
	if w := workflowRequest(router, key, http.MethodDelete, permanent, ""); w.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d %s", w.Code, w.Body.String())
	}
	sessions, err := h.store.(*store.DuckDBStore).ListBrowserSessions(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions {
		if session.CredentialID == created.ID {
			t.Fatalf("session of the deleted key remains: %+v", session)
		}
	}

	// A key of another workspace cannot be deleted through this workspace.
	other, err := h.registry.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := h.store.CreateAPIKey(ctx, store.CreateAPIKeyInput{WorkspaceID: &other.ID, RoleID: "viewer", Name: "foreign"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.RevokeAPIKey(ctx, foreign.ID); err != nil {
		t.Fatal(err)
	}
	if w := workflowRequest(router, key, http.MethodDelete, "/workspaces/workflow/apikeys/"+foreign.ID+"/permanent", ""); w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace delete: %d %s", w.Code, w.Body.String())
	}
}
