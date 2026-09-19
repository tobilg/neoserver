package wmts

import (
	"database/sql"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	ducksource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestIdentifyPreservesSourceNumbersAcrossFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "precision.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`LOAD spatial; CREATE TABLE records(id INTEGER PRIMARY KEY, positive BIGINT, negative BIGINT, amount DECIMAL(30,15), nested JSON, missing BIGINT, geom GEOMETRY); INSERT INTO records VALUES(1,9007199254740993,-9007199254740993,123456789012345.123456789012345,'{"value":9007199254740995}',NULL,ST_Point(7,51))`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	ds, err := ducksource.New("precision", ducksource.Config{Path: path, ReadOnly: true, Extensions: []string{"spatial"}, SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	h, ws := testHandler()
	layer := &workspace.Layer{ID: "precise", PublicID: "precise", SourceLayer: "records", Enabled: true, Public: true}
	ws.Services["precision"] = &workspace.Service{ID: "precision", Type: store.ServiceTypeDuckDB, Enabled: true, DataSource: ds, Layers: map[string]*workspace.Layer{"precise": layer}}
	for _, view := range []bool{false, true} {
		layer.IsSQLView = view
		if view {
			layer.SQLViewConfig = &workspace.SQLViewConfig{SQL: "SELECT * FROM records WHERE id=1", GeometryColumn: "geom", GeometryType: "Point", IDColumn: "id", SRID: 4326, ReadOnly: true, Properties: []*workspace.SQLViewProperty{{Name: "positive", Type: "integer"}, {Name: "negative", Type: "integer"}, {Name: "amount", Type: "number"}, {Name: "nested", Type: "object"}, {Name: "missing", Type: "integer"}}}
		}
		for _, format := range []string{"application/json", "application/xml", "text/html", "text/plain"} {
			t.Run(fmtTestName(view, format), func(t *testing.T) {
				w := httptest.NewRecorder()
				target := "http://example.test/wmts?SERVICE=WMTS&REQUEST=GetFeatureInfo&VERSION=1.0.0&LAYER=precise&STYLE=default&FORMAT=image%2Fpng&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=0&TILEROW=0&TILECOL=0&I=133&J=86&INFOFORMAT=" + url.QueryEscape(format)
				h.kvp(w, wmtsRequest(ws, target))
				body := w.Body.String()
				if w.Code != 200 {
					t.Fatalf("%d %s", w.Code, body)
				}
				for _, token := range []string{"9007199254740993", "-9007199254740993", "123456789012345.123456789012345", "9007199254740995"} {
					if !strings.Contains(body, token) {
						t.Errorf("lost source number %s in %s", token, body)
					}
				}
				if format == "application/json" && !strings.Contains(body, `"missing":null`) {
					t.Fatalf("lost null in %s", body)
				}
			})
		}
	}
}

func fmtTestName(view bool, format string) string {
	if view {
		return "sql-view/" + format
	}
	return "table/" + format
}
