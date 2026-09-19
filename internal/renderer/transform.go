// Package renderer provides map rendering functionality for WMS.
package renderer

import (
	"math"

	"github.com/tobilg/neoserver/internal/query"
)

// Transform handles coordinate transformations between map and pixel coordinates.
type Transform struct {
	bbox   query.BBox
	width  int
	height int
	scaleX float64
	scaleY float64
}

// NewTransform creates a new coordinate transformer.
func NewTransform(bbox query.BBox, width, height int) *Transform {
	bboxWidth := bbox.MaxX - bbox.MinX
	bboxHeight := bbox.MaxY - bbox.MinY

	// Prevent division by zero
	if bboxWidth == 0 {
		bboxWidth = 1
	}
	if bboxHeight == 0 {
		bboxHeight = 1
	}

	return &Transform{
		bbox:   bbox,
		width:  width,
		height: height,
		scaleX: float64(width) / bboxWidth,
		scaleY: float64(height) / bboxHeight,
	}
}

// ToPixel converts map coordinates to pixel coordinates.
// Note: Y is inverted because pixel coordinates have origin at top-left.
func (t *Transform) ToPixel(x, y float64) (float64, float64) {
	px := (x - t.bbox.MinX) * t.scaleX
	py := float64(t.height) - (y-t.bbox.MinY)*t.scaleY
	return px, py
}

// ToMap converts pixel coordinates to map coordinates.
func (t *Transform) ToMap(px, py float64) (float64, float64) {
	x := px/t.scaleX + t.bbox.MinX
	y := (float64(t.height)-py)/t.scaleY + t.bbox.MinY
	return x, y
}

// PixelSize returns the size of a pixel in map units.
func (t *Transform) PixelSize() float64 {
	// Use the average of X and Y scales
	return (1.0/t.scaleX + 1.0/t.scaleY) / 2.0
}

// ScaleDenominator calculates the map scale denominator.
// Assumes the CRS is in meters (e.g., EPSG:3857).
// For geographic CRS (EPSG:4326), this is an approximation.
func (t *Transform) ScaleDenominator() float64 {
	// Standard pixel size is 0.28mm (OGC standard)
	standardPixelSize := 0.00028 // in meters

	// Calculate ground distance per pixel
	pixelSizeInMapUnits := t.PixelSize()

	// For geographic coordinates, convert degrees to approximate meters
	// This is a rough approximation at mid-latitudes
	if t.bbox.MinX >= -180 && t.bbox.MaxX <= 180 && t.bbox.MinY >= -90 && t.bbox.MaxY <= 90 {
		// Approximate meters per degree at the center latitude
		centerLat := (t.bbox.MinY + t.bbox.MaxY) / 2.0
		metersPerDegree := 111320.0 * cosDeg(centerLat) // approximate
		pixelSizeInMapUnits *= metersPerDegree
	}

	return pixelSizeInMapUnits / standardPixelSize
}

// cosDeg returns the cosine of an angle in degrees.
func cosDeg(degrees float64) float64 {
	return math.Cos(degrees * math.Pi / 180.0)
}

// BBox returns the bounding box used for this transform.
func (t *Transform) BBox() query.BBox {
	return t.bbox
}

// Width returns the image width in pixels.
func (t *Transform) Width() int {
	return t.width
}

// Height returns the image height in pixels.
func (t *Transform) Height() int {
	return t.height
}

// Contains checks if a point is within the transform's bounding box.
func (t *Transform) Contains(x, y float64) bool {
	return x >= t.bbox.MinX && x <= t.bbox.MaxX &&
		y >= t.bbox.MinY && y <= t.bbox.MaxY
}

// Intersects checks if a bounding box intersects with the transform's bounding box.
func (t *Transform) Intersects(minX, minY, maxX, maxY float64) bool {
	return !(maxX < t.bbox.MinX || minX > t.bbox.MaxX ||
		maxY < t.bbox.MinY || minY > t.bbox.MaxY)
}
