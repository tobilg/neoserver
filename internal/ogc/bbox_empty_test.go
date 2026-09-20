package ogc

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/neoserver/internal/workspace"
)

// Keep an intentional empty result alongside the globally populated ETS data.
func TestBBoxReturnsEmptyFeatureCollectionOutsidePublishedData(t *testing.T) {
	ds := boundarySource(t)
	ws := newTestWorkspace("bbox", true)
	addTestLayer(ws, "source", "records", ds)
	ws.Services["source"].Layers["records"].SourceLayer = "records"
	h := newTestHandler()
	for _, tc := range []struct {
		bbox  string
		count int
	}{{"6,50,7.5,51.5", 1}, {"-1,10,1,11", 0}} {
		request := httptest.NewRequest("GET", "/collections/records/items?bbox="+tc.bbox, nil)
		ctx := withChiContext(workspace.WithWorkspace(request.Context(), ws), map[string]string{"collectionId": "records"})
		recorder := httptest.NewRecorder()
		h.items(recorder, request.WithContext(ctx))
		var collection struct {
			Type          string            `json:"type"`
			Features      []json.RawMessage `json:"features"`
			NumberMatched int               `json:"numberMatched"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &collection); err != nil || recorder.Code != 200 || collection.Type != "FeatureCollection" || collection.Features == nil || len(collection.Features) != tc.count || collection.NumberMatched != tc.count {
			t.Fatalf("bbox %s: %d %s (%v)", tc.bbox, recorder.Code, recorder.Body.String(), err)
		}
	}
}
