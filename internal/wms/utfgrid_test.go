package wms

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type utfGridDataSource struct{ feature datasource.RenderFeature }

func (*utfGridDataSource) Type() store.ServiceType      { return store.ServiceTypePostGIS }
func (*utfGridDataSource) ID() string                   { return "utfgrid" }
func (*utfGridDataSource) Health(context.Context) error { return nil }
func (*utfGridDataSource) Close() error                 { return nil }
func (*utfGridDataSource) DiscoverLayers(context.Context) ([]*datasource.DiscoveredLayer, error) {
	return nil, nil
}
func (*utfGridDataSource) Query(context.Context, string, datasource.QueryParams) ([]json.RawMessage, error) {
	return nil, nil
}
func (d *utfGridDataSource) QueryWKB(context.Context, string, datasource.QueryParams) ([]datasource.RenderFeature, error) {
	return []datasource.RenderFeature{d.feature}, nil
}
func (*utfGridDataSource) QueryByID(context.Context, string, string, int) (json.RawMessage, bool, error) {
	return nil, false, nil
}
func (*utfGridDataSource) Count(context.Context, string, datasource.QueryParams) (int, error) {
	return 1, nil
}
func (*utfGridDataSource) GetLayerInfo(context.Context, string) (*datasource.LayerInfo, error) {
	return &datasource.LayerInfo{Name: "roads", GeometryType: "Point", SRID: 4326}, nil
}

func utfGridPointWKB(x, y float64) []byte {
	body := make([]byte, 21)
	body[0] = 1
	binary.LittleEndian.PutUint32(body[1:5], uint32(renderer.WKBPoint))
	binary.LittleEndian.PutUint64(body[5:13], math.Float64bits(x))
	binary.LittleEndian.PutUint64(body[13:21], math.Float64bits(y))
	return body
}

func TestUTFGridHitBufferPaintsGeometryAndEncodesKeys(t *testing.T) {
	grid := newUTFGridHitBuffer(query.BBox{MinX: 0, MinY: 0, MaxX: 16, MaxY: 16}, 64, 64)
	grid.paint(&renderer.Geometry{Type: renderer.WKBPoint, Coordinates: [][]float64{{8, 8}}}, 1)
	grid.paint(&renderer.Geometry{Type: renderer.WKBLineString, Coordinates: [][]float64{{0, 0}, {16, 16}}}, 2)
	grid.paint(&renderer.Geometry{Type: renderer.WKBPolygon, Rings: [][][]float64{{{2, 2}, {6, 2}, {6, 6}, {2, 6}, {2, 2}}}}, 3)
	document := grid.document([]string{"", "point", "line", "polygon"}, map[string]map[string]any{"point": {"name": "Point"}})
	if len(document.Grid) != 16 || len([]rune(document.Grid[0])) != 16 {
		t.Fatalf("grid dimensions = %dx%d", len([]rune(document.Grid[0])), len(document.Grid))
	}
	seen := map[rune]bool{}
	for _, row := range document.Grid {
		for _, value := range row {
			seen[value] = true
		}
	}
	for index := 0; index <= 3; index++ {
		if !seen[utfGridRune(index)] {
			t.Fatalf("UTFGrid index %d was not painted", index)
		}
	}
	body, err := json.Marshal(document)
	if err != nil || !json.Valid(body) {
		t.Fatalf("UTFGrid JSON: %v", err)
	}
}

func TestUTFGridRuneSkipsJSONEscapes(t *testing.T) {
	for index := 0; index < 128; index++ {
		value := utfGridRune(index)
		if value == '"' || value == '\\' {
			t.Fatalf("index %d encoded reserved rune %q", index, value)
		}
	}
}

func TestGetMapUTFGridReturnsStableIDsAndProperties(t *testing.T) {
	source := &utfGridDataSource{feature: datasource.RenderFeature{ID: "7", Geometry: utfGridPointWKB(8, 8), Properties: map[string]any{"name": "Main"}}}
	layer := &workspace.Layer{ID: "layer", PublicID: "roads", SourceLayer: "roads", Enabled: true, Public: true, CRSDefault: 4326}
	service := &workspace.Service{ID: "service", Enabled: true, DataSource: source, Layers: map[string]*workspace.Layer{"roads": layer}}
	ws := &workspace.Workspace{
		ID: "workspace", Name: "demo", Services: map[string]*workspace.Service{"service": service}, Styles: map[string]*workspace.Style{},
		Settings: &store.WorkspaceSettings{WMS: store.WMSSettings{Enabled: true, Public: true, MaxWidth: 1024, MaxHeight: 1024, MaxPixels: 1024 * 1024}},
	}
	h := &workspaceHandler{
		cfg:         conf.Config{WMS: conf.WMS{MaxWidth: 1024, MaxHeight: 1024, MaxPixels: 1024 * 1024, MaxRenderFeatures: 10, MaxRenderVertices: 100}},
		renderSlots: make(chan struct{}, 1), styleCache: map[string]cachedStyle{},
	}
	request := httptest.NewRequest(http.MethodGet, "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=roads&STYLES=&CRS=CRS:84&BBOX=0,0,16,16&WIDTH=64&HEIGHT=64&FORMAT=application/json%3Btype%3Dutfgrid", nil)
	recorder := httptest.NewRecorder()
	h.handleGetMap(recorder, request, ws)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != FormatUTFGrid || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("UTFGrid response: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	var document utfGridDocument
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Grid) != 16 || len(document.Keys) != 2 || document.Keys[1] != "roads.7" || document.Data["roads.7"]["name"] != "Main" {
		t.Fatalf("UTFGrid document = %+v", document)
	}
}
