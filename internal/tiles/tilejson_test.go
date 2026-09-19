package tiles

import (
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

func vectorOpts() TileJSONOptions {
	return TileJSONOptions{
		BaseURL:       "http://localhost:9000/",
		WorkspaceName: "demo",
		CollectionID:  "roads",
		TileMatrixSet: TMSWebMercatorQuad,
		DataType:      DataTypeVector,
		MinZoom:       2,
		MaxZoom:       18,
		Format:        "mvt",
	}
}

func TestGenerateTileJSON_WithExtent(t *testing.T) {
	layer := &workspace.Layer{PublicID: "roads", Title: "Roads", Description: "road net"}
	info := testLayerInfo("roads")
	info.Extent = &datasource.Extent{MinX: 10, MinY: 20, MaxX: 30, MaxY: 40}

	tj := GenerateTileJSON(layer, info, vectorOpts())

	if tj.TileJSON != "3.0.0" || tj.Scheme != "xyz" {
		t.Errorf("unexpected header fields: %+v", tj)
	}
	if tj.Name != "Roads" || tj.Description != "road net" {
		t.Errorf("layer metadata not mapped: %+v", tj)
	}
	wantBounds := []float64{10, 20, 30, 40}
	for i, v := range wantBounds {
		if tj.Bounds[i] != v {
			t.Fatalf("bounds = %v, want %v", tj.Bounds, wantBounds)
		}
	}
	// Center is the extent midpoint with minzoom.
	if tj.Center[0] != 20 || tj.Center[1] != 30 || tj.Center[2] != 2 {
		t.Errorf("center = %v, want [20 30 2]", tj.Center)
	}
	if tj.MinZoom != 2 || tj.MaxZoom != 18 {
		t.Errorf("zoom range = [%d, %d], want [2, 18]", tj.MinZoom, tj.MaxZoom)
	}

	if len(tj.VectorLayers) != 1 {
		t.Fatalf("expected one vector layer, got %+v", tj.VectorLayers)
	}
	vl := tj.VectorLayers[0]
	if vl.ID != "roads" {
		t.Errorf("vector layer ID = %q, want roads", vl.ID)
	}
	if vl.Fields["id"] != "Number" || vl.Fields["name"] != "String" {
		t.Errorf("field types = %v, want id:Number name:String", vl.Fields)
	}
}

func TestGenerateTileJSON_WorldDefaultBounds(t *testing.T) {
	layer := &workspace.Layer{PublicID: "roads", Title: "Roads"}
	tj := GenerateTileJSON(layer, nil, vectorOpts())

	want := []float64{-180, -85.051129, 180, 85.051129}
	for i, v := range want {
		if tj.Bounds[i] != v {
			t.Fatalf("bounds = %v, want world default %v", tj.Bounds, want)
		}
	}
	if tj.Center[0] != 0 || tj.Center[1] != 0 {
		t.Errorf("center = %v, want origin", tj.Center)
	}
	// No layer info → no vector layer entry.
	if len(tj.VectorLayers) != 0 {
		t.Errorf("expected no vector layers without layer info, got %+v", tj.VectorLayers)
	}
}

func TestGenerateTileJSON_MapDataTypeHasNoVectorLayers(t *testing.T) {
	layer := &workspace.Layer{PublicID: "roads", Title: "Roads"}
	opts := vectorOpts()
	opts.DataType = DataTypeMap
	opts.Format = "png"

	tj := GenerateTileJSON(layer, testLayerInfo("roads"), opts)
	if len(tj.VectorLayers) != 0 {
		t.Errorf("map tilejson must not contain vector_layers, got %+v", tj.VectorLayers)
	}
	if len(tj.Tiles) != 1 {
		t.Fatalf("expected one tile URL, got %v", tj.Tiles)
	}
}

func TestBuildTileURL(t *testing.T) {
	tests := []struct {
		name string
		opts TileJSONOptions
		want string
	}{
		{
			name: "vector",
			opts: TileJSONOptions{BaseURL: "http://host/", WorkspaceName: "demo", CollectionID: "roads", TileMatrixSet: TMSWebMercatorQuad, DataType: DataTypeVector},
			want: "http://host/workspaces/demo/ogc-tiles/collections/roads/tiles/WebMercatorQuad/{z}/{y}/{x}",
		},
		{
			name: "map with format",
			opts: TileJSONOptions{BaseURL: "http://host", WorkspaceName: "demo", CollectionID: "roads", TileMatrixSet: TMSWebMercatorQuad, DataType: DataTypeMap, Format: "webp"},
			want: "http://host/workspaces/demo/ogc-tiles/collections/roads/map/tiles/WebMercatorQuad/{z}/{y}/{x}?f=webp",
		},
		{
			name: "map defaults to png",
			opts: TileJSONOptions{BaseURL: "http://host", WorkspaceName: "demo", CollectionID: "roads", TileMatrixSet: TMSWebMercatorQuad, DataType: DataTypeMap},
			want: "http://host/workspaces/demo/ogc-tiles/collections/roads/map/tiles/WebMercatorQuad/{z}/{y}/{x}?f=png",
		},
		{
			name: "escapes every identifier segment",
			opts: TileJSONOptions{BaseURL: "http://host", WorkspaceName: "a/b", CollectionID: "café?#", TileMatrixSet: "custom%set", DataType: DataTypeVector},
			want: "http://host/workspaces/a%2Fb/ogc-tiles/collections/caf%C3%A9%3F%23/tiles/custom%25set/{z}/{y}/{x}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildTileURL(tt.opts); got != tt.want {
				t.Errorf("buildTileURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapJSONTypeToTileJSON(t *testing.T) {
	tests := []struct {
		in   datasource.JSONType
		want string
	}{
		{datasource.JSONTypeBoolean, "Boolean"},
		{datasource.JSONTypeInteger, "Number"},
		{datasource.JSONTypeNumber, "Number"},
		{datasource.JSONTypeString, "String"},
		{datasource.JSONTypeObject, "String"},
		{datasource.JSONType("bogus"), "String"},
	}
	for _, tt := range tests {
		if got := mapJSONTypeToTileJSON(tt.in); got != tt.want {
			t.Errorf("mapJSONTypeToTileJSON(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTileJSONForWorkspace_FiltersLayers(t *testing.T) {
	ws := newTestWorkspace()
	addTestLayer(ws, "open", nil)
	secret := addTestLayer(ws, "secret", nil)
	secret.AllowedRoles = []string{"admin"}
	disabled := addTestLayer(ws, "disabled", nil)
	disabled.Enabled = false

	opts := vectorOpts()
	opts.Role = "" // anonymous

	tj := TileJSONForWorkspace(ws, opts)
	if len(tj.VectorLayers) != 1 || tj.VectorLayers[0].ID != "open" {
		t.Fatalf("anonymous caller must only see the open layer, got %+v", tj.VectorLayers)
	}

	// An admin sees the restricted layer too.
	opts.Role = "admin"
	tj = TileJSONForWorkspace(ws, opts)
	ids := map[string]bool{}
	for _, vl := range tj.VectorLayers {
		ids[vl.ID] = true
	}
	if !ids["open"] || !ids["secret"] || len(ids) != 2 {
		t.Fatalf("admin must see open and secret layers, got %v", ids)
	}

	// Map data type produces no vector layers.
	opts.DataType = DataTypeMap
	tj = TileJSONForWorkspace(ws, opts)
	if len(tj.VectorLayers) != 0 {
		t.Errorf("map tilejson must not list vector layers, got %+v", tj.VectorLayers)
	}
}
