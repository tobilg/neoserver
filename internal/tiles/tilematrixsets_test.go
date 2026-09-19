package tiles

import (
	"math"
	"strings"
	"testing"
)

const bboxEpsilon = 1e-6

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= bboxEpsilon
}

func TestGetTileMatrixSetDefinition_WebMercatorQuad(t *testing.T) {
	def, err := GetTileMatrixSetDefinition(TMSWebMercatorQuad)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.ID != TMSWebMercatorQuad || def.CRS != CRS3857URI || def.URI != TMSWebMercatorQuadURI {
		t.Errorf("unexpected identity fields: %+v", def)
	}
	if len(def.TileMatrices) != 25 {
		t.Fatalf("expected 25 zoom levels, got %d", len(def.TileMatrices))
	}
	z0 := def.TileMatrices[0]
	if z0.MatrixWidth != 1 || z0.MatrixHeight != 1 {
		t.Errorf("z0 matrix should be 1x1, got %dx%d", z0.MatrixWidth, z0.MatrixHeight)
	}
	if !almostEqual(z0.ScaleDenominator, WebMercatorScaleDenom0) {
		t.Errorf("z0 scale = %v, want %v", z0.ScaleDenominator, WebMercatorScaleDenom0)
	}
	// Scale and cell size halve, matrix doubles per zoom level.
	for z := 1; z < 25; z++ {
		prev, cur := def.TileMatrices[z-1], def.TileMatrices[z]
		if !almostEqual(cur.ScaleDenominator*2, prev.ScaleDenominator) {
			t.Errorf("z%d scale %v is not half of z%d scale %v", z, cur.ScaleDenominator, z-1, prev.ScaleDenominator)
		}
		if !almostEqual(cur.CellSize*2, prev.CellSize) {
			t.Errorf("z%d cell size %v is not half of z%d cell size %v", z, cur.CellSize, z-1, prev.CellSize)
		}
		if cur.MatrixWidth != prev.MatrixWidth*2 || cur.MatrixHeight != prev.MatrixHeight*2 {
			t.Errorf("z%d matrix %dx%d should double z%d matrix %dx%d", z, cur.MatrixWidth, cur.MatrixHeight, z-1, prev.MatrixWidth, prev.MatrixHeight)
		}
	}
}

func TestGetTileMatrixSetDefinition_WorldCRS84Quad(t *testing.T) {
	def, err := GetTileMatrixSetDefinition(TMSWorldCRS84Quad)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.CRS != CRS4326URI {
		t.Errorf("CRS = %q, want %q", def.CRS, CRS4326URI)
	}
	if len(def.TileMatrices) != 25 {
		t.Fatalf("expected 25 zoom levels, got %d", len(def.TileMatrices))
	}
	z0 := def.TileMatrices[0]
	if z0.MatrixWidth != 2 || z0.MatrixHeight != 1 {
		t.Errorf("CRS84 z0 matrix should be 2x1, got %dx%d", z0.MatrixWidth, z0.MatrixHeight)
	}
	z1 := def.TileMatrices[1]
	if z1.MatrixWidth != 4 || z1.MatrixHeight != 2 {
		t.Errorf("CRS84 z1 matrix should be 4x2, got %dx%d", z1.MatrixWidth, z1.MatrixHeight)
	}
}

func TestGetTileMatrixSetDefinition_Unknown(t *testing.T) {
	if _, err := GetTileMatrixSetDefinition("NoSuchTMS"); err == nil {
		t.Fatal("expected error for unknown tile matrix set")
	}
}

func TestGetSupportedTileMatrixSets(t *testing.T) {
	items := GetSupportedTileMatrixSets()
	if len(items) != 2 {
		t.Fatalf("expected 2 tile matrix sets, got %d", len(items))
	}
	byID := map[string]TileMatrixSetItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	if byID[TMSWebMercatorQuad].URI != TMSWebMercatorQuadURI || byID[TMSWebMercatorQuad].CRS != CRS3857URI {
		t.Errorf("unexpected WebMercatorQuad item: %+v", byID[TMSWebMercatorQuad])
	}
	if byID[TMSWorldCRS84Quad].URI != TMSWorldCRS84QuadURI || byID[TMSWorldCRS84Quad].CRS != CRS4326URI {
		t.Errorf("unexpected WorldCRS84Quad item: %+v", byID[TMSWorldCRS84Quad])
	}
}

