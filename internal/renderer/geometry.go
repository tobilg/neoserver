package renderer

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// GeometryType represents the WKB geometry type.
type GeometryType uint32

const (
	WKBPoint              GeometryType = 1
	WKBLineString         GeometryType = 2
	WKBPolygon            GeometryType = 3
	WKBMultiPoint         GeometryType = 4
	WKBMultiLineString    GeometryType = 5
	WKBMultiPolygon       GeometryType = 6
	WKBGeometryCollection GeometryType = 7
)

// Geometry represents a parsed geometry.
type Geometry struct {
	Type        GeometryType
	Coordinates [][]float64   // For Point, LineString
	Rings       [][][]float64 // For Polygon
	Geometries  []Geometry    // For Multi* and GeometryCollection
}

// ParseWKB parses a Well-Known Binary geometry.
func ParseWKB(data []byte) (*Geometry, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("WKB data too short")
	}

	r := &wkbReader{data: data, pos: 0}
	return r.readGeometry()
}

type wkbReader struct {
	data      []byte
	pos       int
	byteOrder binary.ByteOrder
}

func (r *wkbReader) readGeometry() (*Geometry, error) {
	// Read byte order
	if r.pos >= len(r.data) {
		return nil, io.EOF
	}
	bo := r.data[r.pos]
	r.pos++

	if bo == 0 {
		r.byteOrder = binary.BigEndian
	} else {
		r.byteOrder = binary.LittleEndian
	}

	// Read geometry type
	if r.pos+4 > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	geomType := GeometryType(r.byteOrder.Uint32(r.data[r.pos:]))
	r.pos += 4

	// Handle SRID embedded in type (EWKB)
	hasSRID := (geomType & 0x20000000) != 0
	hasZ := (geomType & 0x80000000) != 0
	hasM := (geomType & 0x40000000) != 0
	geomType &= 0x0FFFFFFF // Clear flags

	// Skip SRID if present
	if hasSRID {
		if r.pos+4 > len(r.data) {
			return nil, io.ErrUnexpectedEOF
		}
		r.pos += 4
	}

	dims := 2
	if hasZ {
		dims++
	}
	if hasM {
		dims++
	}

	switch geomType {
	case WKBPoint:
		return r.readPoint(dims)
	case WKBLineString:
		return r.readLineString(dims)
	case WKBPolygon:
		return r.readPolygon(dims)
	case WKBMultiPoint:
		return r.readMultiPoint()
	case WKBMultiLineString:
		return r.readMultiLineString()
	case WKBMultiPolygon:
		return r.readMultiPolygon()
	case WKBGeometryCollection:
		return r.readGeometryCollection()
	default:
		return nil, fmt.Errorf("unsupported geometry type: %d", geomType)
	}
}

func (r *wkbReader) readPoint(dims int) (*Geometry, error) {
	coords, err := r.readCoordinates(1, dims)
	if err != nil {
		return nil, err
	}
	return &Geometry{
		Type:        WKBPoint,
		Coordinates: coords,
	}, nil
}

func (r *wkbReader) readLineString(dims int) (*Geometry, error) {
	numPoints, err := r.readUint32()
	if err != nil {
		return nil, err
	}
	coords, err := r.readCoordinates(int(numPoints), dims)
	if err != nil {
		return nil, err
	}
	return &Geometry{
		Type:        WKBLineString,
		Coordinates: coords,
	}, nil
}

func (r *wkbReader) readPolygon(dims int) (*Geometry, error) {
	numRings, err := r.readUint32()
	if err != nil {
		return nil, err
	}

	rings := make([][][]float64, numRings)
	for i := uint32(0); i < numRings; i++ {
		numPoints, err := r.readUint32()
		if err != nil {
			return nil, err
		}
		coords, err := r.readCoordinates(int(numPoints), dims)
		if err != nil {
			return nil, err
		}
		rings[i] = coords
	}

	return &Geometry{
		Type:  WKBPolygon,
		Rings: rings,
	}, nil
}

func (r *wkbReader) readMultiPoint() (*Geometry, error) {
	numGeoms, err := r.readUint32()
	if err != nil {
		return nil, err
	}

	geoms := make([]Geometry, numGeoms)
	for i := uint32(0); i < numGeoms; i++ {
		g, err := r.readGeometry()
		if err != nil {
			return nil, err
		}
		geoms[i] = *g
	}

	return &Geometry{
		Type:       WKBMultiPoint,
		Geometries: geoms,
	}, nil
}

