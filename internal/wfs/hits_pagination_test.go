package wfs

import (
	"encoding/xml"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHitsLinkRetrievesResultsAndPreservesQuery(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		for _, count := range []int{1, 2, 100} {
			t.Run(fmt.Sprintf("%s/count=%d", method, count), func(t *testing.T) {
				h, ws, source := pagingFixture()
				request := httptest.NewRequest("GET", fmt.Sprintf("http://example.test/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=roads&RESULTTYPE=hits&COUNT=%d&PROPERTYNAME=name&SORTBY=name+D&SRSNAME=EPSG:3857&BBOX=7,50,8,52&api_key=never-in-links", count), nil)
				if method == "POST" {
					request = httptest.NewRequest("POST", "http://example.test/wfs?api_key=never-in-links", strings.NewReader(fmt.Sprintf(`<GetFeature service="WFS" version="2.0.0" resultType="hits" count="%d"><Query typeNames="roads" srsName="EPSG:3857"><PropertyName>name</PropertyName><fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0"><fes:PropertyIsEqualTo><fes:ValueReference>name</fes:ValueReference><fes:Literal>control</fes:Literal></fes:PropertyIsEqualTo></fes:Filter></Query></GetFeature>`, count)))
				}
				response := httptest.NewRecorder()
				h.handleGetFeature(response, request, ws)
				var hits struct {
					Next     string `xml:"next,attr"`
					Returned int    `xml:"numberReturned,attr"`
				}
				if err := xml.Unmarshal(response.Body.Bytes(), &hits); err != nil || response.Code != 200 || hits.Next == "" || hits.Returned != 0 {
					t.Fatalf("hits: %d %s (%v)", response.Code, response.Body, err)
				}
				link, err := url.Parse(hits.Next)
				if err != nil {
					t.Fatal(err)
				}
				query := link.Query()
				if query.Get("resultType") != "results" || query.Get("startIndex") != "0" || query.Get("count") != fmt.Sprint(count) || strings.Contains(hits.Next, "never-in-links") || query.Get("srsName") != "EPSG:3857" || query.Get("propertyName") != "name" {
					t.Fatalf("incorrect continuation: %s", hits.Next)
				}
				if method == "POST" && query.Get("filter") == "" {
					t.Fatal("lost XML filter")
				}
				if method == "GET" && (query.Get("sortBy") != "name D" || query.Get("bbox") == "") {
					t.Fatal("lost sorting or bbox")
				}
				page := httptest.NewRecorder()
				h.handleGetFeature(page, httptest.NewRequest("GET", hits.Next, nil), ws)
				if page.Code != 200 || !strings.Contains(page.Body.String(), `numberReturned="1"`) || len(source.queries) != 1 || source.queries[0].Offset != 0 {
					t.Fatalf("continuation did not retrieve first page: %d %s", page.Code, page.Body)
				}
			})
		}
	}
}

func TestHitsLinkBoundsAndRequestImmutability(t *testing.T) {
	for _, tc := range []struct {
		total, start, count int
		hasNext             bool
	}{{0, 0, 1, false}, {1, 0, 1, true}, {1, 0, 10, true}, {5, 2, 10, true}, {5, 5, 1, false}, {5, 0, 0, false}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			req := &GetFeatureRequest{ResultType: ResultTypeHits}
			w := httptest.NewRecorder()
			WriteHitsResponse(w, tc.total, tc.start, tc.count, "http://example.test/wfs", "roads", req)
			var result struct {
				Next string `xml:"next,attr"`
			}
			if err := xml.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if (result.Next != "") != tc.hasNext || req.ResultType != ResultTypeHits {
				t.Fatalf("unexpected hits link or mutated request: %s", w.Body)
			}
			if tc.hasNext {
				u, err := url.Parse(result.Next)
				if err != nil || u.Query().Get("startIndex") != fmt.Sprint(tc.start) {
					t.Fatalf("incorrect initial offset: %s", result.Next)
				}
			}
		})
	}
}
