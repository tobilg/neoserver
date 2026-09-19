package rbac

import (
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/workspace"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperationPoliciesReuseCasbinModel(t *testing.T) {
	enforcer, err := NewEnforcer(NewMemoryAdapter())
	if err != nil {
		t.Fatal(err)
	}
	if err = enforcer.AddOperationPolicy("analyst", "ws-1", "wfs", "GetFeature", ActionRead); err != nil {
		t.Fatal(err)
	}
	if err = enforcer.AddOperationPolicy("publisher", "ws-1", "wfs", "", ActionWrite); err != nil {
		t.Fatal(err)
	}
	allowed, _ := enforcer.CanAccessOperation("analyst", "ws-1", "wfs", "GetFeature", ActionRead)
	denied, _ := enforcer.CanAccessOperation("analyst", "ws-1", "wfs", "Transaction", ActionWrite)
	serviceWide, _ := enforcer.CanAccessOperation("publisher", "ws-1", "wfs", "Transaction", ActionWrite)
	if !allowed || denied || !serviceWide {
		t.Fatalf("operation decisions: allowed=%v denied=%v serviceWide=%v", allowed, denied, serviceWide)
	}
}

func TestOperationAuthorizationUsesXMLAndBindsCustomGrant(t *testing.T) {
	e, _ := NewEnforcer(NewMemoryAdapter())
	if err := e.AddOperationPolicy("custom", "ws", "wfs", "GetCapabilities", ActionRead); err != nil {
		t.Fatal(err)
	}
	h := RequireServiceOperation(e, "wfs")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && !HasOperationGrant(r, "ws", "wfs", ActionWrite) {
			t.Error("missing exact write grant")
		}
		if HasOperationGrant(r, "other", "wfs", ActionWrite) {
			t.Error("grant escaped workspace")
		}
		w.WriteHeader(204)
	}))
	call := func(method, path, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		ctx := identity.WithIdentity(r.Context(), &identity.Identity{Roles: map[string]string{"ws": "custom"}})
		ctx = workspace.WithWorkspace(ctx, &workspace.Workspace{ID: "ws"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r.WithContext(ctx))
		return w.Code
	}
	if status := call("POST", "/workspaces/demo/wfs?request=GetCapabilities", `<GetFeature/>`); status != 403 {
		t.Fatalf("read bypass: %d", status)
	}
	if status := call("POST", "/workspaces/demo/wfs?request=GetCapabilities", `<Transaction/>`); status != 403 {
		t.Fatalf("write bypass: %d", status)
	}
	if status := call("GET", "/workspaces/demo/wfs?request=GetCapabilities&REQUEST=Transaction", ""); status != 400 {
		t.Fatalf("duplicate: %d", status)
	}
	if err := e.AddOperationPolicy("custom", "ws", "wfs", "Transaction", ActionWrite); err != nil {
		t.Fatal(err)
	}
	if status := call("POST", "/workspaces/demo/wfs?request=GetCapabilities", `<Transaction/>`); status != 204 {
		t.Fatalf("granted write: %d", status)
	}
}

func TestOperationActionUsesProtocolSemanticsForPOST(t *testing.T) {
	if operationAction("wms", "GETMAP", http.MethodPost) {
		t.Fatal("WMS GetMap POST was classified as a write")
	}
	if operationAction("wcs", "GETCOVERAGE", http.MethodPost) {
		t.Fatal("WCS GetCoverage POST was classified as a write")
	}
	if !operationAction("wfs", "TRANSACTION", http.MethodPost) {
		t.Fatal("WFS Transaction POST was classified as a read")
	}
}

func TestRequestOperationReadsAndRestoresXMLBody(t *testing.T) {
	body := `<wfs:GetFeature xmlns:wfs="http://www.opengis.net/wfs/2.0"/>`
	request, err := http.NewRequest(http.MethodPost, "http://example.test/wfs", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	operation, updated := requestOperation(request)
	if operation != "GETFEATURE" || updated == nil {
		t.Fatalf("operation=%q updated=%v", operation, updated != nil)
	}
	restored, err := io.ReadAll(updated.Body)
	if err != nil || string(restored) != body {
		t.Fatalf("restored body=%q err=%v", restored, err)
	}
}
