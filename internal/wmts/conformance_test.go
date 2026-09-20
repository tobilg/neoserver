package wmts

import (
	"bytes"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestCapabilitiesNegotiationAndSequence(t *testing.T) {
	h, ws := testHandler()
	for _, tc := range []struct {
		query, media, body string
		status             int
	}{
		{"ACCEPTVERSIONS=1.1.0,1.2.0", "application/xml", "VersionNegotiationFailed", 400},
		{"ACCEPTVERSIONS=9.0,1.0.0&ACCEPTFORMATS=bogus,text/xml", "text/xml", "<Contents>", 200},
		{"ACCEPTFORMATS=bogus", "application/xml", "<Contents>", 200},
		{"UPDATESEQUENCE=0", "application/xml", "<Contents>", 200},
		{"UPDATESEQUENCE=1", "application/xml", `updateSequence="1"></Capabilities>`, 200},
		{"UPDATESEQUENCE=2", "application/xml", "InvalidUpdateSequence", 400},
		{"UPDATESEQUENCE=bogus", "application/xml", "InvalidUpdateSequence", 400},
	} {
		t.Run(tc.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			h.capabilities(recorder, wmtsRequest(ws, "/?"+tc.query))
			if recorder.Code != tc.status || recorder.Header().Get("Content-Type") != tc.media || !strings.Contains(recorder.Body.String(), tc.body) {
				t.Fatalf("response = %d %s %s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			if tc.status == 400 && strings.Contains(recorder.Body.String(), "locator=") {
				t.Fatal("negotiation exceptions must omit locator")
			}
		})
	}
}

func TestWellKnownScaleSetIsAdvertisedOnlyForMatchingGrid(t *testing.T) {
	for _, tc := range []struct {
		id      string
		minZoom int
		want    bool
	}{
		{tiles.TMSWebMercatorQuad, 0, true},
		{tiles.TMSWebMercatorQuad, 1, false},
		{tiles.TMSWorldCRS84Quad, 0, false},
	} {
		definition, err := tiles.GetTileMatrixSetDefinition(tc.id)
		if err != nil {
			t.Fatal(err)
		}
		var document bytes.Buffer
		writeMatrixSet(&document, definition, tc.minZoom, 2)
		if got := strings.Contains(document.String(), "<WellKnownScaleSet>"); got != tc.want {
			t.Fatalf("%s starting at %d: %s", tc.id, tc.minZoom, document.String())
		}
	}
}

func TestCustomMatrixIDsAreUsedForLimitsAndValidation(t *testing.T) {
	h, ws := testHandler()
	definition, _ := tiles.GetTileMatrixSetDefinition(tiles.TMSWorldCRS84Quad)
	definition.ID = "custom-offset"
	definition.TileMatrices = definition.TileMatrices[2:3]
	if err := tiles.ReplaceCustomTileMatrixSets([]*tiles.TileMatrixSetDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tiles.ReplaceCustomTileMatrixSets(nil) })
	ws.Settings.OGCTilesAPI.Settings.TileMatrixSets = []string{definition.ID}
	ws.Settings.WMTS.TileMatrixLimitsEnabled = true
	resource := ws.GetResource("elevation")
	resource.Coverage.NativeExtent = &store.SpatialExtent{MinX: 10, MinY: 10, MaxX: 20, MaxY: 20, SRID: 4326}
	var document bytes.Buffer
	writeMatrixLimits(&document, resource, definition.ID, 2, 2)
	if !strings.Contains(document.String(), "<TileMatrix>2</TileMatrix>") {
		t.Fatal(document.String())
	}
	request := tileRequest{Version: version, Layer: "elevation", Style: "default", MatrixSet: definition.ID, Matrix: 2, Row: 1, Column: 4, Format: "image/png"}
	recorder := httptest.NewRecorder()
	if _, _, _, ok := h.validateTileRequest(recorder, wmtsRequest(ws, "/"), request); !ok {
		t.Fatal(recorder.Body.String())
	}
}

