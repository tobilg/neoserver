package datasource_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	ducksource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/datasource/geoparquet"
	"github.com/tobilg/neoserver/internal/datasource/vectorfile"
)

// DuckDB-backed cases LOAD the spatial extension, which the server installs at
// startup. Install it here so the package does not depend on an extension cache
// left behind by an earlier run.
func TestMain(m *testing.M) {
	if err := datasource.PreloadExtensions(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		fmt.Fprintf(os.Stderr, "preload duckdb extensions: %v\n", err)
	}
	os.Exit(m.Run())
}

func TestDuckDBBackedFeatureJSONPreservesIntegerTokens(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "ids.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`LOAD spatial; CREATE TABLE points(id BIGINT PRIMARY KEY,amount BIGINT,geom GEOMETRY); INSERT INTO points VALUES(9007199254740992,9007199254740992,ST_Point(7,51)),(9007199254740993,9007199254740993,ST_Point(8,52))`); err != nil {
		t.Fatal(err)
	}
	parquet := filepath.Join(dir, "ids.parquet")
	if _, err = db.Exec("COPY points TO '" + strings.ReplaceAll(parquet, "'", "''") + "' (FORMAT PARQUET)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	duck, err := ducksource.New("duck", ducksource.Config{Path: path, ReadOnly: true, Extensions: []string{"spatial"}, SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	defer duck.Close()
	parq, err := geoparquet.New("parquet", geoparquet.Config{Path: parquet, IDColumn: "id", GeometryColumn: "geom", SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	defer parq.Close()
	geojson := filepath.Join(dir, "ids.geojson")
	if err := os.WriteFile(geojson, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"id":9007199254740992,"amount":9007199254740992},"geometry":{"type":"Point","coordinates":[7,51]}},{"type":"Feature","properties":{"id":9007199254740993,"amount":9007199254740993},"geometry":{"type":"Point","coordinates":[8,52]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	vector, err := vectorfile.New("vector", vectorfile.Config{Path: geojson, IDColumn: "id"})
	if err != nil {
		t.Fatal(err)
	}
	defer vector.Close()
	check := func(raw json.RawMessage) string {
		t.Helper()
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var feature struct {
			ID         json.Number            `json:"id"`
			Properties map[string]json.Number `json:"properties"`
		}
		if err := decoder.Decode(&feature); err != nil {
			t.Fatal(err)
		}
		if feature.ID.String() != feature.Properties["amount"].String() {
			t.Fatalf("id/property diverged: %s", raw)
		}
		return feature.ID.String()
	}
	for name, ds := range map[string]datasource.DataSource{"duckdb": duck, "geoparquet": parq, "vectorfile": vector} {
		t.Run(name, func(t *testing.T) {
			layers, err := ds.DiscoverLayers(ctx)
			if err != nil || len(layers) != 1 {
				t.Fatalf("discover %v %v", layers, err)
			}
			features, err := ds.Query(ctx, layers[0].Name, datasource.QueryParams{Limit: 10, OutputSRID: 4326})
			if err != nil || len(features) != 2 {
				t.Fatalf("query %d %v", len(features), err)
			}
			seen := map[string]bool{}
			for _, raw := range features {
				seen[check(raw)] = true
			}
			for _, id := range []string{"9007199254740992", "9007199254740993"} {
				if !seen[id] {
					t.Fatalf("numeric ID lost: %v", seen)
				}
				raw, found, err := ds.QueryByID(ctx, layers[0].Name, id, 4326)
				if err != nil || !found || check(raw) != id {
					t.Fatalf("lookup %s: %s %v", id, raw, err)
				}
			}
		})
	}
	features, err := duck.QuerySQLView(ctx, &datasource.SQLViewConfig{SQL: "SELECT id,amount,geom FROM points", IDColumn: "id", GeometryColumn: "geom", SRID: 4326, Properties: []*datasource.SQLViewProperty{{Name: "amount", Type: "integer"}}}, datasource.QueryParams{Limit: 10, OutputSRID: 4326})
	if err != nil || len(features) != 2 {
		t.Fatalf("SQL view %v %v", features, err)
	}
	seen := map[string]bool{}
	for _, raw := range features {
		seen[check(raw)] = true
	}
	if !seen["9007199254740992"] || !seen["9007199254740993"] {
		t.Fatalf("SQL view numeric loss: %v", seen)
	}
}
