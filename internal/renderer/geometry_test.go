package renderer

import (
	"encoding/binary"
	"math"
	"testing"
)

// Helper to create WKB point
func makeWKBPoint(x, y float64) []byte {
	buf := make([]byte, 21)
	buf[0] = 1 // Little endian
	binary.LittleEndian.PutUint32(buf[1:], uint32(WKBPoint))
	binary.LittleEndian.PutUint64(buf[5:], math.Float64bits(x))
	binary.LittleEndian.PutUint64(buf[13:], math.Float64bits(y))
	return buf
}

// Helper to create WKB linestring
func makeWKBLineString(coords [][]float64) []byte {
	size := 1 + 4 + 4 + len(coords)*16
	buf := make([]byte, size)
	buf[0] = 1 // Little endian
	binary.LittleEndian.PutUint32(buf[1:], uint32(WKBLineString))
	binary.LittleEndian.PutUint32(buf[5:], uint32(len(coords)))
	offset := 9
	for _, c := range coords {
		binary.LittleEndian.PutUint64(buf[offset:], math.Float64bits(c[0]))
		binary.LittleEndian.PutUint64(buf[offset+8:], math.Float64bits(c[1]))
		offset += 16
	}
	return buf
}

// Helper to create WKB polygon
func makeWKBPolygon(rings [][][]float64) []byte {
	size := 1 + 4 + 4 // header + type + numRings
	for _, ring := range rings {
		size += 4 + len(ring)*16 // numPoints + coords
	}
	buf := make([]byte, size)
	buf[0] = 1 // Little endian
	binary.LittleEndian.PutUint32(buf[1:], uint32(WKBPolygon))
	binary.LittleEndian.PutUint32(buf[5:], uint32(len(rings)))
	offset := 9
	for _, ring := range rings {
		binary.LittleEndian.PutUint32(buf[offset:], uint32(len(ring)))
		offset += 4
		for _, c := range ring {
			binary.LittleEndian.PutUint64(buf[offset:], math.Float64bits(c[0]))
			binary.LittleEndian.PutUint64(buf[offset+8:], math.Float64bits(c[1]))
			offset += 16
		}
	}
	return buf
}

func TestParseWKB_Point(t *testing.T) {
	wkb := makeWKBPoint(10.5, 20.5)

	geom, err := ParseWKB(wkb)
	if err != nil {
		t.Fatalf("ParseWKB failed: %v", err)
	}

	if geom.Type != WKBPoint {
		t.Errorf("expected type Point, got %v", geom.Type)
	}
	if len(geom.Coordinates) != 1 {
		t.Errorf("expected 1 coordinate, got %d", len(geom.Coordinates))
	}
	if geom.Coordinates[0][0] != 10.5 || geom.Coordinates[0][1] != 20.5 {
		t.Errorf("coordinates mismatch: %v", geom.Coordinates)
	}
}

func TestParseWKB_LineString(t *testing.T) {
	coords := [][]float64{{0, 0}, {10, 10}, {20, 0}}
	wkb := makeWKBLineString(coords)

	geom, err := ParseWKB(wkb)
	if err != nil {
		t.Fatalf("ParseWKB failed: %v", err)
	}

	if geom.Type != WKBLineString {
		t.Errorf("expected type LineString, got %v", geom.Type)
	}
	if len(geom.Coordinates) != 3 {
		t.Errorf("expected 3 coordinates, got %d", len(geom.Coordinates))
	}
}

func TestParseWKB_Polygon(t *testing.T) {
	rings := [][][]float64{
		{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}},
	}
	wkb := makeWKBPolygon(rings)

	geom, err := ParseWKB(wkb)
	if err != nil {
		t.Fatalf("ParseWKB failed: %v", err)
	}

	if geom.Type != WKBPolygon {
		t.Errorf("expected type Polygon, got %v", geom.Type)
	}
	if len(geom.Rings) != 1 {
		t.Errorf("expected 1 ring, got %d", len(geom.Rings))
	}
	if len(geom.Rings[0]) != 5 {
		t.Errorf("expected 5 points in ring, got %d", len(geom.Rings[0]))
	}
}

func TestParseWKB_TooShort(t *testing.T) {
	_, err := ParseWKB([]byte{1, 2, 3})
	if err == nil {
		t.Error("expected error for short data")
	}
}

