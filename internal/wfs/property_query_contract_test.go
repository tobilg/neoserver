package wfs

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPropertyQueryUsesEffectiveLimitsAndSelection(t *testing.T) {
	h, ws, source := pagingFixture()
	target := "http://example.test/wfs?SERVICE=WFS&REQUEST=GetPropertyValue&TYPENAMES=roads&VALUEREFERENCE=name&COUNT=1&STARTINDEX=3&BBOX=6,50,10,54,CRS:84&SORTBY=name+D"
	for _, workspaceLimit := range []bool{false, true} {
		h.cfg.WFS.MaxOffset, ws.Settings.WFS.MaxOffset = 0, 0
		if workspaceLimit {
			ws.Settings.WFS.MaxOffset = 1
		} else {
			h.cfg.WFS.MaxOffset = 1
		}
		w := httptest.NewRecorder()
		h.handleGetPropertyValue(w, httptest.NewRequest("GET", target, nil), ws)
		if w.Code == 200 || len(source.queries) != 0 {
			t.Fatalf("offset guard bypassed: %d %s", w.Code, w.Body)
		}
	}
	h.cfg.WFS.MaxOffset, ws.Settings.WFS.MaxOffset = 10, 10
	w := httptest.NewRecorder()
	h.handleGetPropertyValue(w, httptest.NewRequest("GET", target, nil), ws)
	if w.Code != 200 || len(source.queries) != 1 {
		t.Fatalf("query: %d %s", w.Code, w.Body)
	}
	p := source.queries[0]
	if p.Offset != 3 || p.BBox == nil || len(p.SortBy) != 1 || !p.SortBy[0].Desc {
		t.Fatalf("lost query fields: %+v", p)
	}
	source.failCount = true
	w = httptest.NewRecorder()
	h.handleGetPropertyValue(w, httptest.NewRequest("GET", target, nil), ws)
	if w.Code == 200 {
		t.Fatalf("count failure concealed: %s", w.Body)
	}
	source.failCount = false
	source.countError = context.DeadlineExceeded
	w = httptest.NewRecorder()
	h.handleGetPropertyValue(w, httptest.NewRequest("GET", target, nil), ws)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `numberMatched="unknown"`) {
		t.Fatalf("timeout count: %d %s", w.Code, w.Body)
	}
}

func TestPropertyQueryXMLSharesFilterSortAndLimitParsing(t *testing.T) {
	h, ws, source := pagingFixture()
	ws.Settings.WFS.MaxFeatures = 1
	body := `<GetPropertyValue service="WFS" version="2.0.0" count="9" valueReference="name"><Query typeNames="roads"><Filter><PropertyIsEqualTo><ValueReference>name</ValueReference><Literal>control</Literal></PropertyIsEqualTo></Filter><SortBy><SortProperty><ValueReference>name</ValueReference><SortOrder>DESC</SortOrder></SortProperty></SortBy></Query></GetPropertyValue>`
	w := httptest.NewRecorder()
	h.handleGetPropertyValue(w, httptest.NewRequest("POST", "http://example.test/wfs", strings.NewReader(body)), ws)
	if w.Code != 200 || len(source.queries) != 1 {
		t.Fatalf("XML query: %d %s", w.Code, w.Body)
	}
	p := source.queries[0]
	if p.Limit != 1 || p.Predicate == nil || len(p.SortBy) != 1 || !p.SortBy[0].Desc {
		t.Fatalf("XML fields lost: %+v", p)
	}
	for _, count := range []string{"-1", "oops"} {
		w = httptest.NewRecorder()
		h.handleGetPropertyValue(w, httptest.NewRequest("POST", "http://example.test/wfs", strings.NewReader(strings.Replace(body, `count="9"`, `count="`+count+`"`, 1))), ws)
		if w.Code == 200 {
			t.Fatalf("invalid XML count accepted: %s", count)
		}
	}
}

func TestPropertyHitsRequiresASuccessfulCount(t *testing.T) {
	h, ws, source := pagingFixture()
	target := "http://example.test/wfs?SERVICE=WFS&REQUEST=GetPropertyValue&TYPENAMES=roads&VALUEREFERENCE=name&RESULTTYPE=hits"
	w := httptest.NewRecorder()
	h.handleGetPropertyValue(w, httptest.NewRequest("GET", target, nil), ws)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `numberMatched="2"`) || len(source.queries) != 0 {
		t.Fatalf("hits query: %d %s", w.Code, w.Body)
	}
	for _, timeout := range []bool{false, true} {
		source.failCount = !timeout
		if timeout {
			source.countError = context.DeadlineExceeded
		}
		w = httptest.NewRecorder()
		h.handleGetPropertyValue(w, httptest.NewRequest("GET", target, nil), ws)
		if w.Code == 200 {
			t.Fatalf("fabricated hits count: %s", w.Body)
		}
	}
}
