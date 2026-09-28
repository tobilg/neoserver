package wcs

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestRequireAuthEnforcesWorkspaceAccess(t *testing.T) {
	ws := &workspace.Workspace{ID: "ws-1"}
	member := &identity.Identity{Subject: "ada", Roles: map[string]string{"ws-1": "viewer"}}
	outsider := &identity.Identity{Subject: "bob", Roles: map[string]string{"ws-2": "admin"}}
	for _, tc := range []struct {
		name         string
		public       bool
		requireHTTPS bool
		id           *identity.Identity
		want         int // 0: request continues
	}{
		{name: "public service", public: true},
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "other workspace", id: outsider, want: http.StatusForbidden},
		{name: "member", id: member},
		{name: "plaintext with HTTPS required", requireHTTPS: true, id: member, want: http.StatusUpgradeRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &handler{cfg: conf.Config{Auth: conf.Auth{RequireHTTPS: tc.requireHTTPS}}}
			request := httptest.NewRequest(http.MethodGet, "/workspaces/ws-1/wcs?SERVICE=WCS&REQUEST=GetCoverage", nil)
			if tc.id != nil {
				request = request.WithContext(identity.WithIdentity(context.Background(), tc.id))
			}
			response := httptest.NewRecorder()
			stopped := h.requireAuth(response, request, ws, tc.public)
			if stopped != (tc.want != 0) || (tc.want != 0 && response.Code != tc.want) {
				t.Fatalf("stopped=%v status=%d, want %d", stopped, response.Code, tc.want)
			}
		})
	}
}

func TestExceptionReportsFollowOWS20(t *testing.T) {
	for _, tc := range []struct {
		err     *requestError
		status  int
		code    string
		locator string
	}{
		{missing("coverageId"), http.StatusBadRequest, "MissingParameterValue", "coverageId"},
		{invalid("format", "unsupported format"), http.StatusBadRequest, "InvalidParameterValue", "format"},
		{noCoverage(), http.StatusNotFound, "NoSuchCoverage", "coverageId"},
		{invalidAxis("unknown axis"), http.StatusNotFound, "InvalidAxisLabel", "subset"},
	} {
		response := httptest.NewRecorder()
		writeException(response, tc.err)
		var report exceptionReport
		if err := xml.Unmarshal(response.Body.Bytes(), &report); err != nil {
			t.Fatalf("%s: %v", tc.code, err)
		}
		if response.Code != tc.status || response.Header().Get("Content-Type") != "application/xml" ||
			report.XMLName.Space != "http://www.opengis.net/ows/2.0" || report.Version != "2.0.0" ||
			report.Error.Code != tc.code || report.Error.Locator != tc.locator || report.Error.Text == "" {
			t.Errorf("%s: status %d report %+v", tc.code, response.Code, report)
		}
	}
}
