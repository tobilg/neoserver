package wfs

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestCapabilitiesNegotiateVersionAndSeparateCacheEntries(t *testing.T) {
	manager, err := cache.NewManager(cache.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	h := &workspaceHandler{cfg: conf.Config{WFS: conf.WFS{AppNamespacePrefix: "app"}}, cache: manager}
	ws := &workspace.Workspace{ID: "versions", Name: "versions"}
	for _, tc := range []struct{ query, version string }{
		{"", "2.0.0"}, {"VERSION=2.0.2", "2.0.2"}, {"VERSION=2.0.0", "2.0.0"},
		{"VERSION=2.0.0&ACCEPTVERSIONS=9.0,2.0.2,2.0.0", "2.0.2"},
		{"ACCEPTVERSIONS=2.0.0,2.0.2", "2.0.0"},
	} {
		recorder := httptest.NewRecorder()
		h.handleGetCapabilities(recorder, httptest.NewRequest("GET", "/?"+tc.query, nil), ws)
		if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `WFS_Capabilities version="`+tc.version+`"`) {
			t.Fatalf("%s: %s", tc.query, recorder.Body.String())
		}
	}
	// Ristretto commits asynchronously. Exercise actual cache hits for each
	// version; a cache key shared by both versions would return the wrong root.
	for _, version := range []string{"2.0.0", "2.0.2"} {
		deadline := time.Now().Add(time.Second)
		for {
			recorder := httptest.NewRecorder()
			h.handleGetCapabilities(recorder, httptest.NewRequest("GET", "/?VERSION="+version, nil), ws)
			if !strings.Contains(recorder.Body.String(), `WFS_Capabilities version="`+version+`"`) {
				t.Fatal("cached response has wrong version")
			}
			if recorder.Header().Get("X-Cache") == "HIT" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("capabilities cache did not accept response")
			}
			time.Sleep(time.Millisecond)
		}
	}
	recorder := httptest.NewRecorder()
	h.handleGetCapabilities(recorder, httptest.NewRequest("GET", "/?ACCEPTVERSIONS=9.0", nil), ws)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "VersionNegotiationFailed") {
		t.Fatal(recorder.Body.String())
	}
}

func Test202LockIDAndQueryAreMutuallyExclusive(t *testing.T) {
	h := &workspaceHandler{}
	for _, target := range []string{"/?SERVICE=WFS&VERSION=2.0.2&LOCKID=existing&TYPENAMES=roads", "/?SERVICE=WFS&VERSION=2.0.2&LOCKID=existing&STOREDQUERY_ID=urn:ogc:def:query:OGC-WFS::GetFeatureById&ID=roads.1"} {
		recorder := httptest.NewRecorder()
		h.handleLockFeature(recorder, httptest.NewRequest("GET", target, nil), &workspace.Workspace{ID: "ws"})
		if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "OperationParsingFailed") {
			t.Fatal(recorder.Body.String())
		}
	}
	request := httptest.NewRequest("POST", "/", strings.NewReader(`<wfs:LockFeature xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.2" lockId="existing"><wfs:Query typeNames="roads"/></wfs:LockFeature>`))
	recorder := httptest.NewRecorder()
	h.handleLockFeature(recorder, request, &workspace.Workspace{ID: "ws"})
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "OperationParsingFailed") {
		t.Fatal(recorder.Body.String())
	}
}

func TestTransactionResponseUsesRequestedSupportedVersion(t *testing.T) {
	for _, version := range []string{"2.0.0", "2.0.2"} {
		recorder := httptest.NewRecorder()
		WriteTransactionResponse(recorder, &TransactionResponse{Version: version})
		if !strings.Contains(recorder.Body.String(), `TransactionResponse version="`+version+`"`) {
			t.Fatal(recorder.Body.String())
		}
	}
}

func TestFeatureResponsesPreserveRequestVersionInLinks(t *testing.T) {
	for _, version := range []string{"2.0.0", "2.0.2"} {
		req := &GetFeatureRequest{Version: version}
		info := &datasource.LayerInfo{Name: "roads", SRID: 4326}
		feature := []byte(`{"type":"Feature","id":1,"geometry":{"type":"Point","coordinates":[7,51]},"properties":{}}`)
		for _, writer := range []func(*httptest.ResponseRecorder){
			func(w *httptest.ResponseRecorder) {
				WriteGMLFeatureCollectionMatched(w, info, [][]byte{feature}, "2", 0, 1, "http://example.org/app", "app", 4326, "http://example.org/wfs", "roads", req)
			},
			func(w *httptest.ResponseRecorder) {
				WriteGMLFeatureCollectionWithLock(w, info, [][]byte{feature}, 2, 0, 1, "http://example.org/app", "app", 4326, "http://example.org/wfs", "roads", "lock", req)
			},
			func(w *httptest.ResponseRecorder) {
				WriteGMLSingleFeature(w, info, feature, "http://example.org/app", "app", 4326, "roads", "http://example.org/wfs", "roads.1", req)
			},
		} {
			recorder := httptest.NewRecorder()
			writer(recorder)
			body := recorder.Body.String()
			if recorder.Code != 200 || !strings.Contains(body, "version="+version) {
				t.Fatalf("%s response: %s", version, body)
			}
			if version == "2.0.2" && strings.Contains(body, "version=2.0.0") {
				t.Fatalf("2.0.2 response downgraded a link: %s", body)
			}
		}
	}
}
