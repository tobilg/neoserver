package ogc

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/datasource"
)

func TestItemsOriginRefreshAndCachedTimestamp(t *testing.T) {
	h := newTestHandler()
	var err error
	h.cache, err = cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer h.cache.Close()
	ws := newTestWorkspace("timestamps", true)
	matched := 1
	addTestLayer(ws, "svc", "places", &mockDataSource{countFunc: func(context.Context, datasource.QueryParams) (int, error) { return matched, nil }})
	run := func(control string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/collections/places/items", nil)
		r.Header.Set("Cache-Control", control)
		ctx := withChiContext(withWorkspace(r.Context(), ws), map[string]string{"collectionId": "places"})
		w := httptest.NewRecorder()
		h.items(w, r.WithContext(ctx))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		return w
	}
	warm := func() *httptest.ResponseRecorder {
		deadline := time.Now().Add(time.Second)
		for {
			w := run("")
			if w.Header().Get("X-Cache") == "HIT" {
				return w
			}
			if time.Now().After(deadline) {
				t.Fatal("cache did not admit response")
			}
			time.Sleep(time.Millisecond)
		}
	}
	first := warm()
	if next := run(""); first.Body.String() != next.Body.String() {
		t.Fatal("cache reuse changed response timestamp")
	}
	before := time.Now().UTC()
	matched = 7
	fresh := run("no-cache")
	if fresh.Header().Get("X-Cache") != "MISS" {
		t.Fatal("no-cache reused stale response")
	}
	var body struct {
		Timestamp time.Time `json:"timeStamp"`
		Matched   int       `json:"numberMatched"`
	}
	if err := json.Unmarshal(fresh.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Timestamp.Before(before) || body.Timestamp.After(time.Now().UTC()) {
		t.Fatalf("origin timestamp outside generation interval: %s", fresh.Body)
	}
	if body.Matched != 7 {
		t.Fatal("origin refresh reused stale count metadata")
	}
	after := warm()
	if after.Body.String() != fresh.Body.String() {
		t.Fatal("refresh did not preserve the new generation timestamp")
	}
	if response := run("no-store"); response.Header().Get("X-Cache") != "MISS" {
		t.Fatal("no-store reused cached data")
	} else if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("no-store response omitted downstream cache policy")
	}
	if response := run(""); response.Body.String() != after.Body.String() {
		t.Fatal("no-store modified cached data")
	}
}
