package sld

import (
	"image/color"

	"github.com/tobilg/neoserver/internal/conf"
)

// DefaultStyle creates a default style from configuration.
func DefaultStyle(cfg conf.WMSStyle) *Style {
	fillColor := parseColor(cfg.FillColor)
	strokeColor := parseColor(cfg.StrokeColor)

	// Apply fill opacity to the alpha channel
	fillAlpha := uint8(cfg.FillOpacity * 255)
	if fillAlpha == 0 && cfg.FillOpacity > 0 {
		fillAlpha = 1 // Minimum visible alpha
	}
	fillColor.A = fillAlpha

	pointRadius := cfg.PointRadius
	if pointRadius <= 0 {
		pointRadius = 5.0
	}

	strokeWidth := cfg.StrokeWidth
	if strokeWidth <= 0 {
		strokeWidth = 1.0
	}

	return &Style{
		Name:             "default",
		Title:            "Default Style",
		CompositeOpacity: 1,
		Rules: []ResolvedRule{
			{
				Name:  "default",
				Title: "Default Rule",
				PointStyle: &PointStyle{
					Shape:       "circle",
					Size:        pointRadius * 2, // Diameter
					Rotation:    0,
					Opacity:     1.0,
					FillColor:   fillColor,
					StrokeColor: strokeColor,
					StrokeWidth: strokeWidth,
				},
				LineStyle: &LineStyle{
					Color:    strokeColor,
					Width:    strokeWidth,
					Opacity:  1.0,
					LineCap:  "round",
					LineJoin: "round",
				},
				PolygonStyle: &PolygonStyle{
					FillColor:     fillColor,
					FillOpacity:   cfg.FillOpacity,
					StrokeColor:   strokeColor,
					StrokeWidth:   strokeWidth,
					StrokeOpacity: 1.0,
				},
			},
		},
	}
}

// DefaultPointStyle creates a default point style.
func DefaultPointStyle() *PointStyle {
	return &PointStyle{
		Shape:       "circle",
		Size:        10.0,
		Rotation:    0,
		Opacity:     1.0,
		FillColor:   color.RGBA{R: 51, G: 136, B: 255, A: 128},
		StrokeColor: color.RGBA{R: 51, G: 136, B: 255, A: 255},
		StrokeWidth: 2.0,
	}
}

// DefaultLineStyle creates a default line style.
func DefaultLineStyle() *LineStyle {
	return &LineStyle{
		Color:    color.RGBA{R: 51, G: 136, B: 255, A: 255},
		Width:    2.0,
		Opacity:  1.0,
		LineCap:  "round",
		LineJoin: "round",
	}
}

// DefaultPolygonStyle creates a default polygon style.
func DefaultPolygonStyle() *PolygonStyle {
	return &PolygonStyle{
		FillColor:     color.RGBA{R: 51, G: 136, B: 255, A: 128},
		FillOpacity:   0.5,
		StrokeColor:   color.RGBA{R: 51, G: 136, B: 255, A: 255},
		StrokeWidth:   2.0,
		StrokeOpacity: 1.0,
	}
}

// MergePointStyle merges a point style with defaults.
func MergePointStyle(style *PointStyle, defaults *PointStyle) *PointStyle {
	if style == nil {
		return defaults
	}
	result := *style
	if result.Size <= 0 {
		result.Size = defaults.Size
	}
	if result.Shape == "" {
		result.Shape = defaults.Shape
	}
	return &result
}

// MergeLineStyle merges a line style with defaults.
func MergeLineStyle(style *LineStyle, defaults *LineStyle) *LineStyle {
	if style == nil {
		return defaults
	}
	result := *style
	if result.Width <= 0 {
		result.Width = defaults.Width
	}
	if result.LineCap == "" {
		result.LineCap = defaults.LineCap
	}
	if result.LineJoin == "" {
		result.LineJoin = defaults.LineJoin
	}
	return &result
}

// MergePolygonStyle merges a polygon style with defaults.
func MergePolygonStyle(style *PolygonStyle, defaults *PolygonStyle) *PolygonStyle {
	if style == nil {
		return defaults
	}
	result := *style
	if result.StrokeWidth <= 0 {
		result.StrokeWidth = defaults.StrokeWidth
	}
	return &result
}

// ColorWithOpacity returns a color with modified opacity.
func ColorWithOpacity(c color.RGBA, opacity float64) color.RGBA {
	alpha := uint8(float64(c.A) * opacity)
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha}
}

// GetEffectiveStyle returns the effective style for a geometry type.
// It selects the appropriate symbolizer from the resolved rule.
func GetEffectiveStyle(rule *ResolvedRule, geomType string) interface{} {
	switch geomType {
	case "Point", "MultiPoint":
		if rule.PointStyle != nil {
			return rule.PointStyle
		}
		return DefaultPointStyle()
	case "LineString", "MultiLineString":
		if rule.LineStyle != nil {
			return rule.LineStyle
		}
		return DefaultLineStyle()
	case "Polygon", "MultiPolygon":
		if rule.PolygonStyle != nil {
			return rule.PolygonStyle
		}
		return DefaultPolygonStyle()
	default:
		// For geometry collections, prefer polygon, then line, then point
		if rule.PolygonStyle != nil {
			return rule.PolygonStyle
		}
		if rule.LineStyle != nil {
			return rule.LineStyle
		}
		if rule.PointStyle != nil {
			return rule.PointStyle
		}
		return DefaultPolygonStyle()
	}
}
