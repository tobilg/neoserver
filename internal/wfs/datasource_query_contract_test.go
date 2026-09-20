package wfs

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	ducksource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/datasource/geoparquet"
	"github.com/tobilg/neoserver/internal/datasource/vectorfile"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/workspace"
)

func queryContractSources(t *testing.T) map[string]datasource.DataSource {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`LOAD spatial; CREATE TABLE points(id INTEGER PRIMARY KEY, name VARCHAR, secret VARCHAR, geom GEOMETRY);
	INSERT INTO points VALUES (1,'one','hidden-one',ST_Point(7,51)),(2,'two','hidden-two',ST_Point(8,52)),(3,NULL,'hidden-three',ST_Point(9,53))`)
	if err != nil {
		t.Fatal(err)
	}
	parquet := filepath.Join(dir, "points.parquet")
	if _, err = db.Exec("COPY points TO '" + strings.ReplaceAll(parquet, "'", "''") + "' (FORMAT PARQUET)"); err != nil {
		t.Fatal(err)
	}
	wkbParquet := filepath.Join(dir, "wkb.parquet")
	if _, err = db.Exec("COPY (SELECT id,name,secret,ST_AsWKB(geom) AS geom FROM points) TO '" + strings.ReplaceAll(wkbParquet, "'", "''") + "' (FORMAT PARQUET)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	duck, err := ducksource.New("duck", ducksource.Config{Path: path, ReadOnly: true, Extensions: []string{"spatial"}, SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	parq, err := geoparquet.New("parquet", geoparquet.Config{Path: parquet, GeometryColumn: "geom", IDColumn: "id", SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	geojson := filepath.Join(dir, "points.geojson")
	if err := os.WriteFile(geojson, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"id":1,"name":"one"},"geometry":{"type":"Point","coordinates":[7,51]}},{"type":"Feature","properties":{"id":2,"name":"two"},"geometry":{"type":"Point","coordinates":[8,52]}},{"type":"Feature","properties":{"id":3,"name":null},"geometry":{"type":"Point","coordinates":[9,53]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	vector, err := vectorfile.New("vector", vectorfile.Config{Path: geojson, IDColumn: "id"})
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]datasource.DataSource{"duckdb": duck, "geoparquet": parq, "vectorfile": vector}
	wkbSource, err := geoparquet.New("wkb", geoparquet.Config{Path: wkbParquet, GeometryColumn: "geom", IDColumn: "id", SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	sources["geoparquet-wkb"] = wkbSource
	for _, ds := range sources {
		t.Cleanup(func() { ds.Close() })
	}
	return sources
}

func TestFeaturePredicatesAcrossDataSources(t *testing.T) {
	for name, ds := range queryContractSources(t) {
		t.Run(name, func(t *testing.T) {
			layers, err := ds.DiscoverLayers(context.Background())
			if err != nil || len(layers) != 1 {
				t.Fatalf("discovery: %v %v", layers, err)
			}
			info, err := ds.GetLayerInfo(context.Background(), layers[0].Name)
			if err != nil {
				t.Fatal(err)
			}
			h, ws, _ := pagingFixture()
			ws.Services["source"].DataSource = ds
			layer := ws.Services["source"].Layers["roads"]
			layer.SourceLayer = layers[0].Name
			equality := `<Filter><PropertyIsEqualTo><ValueReference>name</ValueReference><Literal>one</Literal></PropertyIsEqualTo></Filter>`
			spatial := `<Filter><BBOX><Envelope srsName="EPSG:4326"><lowerCorner>6 50</lowerCorner><upperCorner>7.5 51.5</upperCorner></Envelope></BBOX></Filter>`
			null := `<Filter><PropertyIsNull><ValueReference>name</ValueReference></PropertyIsNull></Filter>`
			for _, tc := range []struct {
				name   string
				filter string
				ids    []string
				bbox   string
				want   int
			}{
				{"equality", equality, nil, "", 1},
				{"resource ID", "", []string{"roads.1"}, "", 1},
				{"spatial", spatial, nil, "", 1},
				{"authority BBOX", `<Filter><BBOX><Envelope srsName="urn:ogc:def:crs:EPSG::4326"><lowerCorner>50 6</lowerCorner><upperCorner>51.5 7.5</upperCorner></Envelope></BBOX></Filter>`, nil, "", 1},
				{"authority Point", `<Filter><Intersects><Point srsName="http://www.opengis.net/def/crs/EPSG/0/4326"><pos>51 7</pos></Point></Intersects></Filter>`, nil, "", 1},
				{"point features intersect curve", `<Filter><Intersects><ValueReference>geom</ValueReference><LineString srsName="CRS:84"><posList>6.5 50.5 7.5 51.5</posList></LineString></Intersects></Filter>`, nil, "", 1},
				{"point features intersect polygon", `<Filter><Intersects><ValueReference>geom</ValueReference><Polygon srsName="CRS:84"><exterior><LinearRing><posList>6 50 7.5 50 7.5 51.5 6 51.5 6 50</posList></LinearRing></exterior></Polygon></Intersects></Filter>`, nil, "", 1},
				{"null", null, nil, "", 3},
				{"bbox and filter", equality, nil, "6,50,10,54,CRS:84", 1},
				{"authority KVP bbox and filter", equality, nil, "50,6,54,10,urn:ogc:def:crs:EPSG::4326", 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					for _, method := range []string{"GET", "POST"} {
						q := url.Values{"SERVICE": {"WFS"}, "REQUEST": {"GetFeature"}, "TYPENAMES": {"roads"}, "OUTPUTFORMAT": {"application/json"}, "FILTER": {tc.filter}, "COUNT": {"1"}, "BBOX": {tc.bbox}}
						if tc.ids != nil {
							q.Set("RESOURCEID", strings.Join(tc.ids, ","))
						}
						body := ""
						if method == "POST" {
							filter := tc.filter
							if tc.ids != nil {
								filter = `<Filter><ResourceId rid="roads.1"/></Filter>`
							}
							body = `<GetFeature service="WFS" version="2.0.0" count="1" outputFormat="application/json"><Query typeNames="roads">` + filter + `</Query></GetFeature>`
							q.Del("FILTER")
							q.Del("RESOURCEID")
							q.Del("TYPENAMES")
						}
						req := httptest.NewRequest(method, "http://example.test/wfs?"+q.Encode(), strings.NewReader(body))
						w := httptest.NewRecorder()
						h.handleGetFeature(w, req, ws)
						var response struct {
							Features []struct {
								ID any `json:"id"`
							} `json:"features"`
							Matched int `json:"numberMatched"`
						}
						if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || len(response.Features) != 1 || fmt.Sprint(response.Features[0].ID) != fmt.Sprint(tc.want) || response.Matched != 1 {
							t.Fatalf("%s: %d %s (%v)", method, w.Code, w.Body, err)
						}
					}
					req := &GetFeatureRequest{Filter: tc.filter, ResourceID: tc.ids, Count: 10}
					params, err := h.buildQueryParams(req, info, 4326, "roads")
					if err != nil {
						t.Fatal(err)
					}
					rendered, err := ds.QueryWKB(context.Background(), layer.SourceLayer, params)
					if err != nil || len(rendered) != 1 {
						t.Fatalf("render filter ignored: %d %v", len(rendered), err)
					}
				})
			}
			if name == "duckdb" {
				layer.IsSQLView = true
				layer.SQLViewConfig = &workspace.SQLViewConfig{SQL: "SELECT id,name,geom FROM points WHERE id < 3", GeometryColumn: "geom", IDColumn: "id", SRID: 4326, ReadOnly: true, Properties: []*workspace.SQLViewProperty{{Name: "name", Type: "string"}}}
				for _, source := range []string{"points", "_sql_view_"} {
					layer.SourceLayer = source
					params, err := h.buildQueryParams(&GetFeatureRequest{Filter: equality, Count: 10}, info, 4326, "roads")
					if err != nil {
						t.Fatal(err)
					}
					rows, err := layer.QueryFeatures(context.Background(), ds, params)
					count, countErr := layer.CountFeatures(context.Background(), ds, params)
					if err != nil || countErr != nil || len(rows) != 1 || count != 1 {
						t.Fatalf("SQL view predicate: %d %d %v %v", len(rows), count, err, countErr)
					}
					for _, id := range []string{"roads.1", "roads.3"} {
						w := httptest.NewRecorder()
						h.handleGetFeature(w, httptest.NewRequest("GET", "http://example.test/wfs?SERVICE=WFS&REQUEST=GetFeature&STOREDQUERY_ID=urn:ogc:def:query:OGC-WFS::GetFeatureById&ID="+id, nil), ws)
						want := 200
						if id == "roads.3" {
							want = 404
						}
						if w.Code != want || strings.Contains(w.Body.String(), "hidden-") {
							t.Fatalf("stored SQL-view lookup: %d %s", w.Code, w.Body)
						}
					}
				}
				layer.SourceLayer = "_sql_view_"
				w := httptest.NewRecorder()
				h.handleGetPropertyValue(w, httptest.NewRequest("GET", "http://example.test/wfs?SERVICE=WFS&REQUEST=GetPropertyValue&TYPENAMES=roads&VALUEREFERENCE=name", nil), ws)
				var values struct {
					Values []string `xml:"member"`
				}
				if err := xml.Unmarshal(w.Body.Bytes(), &values); err != nil || w.Code != 200 || !reflect.DeepEqual(values.Values, []string{"one", "two"}) {
					t.Fatalf("SQL-view properties: %d %s %v", w.Code, w.Body, err)
				}
			}
		})
	}
}

func TestResourceIDRejectsConflictingSelectionConstraints(t *testing.T) {
	h, _, source := pagingFixture()
	info, err := source.GetLayerInfo(context.Background(), "roads")
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []*GetFeatureRequest{
		{ResourceID: []string{"roads.1"}, Filter: "name = 'other'"},
		{ResourceID: []string{"roads.1"}, BBox: &query.BBox{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}},
	} {
		if _, err := h.buildQueryParams(req, info, 4326, "roads"); err == nil {
			t.Fatal("selection constraint silently dropped")
		}
	}
}
