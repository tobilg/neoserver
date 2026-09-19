package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	ducksource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/store"
)

func TestManagedImportSpatialContract(t *testing.T) {
	for _, target := range []int{4326, 3857, 32632} {
		for _, parquet := range []bool{false, true} {
			t.Run(fmt.Sprintf("srid=%d/parquet=%v", target, parquet), func(t *testing.T) {
				m, catalog, ws := revisionFixture(t)
				ctx := context.Background()
				body := []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"control"},"geometry":{"type":"Point","coordinates":[7,51]}}]}`)
				name := "points.geojson"
				if parquet {
					name = "points.parquet"
					path := filepath.Join(t.TempDir(), name)
					db, err := sql.Open("duckdb", "")
					if err != nil {
						t.Fatal(err)
					}
					_, err = db.Exec("LOAD spatial; COPY (SELECT 'control' AS name, ST_Point(7,51) AS geom) TO '" + path + "' (FORMAT PARQUET)")
					db.Close()
					if err != nil {
						t.Fatal(err)
					}
					body, err = os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
				}
				job, err := m.CreateUpload(ctx, ws, "contract", name, "test", bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				job = runRevisionJob(t, m)
				if job.Discovery == nil {
					t.Fatalf("discovery: %+v", job)
				}
				source := job.Discovery.Layers[0]
				_, err = m.SetPlan(ctx, job.ID, store.ImportPlan{ServiceName: "contract", Layers: []store.ImportLayerPlan{{SourceLayer: source.Name, PublicID: "points", GeometryColumn: source.GeometryColumn, SourceSRID: 4326, TargetSRID: target, Enabled: true}}})
				if err != nil {
					t.Fatal(err)
				}
				job = runRevisionJob(t, m)
				if job.Status != store.ImportReadyToPublish {
					t.Fatalf("transform: %s", job.ErrorMessage)
				}
				asset, err := catalog.GetManagedAssetByImport(ctx, job.ID)
				if err != nil {
					t.Fatal(err)
				}
				// Reopening the encrypted output proves metadata/identity survive restart.
				for range 2 {
					ds, err := ducksource.New("contract", ducksource.Config{Path: asset.Path, EncryptionKey: asset.EncryptionKey, ReadOnly: true, Extensions: []string{"spatial"}})
					if err != nil {
						t.Fatal(err)
					}
					info, err := ds.GetLayerInfo(ctx, "points")
					if err != nil {
						ds.Close()
						t.Fatal(err)
					}
					if info.SRID != target || info.IDColumn != "__neoserver_id" {
						t.Fatalf("metadata: %+v", info)
					}
					feature, found, err := ds.QueryByID(ctx, "points", "1", 4326)
					if err != nil || !found {
						t.Fatalf("lookup: %v %v", found, err)
					}
					var value struct {
						ID       int `json:"id"`
						Geometry struct {
							Coordinates []float64 `json:"coordinates"`
						} `json:"geometry"`
					}
					if err := json.Unmarshal(feature, &value); err != nil {
						t.Fatal(err)
					}
					if value.ID != 1 || len(value.Geometry.Coordinates) != 2 || math.Abs(value.Geometry.Coordinates[0]-7) > 1e-7 || math.Abs(value.Geometry.Coordinates[1]-51) > 1e-7 {
						t.Fatalf("wrong feature: %s", feature)
					}
					features, err := ds.Query(ctx, "points", datasource.QueryParams{Limit: 10, OutputSRID: 4326})
					if err != nil || len(features) != 1 {
						t.Fatalf("query: %v %v", features, err)
					}
					ds.Close()
				}
			})
		}
	}
}

func TestParquetTransformRepresentation(t *testing.T) {
	for _, typ := range []string{"GEOMETRY", "GEOMETRY('EPSG:4326')", "BLOB", "WKB_BLOB"} {
		source := store.ImportDiscoveredLayer{GeometryColumn: "geom", Properties: []store.ImportProperty{{Name: "geom", Type: typ}}}
		query, _, err := buildTransformSQL("points.parquet", source, store.ImportLayerPlan{PublicID: "points", TargetGeometry: "geom", SourceSRID: 4326, TargetSRID: 4326})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(query, "ST_GeomFromWKB") == strings.HasPrefix(typ, "GEOMETRY") {
			t.Fatalf("%s: %s", typ, query)
		}
	}
}
