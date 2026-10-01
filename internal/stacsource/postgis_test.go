package stacsource

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tobilg/neoserver/internal/datasource"
	_ "github.com/tobilg/neoserver/internal/datasource/postgis"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// The opt-in target uses a disposable PostGIS database; all test objects live
// in one uniquely named schema and are removed when the test finishes.
func TestPostGISPublicationAndSQLViewBoundary(t *testing.T) {
	dsn := os.Getenv("NEOSRV_STAC_TEST_POSTGIS")
	if dsn == "" {
		t.Skip("set NEOSRV_STAC_TEST_POSTGIS to a disposable PostGIS database")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	schema := fmt.Sprintf("stac_%d", time.Now().UnixNano())
	_, err = connection.Exec(ctx, `CREATE SCHEMA `+schema+`;CREATE TABLE `+schema+`.scenes AS SELECT i id,'scene-'||i scene_id,'2026-01-01T00:00:00Z'::timestamptz acquired,'https://example.org/'||i||'.tif' href,i=1 published,ST_SetSRID(ST_Point(i,2),4326) geom FROM generate_series(1,2) t(i);ALTER TABLE `+schema+`.scenes ADD PRIMARY KEY(id);`)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	config := connection.Config()
	raw, _ := json.Marshal(store.PostGISConnectionInfo{Host: config.Host, Port: int(config.Port), Database: config.Database, User: config.User, Password: config.Password, SSLMode: "disable", Schemas: []string{schema}})
	ds, err := datasource.CreateFromService(&store.Service{ID: "source", Type: store.ServiceTypePostGIS, ConnectionInfo: raw})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	for _, view := range []bool{false, true} {
		t.Run(fmt.Sprintf("sql-view=%t", view), func(t *testing.T) {
			layer := &workspace.Layer{ID: "layer", PublicID: "scenes", SourceLayer: schema + ".scenes", Enabled: true, Public: true}
			if view {
				layer.IsSQLView = true
				layer.SourceLayer = "DO_NOT_QUERY"
				layer.SQLViewConfig = &workspace.SQLViewConfig{SQL: "SELECT scene_id,acquired,href,geom FROM " + schema + ".scenes WHERE published=true", IDColumn: "scene_id", GeometryColumn: "geom", GeometryType: "POINT", SRID: 4326, ReadOnly: true, Properties: []*workspace.SQLViewProperty{{Name: "scene_id", Type: "text"}, {Name: "acquired", Type: "timestamptz"}, {Name: "href", Type: "text"}}}
			}
			ws := &workspace.Workspace{ID: "workspace", Name: "demo", Services: map[string]*workspace.Service{"source": {ID: "source", Enabled: true, Type: store.ServiceTypePostGIS, ConnectionInfo: raw, DataSource: ds, Layers: map[string]*workspace.Layer{"scenes": layer}}}}
			binding := &staccatalog.Binding{ID: "binding", ServiceID: "source", ResourceID: "layer", ResourceKind: "layer", Mode: "mapped", Mapping: staccatalog.Mapping{ID: "scene_id", Datetime: staccatalog.Value{Property: "acquired"}, Assets: map[string]staccatalog.AssetMapping{"data": {Href: staccatalog.Value{Property: "href"}}}}}
			c := &staccatalog.Collection{Document: stacmodel.Collection("scenes", "Scenes", "Source mapping", "other", nil), Binding: binding}
			count := 0
			adapter := Adapter{}
			_, err := adapter.Scan(ctx, ws, c, 100, func(d stacmodel.Document, _ []staccatalog.LocalAsset) error {
				count++
				return stacmodel.Validate(d, "item")
			})
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if view {
				want = 1
			}
			if count != want {
				t.Fatalf("source publication returned %d Items, want %d", count, want)
			}
		})
	}
}
