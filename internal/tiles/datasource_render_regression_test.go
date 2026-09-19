package tiles

import (
	"bytes"
	"context"
	"database/sql"
	"image/png"
	"math"
	"path/filepath"
	"testing"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/mvt"
	"github.com/paulmach/orb/encoding/wkb"
	"github.com/tobilg/neoserver/internal/datasource"
	ducksource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/datasource/geoparquet"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/datasource/vectorfile"
	"github.com/tobilg/neoserver/internal/workspace"
)

// Execute the production adapters against real files. A 200/valid protobuf
// alone cannot catch wrong projections or a nonexistent DuckDB WKB function.
func TestRealDuckDBFamilyRendering(t *testing.T) {
	pathpolicy.Configure([]string{"**"})
	t.Cleanup(func() { pathpolicy.Configure(nil) })
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "points.db")
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		"INSTALL spatial; LOAD spatial",
		"CREATE TABLE points AS SELECT 1 AS id, ST_Point(0, 66.51326044311186) AS geom",
		"COPY points TO '" + filepath.Join(dir, "points.parquet") + "' (FORMAT PARQUET)",
		"COPY points TO '" + filepath.Join(dir, "points.geojson") + "' (FORMAT GDAL, DRIVER 'GeoJSON')",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		open func() (datasource.DataSource, error)
	}{
		{"duckdb", func() (datasource.DataSource, error) {
			return ducksource.New("test", ducksource.Config{Path: dbPath, SRID: 4326, ReadOnly: true, Extensions: []string{"spatial"}})
		}},
		{"geoparquet", func() (datasource.DataSource, error) {
			return geoparquet.New("test", geoparquet.Config{Path: filepath.Join(dir, "points.parquet"), GeometryColumn: "geom", IDColumn: "id"})
		}},
		{"vectorfile", func() (datasource.DataSource, error) {
			return vectorfile.New("test", vectorfile.Config{Path: filepath.Join(dir, "points.geojson")})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds, err := tc.open()
			if err != nil {
				t.Fatal(err)
			}
			defer ds.Close()
			ctx := context.Background()
			info, err := ds.GetLayerInfo(ctx, "points")
			if err != nil {
				t.Fatal(err)
			}
			for _, tolerance := range []float64{0, 0.1} {
				rows, err := ds.QueryWKB(ctx, "points", datasource.QueryParams{OutputSRID: 3857, Limit: 10, SimplifyTolerance: tolerance})
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 1 {
					t.Fatalf("rows = %d", len(rows))
				}
				geometry, err := wkb.Unmarshal(rows[0].Geometry)
				if err != nil {
					t.Fatal(err)
				}
				point, ok := geometry.(orb.Point)
				if !ok || math.Abs(point[1]-10018754.17139462) > 1 {
					t.Fatalf("projected geometry = %v", geometry)
				}
			}
			for _, grid := range []struct {
				tms  string
				x, y float64
			}{
				{TMSWebMercatorQuad, 2048, 1024},
				{TMSWorldCRS84Quad, 4096, (90 - 66.51326044311186) / 180 * 4096},
			} {
				data, err := NewMVTGenerator(4096).GenerateTile(ctx, ds, info, grid.tms, 0, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				layers, err := mvt.Unmarshal(data)
				if err != nil || len(layers) != 1 || len(layers[0].Features) != 1 {
					t.Fatalf("tile = %v, %v", layers, err)
				}
				point := layers[0].Features[0].Geometry.(orb.Point)
				if math.Abs(point[0]-grid.x) > 1 || math.Abs(point[1]-grid.y) > 1 {
					t.Fatalf("%s: position %v, want [%v,%v]", grid.tms, point, grid.x, grid.y)
				}
			}
			// Public identity must never be used for the underlying table lookup.
			for _, publicID := range []string{"renamed_points", "public.renamed_again"} {
				ws := newTestWorkspace()
				layer := addTestLayer(ws, publicID, ds)
				layer.SourceLayer = "points"
				engine := NewEngine(newTestConfig(), newTestLogger(), nil, nil)
				request := EngineRequest{Workspace: ws, Resource: ws.GetResource(publicID), TileType: "vector", MatrixSet: TMSWebMercatorQuad, Format: MediaTypeMVT}
				result, err := engine.Fetch(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := mvt.Unmarshal(result.Data)
				if err != nil || len(encoded) != 1 || encoded[0].Name != publicID {
					t.Fatalf("public identity: %v %v", encoded, err)
				}
				request.TileType, request.Format = "map", MediaTypePNG
				result, err = engine.Fetch(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(result.Data))
				if err != nil {
					t.Fatal(err)
				}
				_, _, _, alpha := img.At(128, 64).RGBA()
				if alpha == 0 {
					t.Fatal("map tile lacks the projected point")
				}
				if tc.name == "duckdb" {
					layer.IsSQLView = true
					layer.SQLViewConfig = &workspace.SQLViewConfig{SQL: "SELECT * FROM points", GeometryColumn: "geom", GeometryType: "Point", SRID: 4326, IDColumn: "id", ReadOnly: true}
					request.TileType, request.Format = "vector", MediaTypeMVT
					result, err = engine.Fetch(ctx, request)
					if err != nil {
						t.Fatal(err)
					}
					encoded, err = mvt.Unmarshal(result.Data)
					if err != nil || len(encoded) != 1 || encoded[0].Name != publicID {
						t.Fatalf("SQL view identity: %v %v", encoded, err)
					}
					point := encoded[0].Features[0].Geometry.(orb.Point)
					if math.Abs(point[0]-2048) > 1 || math.Abs(point[1]-1024) > 1 {
						t.Fatalf("SQL view projection: %v", point)
					}
					metadata, _, err := tileLayerInfo(ctx, ds, layer)
					if err != nil {
						t.Fatal(err)
					}
					tileJSON := GenerateTileJSON(layer, metadata, TileJSONOptions{CollectionID: publicID, DataType: DataTypeVector})
					if len(tileJSON.VectorLayers) != 1 || tileJSON.VectorLayers[0].ID != encoded[0].Name {
						t.Fatalf("SQL view TileJSON: %+v", tileJSON)
					}
				}
			}
		})
	}
}
