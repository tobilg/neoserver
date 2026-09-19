package wfs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	ducksource "github.com/tobilg/neoserver/internal/datasource/duckdb"
	"github.com/tobilg/neoserver/internal/workspace"
)

type representationSource struct {
	*pagingContractSource
	feature  json.RawMessage
	lookedUp string
}

func TestBigintIdentifiersRoundTripThroughNativeQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identities.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`LOAD spatial; CREATE TABLE identities(id BIGINT PRIMARY KEY, amount BIGINT, geom GEOMETRY); INSERT INTO identities VALUES(9007199254740992,9007199254740992,ST_Point(7,51)),(9007199254740993,9007199254740993,ST_Point(7,51))`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	ds, err := ducksource.New("identities", ducksource.Config{Path: path, ReadOnly: true, Extensions: []string{"spatial"}, SRID: 4326})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	h, ws, _ := pagingFixture()
	ws.Services["source"].DataSource = ds
	ws.Services["source"].Layers = map[string]*workspace.Layer{"public.roads": {PublicID: "public.roads", SourceLayer: "identities", Enabled: true, Public: true}}
	w := httptest.NewRecorder()
	h.handleGetFeature(w, httptest.NewRequest("GET", "/wfs?TYPENAMES=public.roads", nil), ws)
	for _, local := range []string{"9007199254740992", "9007199254740993"} {
		id := "public.roads." + local
		if w.Code != 200 || !strings.Contains(w.Body.String(), `gml:id="`+id+`"`) {
			t.Fatalf("source numeric identity: %d %s", w.Code, w.Body)
		}
		for _, query := range []string{"STOREDQUERY_ID=urn:ogc:def:query:OGC-WFS::GetFeatureById&ID=" + id, "TYPENAMES=public.roads&RESOURCEID=" + id} {
			response := httptest.NewRecorder()
			h.handleGetFeature(response, httptest.NewRequest("GET", "/wfs?"+query, nil), ws)
			if response.Code != 200 || strings.Count(response.Body.String(), `gml:id="`+id+`"`) != 1 || !strings.Contains(response.Body.String(), ">"+local+"</app:amount>") {
				t.Fatalf("lookup roundtrip: %d %s", response.Code, response.Body)
			}
		}
	}
	w = httptest.NewRecorder()
	h.handleGetPropertyValue(w, httptest.NewRequest("GET", "/wfs?TYPENAMES=public.roads&VALUEREFERENCE=geom&SRSNAME=EPSG:3857&COUNT=1", nil), ws)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "779236.") || !strings.Contains(w.Body.String(), "6621293.") || !strings.Contains(w.Body.String(), SRSNameFromSRID(3857)) {
		t.Fatalf("native transformed geometry: %d %s", w.Code, w.Body)
	}
}

func (s *representationSource) Query(_ context.Context, _ string, p datasource.QueryParams) ([]json.RawMessage, error) {
	s.queries = append(s.queries, p)
	return []json.RawMessage{s.feature}, nil
}
func (s *representationSource) QueryByID(_ context.Context, _ string, id string, _ int) (json.RawMessage, bool, error) {
	s.lookedUp = id
	return s.feature, true, nil
}

func TestNumericFeatureIdentityAndPropertyFidelity(t *testing.T) {
	for _, raw := range []string{"1", "1000000", "9007199254740992", "9007199254740993", "9223372036854775807", "-9223372036854775808", `"local.part"`} {
		t.Run(raw, func(t *testing.T) {
			h, ws, base := pagingFixture()
			ds := &representationSource{pagingContractSource: base, feature: json.RawMessage(fmt.Sprintf(`{"type":"Feature","id":%s,"properties":{"name":%s},"geometry":{"type":"Point","coordinates":[7.25,51.5]}}`, raw, raw))}
			ws.Services["source"].DataSource = ds
			layer := ws.Services["source"].Layers["roads"]
			layer.PublicID = "public.roads"
			delete(ws.Services["source"].Layers, "roads")
			ws.Services["source"].Layers[layer.PublicID] = layer
			local := strings.Trim(raw, `"`)
			id := "public.roads." + local
			w := httptest.NewRecorder()
			h.handleGetFeature(w, httptest.NewRequest("GET", "/wfs?TYPENAMES=public.roads", nil), ws)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `gml:id="`+id+`"`) || !strings.Contains(w.Body.String(), ">"+local+"</gml:name>") || !strings.Contains(w.Body.String(), "51.5 7.25") {
				t.Fatalf("collection fidelity: %d %s", w.Code, w.Body)
			}
			for _, ref := range []string{"@gml:id", "name"} {
				w = httptest.NewRecorder()
				h.handleGetPropertyValue(w, httptest.NewRequest("GET", "/wfs?TYPENAMES=public.roads&VALUEREFERENCE="+url.QueryEscape(ref), nil), ws)
				want := local
				if ref == "@gml:id" {
					want = id
				}
				if w.Code != 200 || !strings.Contains(w.Body.String(), ">"+want+"</wfs:member>") {
					t.Fatalf("property fidelity: %d %s", w.Code, w.Body)
				}
			}
			for _, rid := range []string{id, "app:" + id} {
				w = httptest.NewRecorder()
				h.handleGetFeatureById(context.Background(), w, httptest.NewRequest("GET", "/wfs", nil), ws, &GetFeatureRequest{StoredQueryParams: map[string]string{"ID": rid}})
				if w.Code != 200 || ds.lookedUp != local || !strings.Contains(w.Body.String(), `gml:id="`+rid+`"`) {
					t.Fatalf("single feature: %d %s lookup %q", w.Code, w.Body, ds.lookedUp)
				}
				for _, f := range []*FESFilter{{ResourceId: []FESResourceId{{Rid: rid}}}, {PropertyIsEqualTo: &FESComparison{ValueReference: "@gml:id", Literal: rid}}} {
					_, args, _, err := CompileFES(f, FESCompileOptions{CollectionID: "app:public.roads"})
					if err != nil || len(args) != 1 || args[0] != local {
						t.Fatalf("identifier filter %q: %v %v", rid, args, err)
					}
				}
			}
		})
	}
}

