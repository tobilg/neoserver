package wms

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type wmsCoverageSource struct{}

func (*wmsCoverageSource) Type() store.ServiceType      { return store.ServiceTypeRasterFile }
func (*wmsCoverageSource) ID() string                   { return "raster" }
func (*wmsCoverageSource) Health(context.Context) error { return nil }
func (*wmsCoverageSource) Close() error                 { return nil }
func (*wmsCoverageSource) DiscoverCoverages(context.Context) ([]*datasource.DiscoveredCoverage, error) {
	info := wmsCoverageInfo()
	return []*datasource.DiscoveredCoverage{{SourceCoverage: "raster", Info: *info}}, nil
}
func (*wmsCoverageSource) GetCoverageInfo(context.Context, string) (*datasource.CoverageInfo, error) {
	return wmsCoverageInfo(), nil
}
func (*wmsCoverageSource) ExtractCoverage(context.Context, string, datasource.CoverageWindow) ([]byte, error) {
	return nil, nil
}
func (*wmsCoverageSource) ReadCoverage(context.Context, string, datasource.CoverageWindow) (*datasource.CoverageRaster, error) {
	return nil, nil
}
func (*wmsCoverageSource) RenderCoverage(_ context.Context, _ string, request datasource.CoverageRenderRequest) (*datasource.CoverageRenderGrid, error) {
	count := request.Width * request.Height
	values := make([]float64, count)
	valid := make([]bool, count)
	for i := range values {
		values[i] = float64(i % 256)
		valid[i] = true
	}
	return &datasource.CoverageRenderGrid{Width: request.Width, Height: request.Height, BandNumbers: []int{1}, Bands: [][]float64{values}, Valid: valid, BandInfo: []datasource.CoverageBand{{Band: 1, Name: "elevation", DataType: "Byte"}}}, nil
}
func wmsCoverageInfo() *datasource.CoverageInfo {
	return &datasource.CoverageInfo{CRS: "http://www.opengis.net/def/crs/EPSG/0/4326", SRID: 4326, AxisLabels: [2]string{"x", "y"}, Width: 4, Height: 4, OriginX: -180, OriginY: 90, ResolutionX: 90, ResolutionY: -45, Envelope: [4]float64{-180, -90, 180, 90}, Bands: []datasource.CoverageBand{{Band: 1, Name: "elevation", DataType: "Byte"}}}
}
func rasterWMSFixture() (*workspaceHandler, *workspace.Workspace) {
	source := &wmsCoverageSource{}
	coverage := &workspace.Coverage{ID: "cov", PublicID: "elevation", SourceCoverage: "raster", Title: "Elevation", Enabled: true, Public: true, Resampling: "nearest"}
	service := &workspace.Service{ID: "svc", Enabled: true, CoverageSource: source, Coverages: map[string]*workspace.Coverage{"elevation": coverage}}
	ws := &workspace.Workspace{ID: "ws", Name: "demo", Services: map[string]*workspace.Service{"svc": service}, Styles: map[string]*workspace.Style{}, Settings: &store.WorkspaceSettings{WMS: store.WMSSettings{Enabled: true, Public: true, MaxWidth: 1024, MaxHeight: 1024, MaxPixels: 1024 * 1024}}}
	h := &workspaceHandler{cfg: conf.Config{Server: conf.Server{UrlBase: "http://example.test"}, WMS: conf.WMS{MaxWidth: 1024, MaxHeight: 1024, MaxPixels: 1024 * 1024, MaxRenderFeatures: 100, MaxRenderVertices: 1000}}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), renderSlots: make(chan struct{}, 1), styleCache: map[string]cachedStyle{}}
	return h, ws
}

func TestCoverageWMSCapabilitiesAndOperations(t *testing.T) {
	h, ws := rasterWMSFixture()
	caps := httptest.NewRecorder()
	h.handleGetCapabilities(caps, httptest.NewRequest(http.MethodGet, "/wms", nil), ws)
	if caps.Code != http.StatusOK || !strings.Contains(caps.Body.String(), "<Name>elevation</Name>") {
		t.Fatalf("coverage missing from capabilities: %d %s", caps.Code, caps.Body.String())
	}
	if !strings.Contains(caps.Body.String(), `<BoundingBox CRS="EPSG:4326" minx="-90" miny="-180" maxx="90" maxy="180"/>`) {
		t.Fatalf("coverage EPSG:4326 bounds do not use WMS 1.3 axis order: %s", caps.Body.String())
	}
	mapReq := httptest.NewRequest(http.MethodGet, "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=elevation&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180&WIDTH=32&HEIGHT=16&FORMAT=image/png&TRANSPARENT=true", nil)
	mapRec := httptest.NewRecorder()
	h.handleGetMap(mapRec, mapReq, ws)
	if mapRec.Code != http.StatusOK || mapRec.Header().Get("Content-Type") != FormatPNG || mapRec.Body.Len() < 20 {
		t.Fatalf("coverage GetMap failed: %d %s", mapRec.Code, mapRec.Body.String())
	}
	infoReq := httptest.NewRequest(http.MethodGet, "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo&LAYERS=elevation&QUERY_LAYERS=elevation&STYLES=&CRS=EPSG:4326&BBOX=-90,-180,90,180&WIDTH=32&HEIGHT=16&I=10&J=10&FORMAT=image/png&INFO_FORMAT=application/json", nil)
	infoRec := httptest.NewRecorder()
	h.handleGetFeatureInfo(infoRec, infoReq, ws)
	if infoRec.Code != http.StatusOK || !strings.Contains(infoRec.Body.String(), "elevation") {
		t.Fatalf("coverage GetFeatureInfo failed: %d %s", infoRec.Code, infoRec.Body.String())
	}
	legendReq := httptest.NewRequest(http.MethodGet, "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetLegendGraphic&LAYER=elevation&STYLE=&FORMAT=image/png&WIDTH=32&HEIGHT=8", nil)
	legendRec := httptest.NewRecorder()
	h.handleGetLegendGraphic(legendRec, legendReq, ws)
	if legendRec.Code != http.StatusOK || legendRec.Body.Len() < 20 {
		t.Fatalf("coverage legend failed: %d %s", legendRec.Code, legendRec.Body.String())
	}
}

func TestDefaultDimensionWarning(t *testing.T) {
	_, ws := rasterWMSFixture()
	ws.Services["svc"].Coverages["elevation"].Dimensions = []*workspace.Dimension{{
		Name:    "time",
		Default: "2000-01-01T00:00:00Z",
	}}
	req := &GetMapRequest{Layers: []string{"elevation"}}
	recorder := httptest.NewRecorder()

	addDefaultDimensionWarnings(recorder, req, ws)

	if got, want := recorder.Header().Get("Warning"), "99 Default value used: time=2000-01-01T00:00:00Z"; got != want {
		t.Fatalf("Warning = %q, want %q", got, want)
	}
}
