package tiles

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestConfig() conf.Config {
	return conf.Config{
		Server: conf.Server{
			UrlBase: "http://localhost:9000",
		},
		Tiles: conf.Tiles{
			Enabled:              true,
			MinZoom:              0,
			MaxZoom:              22,
			TileSize:             4096,
			RenderQueueTimeoutMS: 2000,
		},
	}
}

// newTilesSettings returns workspace settings with OGC Tiles enabled and public,
// with both vector and map tiles switched on.
func newTilesSettings() *store.WorkspaceSettings {
	return &store.WorkspaceSettings{
		OGCTilesAPI: store.OGCTilesAPISettings{
			Enabled: true,
			Public:  true,
			Settings: store.OGCTilesAPIInnerSettings{
				TileMatrixSets: []string{TMSWebMercatorQuad, TMSWorldCRS84Quad},
				VectorTiles:    store.OGCTilesAPIVectorSettings{Enabled: true, Formats: []string{MediaTypeMVT}},
				MapTiles:       store.OGCTilesAPIMapSettings{Enabled: true, Formats: []string{MediaTypePNG, MediaTypeJPEG, MediaTypeWEBP}},
			},
		},
	}
}

func newTestWorkspace() *workspace.Workspace {
	return &workspace.Workspace{
		ID:       "ws1",
		Name:     "demo",
		Settings: newTilesSettings(),
	}
}

// addTestLayer registers an enabled layer under service svc1 (created on demand).
func addTestLayer(ws *workspace.Workspace, publicID string, ds datasource.DataSource) *workspace.Layer {
	svc := ws.GetService("svc1")
	if svc == nil {
		svc = &workspace.Service{ID: "svc1", Name: "svc1", Type: store.ServiceTypeDuckDB, Enabled: true, DataSource: ds}
		ws.AddService(svc)
	}
	if ds != nil {
		svc.DataSource = ds
	}
	layer := &workspace.Layer{
		ID:          publicID,
		PublicID:    publicID,
		SourceLayer: publicID,
		Title:       "Test Layer",
		Enabled:     true,
		CRSDefault:  4326,
	}
	svc.AddLayer(layer)
	return layer
}

func addTestCoverage(ws *workspace.Workspace, publicID string) *workspace.Coverage {
	source := &fakeCoverageSource{}
	svc := &workspace.Service{ID: "raster-svc", Name: "raster-svc", Type: store.ServiceTypeRasterFile, Enabled: true, CoverageSource: source, Coverages: map[string]*workspace.Coverage{}}
	ws.AddService(svc)
	coverage := &workspace.Coverage{ID: publicID, PublicID: publicID, SourceCoverage: "raster", Title: "Test Coverage", Enabled: true, Public: true, Resampling: "nearest"}
	svc.Coverages[publicID] = coverage
	return coverage
}

type fakeCoverageSource struct{}

func (*fakeCoverageSource) Type() store.ServiceType      { return store.ServiceTypeRasterFile }
func (*fakeCoverageSource) ID() string                   { return "fake-raster" }
func (*fakeCoverageSource) Health(context.Context) error { return nil }
func (*fakeCoverageSource) Close() error                 { return nil }
func (*fakeCoverageSource) DiscoverCoverages(context.Context) ([]*datasource.DiscoveredCoverage, error) {
	info := fakeCoverageInfo()
	return []*datasource.DiscoveredCoverage{{SourceCoverage: "raster", Info: *info}}, nil
}
func (*fakeCoverageSource) GetCoverageInfo(context.Context, string) (*datasource.CoverageInfo, error) {
	return fakeCoverageInfo(), nil
}
func (*fakeCoverageSource) ExtractCoverage(context.Context, string, datasource.CoverageWindow) ([]byte, error) {
	return nil, nil
}
func (*fakeCoverageSource) ReadCoverage(context.Context, string, datasource.CoverageWindow) (*datasource.CoverageRaster, error) {
	return nil, nil
}
func (*fakeCoverageSource) RenderCoverage(_ context.Context, _ string, request datasource.CoverageRenderRequest) (*datasource.CoverageRenderGrid, error) {
	count := request.Width * request.Height
	values := make([]float64, count)
	valid := make([]bool, count)
	for i := range values {
		values[i] = float64(i % 256)
		valid[i] = true
	}
	return &datasource.CoverageRenderGrid{Width: request.Width, Height: request.Height, BandNumbers: []int{1}, Bands: [][]float64{values}, Valid: valid, BandInfo: []datasource.CoverageBand{{Band: 1, Name: "band1", DataType: "Byte"}}}, nil
}
func fakeCoverageInfo() *datasource.CoverageInfo {
	return &datasource.CoverageInfo{CRS: "http://www.opengis.net/def/crs/EPSG/0/4326", SRID: 4326, AxisLabels: [2]string{"x", "y"}, Width: 4, Height: 4, OriginX: -180, OriginY: 90, ResolutionX: 90, ResolutionY: -45, Envelope: [4]float64{-180, -90, 180, 90}, Bands: []datasource.CoverageBand{{Band: 1, Name: "band1", DataType: "Byte"}}}
}

