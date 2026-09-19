package mgmt

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestUpdateWMTSSettingsValidatesProviderSiteAndAppliesDefaults(t *testing.T) {
	mockStore := newMockStore()
	workspace, err := mockStore.CreateWorkspace(t.Context(), store.CreateWorkspaceInput{Name: "wmts-settings"})
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, mockStore)

	run := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/"+workspace.ID+"/settings/wmts", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		request = request.WithContext(withChiContext(request.Context(), map[string]string{"workspace": workspace.ID}))
		recorder := httptest.NewRecorder()
		h.updateWMTSSettings(recorder, request)
		return recorder
	}

	invalid := run(`{"enabled":true,"public":true,"feature_info_enabled":true,"provider_site":"relative/path"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid provider site status = %d, body=%s", invalid.Code, invalid.Body.String())
	}
	valid := run(`{"enabled":true,"public":true,"feature_info_enabled":true,"provider_site":"https://example.test/maps"}`)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid settings status = %d, body=%s", valid.Code, valid.Body.String())
	}
	settings := mockStore.wmtsSettings[workspace.ID]
	if settings.ProviderName != "neoserver" || settings.ProviderSite != "https://example.test/maps" || settings.VectorTilesEnabled {
		t.Fatalf("stored WMTS settings = %+v", settings)
	}
}

func TestWMTSSettingsOpenAPISchemaIncludesProviderAndVectorFields(t *testing.T) {
	schema := getSchemas()["WMTSSettings"].Value
	for _, name := range []string{"vector_tiles_enabled", "provider_name", "provider_site", "contact_name", "contact_position", "contact_email"} {
		if schema.Properties[name] == nil {
			t.Errorf("WMTSSettings schema missing %q", name)
		}
	}
}