func (r *wkbReader) readMultiLineString() (*Geometry, error) {
	numGeoms, err := r.readUint32()
	if err != nil {
		return nil, err
	}

	geoms := make([]Geometry, numGeoms)
	for i := uint32(0); i < numGeoms; i++ {
		g, err := r.readGeometry()
		if err != nil {
			return nil, err
		}
		geoms[i] = *g
	}

	return &Geometry{
		Type:       WKBMultiLineString,
		Geometries: geoms,
	}, nil
}

func (r *wkbReader) readMultiPolygon() (*Geometry, error) {
	numGeoms, err := r.readUint32()
	if err != nil {
		return nil, err
	}

	geoms := make([]Geometry, numGeoms)
	for i := uint32(0); i < numGeoms; i++ {
		g, err := r.readGeometry()
		if err != nil {
			return nil, err
		}
		geoms[i] = *g
	}

	return &Geometry{
		Type:       WKBMultiPolygon,
		Geometries: geoms,
	}, nil
}

func (r *wkbReader) readGeometryCollection() (*Geometry, error) {
	numGeoms, err := r.readUint32()
	if err != nil {
		return nil, err
	}

	geoms := make([]Geometry, numGeoms)
	for i := uint32(0); i < numGeoms; i++ {
		g, err := r.readGeometry()
		if err != nil {
			return nil, err
		}
		geoms[i] = *g
	}

	return &Geometry{
		Type:       WKBGeometryCollection,
		Geometries: geoms,
	}, nil
}

func (r *wkbReader) readCoordinates(numPoints, dims int) ([][]float64, error) {
	bytesNeeded := numPoints * dims * 8
	if r.pos+bytesNeeded > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}

	coords := make([][]float64, numPoints)
	for i := 0; i < numPoints; i++ {
		coord := make([]float64, 2) // Only keep X, Y
		for d := 0; d < dims; d++ {
			val := math.Float64frombits(r.byteOrder.Uint64(r.data[r.pos:]))
			r.pos += 8
			if d < 2 { // Only store X and Y
				coord[d] = val
			}
		}
		coords[i] = coord
	}

	return coords, nil
}

func (r *wkbReader) readUint32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := r.byteOrder.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

// GetBounds calculates the bounding box of a geometry.
func (g *Geometry) GetBounds() (minX, minY, maxX, maxY float64) {
	minX, minY = math.MaxFloat64, math.MaxFloat64
	maxX, maxY = -math.MaxFloat64, -math.MaxFloat64

	g.visitCoordinates(func(x, y float64) {
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	})

	return
}

// visitCoordinates visits all coordinates in the geometry.
func (g *Geometry) visitCoordinates(fn func(x, y float64)) {
	switch g.Type {
	case WKBPoint, WKBLineString:
		for _, coord := range g.Coordinates {
			if len(coord) >= 2 {
				fn(coord[0], coord[1])
			}
		}
	case WKBPolygon:
		for _, ring := range g.Rings {
			for _, coord := range ring {
				if len(coord) >= 2 {
					fn(coord[0], coord[1])
				}
			}
		}
	case WKBMultiPoint, WKBMultiLineString, WKBMultiPolygon, WKBGeometryCollection:
		for i := range g.Geometries {
			g.Geometries[i].visitCoordinates(fn)
		}
	}
}

// TypeName returns the name of the geometry type.
func (g *Geometry) TypeName() string {
	switch g.Type {
	case WKBPoint:
		return "Point"
	case WKBLineString:
		return "LineString"
	case WKBPolygon:
		return "Polygon"
	case WKBMultiPoint:
		return "MultiPoint"
	case WKBMultiLineString:
		return "MultiLineString"
	case WKBMultiPolygon:
		return "MultiPolygon"
	case WKBGeometryCollection:
		return "GeometryCollection"
	default:
		return "Unknown"
	}
}

// VertexCount returns the total number of coordinate tuples in the geometry.
func (g *Geometry) VertexCount() int {
	count := 0
	g.visitCoordinates(func(_, _ float64) { count++ })
	return count
}
