// Package geojson provides GeoJSON parsing and validation utilities for OGC API tests.
package geojson

import (
	"encoding/json"
	"fmt"
)

// FeatureCollection represents a GeoJSON FeatureCollection.
type FeatureCollection struct {
	Type           string    `json:"type"`
	Features       []Feature `json:"features"`
	NumberMatched  *int      `json:"numberMatched,omitempty"`
	NumberReturned *int      `json:"numberReturned,omitempty"`
	TimeStamp      string    `json:"timeStamp,omitempty"`
	Links          []Link    `json:"links,omitempty"`
}

// Feature represents a GeoJSON Feature.
type Feature struct {
	Type       string         `json:"type"`
	ID         any            `json:"id,omitempty"`
	Geometry   *Geometry      `json:"geometry"`
	Properties map[string]any `json:"properties"`
	Links      []Link         `json:"links,omitempty"`
}

// Geometry represents a GeoJSON geometry.
type Geometry struct {
	Type        string `json:"type"`
	Coordinates any    `json:"coordinates"`
}

// Link represents a link in GeoJSON responses.
type Link struct {
	Href  string `json:"href"`
	Rel   string `json:"rel"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

// ParseFeatureCollection parses a FeatureCollection from JSON bytes.
func ParseFeatureCollection(data []byte) (*FeatureCollection, error) {
	var fc FeatureCollection
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("failed to parse FeatureCollection: %w", err)
	}
	return &fc, nil
}

// ParseFeatureCollectionFromMap parses a FeatureCollection from a map.
func ParseFeatureCollectionFromMap(data map[string]any) (*FeatureCollection, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal map: %w", err)
	}
	return ParseFeatureCollection(jsonBytes)
}

// ParseFeature parses a Feature from JSON bytes.
func ParseFeature(data []byte) (*Feature, error) {
	var f Feature
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("failed to parse Feature: %w", err)
	}
	return &f, nil
}

// ParseFeatureFromMap parses a Feature from a map.
func ParseFeatureFromMap(data map[string]any) (*Feature, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal map: %w", err)
	}
	return ParseFeature(jsonBytes)
}

// GetFeatureID returns the feature ID as a string.
func GetFeatureID(f *Feature) string {
	if f.ID == nil {
		return ""
	}
	return fmt.Sprintf("%v", f.ID)
}

// ExtractCoordinates extracts all coordinates from a geometry.
// Returns a slice of [lon, lat] pairs for validation.
func ExtractCoordinates(g *Geometry) ([][2]float64, error) {
	if g == nil {
		return nil, nil
	}

	switch g.Type {
	case "Point":
		return extractPointCoords(g.Coordinates)
	case "MultiPoint":
		return extractMultiPointCoords(g.Coordinates)
	case "LineString":
		return extractLineStringCoords(g.Coordinates)
	case "MultiLineString":
		return extractMultiLineStringCoords(g.Coordinates)
	case "Polygon":
		return extractPolygonCoords(g.Coordinates)
	case "MultiPolygon":
		return extractMultiPolygonCoords(g.Coordinates)
	case "GeometryCollection":
		return extractGeometryCollectionCoords(g.Coordinates)
	default:
		return nil, fmt.Errorf("unknown geometry type: %s", g.Type)
	}
}

// GetBounds returns the bounding box of a geometry.
func GetBounds(g *Geometry) (minX, minY, maxX, maxY float64, err error) {
	coords, err := ExtractCoordinates(g)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if len(coords) == 0 {
		return 0, 0, 0, 0, fmt.Errorf("no coordinates found")
	}

	minX, minY = coords[0][0], coords[0][1]
	maxX, maxY = coords[0][0], coords[0][1]

	for _, c := range coords[1:] {
		if c[0] < minX {
			minX = c[0]
		}
		if c[0] > maxX {
			maxX = c[0]
		}
		if c[1] < minY {
			minY = c[1]
		}
		if c[1] > maxY {
			maxY = c[1]
		}
	}

	return minX, minY, maxX, maxY, nil
}

// Helper functions for coordinate extraction

func extractPointCoords(coords any) ([][2]float64, error) {
	arr, ok := coords.([]any)
	if !ok || len(arr) < 2 {
		return nil, fmt.Errorf("invalid Point coordinates")
	}
	lon, ok1 := toFloat64(arr[0])
	lat, ok2 := toFloat64(arr[1])
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("invalid Point coordinate values")
	}
	return [][2]float64{{lon, lat}}, nil
}

func extractMultiPointCoords(coords any) ([][2]float64, error) {
	arr, ok := coords.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid MultiPoint coordinates")
	}
	var result [][2]float64
	for _, p := range arr {
		pCoords, err := extractPointCoords(p)
		if err != nil {
			return nil, err
		}
		result = append(result, pCoords...)
	}
	return result, nil
}

func extractLineStringCoords(coords any) ([][2]float64, error) {
	return extractMultiPointCoords(coords)
}

func extractMultiLineStringCoords(coords any) ([][2]float64, error) {
	arr, ok := coords.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid MultiLineString coordinates")
	}
	var result [][2]float64
	for _, line := range arr {
		lineCoords, err := extractLineStringCoords(line)
		if err != nil {
			return nil, err
		}
		result = append(result, lineCoords...)
	}
	return result, nil
}

func extractPolygonCoords(coords any) ([][2]float64, error) {
	return extractMultiLineStringCoords(coords)
}

func extractMultiPolygonCoords(coords any) ([][2]float64, error) {
	arr, ok := coords.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid MultiPolygon coordinates")
	}
	var result [][2]float64
	for _, poly := range arr {
		polyCoords, err := extractPolygonCoords(poly)
		if err != nil {
			return nil, err
		}
		result = append(result, polyCoords...)
	}
	return result, nil
}

func extractGeometryCollectionCoords(coords any) ([][2]float64, error) {
	// GeometryCollection uses "geometries" instead of "coordinates"
	// This is handled specially
	return nil, nil
}

func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}
