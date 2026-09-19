package mgmt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
)

type publicationTestSource struct{ datasource.DataSource }

func (*publicationTestSource) ID() string                   { return "test" }
func (*publicationTestSource) Type() store.ServiceType      { return store.ServiceTypePostGIS }
func (*publicationTestSource) Close() error                 { return nil }
func (*publicationTestSource) Health(context.Context) error { return nil }
func (*publicationTestSource) GetLayerInfo(_ context.Context, name string) (*datasource.LayerInfo, error) {
	if name == "missing" {
		return nil, errors.New("missing table")
	}
	return &datasource.LayerInfo{Name: name, GeometryColumn: "geom", SRID: 4326}, nil
}
func (*publicationTestSource) Query(_ context.Context, name string, _ datasource.QueryParams) ([]json.RawMessage, error) {
	if name == "dropped" {
		return nil, errors.New("table removed after discovery")
	}
	return nil, nil
}
func publicationTestFactory(*store.Service) (datasource.DataSource, error) {
	return &publicationTestSource{}, nil
}

func TestPublishValidatesSourceAndPermitsExplicitDisabledDraft(t *testing.T) {
	for _, tc := range []struct {
		source  string
		enabled bool
		code    int
	}{{"missing", true, 422}, {"dropped", true, 422}, {"missing", false, 201}} {
		catalog := newMockStore()
		catalog.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
		catalog.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1", Enabled: true}
		h := newTestHandlerWithEnforcer(t, catalog, publicationTestFactory)
		body, _ := json.Marshal(map[string]any{"source_layer": tc.source, "public_id": "test", "enabled": tc.enabled})
		r := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
		r = r.WithContext(withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1"}))
		w := httptest.NewRecorder()
		h.createLayer(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.source, w.Code, w.Body.String())
		}
		if tc.code != 201 && len(catalog.layers) != 0 {
			t.Fatal("rejected publication left a layer")
		}
	}
}

func TestLayerMetadataClearsAndConflicts(t *testing.T) {
	h, router, ws, key := releaseWorkflowFixture(t)
	svc, err := h.registry.CreateService(context.Background(), store.CreateServiceInput{WorkspaceID: ws, Name: "source", Type: store.ServiceTypePostGIS, ConnectionInfo: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	layer, err := h.registry.CreateLayer(context.Background(), ws, svc.ID, store.CreateLayerInput{ServiceID: svc.ID, PublicID: "roads", SourceLayer: "public.roads", Description: "Original", Dimensions: []*store.Dimension{{Name: "time"}}})
	if err != nil {
		t.Fatal(err)
	}
	path := "/workspaces/workflow/services/source/layers/" + layer.ID
	response := workflowRequest(router, key, "PUT", path, `{"description":"","dimensions":[]}`)
	if response.Code != 200 {
		t.Fatalf("clear: %d %s", response.Code, response.Body.String())
	}
	stored, err := h.store.GetLayer(context.Background(), layer.ID)
	if err != nil || stored.Description != "" || len(stored.Dimensions) != 0 {
		t.Fatalf("not cleared: %+v %v", stored, err)
	}
	response = workflowRequest(router, key, "POST", "/workspaces/workflow/services/source/layers", `{"public_id":"roads","source_layer":"public.roads","enabled":false}`)
	if response.Code != 409 {
		t.Fatalf("duplicate: %d %s", response.Code, response.Body.String())
	}
	_, err = h.registry.CreateLayer(context.Background(), ws, svc.ID, store.CreateLayerInput{ServiceID: svc.ID, PublicID: "occupied", SourceLayer: "public.roads"})
	if err != nil {
		t.Fatal(err)
	}
	response = workflowRequest(router, key, "PUT", path, `{"public_id":"occupied"}`)
	if response.Code != 409 {
		t.Fatalf("rename conflict: %d %s", response.Code, response.Body.String())
	}
	stored, err = h.store.GetLayer(context.Background(), layer.ID)
	if err != nil || stored.PublicID != "roads" {
		t.Fatalf("conflicting rename changed publication: %+v %v", stored, err)
	}
}
