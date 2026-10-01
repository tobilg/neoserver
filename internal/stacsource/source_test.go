package stacsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	_ "github.com/tobilg/neoserver/internal/datasource/duckdb"
	_ "github.com/tobilg/neoserver/internal/datasource/geoparquet"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	_ "github.com/tobilg/neoserver/internal/datasource/rasterfile"
	_ "github.com/tobilg/neoserver/internal/datasource/vectorfile"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestProviderPublicationsAndSQLViewBoundary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	pathpolicy.Configure([]string{root + "/**"})
	database := filepath.Join(root, "source.duckdb")
	parquet := filepath.Join(root, "scenes.parquet")
	geojson := filepath.Join(root, "scenes.geojson")
	db, err := sql.Open("duckdb", database)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`LOAD spatial;CREATE TABLE scenes AS SELECT i::INTEGER id, 'scene-'||i scene_id, '2026-01-01T00:00:00Z' acquired, 'https://example.org/data/'||i||'.tif' href, i=1 published, ST_Point(7+i,52) geom FROM range(1,3) t(i);CREATE SCHEMA __neoserver;CREATE TABLE __neoserver.layers(name VARCHAR,srid INTEGER);INSERT INTO __neoserver.layers VALUES('scenes',4326);`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("COPY scenes TO '" + parquet + "' (FORMAT PARQUET)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("COPY scenes TO '" + geojson + "' (FORMAT GDAL, DRIVER 'GeoJSON')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for _, test := range []struct {
		kind store.ServiceType
		path string
		view bool
	}{{store.ServiceTypeDuckDB, database, false}, {store.ServiceTypeDuckDB, database, true}, {store.ServiceTypeGeoParquet, parquet, false}, {store.ServiceTypeVectorFile, geojson, false}} {
		name := string(test.kind)
		if test.view {
			name += "-sql-view"
		}
		t.Run(name, func(t *testing.T) {
			connection, _ := json.Marshal(map[string]any{"path": test.path, "srid": 4326})
			ds, err := datasource.CreateFromService(&store.Service{ID: "source", Type: test.kind, ConnectionInfo: connection})
			if err != nil {
				t.Fatal(err)
			}
			defer ds.Close()
			layers, err := ds.DiscoverLayers(ctx)
			if err != nil || len(layers) != 1 {
				t.Fatalf("discover: %+v %v", layers, err)
			}
			layer := &workspace.Layer{ID: "layer", PublicID: "published-scenes", SourceLayer: layers[0].Name, Enabled: true, Public: true}
			if test.view {
				layer.IsSQLView = true
				layer.SourceLayer = "DO_NOT_QUERY"
				layer.SQLViewConfig = &workspace.SQLViewConfig{SQL: "SELECT scene_id,acquired,href,geom FROM scenes WHERE published=true", IDColumn: "scene_id", GeometryColumn: "geom", GeometryType: "POINT", SRID: 4326, ReadOnly: true, Properties: []*workspace.SQLViewProperty{{Name: "scene_id", Type: "string"}, {Name: "acquired", Type: "string"}, {Name: "href", Type: "string"}}}
			}
			service := &workspace.Service{ID: "source", Enabled: true, Type: test.kind, ConnectionInfo: connection, DataSource: ds, Layers: map[string]*workspace.Layer{layer.PublicID: layer}}
			ws := &workspace.Workspace{ID: "workspace", Name: "demo", Services: map[string]*workspace.Service{"source": service}, Settings: &store.WorkspaceSettings{OGCAPI: store.OGCAPISettings{Enabled: true}}}
			cfg := conf.Config{}
			cfg.Server.UrlBase = "http://example.test"
			adapter := Adapter{Config: cfg}
			binding := &staccatalog.Binding{ID: "binding", ServiceID: "source", ResourceID: "layer", ResourceKind: "layer", Mode: "dataset", RefreshIntervalSec: 900}
			c := &staccatalog.Collection{Document: stacmodel.Collection("scenes", "Scenes", "Scene inventory", "other", nil), Binding: binding, Public: true}
			emitted := 0
			doc, err := adapter.Scan(ctx, ws, c, 100, func(stacmodel.Document, []staccatalog.LocalAsset) error { emitted++; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if emitted != 0 {
				t.Fatal("dataset publication fabricated Items")
			}
			if err = stacmodel.Validate(doc, "collection"); err != nil {
				t.Fatal(err)
			}
			binding.Mode = "mapped"
			binding.Mapping = staccatalog.Mapping{ID: "scene_id", Datetime: staccatalog.Value{Property: "acquired"}, Assets: map[string]staccatalog.AssetMapping{"data": {Href: staccatalog.Value{Property: "href"}}}}
			var ids []string
			_, err = adapter.Scan(ctx, ws, c, 100, func(d stacmodel.Document, _ []staccatalog.LocalAsset) error {
				ids = append(ids, d.String("id"))
				return stacmodel.Validate(d, "item")
			})
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if test.view {
				want = 1
			}
			if len(ids) != want || ids[0] != "scene-1" {
				t.Fatalf("publication boundary: %v", ids)
			}
			binding.Mapping.ID = "unexposed"
			if err = adapter.Validate(ctx, ws, binding); err == nil {
				t.Fatal("missing mapping property accepted")
			}
			layer.Public = false
			layer.AllowedRoles = []string{"admin"}
			if Visible(ws, c, "viewer") || Visible(ws, c, "") {
				t.Fatal("source policy bypass")
			}
			if !Visible(ws, c, "admin") {
				t.Fatal("source admin denied")
			}
			layer.Enabled = false
			if Visible(ws, c, "super_admin") {
				t.Fatal("disabled source leaked")
			}
		})
	}
}

func TestRasterPublicationNeedsTimeAndKeepsStableIDs(t *testing.T) {
	ctx := context.Background()
	source, err := filepath.Abs("../../testing/fixtures/raster/rectified-grid-coverage.tif")
	if err != nil {
		t.Fatal(err)
	}
	pathpolicy.Configure([]string{source})
	connection, _ := json.Marshal(store.RasterFileConnectionInfo{Path: source})
	ds, err := datasource.CreateFromService(&store.Service{ID: "raster", Type: store.ServiceTypeRasterFile, ConnectionInfo: connection})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	coverageSource := ds.(datasource.CoverageDataSource)
	coverages, err := coverageSource.DiscoverCoverages(ctx)
	if err != nil || len(coverages) == 0 {
		t.Fatalf("coverage discovery: %v", err)
	}
	cov := &workspace.Coverage{ID: "coverage", PublicID: "raster", SourceCoverage: coverages[0].SourceCoverage, Enabled: true}
	svc := &workspace.Service{ID: "raster", Enabled: true, Type: store.ServiceTypeRasterFile, ConnectionInfo: connection, CoverageSource: coverageSource, Coverages: map[string]*workspace.Coverage{"raster": cov}}
	ws := &workspace.Workspace{ID: "workspace", Name: "demo", Services: map[string]*workspace.Service{"raster": svc}}
	c := &staccatalog.Collection{Document: stacmodel.Collection("raster", "Raster", "Raster", "other", nil), Binding: &staccatalog.Binding{ID: "binding", ServiceID: "raster", ResourceID: "coverage", ResourceKind: "coverage", Mode: "raster"}}
	a := Adapter{}
	emit := func(stacmodel.Document, []staccatalog.LocalAsset) error { return nil }
	if _, err = a.Scan(ctx, ws, c, 100, emit); err == nil {
		t.Fatal("raster with no acquisition time accepted")
	}
	c.Binding.Mapping.Datetime = staccatalog.Value{Constant: "2026-01-01T00:00:00Z"}
	var ids []string
	for i := 0; i < 2; i++ {
		_, err = a.Scan(ctx, ws, c, 100, func(d stacmodel.Document, assets []staccatalog.LocalAsset) error {
			ids = append(ids, d.String("id"))
			if len(assets) != 1 || assets[0].Path != source {
				t.Fatalf("local binding: %+v", assets)
			}
			return stacmodel.Validate(d, "item")
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if ids[0] != ids[1] {
		t.Fatal("raster ID changed on refresh")
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal(err)
	}
}