func TestDimensionCurrentDefaultsAndRejection(t *testing.T) {
	dimension := &workspace.Dimension{Name: "time", Default: "current", Current: true, Extent: "2000-01-01T00:00:00Z/2000-01-01T00:01:00Z/PT5S"}
	now := time.Date(2000, 1, 1, 0, 0, 13, 0, time.UTC)
	for _, tc := range []struct {
		input, want string
		fails       bool
	}{
		{"current", "2000-01-01T00:00:10Z", false},
		{"2000-01-01T00:00:15Z", "2000-01-01T00:00:15Z", false},
		{"2000-01-01T00:00:13Z", "", true},
		{"1999-01-01T00:00:00Z", "", true},
		{"bogus", "", true},
	} {
		got, err := resolveDimension(dimension, tc.input, now)
		if (err != nil) != tc.fails || got != tc.want {
			t.Fatalf("resolve %q = %q, %v", tc.input, got, err)
		}
	}
	h, ws := testHandler()
	resource := ws.GetResource("roads")
	resource.Layer.Dimensions = []*workspace.Dimension{dimension, {Name: "elevation", Default: "100", Extent: "100,200"}}
	request := tileRequest{Time: "default", Elevation: "default"}
	if !h.resolveDimensions(httptest.NewRecorder(), resource, &request) || request.Time != "2000-01-01T00:01:00Z" || request.Elevation != "100" {
		t.Fatalf("defaults/current = %+v", request)
	}
	request.Elevation = "999"
	recorder := httptest.NewRecorder()
	if h.resolveDimensions(recorder, resource, &request) || recorder.Code != 400 || !strings.Contains(recorder.Body.String(), `locator="elevation"`) {
		t.Fatalf("invalid value response: %s", recorder.Body.String())
	}
	var document bytes.Buffer
	writeDimensions(&document, resource)
	if !strings.Contains(document.String(), "<Current>true</Current>") || strings.Count(document.String(), "<Dimension>") != 2 {
		t.Fatal(document.String())
	}
}

func TestMatrixLimitsAdvertisementMatchesValidationAndOptIn(t *testing.T) {
	h, ws := testHandler()
	resource := ws.GetResource("elevation")
	resource.Coverage.NativeExtent = &store.SpatialExtent{MinX: 10, MinY: 10, MaxX: 20, MaxY: 20, SRID: 4326}
	definition, _ := tiles.GetTileMatrixSetDefinition(tiles.TMSWorldCRS84Quad)
	limits, ok := resourceMatrixLimits(resource, definition.ID, definition.TileMatrices[2])
	if !ok || limits.minCol != 4 || limits.maxCol != 4 || limits.minRow != 1 || limits.maxRow != 1 {
		t.Fatalf("limits = %+v, %v", limits, ok)
	}
	request := tileRequest{Version: version, Layer: "elevation", Style: "default", MatrixSet: definition.ID, Matrix: 2, Row: 0, Column: 4, Format: "image/png"}
	if _, _, _, ok := h.validateTileRequest(httptest.NewRecorder(), wmtsRequest(ws, "/"), request); !ok {
		t.Fatal("limits applied without opt-in")
	}
	ws.Settings.WMTS.TileMatrixLimitsEnabled = true
	recorder := httptest.NewRecorder()
	if _, _, _, ok := h.validateTileRequest(recorder, wmtsRequest(ws, "/"), request); ok || !strings.Contains(recorder.Body.String(), `locator="TileRow"`) {
		t.Fatal(recorder.Body.String())
	}
	request.Row = 1
	if _, _, _, ok := h.validateTileRequest(httptest.NewRecorder(), wmtsRequest(ws, "/"), request); !ok {
		t.Fatal("advertised tile was rejected")
	}
	var document bytes.Buffer
	writeMatrixLimits(&document, resource, definition.ID, 2, 2)
	if !strings.Contains(document.String(), "<MinTileRow>1</MinTileRow><MaxTileRow>1</MaxTileRow><MinTileCol>4</MinTileCol><MaxTileCol>4</MaxTileCol>") {
		t.Fatal(document.String())
	}
	resource.Coverage.NativeExtent.Stale = true
	if _, ok := resourceMatrixLimits(resource, definition.ID, definition.TileMatrices[2]); ok {
		t.Fatal("stale bounds used")
	}
}

func TestWMTSLegendWorksWithWMSDisabledAndHonorsVisibility(t *testing.T) {
	h, ws := testHandler()
	router := chi.NewRouter()
	RegisterWorkspaceRoutes(router, WorkspaceDependencies{Config: h.cfg, Logger: h.logger, Engine: h.engine})
	for _, tc := range []struct {
		path   string
		status int
	}{{"/1.0.0/elevation/default/legend.png", 200}, {"/1.0.0/elevation/missing/legend.png", 400}, {"/1.0.0/missing/default/legend.png", 400}} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, wmtsRequest(ws, tc.path))
		if recorder.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, recorder.Code, recorder.Body.String())
		}
		if tc.status == 200 {
			picture, err := png.Decode(recorder.Body)
			if err != nil || picture.Bounds().Dx() != 20 || picture.Bounds().Dy() != 20 {
				t.Fatalf("legend = %v, %v", picture, err)
			}
		}
	}
	ws.Services["service"].Coverages["elevation"].Public = false
	ws.Services["service"].Coverages["elevation"].AllowedRoles = []string{"private"}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, wmtsRequest(ws, "/1.0.0/elevation/default/legend.png"))
	if recorder.Code == 200 {
		t.Fatal("private legend was exposed")
	}
}