// newTestHandler builds a workspaceHandler ready for direct handler invocation.
func newTestHandler(cfg conf.Config) *workspaceHandler {
	return &workspaceHandler{
		cfg:          cfg,
		logger:       newTestLogger(),
		mvtGen:       NewMVTGenerator(4096),
		mapGen:       NewMapTileGenerator(256),
		renderSlots:  make(chan struct{}, 1),
		queueTimeout: 200 * time.Millisecond,
	}
}

func withChiContext(ctx context.Context, params map[string]string) context.Context {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

func withWorkspace(ctx context.Context, ws *workspace.Workspace) context.Context {
	return workspace.WithWorkspace(ctx, ws)
}

func withIdentity(ctx context.Context, subject string, roles map[string]string) context.Context {
	return identity.WithIdentity(ctx, &identity.Identity{Subject: subject, Roles: roles})
}

// tileRequest builds a GET request with the workspace and chi URL params in context.
func tileRequest(ws *workspace.Workspace, target string, params map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	ctx := withWorkspace(req.Context(), ws)
	ctx = withChiContext(ctx, params)
	return req.WithContext(ctx)
}

func decodeErrBody(rec *httptest.ResponseRecorder) map[string]string {
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body
}

// pointWKB returns a little-endian WKB encoding of POINT(x y).
func pointWKB(x, y float64) []byte {
	buf := make([]byte, 21)
	buf[0] = 1 // little endian
	binary.LittleEndian.PutUint32(buf[1:5], 1)
	binary.LittleEndian.PutUint64(buf[5:13], math.Float64bits(x))
	binary.LittleEndian.PutUint64(buf[13:21], math.Float64bits(y))
	return buf
}

func pointFeature(id int, x, y float64) json.RawMessage {
	f := map[string]any{
		"type":       "Feature",
		"id":         id,
		"properties": map[string]any{"name": "p"},
		"geometry":   map[string]any{"type": "Point", "coordinates": []float64{x, y}},
	}
	raw, _ := json.Marshal(f)
	return raw
}

func testLayerInfo(name string) *datasource.LayerInfo {
	return &datasource.LayerInfo{
		Name:           name,
		Schema:         "public",
		GeometryColumn: "geom",
		GeometryType:   "Point",
		SRID:           4326,
		IDColumn:       "id",
		Properties: []datasource.PropertyInfo{
			{Name: "id", Type: "int4", JSONType: datasource.JSONTypeInteger, Ordinal: 0},
			{Name: "name", Type: "text", JSONType: datasource.JSONTypeString, Ordinal: 1},
		},
	}
}

// fakeDataSource implements datasource.DataSource for handler and generator tests.
type fakeDataSource struct {
	svcType      store.ServiceType
	layerInfo    *datasource.LayerInfo
	layerInfoErr error
	queryResult  []json.RawMessage
	queryErr     error
	wkbResult    []datasource.RenderFeature
	wkbErr       error
	queryDelay   time.Duration

	queryCalls int64

	// Guarded: the tile engine renders concurrently, so a test that drives it
	// from several goroutines would otherwise race on this recording.
	paramsMu   sync.Mutex
	lastParams datasource.QueryParams
}

func (f *fakeDataSource) recordParams(params datasource.QueryParams) {
	f.paramsMu.Lock()
	defer f.paramsMu.Unlock()
	f.lastParams = params
}

func (f *fakeDataSource) params() datasource.QueryParams {
	f.paramsMu.Lock()
	defer f.paramsMu.Unlock()
	return f.lastParams
}

func (f *fakeDataSource) Type() store.ServiceType {
	if f.svcType == "" {
		return store.ServiceTypeDuckDB
	}
	return f.svcType
}

func (f *fakeDataSource) ID() string { return "fake" }

func (f *fakeDataSource) DiscoverLayers(ctx context.Context) ([]*datasource.DiscoveredLayer, error) {
	return nil, nil
}

func (f *fakeDataSource) Query(ctx context.Context, layer string, params datasource.QueryParams) ([]json.RawMessage, error) {
	atomic.AddInt64(&f.queryCalls, 1)
	f.lastParams = params
	if f.queryDelay > 0 {
		time.Sleep(f.queryDelay)
	}
	return f.queryResult, f.queryErr
}

func (f *fakeDataSource) QueryWKB(ctx context.Context, layer string, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	atomic.AddInt64(&f.queryCalls, 1)
	f.recordParams(params)
	return f.wkbResult, f.wkbErr
}

func (f *fakeDataSource) QueryByID(ctx context.Context, layer, featureID string, outputSRID int) (json.RawMessage, bool, error) {
	return nil, false, nil
}

func (f *fakeDataSource) Count(ctx context.Context, layer string, params datasource.QueryParams) (int, error) {
	return 0, nil
}

func (f *fakeDataSource) GetLayerInfo(ctx context.Context, layer string) (*datasource.LayerInfo, error) {
	if f.layerInfoErr != nil {
		return nil, f.layerInfoErr
	}
	if f.layerInfo != nil {
		return f.layerInfo, nil
	}
	return testLayerInfo(layer), nil
}

func (f *fakeDataSource) Health(ctx context.Context) error { return nil }
func (f *fakeDataSource) Close() error                     { return nil }

// fakeSQLViewDataSource adds SQL view support on top of fakeDataSource.
type fakeSQLViewDataSource struct {
	fakeDataSource
	sqlViewRows []json.RawMessage
	sqlViewWKB  []datasource.RenderFeature
	sqlViewErr  error
}

func (f *fakeSQLViewDataSource) ValidateSQLView(ctx context.Context, sql string) error { return nil }

func (f *fakeSQLViewDataSource) DiscoverSQLViewColumns(ctx context.Context, sql string) (*datasource.SQLViewDiscovery, error) {
	return nil, nil
}

func (f *fakeSQLViewDataSource) QuerySQLView(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) ([]json.RawMessage, error) {
	return f.sqlViewRows, f.sqlViewErr
}

func (f *fakeSQLViewDataSource) QuerySQLViewWKB(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) ([]datasource.RenderFeature, error) {
	return f.sqlViewWKB, f.sqlViewErr
}

func (f *fakeSQLViewDataSource) CountSQLView(ctx context.Context, config *datasource.SQLViewConfig, params datasource.QueryParams) (int, error) {
	return 0, nil
}

// validSLD is a minimal parseable SLD body (polygon symbolizer only).
const validSLD = `<?xml version="1.0" encoding="UTF-8"?>
<StyledLayerDescriptor version="1.1.0">
  <NamedLayer>
    <Name>testlayer</Name>
    <UserStyle>
      <Name>teststyle</Name>
      <Title>Test Style</Title>
      <IsDefault>true</IsDefault>
      <FeatureTypeStyle>
        <Rule>
          <Name>rule1</Name>
          <PolygonSymbolizer>
            <Fill>
              <CssParameter name="fill">#ff0000</CssParameter>
            </Fill>
          </PolygonSymbolizer>
        </Rule>
      </FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>`
