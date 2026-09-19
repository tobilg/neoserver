package geojson

import (
	"fmt"
	"testing"
)

// CRS84 coordinate bounds.
const (
	CRS84MinLon = -180.0
	CRS84MaxLon = 180.0
	CRS84MinLat = -90.0
	CRS84MaxLat = 90.0
)

// BBox represents a bounding box.
type BBox struct {
	MinX, MinY, MaxX, MaxY float64
}

// Contains checks if a point is within the bounding box.
func (b BBox) Contains(x, y float64) bool {
	return x >= b.MinX && x <= b.MaxX && y >= b.MinY && y <= b.MaxY
}

// Intersects checks if another bbox intersects with this one.
func (b BBox) Intersects(other BBox) bool {
	return !(other.MaxX < b.MinX || other.MinX > b.MaxX ||
		other.MaxY < b.MinY || other.MinY > b.MaxY)
}

// ValidateFeatureCollection validates a FeatureCollection structure.
func ValidateFeatureCollection(t *testing.T, fc *FeatureCollection) {
	t.Helper()

	if fc.Type != "FeatureCollection" {
		t.Errorf("Expected type 'FeatureCollection', got %q", fc.Type)
	}

	if fc.Features == nil {
		t.Error("Features property is missing")
	}

	// Validate numberReturned if present
	if fc.NumberReturned != nil && *fc.NumberReturned != len(fc.Features) {
		t.Errorf("numberReturned (%d) does not match features count (%d)",
			*fc.NumberReturned, len(fc.Features))
	}
}

// ValidateFeature validates a Feature structure.
func ValidateFeature(t *testing.T, f *Feature) {
	t.Helper()

	if f.Type != "Feature" {
		t.Errorf("Expected type 'Feature', got %q", f.Type)
	}
}

// ValidateGeometryInCRS84 validates that all coordinates are within CRS84 bounds.
func ValidateGeometryInCRS84(t *testing.T, g *Geometry, featureID string) {
	t.Helper()

	if g == nil {
		return // Features without geometry are allowed
	}

	coords, err := ExtractCoordinates(g)
	if err != nil {
		t.Errorf("Feature %s: failed to extract coordinates: %v", featureID, err)
		return
	}

	for _, c := range coords {
		lon, lat := c[0], c[1]
		if lon < CRS84MinLon || lon > CRS84MaxLon {
			t.Errorf("Feature %s: longitude %f is outside CRS84 range [%f, %f]",
				featureID, lon, CRS84MinLon, CRS84MaxLon)
		}
		if lat < CRS84MinLat || lat > CRS84MaxLat {
			t.Errorf("Feature %s: latitude %f is outside CRS84 range [%f, %f]",
				featureID, lat, CRS84MinLat, CRS84MaxLat)
		}
	}
}

// ValidateGeometryInBBox validates that a geometry intersects a bounding box.
func ValidateGeometryInBBox(t *testing.T, g *Geometry, bbox BBox, featureID string) {
	t.Helper()

	if g == nil {
		return // Features without geometry are allowed (they match any bbox)
	}

	minX, minY, maxX, maxY, err := GetBounds(g)
	if err != nil {
		t.Errorf("Feature %s: failed to get geometry bounds: %v", featureID, err)
		return
	}

	geomBBox := BBox{MinX: minX, MinY: minY, MaxX: maxX, MaxY: maxY}
	if !bbox.Intersects(geomBBox) {
		t.Errorf("Feature %s: geometry with bounds [%f,%f,%f,%f] does not intersect bbox [%f,%f,%f,%f]",
			featureID, minX, minY, maxX, maxY, bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY)
	}
}

// ValidateFeaturesInCRS84 validates all features in a collection are in CRS84.
func ValidateFeaturesInCRS84(t *testing.T, fc *FeatureCollection, limit int) {
	t.Helper()

	count := 0
	for _, f := range fc.Features {
		if limit > 0 && count >= limit {
			break
		}
		featureID := GetFeatureID(&f)
		if featureID == "" {
			featureID = fmt.Sprintf("index-%d", count)
		}
		ValidateGeometryInCRS84(t, f.Geometry, featureID)
		count++
	}
}

// ValidateFeaturesInBBox validates all features in a collection intersect a bbox.
func ValidateFeaturesInBBox(t *testing.T, fc *FeatureCollection, bbox BBox, limit int) {
	t.Helper()

	count := 0
	for _, f := range fc.Features {
		if limit > 0 && count >= limit {
			break
		}
		featureID := GetFeatureID(&f)
		if featureID == "" {
			featureID = fmt.Sprintf("index-%d", count)
		}
		ValidateGeometryInBBox(t, f.Geometry, bbox, featureID)
		count++
	}
}
