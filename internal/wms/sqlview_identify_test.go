package wms

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	ducksource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestIdentifyRespectsSQLViewRowsAndColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`LOAD spatial; CREATE TABLE records(id INTEGER PRIMARY KEY,name VARCHAR,secret VARCHAR,geom GEOMETRY); INSERT INTO records VALUES(1,'published','hidden-column',ST_Point(7,51)),(2,'excluded','hidden-row',ST_Point(8,52))`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	ds, err := ducksource.New("source", ducksource.Config{Path: path, ReadOnly: true, Extensions: []string{"spatial"}, SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	layer := &workspace.Layer{ID: "view", PublicID: "filtered", Enabled: true, Public: true, IsSQLView: true, SQLViewConfig: &workspace.SQLViewConfig{SQL: "SELECT id,name,geom FROM records WHERE id=1", GeometryColumn: "geom", GeometryType: "Point", IDColumn: "id", SRID: 4326, ReadOnly: true, Properties: []*workspace.SQLViewProperty{{Name: "name", Type: "string"}}}}
	ws := &workspace.Workspace{ID: "review", Name: "review", Services: map[string]*workspace.Service{"source": {ID: "source", Enabled: true, DataSource: ds, Layers: map[string]*workspace.Layer{"filtered": layer}}}, Settings: &store.WorkspaceSettings{WMS: store.WMSSettings{Enabled: true, Public: true}}}
	h := &workspaceHandler{cfg: conf.Config{WMS: conf.WMS{MaxWidth: 1024, MaxHeight: 1024}}, logger: slog.Default()}
	for _, source := range []string{"records", "_sql_view_"} {
		layer.SourceLayer = source
		for _, included := range []bool{true, false} {
			bbox, x, y := "6.9,50.9,7.1,51.1", 7.0, 51.0
			if !included {
				bbox, x, y = "7.9,51.9,8.1,52.1", 8, 52
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "http://example.test/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo&LAYERS=filtered&QUERY_LAYERS=filtered&STYLES=&CRS=CRS:84&BBOX="+bbox+"&WIDTH=100&HEIGHT=100&I=50&J=50&FORMAT=image/png&INFO_FORMAT=application/json", nil)
			h.handleGetFeatureInfo(w, r, ws)
			var result struct {
				Features []json.RawMessage `json:"features"`
			}
			if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || strings.Contains(w.Body.String(), "hidden-") || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("WMS %s: %d %s", source, w.Code, w.Body)
			}
			want := 0
			if included {
				want = 1
			}
			if len(result.Features) != want {
				t.Fatalf("got %d features want %d", len(result.Features), want)
			}
			values, err := QueryResourceFeatureInfo(context.Background(), ws, ws.GetResource("filtered"), PointFeatureInfoRequest{X: x, Y: y, PixelSize: .002, SRID: 4326, CRS: "CRS:84", FeatureCount: 1})
			if err != nil || len(values) != want {
				t.Fatalf("shared identify %s: %v %v", source, values, err)
			}
			for _, v := range values {
				if _, ok := v["secret"]; ok {
					t.Fatal("shared identify leaked column")
				}
			}
		}
		w := httptest.NewRecorder()
		h.handleGetLegendGraphic(w, httptest.NewRequest("GET", "http://example.test/wms?REQUEST=GetLegendGraphic&LAYER=filtered&FORMAT=image/png", nil), ws)
		if w.Code != 200 {
			t.Fatalf("view legend: %d %s", w.Code, w.Body)
		}
	}
	layer.SQLViewConfig = nil
	if _, err := QueryResourceFeatureInfo(context.Background(), ws, ws.GetResource("filtered"), PointFeatureInfoRequest{}); err == nil {
		t.Fatal("invalid SQL view did not fail closed")
	}
}