func TestIdentifierLongestPublicationPrefix(t *testing.T) {
	_, ws, _ := pagingFixture()
	for _, name := range []string{"public", "public.roads", "public.roads.branch"} {
		ws.Services["source"].Layers[name] = &workspace.Layer{PublicID: name, Enabled: true, Public: true}
	}
	for _, tc := range []struct{ id, typ, local string }{{"public.roads.branch.4.x", "public.roads.branch", "4.x"}, {"app:public.roads.4.x", "app:public.roads", "4.x"}, {"unknown.7", "", ""}, {"public.roads.", "public", "roads."}} {
		typ, local, ok := resolveFeatureIdentifier(ws, tc.id, "app")
		if typ != tc.typ || local != tc.local || ok != (tc.typ != "") {
			t.Fatalf("%q: %q %q %v", tc.id, typ, local, ok)
		}
	}
}

func TestGeometryValueCollectionGETAndXML(t *testing.T) {
	for _, tc := range []struct{ geometry, element string }{
		{`{"type":"Point","coordinates":[7,51]}`, "Point"},
		{`{"type":"LineString","coordinates":[[7,51],[8,52]]}`, "LineString"},
		{`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`, "Polygon"},
		{`{"type":"MultiPoint","coordinates":[[7,51],[8,52]]}`, "MultiPoint"},
		{`{"type":"MultiLineString","coordinates":[[[7,51],[8,52]]]}`, "MultiCurve"},
		{`{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,0]]]]}`, "MultiSurface"},
		{"null", ""},
	} {
		for _, method := range []string{"GET", "POST"} {
			for _, ref := range []string{"geom", "app:geom"} {
				t.Run(tc.element+method+ref, func(t *testing.T) {
					h, ws, base := pagingFixture()
					ds := &representationSource{pagingContractSource: base, feature: json.RawMessage(`{"type":"Feature","id":1,"properties":{"name":1.234567890123456789},"geometry":` + tc.geometry + `}`)}
					ws.Services["source"].DataSource = ds
					target := "/wfs?TYPENAMES=roads&VALUEREFERENCE=" + ref + "&SRSNAME=EPSG:3857&COUNT=1&STARTINDEX=1"
					body := ""
					if method == "POST" {
						target = "/wfs"
						body = `<GetPropertyValue service="WFS" version="2.0.0" count="1" startIndex="1" valueReference="` + ref + `"><Query typeNames="roads" srsName="EPSG:3857"/></GetPropertyValue>`
					}
					w := httptest.NewRecorder()
					h.handleGetPropertyValue(w, httptest.NewRequest(method, target, strings.NewReader(body)), ws)
					if w.Code != 200 || !strings.Contains(w.Body.String(), `numberReturned="1"`) {
						t.Fatalf("geometry response: %d %s", w.Code, w.Body)
					}
					if tc.element == "" {
						if !strings.Contains(w.Body.String(), `xsi:nil="true"`) {
							t.Fatalf("null: %s", w.Body)
						}
					} else if !strings.Contains(w.Body.String(), "<gml:"+tc.element) || !strings.Contains(w.Body.String(), SRSNameFromSRID(3857)) {
						t.Fatalf("geometry: %s", w.Body)
					}
					if len(ds.queries) != 1 || ds.queries[0].OutputSRID != 3857 || ds.queries[0].Offset != 1 || ds.queries[0].Limit != 1 {
						t.Fatalf("lost selection: %+v", ds.queries)
					}
					w = httptest.NewRecorder()
					h.handleGetPropertyValue(w, httptest.NewRequest("GET", "/wfs?TYPENAMES=roads&VALUEREFERENCE=name", nil), ws)
					if !strings.Contains(w.Body.String(), "1.234567890123456789") {
						t.Fatalf("decimal fidelity: %s", w.Body)
					}
				})
			}
		}
	}
}