func TestTileBBox(t *testing.T) {
	tests := []struct {
		name    string
		tms     string
		z, x, y int
		want    TileBounds
	}{
		{
			name: "webmercator z0 full extent",
			tms:  TMSWebMercatorQuad, z: 0, x: 0, y: 0,
			want: TileBounds{MinX: WebMercatorOriginX, MinY: -WebMercatorOriginY, MaxX: -WebMercatorOriginX, MaxY: WebMercatorOriginY},
		},
		{
			name: "webmercator z1 top-left quadrant",
			tms:  TMSWebMercatorQuad, z: 1, x: 0, y: 0,
			want: TileBounds{MinX: WebMercatorOriginX, MinY: 0, MaxX: 0, MaxY: WebMercatorOriginY},
		},
		{
			name: "webmercator z1 bottom-right quadrant",
			tms:  TMSWebMercatorQuad, z: 1, x: 1, y: 1,
			want: TileBounds{MinX: 0, MinY: -WebMercatorOriginY, MaxX: -WebMercatorOriginX, MaxY: 0},
		},
		{
			name: "crs84 z0 left tile is western hemisphere",
			tms:  TMSWorldCRS84Quad, z: 0, x: 0, y: 0,
			want: TileBounds{MinX: -180, MinY: -90, MaxX: 0, MaxY: 90},
		},
		{
			name: "crs84 z0 right tile is eastern hemisphere",
			tms:  TMSWorldCRS84Quad, z: 0, x: 1, y: 0,
			want: TileBounds{MinX: 0, MinY: -90, MaxX: 180, MaxY: 90},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TileBBox(tt.tms, tt.z, tt.x, tt.y)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !almostEqual(got.MinX, tt.want.MinX) || !almostEqual(got.MinY, tt.want.MinY) ||
				!almostEqual(got.MaxX, tt.want.MaxX) || !almostEqual(got.MaxY, tt.want.MaxY) {
				t.Errorf("bbox = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTileBBox_UnknownTMS(t *testing.T) {
	if _, err := TileBBox("NoSuchTMS", 0, 0, 0); err == nil {
		t.Fatal("expected error for unknown TMS")
	}
	if _, err := TileBBoxWGS84("NoSuchTMS", 0, 0, 0); err == nil {
		t.Fatal("expected error for unknown TMS")
	}
}

func TestTileBBoxWGS84_WebMercator(t *testing.T) {
	// z0 covers the full Web Mercator world in WGS84.
	b, err := TileBBoxWGS84(TMSWebMercatorQuad, 0, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(b.MinX, -180) || !almostEqual(b.MaxX, 180) {
		t.Errorf("longitude range = [%v, %v], want [-180, 180]", b.MinX, b.MaxX)
	}
	// Web Mercator latitude clamp is ±85.0511287798066.
	if math.Abs(b.MaxY-85.0511287798066) > 1e-9 || math.Abs(b.MinY+85.0511287798066) > 1e-9 {
		t.Errorf("latitude range = [%v, %v], want ±85.0511287798066", b.MinY, b.MaxY)
	}

	// z1 x0 y0 is the north-western quadrant: lon [-180, 0], lat [0, 85.05...].
	b, err = TileBBoxWGS84(TMSWebMercatorQuad, 1, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(b.MinX, -180) || !almostEqual(b.MaxX, 0) {
		t.Errorf("z1 lon range = [%v, %v], want [-180, 0]", b.MinX, b.MaxX)
	}
	if !almostEqual(b.MinY, 0) || b.MaxY < 85 {
		t.Errorf("z1 lat range = [%v, %v], want [0, ~85.05]", b.MinY, b.MaxY)
	}
}

func TestTileBBoxWGS84_CRS84IsNative(t *testing.T) {
	native, _ := TileBBox(TMSWorldCRS84Quad, 2, 3, 1)
	wgs, err := TileBBoxWGS84(TMSWorldCRS84Quad, 2, 3, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *native != *wgs {
		t.Errorf("CRS84 WGS84 bbox %+v differs from native bbox %+v", wgs, native)
	}
}

func TestValidateTileCoords(t *testing.T) {
	tests := []struct {
		name    string
		tms     string
		z, x, y int
		wantErr string
	}{
		{"valid webmercator z0", TMSWebMercatorQuad, 0, 0, 0, ""},
		{"valid webmercator boundary", TMSWebMercatorQuad, 3, 7, 7, ""},
		{"zoom negative", TMSWebMercatorQuad, -1, 0, 0, "zoom level"},
		{"zoom too high", TMSWebMercatorQuad, 25, 0, 0, "zoom level"},
		{"x negative", TMSWebMercatorQuad, 2, -1, 0, "x coordinate"},
		{"x out of range", TMSWebMercatorQuad, 2, 4, 0, "x coordinate"},
		{"y negative", TMSWebMercatorQuad, 2, 0, -1, "y coordinate"},
		{"y out of range", TMSWebMercatorQuad, 2, 0, 4, "y coordinate"},
		{"crs84 valid wide x", TMSWorldCRS84Quad, 1, 3, 1, ""},
		{"crs84 x out of range", TMSWorldCRS84Quad, 1, 4, 0, "x coordinate"},
		{"crs84 y out of range", TMSWorldCRS84Quad, 1, 0, 2, "y coordinate"},
		{"unknown tms", "NoSuchTMS", 0, 0, 0, "unknown tile matrix set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTileCoords(tt.tms, tt.z, tt.x, tt.y)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestGetTMSSRID(t *testing.T) {
	if srid := GetTMSSRID(TMSWebMercatorQuad); srid != 3857 {
		t.Errorf("WebMercatorQuad SRID = %d, want 3857", srid)
	}
	if srid := GetTMSSRID(TMSWorldCRS84Quad); srid != 4326 {
		t.Errorf("WorldCRS84Quad SRID = %d, want 4326", srid)
	}
	if srid := GetTMSSRID("NoSuchTMS"); srid != 4326 {
		t.Errorf("unknown TMS SRID = %d, want default 4326", srid)
	}
}
