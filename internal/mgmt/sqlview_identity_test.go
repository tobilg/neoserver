package mgmt

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
)

type identityPublicationSource struct {
	datasource.SQLViewDataSource
	suggested string
	reject    bool
	checkedID string
}

func (*identityPublicationSource) ID() string                   { return "test" }
func (*identityPublicationSource) Type() store.ServiceType      { return store.ServiceTypePostGIS }
func (*identityPublicationSource) Close() error                 { return nil }
func (*identityPublicationSource) Health(context.Context) error { return nil }

func (*identityPublicationSource) ValidateSQLView(context.Context, string) error { return nil }
func (s *identityPublicationSource) DiscoverSQLViewColumns(context.Context, string) (*datasource.SQLViewDiscovery, error) {
	return &datasource.SQLViewDiscovery{
		GeometryColumn: "geom", SuggestedIDColumn: s.suggested,
		Columns: []datasource.PropertyInfo{{Name: "record_key", JSONType: datasource.JSONTypeString}, {Name: "name", JSONType: datasource.JSONTypeString}},
	}, nil
}
func (s *identityPublicationSource) ValidateSQLViewIdentity(_ context.Context, config *datasource.SQLViewConfig) error {
	s.checkedID = config.IDColumn
	if s.reject {
		return datasource.ErrSQLViewIdentity
	}
	return nil
}

func TestSQLViewPublicationOwnsSourceAndValidatesIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, id, suggested string
		reject              bool
		status              int
	}{
		{"explicit custom ID", "record_key", "", false, 201},
		{"suggested ID", "", "record_key", false, 201},
		{"missing ID", "", "", false, 400},
		{"column outside view", "secret", "", false, 400},
		{"duplicate or null ID", "record_key", "", true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := newMockStore()
			catalog.workspaces["ws-1"] = &store.Workspace{ID: "ws-1", Name: "workspace-1"}
			catalog.services["svc-1"] = &store.Service{ID: "svc-1", WorkspaceID: "ws-1", Name: "service-1", Enabled: true}
			source := &identityPublicationSource{suggested: tc.suggested, reject: tc.reject}
			h := newTestHandlerWithEnforcer(t, catalog, func(*store.Service) (datasource.DataSource, error) { return source, nil })
			body, _ := json.Marshal(map[string]any{"public_id": "view", "source_layer": "physical_secret", "sql_view": map[string]any{
				"sql": "SELECT record_key, name, geom FROM records", "geometry_column": "geom", "id_column": tc.id, "srid": 4326,
			}})
			r := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
			r = r.WithContext(withChiContext(r.Context(), map[string]string{"workspace": "ws-1", "service": "svc-1"}))
			w := httptest.NewRecorder()
			h.createLayer(w, r)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if tc.status != 201 {
				if len(catalog.layers) != 0 {
					t.Fatal("rejected view persisted")
				}
				return
			}
			if source.checkedID != "record_key" {
				t.Fatal("identity was not validated")
			}
			for _, layer := range catalog.layers {
				if layer.SourceLayer != "_sql_view_" || layer.SQLViewConfig.IDColumn != "record_key" {
					t.Fatalf("unsafe publication: %+v", layer)
				}
			}
		})
	}
}
