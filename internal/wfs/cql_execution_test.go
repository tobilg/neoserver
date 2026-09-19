package wfs

import (
	"context"
	"fmt"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestSpatialCQLExecutesAcrossAdapters(t *testing.T) {
	ctx := context.Background()
	for name, ds := range queryContractSources(t) {
		t.Run(name, func(t *testing.T) {
			layers, err := ds.DiscoverLayers(ctx)
			if err != nil {
				t.Fatal(err)
			}
			info, err := ds.GetLayerInfo(ctx, layers[0].Name)
			if err != nil {
				t.Fatal(err)
			}
			for _, filterSRID := range []int{4326, 3857} {
				literal := "POINT(7 51)"
				predicate := "INTERSECTS"
				if filterSRID == 3857 {
					literal = "POINT(779236.435552915 6621293.722740169)"
					predicate = "DWITHIN"
				}
				expr := fmt.Sprintf("INTERSECTS(%s, ENVELOPE(6, 50, 7.5, 51.5)) AND name = 'one'", info.GeometryColumn)
				if predicate == "DWITHIN" {
					expr = fmt.Sprintf("DWITHIN(%s, %s, 0.000001) AND name = 'one'", info.GeometryColumn, literal)
				}
				p := datasource.QueryParams{Filter: expr, FilterSRID: filterSRID, OutputSRID: 4326, Limit: 10, BBox: &datasource.BBox{MinX: 6, MinY: 50, MaxX: 10, MaxY: 54}, BBoxSRID: 4326}
				rows, err := ds.Query(ctx, layers[0].Name, p)
				if err != nil || len(rows) != 1 {
					t.Fatalf("%d list %d %v", filterSRID, len(rows), err)
				}
				count, err := ds.Count(ctx, layers[0].Name, p)
				if err != nil || count != 1 {
					t.Fatalf("%d count %d %v", filterSRID, count, err)
				}
				rendered, err := ds.QueryWKB(ctx, layers[0].Name, p)
				if err != nil || len(rendered) != 1 {
					t.Fatalf("%d render %d %v", filterSRID, len(rendered), err)
				}
				if name == "duckdb" {
					layer := &workspace.Layer{IsSQLView: true, SQLViewConfig: &workspace.SQLViewConfig{SQL: "SELECT id,name,geom FROM points WHERE id<3", GeometryColumn: "geom", IDColumn: "id", SRID: 4326, Properties: []*workspace.SQLViewProperty{{Name: "name", Type: "string"}}}}
					rows, err := layer.QueryFeatures(ctx, ds, p)
					if err != nil || len(rows) != 1 {
						t.Fatalf("view list %d %v", len(rows), err)
					}
					count, err := layer.CountFeatures(ctx, ds, p)
					if err != nil || count != 1 {
						t.Fatalf("view count %d %v", count, err)
					}
					rendered, err := ds.(datasource.SQLViewDataSource).QuerySQLViewWKB(ctx, &datasource.SQLViewConfig{SQL: layer.SQLViewConfig.SQL, GeometryColumn: "geom", IDColumn: "id", SRID: 4326, Properties: []*datasource.SQLViewProperty{{Name: "name", Type: "string"}}}, p)
					if err != nil || len(rendered) != 1 {
						t.Fatalf("view render %d %v", len(rendered), err)
					}
				}
			}
		})
	}
}
