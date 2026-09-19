package wmts

import (
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

type testCoverageSource struct{}

func (*testCoverageSource) Type() store.ServiceType      { return store.ServiceTypeRasterFile }
func (*testCoverageSource) ID() string                   { return "coverage-source" }
func (*testCoverageSource) Health(context.Context) error { return nil }
func (*testCoverageSource) Close() error                 { return nil }
func (*testCoverageSource) DiscoverCoverages(context.Context) ([]*datasource.DiscoveredCoverage, error) {
	return []*datasource.DiscoveredCoverage{{SourceCoverage: "raster", Info: *testCoverageInfo()}}, nil
}
func (*testCoverageSource) GetCoverageInfo(context.Context, string) (*datasource.CoverageInfo, error) {
	return testCoverageInfo(), nil
}
func (*testCoverageSource) ExtractCoverage(context.Context, string, datasource.CoverageWindow) ([]byte, error) {
	return nil, nil
}
func (*testCoverageSource) ReadCoverage(context.Context, string, datasource.CoverageWindow) (*datasource.CoverageRaster, error) {
	return nil, nil
}
func (*testCoverageSource) RenderCoverage(_ context.Context, _ string, request datasource.CoverageRenderRequest) (*datasource.CoverageRenderGrid, error) {
	values, valid := make([]float64, request.Width*request.Height), make([]bool, request.Width*request.Height)
	for index := range values {
		values[index], valid[index] = 42, true
	}
	return &datasource.CoverageRenderGrid{Width: request.Width, Height: request.Height, BandNumbers: []int{1}, Bands: [][]float64{values}, Valid: valid, BandInfo: []datasource.CoverageBand{{Band: 1, Name: "value", DataType: "Byte"}}}, nil
}

func testCoverageInfo() *datasource.CoverageInfo {
	return &datasource.CoverageInfo{CRS: "EPSG:4326", SRID: 4326, Width: 2, Height: 2, OriginX: -180, OriginY: 90, ResolutionX: 180, ResolutionY: -90, Envelope: [4]float64{-180, -90, 180, 90}, Bands: []datasource.CoverageBand{{Band: 1, Name: "value", DataType: "Byte"}}}
}

func testHandler() (*handler, *workspace.Workspace) {
	cfg := conf.Config{Server: conf.Server{UrlBase: "http://example.test"}, Tiles: conf.Tiles{MinZoom: 0, MaxZoom: 2, TileSize: 4096, MaxFeatures: 100, MaxVertices: 1000, MaxTileBytes: 1 << 20, MaxConcurrentRenders: 1, RenderQueueTimeoutMS: 100, StatementTimeoutMS: 1000}, WMTS: conf.WMTS{Enabled: true, MaxFeatureInfoResults: 3}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := tiles.NewEngine(cfg, logger, nil, nil)
	settings := store.DefaultOGCTilesAPISettings()
	settings.Enabled, settings.Public, settings.Settings.CacheEnabled = true, true, false
	ws := &workspace.Workspace{ID: "workspace", Name: "demo", Services: map[string]*workspace.Service{}, Styles: map[string]*workspace.Style{}, Settings: &store.WorkspaceSettings{OGCTilesAPI: settings, WMTS: store.WMTSSettings{Enabled: true, Public: true, FeatureInfoEnabled: true}}}
	source := &testCoverageSource{}
	service := &workspace.Service{ID: "service", Name: "service", Type: store.ServiceTypeRasterFile, Enabled: true, CoverageSource: source, Coverages: map[string]*workspace.Coverage{}}
	service.Coverages["elevation"] = &workspace.Coverage{ID: "coverage", PublicID: "elevation", SourceCoverage: "raster", Title: "Elevation", Enabled: true, Public: true, TileCacheGeneration: 1}
	ws.Services[service.ID] = service
	ws.Services["features"] = &workspace.Service{ID: "features", Name: "features", Enabled: true, Layers: map[string]*workspace.Layer{
		"roads": {ID: "roads", PublicID: "roads", SourceLayer: "roads", Title: "Roads", Enabled: true, Public: true, TileCacheGeneration: 1},
	}}
	return &handler{cfg: cfg, logger: logger, engine: engine}, ws
}

func wmtsRequest(ws *workspace.Workspace, target string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	return request.WithContext(workspace.WithWorkspace(request.Context(), ws))
}

func TestCapabilitiesAdvertisesKVPRESTAndRasterLayer(t *testing.T) {
	h, ws := testHandler()
	recorder := httptest.NewRecorder()
	h.capabilities(recorder, wmtsRequest(ws, "http://example.test/workspaces/demo/wmts?SERVICE=WMTS&REQUEST=GetCapabilities"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	decoder := xml.NewDecoder(strings.NewReader(recorder.Body.String()))
	for {
		if _, err := decoder.Token(); err != nil {
			if err != io.EOF {
				t.Fatalf("invalid capabilities XML: %v", err)
			}
			break
		}
	}
	body := recorder.Body.String()
	for _, expected := range []string{"<ows:Identifier>elevation</ows:Identifier>", "GetTile", "ResourceURL", "WebMercatorQuad", "image/png", "<ows:ServiceProvider>", "<Themes>"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("capabilities missing %q", expected)
		}
	}
}

func TestCapabilitiesProviderAndSections(t *testing.T) {
	h, ws := testHandler()
	ws.Settings.WMTS.ProviderName = `Maps & More`
	ws.Settings.WMTS.ProviderSite = "https://example.test/provider?a=1&b=2"
	ws.Settings.WMTS.ContactName = "Map Admin"
	ws.Settings.WMTS.ContactPosition = "Operator"
	ws.Settings.WMTS.ContactEmail = "maps@example.test"

	tests := []struct {
		name       string
		sections   string
		want       []string
		doNotWant  []string
		statusCode int
	}{
		{name: "provider", sections: "ServiceProvider", want: []string{"<ows:ServiceProvider>", "Maps &amp; More", "maps@example.test"}, doNotWant: []string{"<ows:ServiceIdentification>", "<Contents>"}, statusCode: http.StatusOK},
		{name: "multiple", sections: "Contents,Themes", want: []string{"<Contents>", "<Themes>", "<LayerRef>roads</LayerRef>"}, doNotWant: []string{"<ows:ServiceProvider>", "<ows:OperationsMetadata>"}, statusCode: http.StatusOK},
		{name: "all", sections: "All", want: []string{"<ows:ServiceIdentification>", "<ows:ServiceProvider>", "<ows:OperationsMetadata>", "<Contents>", "<Themes>", "<ServiceMetadataURL"}, statusCode: http.StatusOK},
		{name: "missing", sections: "", want: []string{"MissingParameterValue", `locator="sections"`}, statusCode: http.StatusBadRequest},
		{name: "invalid", sections: "Bogus", want: []string{"InvalidParameterValue", `locator="sections"`}, statusCode: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "http://example.test/workspaces/demo/wmts?SERVICE=WMTS&REQUEST=GetCapabilities&SECTIONS=" + tt.sections
			recorder := httptest.NewRecorder()
			h.capabilities(recorder, wmtsRequest(ws, target))
			if recorder.Code != tt.statusCode {
				t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
			}
			body := recorder.Body.String()
			for _, expected := range tt.want {
				if !strings.Contains(body, expected) {
					t.Errorf("response missing %q: %s", expected, body)
				}
			}
			for _, unexpected := range tt.doNotWant {
				if strings.Contains(body, unexpected) {
					t.Errorf("response contains %q: %s", unexpected, body)
				}
			}
		})
	}
}

func TestCapabilitiesDefaultProviderName(t *testing.T) {
	h, ws := testHandler()
	ws.Settings.WMTS.ProviderName = ""
	recorder := httptest.NewRecorder()
	h.capabilities(recorder, wmtsRequest(ws, "http://example.test/workspaces/demo/wmts?SERVICE=WMTS&REQUEST=GetCapabilities"))
	if !strings.Contains(recorder.Body.String(), "<ows:ProviderName>neoserver</ows:ProviderName>") {
		t.Fatalf("default provider missing: %s", recorder.Body.String())
	}
}

func TestCapabilitiesDoNotAdvertiseDisabledFeatureInfo(t *testing.T) {
	h, ws := testHandler()
	ws.Settings.WMTS.FeatureInfoEnabled = false
	recorder := httptest.NewRecorder()
	h.capabilities(recorder, wmtsRequest(ws, "http://example.test/workspaces/demo/wmts?SERVICE=WMTS&REQUEST=GetCapabilities"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "GetFeatureInfo") || strings.Contains(recorder.Body.String(), "InfoFormat") {
		t.Fatalf("disabled GetFeatureInfo was advertised: %s", recorder.Body.String())
	}
}

func TestEffectiveCapabilitiesUseOnlyEnabledTileSettings(t *testing.T) {
	_, ws := testHandler()
	resource := ws.GetResource("elevation")
	ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats = []string{tiles.MediaTypePNG, tiles.MediaTypePNG}
	ws.Settings.OGCTilesAPI.Settings.VectorTiles.Enabled = true
	formats := tileFormatsForResource(ws, resource)
	if len(formats) != 1 || formats[0] != tiles.MediaTypePNG {
		t.Fatalf("raster formats = %v, want [%s]", formats, tiles.MediaTypePNG)
	}

	ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled = false
	if formats := tileFormatsForResource(ws, resource); len(formats) != 0 {
		t.Fatalf("formats with map tiles disabled = %v, want none", formats)
	}
}

func TestWMTSVectorTilesRequireExplicitOptIn(t *testing.T) {
	_, ws := testHandler()
	resource := ws.GetResource("roads")
	if resource == nil {
		t.Fatal("feature resource missing")
	}
	if formats := tileFormatsForResource(ws, resource); contains(formats, tiles.MediaTypeMVT) {
		t.Fatalf("default WMTS formats unexpectedly contain MVT: %v", formats)
	}
	if _, ok := tileRequestType(ws, resource, tiles.MediaTypeMVT); ok {
		t.Fatal("default WMTS request validation accepted MVT")
	}
	ws.Settings.WMTS.VectorTilesEnabled = true
	if formats := tileFormatsForResource(ws, resource); !contains(formats, tiles.MediaTypeMVT) {
		t.Fatalf("opt-in WMTS formats missing MVT: %v", formats)
	}
	if tileType, ok := tileRequestType(ws, resource, tiles.MediaTypeMVT); !ok || tileType != "vector" {
		t.Fatalf("opt-in MVT request = %q, %v", tileType, ok)
	}
}

func TestKVPGetTileAndFeatureInfo(t *testing.T) {
	h, ws := testHandler()
	tileRecorder := httptest.NewRecorder()
	h.kvp(tileRecorder, wmtsRequest(ws, "http://example.test/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0&LAYER=elevation&STYLE=default&FORMAT=image%2Fpng&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=0&TILEROW=0&TILECOL=0"))
	if tileRecorder.Code != http.StatusOK || tileRecorder.Header().Get("Content-Type") != "image/png" || tileRecorder.Body.Len() == 0 {
		t.Fatalf("GetTile = %d %s (%d bytes)", tileRecorder.Code, tileRecorder.Header().Get("Content-Type"), tileRecorder.Body.Len())
	}

	infoRecorder := httptest.NewRecorder()
	h.kvp(infoRecorder, wmtsRequest(ws, "http://example.test/wmts?SERVICE=WMTS&REQUEST=GetFeatureInfo&VERSION=1.0.0&LAYER=elevation&STYLE=default&FORMAT=image%2Fpng&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=0&TILEROW=0&TILECOL=0&I=128&J=128&INFOFORMAT=application%2Fjson"))
	if infoRecorder.Code != http.StatusOK || !strings.Contains(infoRecorder.Body.String(), `"value":42`) {
		t.Fatalf("GetFeatureInfo = %d %s", infoRecorder.Code, infoRecorder.Body.String())
	}
}

func TestKVPReportsOWSExceptionForMissingParameters(t *testing.T) {
	h, ws := testHandler()
	recorder := httptest.NewRecorder()
	h.kvp(recorder, wmtsRequest(ws, "http://example.test/wmts?SERVICE=WMTS&REQUEST=GetTile"))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "MissingParameterValue") {
		t.Fatalf("unexpected exception: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestKVPExceptionSemantics(t *testing.T) {
	h, ws := testHandler()
	base := "http://example.test/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0&LAYER=elevation&STYLE=default&FORMAT=image%2Fpng&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=0&TILEROW=0&TILECOL=0"
	tests := []struct {
		name, target, code, locator string
		status                      int
	}{
		{name: "missing service", target: "http://example.test/wmts?REQUEST=GetCapabilities", code: "MissingParameterValue", locator: "service", status: http.StatusBadRequest},
		{name: "unknown request", target: "http://example.test/wmts?SERVICE=WMTS&REQUEST=GetBogus", code: "InvalidParameterValue", locator: "request", status: http.StatusBadRequest},
		{name: "invalid layer", target: strings.Replace(base, "LAYER=elevation", "LAYER=missing", 1), code: "InvalidParameterValue", locator: "layer", status: http.StatusBadRequest},
		{name: "invalid matrix", target: strings.Replace(base, "TILEMATRIX=0", "TILEMATRIX=Bogus", 1), code: "InvalidParameterValue", locator: "TileMatrix", status: http.StatusBadRequest},
		{name: "row out of range", target: strings.Replace(base, "TILEROW=0", "TILEROW=1", 1), code: "TileOutOfRange", locator: "TileRow", status: http.StatusBadRequest},
		{name: "column out of range", target: strings.Replace(base, "TILECOL=0", "TILECOL=1", 1), code: "TileOutOfRange", locator: "TileCol", status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			h.kvp(recorder, wmtsRequest(ws, tt.target))
			body := recorder.Body.String()
			if recorder.Code != tt.status || !strings.Contains(body, `exceptionCode="`+tt.code+`"`) || !strings.Contains(body, `locator="`+tt.locator+`"`) {
				t.Fatalf("response = %d %s", recorder.Code, body)
			}
		})
	}
}

func TestKVPMissingTileParameterLocators(t *testing.T) {
	h, ws := testHandler()
	parameters := []struct {
		queryName string
		locator   string
	}{
		{"VERSION", "version"}, {"LAYER", "layer"}, {"STYLE", "style"}, {"FORMAT", "format"},
		{"TILEMATRIXSET", "TileMatrixSet"}, {"TILEMATRIX", "TileMatrix"}, {"TILEROW", "TileRow"}, {"TILECOL", "TileCol"},
	}
	for _, parameter := range parameters {
		t.Run(parameter.queryName, func(t *testing.T) {
			target := "http://example.test/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0&LAYER=elevation&STYLE=default&FORMAT=image%2Fpng&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=0&TILEROW=0&TILECOL=0"
			request := wmtsRequest(ws, target)
			query := request.URL.Query()
			query.Del(parameter.queryName)
			request.URL.RawQuery = query.Encode()
			recorder := httptest.NewRecorder()
			h.kvp(recorder, request)
			body := recorder.Body.String()
			if recorder.Code != http.StatusBadRequest || !strings.Contains(body, "MissingParameterValue") || !strings.Contains(body, `locator="`+parameter.locator+`"`) {
				t.Fatalf("response = %d %s", recorder.Code, body)
			}
		})
	}
}
