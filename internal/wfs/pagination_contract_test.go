package wfs

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type pagingContractSource struct {
	datasource.DataSource
	failCount  bool
	countError error
	queries    []datasource.QueryParams
}

func (*pagingContractSource) Type() store.ServiceType { return store.ServiceTypeDuckDB }
func (*pagingContractSource) GetLayerInfo(context.Context, string) (*datasource.LayerInfo, error) {
	return &datasource.LayerInfo{Name: "roads", GeometryColumn: "geom", SRID: 4326, IDColumn: "id", Properties: []datasource.PropertyInfo{{Name: "name", Type: "string"}}, PGTypes: map[string]string{"id": "INTEGER", "name": "VARCHAR"}}, nil
}
func (s *pagingContractSource) Count(context.Context, string, datasource.QueryParams) (int, error) {
	if s.countError != nil {
		return 0, s.countError
	}
	if s.failCount {
		return 0, errors.New("simulated count-query failure")
	}
	return 2, nil
}
func (s *pagingContractSource) Query(_ context.Context, _ string, p datasource.QueryParams) ([]json.RawMessage, error) {
	s.queries = append(s.queries, p)
	return []json.RawMessage{json.RawMessage(`{"type":"Feature","id":1,"geometry":{"type":"Point","coordinates":[7,51]},"properties":{"name":"control"}}`)}, nil
}

func pagingFixture() (*workspaceHandler, *workspace.Workspace, *pagingContractSource) {
	source := &pagingContractSource{}
	ws := &workspace.Workspace{ID: "ws", Name: "review", Settings: &store.WorkspaceSettings{WFS: store.WFSSettings{Enabled: true, Public: true}}, Services: map[string]*workspace.Service{"source": {ID: "source", Enabled: true, DataSource: source, Layers: map[string]*workspace.Layer{"roads": {ID: "layer", PublicID: "roads", SourceLayer: "roads", Enabled: true, Public: true}}}}}
	h := &workspaceHandler{logger: slog.Default(), cfg: conf.Config{Server: conf.Server{UrlBase: "http://example.test"}, WFS: conf.WFS{MaxFeatures: 100, DefaultCount: 10, AppNamespace: "urn:review", AppNamespacePrefix: "app"}}}
	return h, ws, source
}

func TestWFSPaginationPreservesQueryAndReportsCountFailure(t *testing.T) {
	h, ws, source := pagingFixture()
	type result struct {
		Next    string `xml:"next,attr"`
		Matched string `xml:"numberMatched,attr"`
	}
	run := func(url string) result {
		w := httptest.NewRecorder()
		h.handleGetFeature(w, httptest.NewRequest("GET", url, nil), ws)
		var r result
		if err := xml.Unmarshal(w.Body.Bytes(), &r); err != nil || w.Code != 200 {
			t.Fatalf("response %d %s error=%v", w.Code, w.Body, err)
		}
		return r
	}
	first := run("http://example.test/workspaces/review/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=roads&COUNT=1&BBOX=7,50,8,52&SRSNAME=EPSG:3857&PROPERTYNAME=name&SORTBY=name+D")
	t.Logf("first next=%s matched=%s", first.Next, first.Matched)
	if first.Next == "" {
		t.Fatal("missing next link")
	}
	run(first.Next)
	if len(source.queries) != 2 {
		t.Fatal(source.queries)
	}
	left, right := source.queries[0], source.queries[1]
	if right.Offset != 1 {
		t.Fatal("wrong continuation offset")
	}
	right.Offset = left.Offset
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("query changed: first=%+v next=%+v", left, right)
	}
	source.failCount = true
	failed := httptest.NewRecorder()
	h.handleGetFeature(failed, httptest.NewRequest("GET", "http://example.test/workspaces/review/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=roads&COUNT=1", nil), ws)
	if failed.Code == 200 || strings.Contains(failed.Body.String(), `numberMatched="1"`) {
		t.Fatalf("fabricated success: %d %s", failed.Code, failed.Body)
	}
}

func TestWFSXMLAndStoredQueryContinuation(t *testing.T) {
	for _, stored := range []bool{false, true} {
		t.Run(map[bool]string{false: "XML POST", true: "stored query"}[stored], func(t *testing.T) {
			h, ws, source := pagingFixture()
			expression := `<Query typeNames="app:roads" srsName="EPSG:3857"><fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0"><fes:PropertyIsEqualTo><fes:ValueReference>name</fes:ValueReference><fes:Literal>control</fes:Literal></fes:PropertyIsEqualTo></fes:Filter></Query>`
			request := httptest.NewRequest("POST", "http://example.test/base/workspaces/review/wfs?api_key=never-in-links", strings.NewReader(`<GetFeature service="WFS" version="2.0.0" count="1">`+expression+`</GetFeature>`))
			response := httptest.NewRecorder()
			if stored {
				h.executeCustomStoredQuery(context.Background(), response, request, ws, &GetFeatureRequest{Count: 1, SRID: 3857}, &StoredQuery{QueryExpression: expression})
			} else {
				h.handleGetFeature(response, request, ws)
			}
			var result struct {
				Next     string `xml:"next,attr"`
				Previous string `xml:"previous,attr"`
			}
			if err := xml.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 || result.Next == "" {
				t.Fatalf("first: %d %s %v", response.Code, response.Body, err)
			}
			if strings.Contains(result.Next, "never-in-links") || strings.Contains(result.Next, "api_key") {
				t.Fatal("credential leaked")
			}
			next := httptest.NewRecorder()
			h.handleGetFeature(next, httptest.NewRequest("GET", result.Next, nil), ws)
			if next.Code != 200 || len(source.queries) != 2 {
				t.Fatalf("next: %d %s", next.Code, next.Body)
			}
			left, right := source.queries[0], source.queries[1]
			right.Offset = left.Offset
			if left.CompiledFilter == "" || !reflect.DeepEqual(left, right) {
				t.Fatalf("effective query changed: %+v %+v", left, right)
			}
			if err := xml.Unmarshal(next.Body.Bytes(), &result); err != nil || result.Previous == "" {
				t.Fatalf("previous missing: %s", next.Body)
			}
			if stored {
				source.failCount = true
				failed := httptest.NewRecorder()
				h.executeCustomStoredQuery(context.Background(), failed, request, ws, &GetFeatureRequest{Count: 1}, &StoredQuery{QueryExpression: expression})
				if failed.Code == 200 {
					t.Fatalf("stored count error concealed: %s", failed.Body)
				}
			}
		})
	}
}

func TestWFSCountTimeoutHasUnknownTotalAndContinuation(t *testing.T) {
	h, ws, source := pagingFixture()
	source.countError = context.DeadlineExceeded
	response := httptest.NewRecorder()
	h.handleGetFeature(response, httptest.NewRequest("GET", "http://example.test/workspaces/review/wfs?SERVICE=WFS&REQUEST=GetFeature&TYPENAMES=roads&COUNT=1", nil), ws)
	var result struct {
		Matched string `xml:"numberMatched,attr"`
		Next    string `xml:"next,attr"`
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 || result.Matched != "unknown" || result.Next == "" {
		t.Fatalf("timeout: %d %s", response.Code, response.Body)
	}
}
