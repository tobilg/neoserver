package wmts

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	ducksource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestWMTSHTTPIdentifyUsesSQLViewPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "points.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`LOAD spatial; CREATE TABLE records(id INTEGER PRIMARY KEY,name VARCHAR,secret VARCHAR,geom GEOMETRY); INSERT INTO records VALUES(1,'published','hidden-column',ST_Point(7,51)),(2,'excluded','hidden-row',ST_Point(-100,-40))`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	ds, err := ducksource.New("view", ducksource.Config{Path: path, ReadOnly: true, Extensions: []string{"spatial"}, SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	h, ws := testHandler()
	ws.Services["features"].DataSource = ds
	layer := ws.Services["features"].Layers["roads"]
	layer.IsSQLView = true
	layer.SQLViewConfig = &workspace.SQLViewConfig{SQL: "SELECT id,name,geom FROM records WHERE id=1", GeometryColumn: "geom", GeometryType: "Point", IDColumn: "id", SRID: 4326, ReadOnly: true, Properties: []*workspace.SQLViewProperty{{Name: "name", Type: "string"}}}
	ws.Groups = map[string]*workspace.LayerGroup{"group": {PublicID: "group", Enabled: true, Public: true, Members: []store.LayerGroupMember{{Resource: "roads"}}}}
	for _, source := range []string{"records", "_sql_view_"} {
		layer.SourceLayer = source
		for _, publication := range []string{"roads", "group"} {
			for _, point := range []struct{ i, j, want int }{{133, 86, 1}, {57, 159, 0}} {
				w := httptest.NewRecorder()
				target := fmt.Sprintf("http://example.test/wmts?SERVICE=WMTS&REQUEST=GetFeatureInfo&VERSION=1.0.0&LAYER=%s&STYLE=default&FORMAT=image%%2Fpng&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=0&TILEROW=0&TILECOL=0&I=%d&J=%d&INFOFORMAT=application%%2Fjson", publication, point.i, point.j)
				h.kvp(w, wmtsRequest(ws, target))
				var result struct {
					Features []json.RawMessage `json:"features"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || len(result.Features) != point.want || strings.Contains(w.Body.String(), "hidden-") || strings.Contains(w.Body.String(), "secret") {
					t.Fatalf("%s/%s/%+v: %d %s (%v)", source, publication, point, w.Code, w.Body, err)
				}
			}
		}
	}
}