func TestParseWKB_Empty(t *testing.T) {
	_, err := ParseWKB([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestGeometry_TypeName(t *testing.T) {
	tests := []struct {
		geomType GeometryType
		want     string
	}{
		{WKBPoint, "Point"},
		{WKBLineString, "LineString"},
		{WKBPolygon, "Polygon"},
		{WKBMultiPoint, "MultiPoint"},
		{WKBMultiLineString, "MultiLineString"},
		{WKBMultiPolygon, "MultiPolygon"},
		{WKBGeometryCollection, "GeometryCollection"},
		{GeometryType(99), "Unknown"},
	}

	for _, tt := range tests {
		geom := &Geometry{Type: tt.geomType}
		got := geom.TypeName()
		if got != tt.want {
			t.Errorf("TypeName() for type %d = %q, want %q", tt.geomType, got, tt.want)
		}
	}
}

func TestGeometry_GetBounds_Point(t *testing.T) {
	geom := &Geometry{
		Type:        WKBPoint,
		Coordinates: [][]float64{{10, 20}},
	}

	minX, minY, maxX, maxY := geom.GetBounds()

	if minX != 10 || maxX != 10 {
		t.Errorf("X bounds: got [%v, %v], want [10, 10]", minX, maxX)
	}
	if minY != 20 || maxY != 20 {
		t.Errorf("Y bounds: got [%v, %v], want [20, 20]", minY, maxY)
	}
}

func TestGeometry_GetBounds_LineString(t *testing.T) {
	geom := &Geometry{
		Type:        WKBLineString,
		Coordinates: [][]float64{{0, 0}, {10, 20}, {5, 15}},
	}

	minX, minY, maxX, maxY := geom.GetBounds()

	if minX != 0 || maxX != 10 {
		t.Errorf("X bounds: got [%v, %v], want [0, 10]", minX, maxX)
	}
	if minY != 0 || maxY != 20 {
		t.Errorf("Y bounds: got [%v, %v], want [0, 20]", minY, maxY)
	}
}

func TestGeometry_GetBounds_Polygon(t *testing.T) {
	geom := &Geometry{
		Type: WKBPolygon,
		Rings: [][][]float64{
			{{0, 0}, {100, 0}, {100, 50}, {0, 50}, {0, 0}},
		},
	}

	minX, minY, maxX, maxY := geom.GetBounds()

	if minX != 0 || maxX != 100 {
		t.Errorf("X bounds: got [%v, %v], want [0, 100]", minX, maxX)
	}
	if minY != 0 || maxY != 50 {
		t.Errorf("Y bounds: got [%v, %v], want [0, 50]", minY, maxY)
	}
}

func TestGeometry_GetBounds_MultiPoint(t *testing.T) {
	geom := &Geometry{
		Type: WKBMultiPoint,
		Geometries: []Geometry{
			{Type: WKBPoint, Coordinates: [][]float64{{0, 0}}},
			{Type: WKBPoint, Coordinates: [][]float64{{10, 20}}},
			{Type: WKBPoint, Coordinates: [][]float64{{-5, 15}}},
		},
	}

	minX, minY, maxX, maxY := geom.GetBounds()

	if minX != -5 || maxX != 10 {
		t.Errorf("X bounds: got [%v, %v], want [-5, 10]", minX, maxX)
	}
	if minY != 0 || maxY != 20 {
		t.Errorf("Y bounds: got [%v, %v], want [0, 20]", minY, maxY)
	}
}

func TestGeometryType_Constants(t *testing.T) {
	if WKBPoint != 1 {
		t.Errorf("WKBPoint = %d, want 1", WKBPoint)
	}
	if WKBLineString != 2 {
		t.Errorf("WKBLineString = %d, want 2", WKBLineString)
	}
	if WKBPolygon != 3 {
		t.Errorf("WKBPolygon = %d, want 3", WKBPolygon)
	}
	if WKBMultiPoint != 4 {
		t.Errorf("WKBMultiPoint = %d, want 4", WKBMultiPoint)
	}
	if WKBMultiLineString != 5 {
		t.Errorf("WKBMultiLineString = %d, want 5", WKBMultiLineString)
	}
	if WKBMultiPolygon != 6 {
		t.Errorf("WKBMultiPolygon = %d, want 6", WKBMultiPolygon)
	}
	if WKBGeometryCollection != 7 {
		t.Errorf("WKBGeometryCollection = %d, want 7", WKBGeometryCollection)
	}
}

func TestParseWKB_BigEndian(t *testing.T) {
	// Create a big-endian WKB point
	buf := make([]byte, 21)
	buf[0] = 0 // Big endian
	binary.BigEndian.PutUint32(buf[1:], uint32(WKBPoint))
	binary.BigEndian.PutUint64(buf[5:], math.Float64bits(10.5))
	binary.BigEndian.PutUint64(buf[13:], math.Float64bits(20.5))

	geom, err := ParseWKB(buf)
	if err != nil {
		t.Fatalf("ParseWKB failed: %v", err)
	}

	if geom.Type != WKBPoint {
		t.Errorf("expected type Point, got %v", geom.Type)
	}
	if geom.Coordinates[0][0] != 10.5 || geom.Coordinates[0][1] != 20.5 {
		t.Errorf("coordinates mismatch: %v", geom.Coordinates)
	}
}

func TestParseWKB_UnsupportedType(t *testing.T) {
	buf := make([]byte, 9)
	buf[0] = 1 // Little endian
	binary.LittleEndian.PutUint32(buf[1:], 99) // Invalid type

	_, err := ParseWKB(buf)
	if err == nil {
		t.Error("expected error for unsupported geometry type")
	}
}
