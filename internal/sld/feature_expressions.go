package sld

import (
	"fmt"
	"image/color"
	"math"
)

// ResolveSymbolizerExpressions evaluates bounded property/env expressions for
// one feature and returns a detached symbolizer safe to mutate for rendering.
func ResolveSymbolizerExpressions(input ResolvedSymbolizer, properties map[string]interface{}, environment map[string]string) (ResolvedSymbolizer, error) {
	values := make(map[string]string, len(properties)+len(environment))
	for key, value := range environment {
		values[key] = value
	}
	for key, value := range properties {
		values[key] = fmt.Sprint(value)
	}
	result := ResolvedSymbolizer{Raster: input.Raster}
	if input.Point != nil {
		value, err := resolvePointExpressions(input.Point, values)
		if err != nil {
			return result, err
		}
		result.Point = value
	}
	if input.Line != nil {
		value := *input.Line
		var err error
		if value.ColorExpression != "" {
			value.Color, err = evaluateColor(value.ColorExpression, values)
		}
		if err == nil && value.WidthExpression != "" {
			value.Width, err = EvaluateNumericExpression(value.WidthExpression, values)
		}
		if err == nil && value.OpacityExpression != "" {
			value.Opacity, err = EvaluateNumericExpression(value.OpacityExpression, values)
		}
		if err == nil && value.DashOffsetExpression != "" {
			value.DashOffset, err = EvaluateNumericExpression(value.DashOffsetExpression, values)
		}
		if err != nil {
			return result, err
		}
		if value.Width < 0 || value.Opacity < 0 || value.Opacity > 1 {
			return result, fmt.Errorf("line width or opacity outside supported range")
		}
		if value.GraphicStroke != nil {
			pattern := *value.GraphicStroke
			pattern.Point, err = resolvePointExpressions(pattern.Point, values)
			if err != nil {
				return result, err
			}
			value.GraphicStroke = &pattern
		}
		result.Line = &value
	}
	if input.Polygon != nil {
		value := *input.Polygon
		var err error
		if value.FillColorExpression != "" {
			value.FillColor, err = evaluateColor(value.FillColorExpression, values)
		}
		if err == nil && value.FillOpacityExpression != "" {
			value.FillOpacity, err = EvaluateNumericExpression(value.FillOpacityExpression, values)
		}
		if err == nil && value.StrokeColorExpression != "" {
			value.StrokeColor, err = evaluateColor(value.StrokeColorExpression, values)
		}
		if err == nil && value.StrokeWidthExpression != "" {
			value.StrokeWidth, err = EvaluateNumericExpression(value.StrokeWidthExpression, values)
		}
		if err == nil && value.StrokeOpacityExpression != "" {
			value.StrokeOpacity, err = EvaluateNumericExpression(value.StrokeOpacityExpression, values)
		}
		if err != nil {
			return result, err
		}
		if value.StrokeWidth < 0 || value.FillOpacity < 0 || value.FillOpacity > 1 || value.StrokeOpacity < 0 || value.StrokeOpacity > 1 {
			return result, fmt.Errorf("polygon width or opacity outside supported range")
		}
		if value.GraphicFill != nil {
			pattern := *value.GraphicFill
			pattern.Point, err = resolvePointExpressions(pattern.Point, values)
			if err != nil {
				return result, err
			}
			value.GraphicFill = &pattern
		}
		if value.GraphicStroke != nil {
			pattern := *value.GraphicStroke
			pattern.Point, err = resolvePointExpressions(pattern.Point, values)
			if err != nil {
				return result, err
			}
			value.GraphicStroke = &pattern
		}
		result.Polygon = &value
	}
	if input.Text != nil {
		value := *input.Text
		var err error
		if value.LabelExpression != "" {
			value.Literal, err = EvaluateStringExpression(value.LabelExpression, values)
			value.PropertyName = ""
		}
		if err == nil && value.FontFamilyExpression != "" {
			value.FontFamily, err = EvaluateStringExpression(value.FontFamilyExpression, values)
		}
		if err == nil && value.FontSizeExpression != "" {
			value.FontSize, err = EvaluateNumericExpression(value.FontSizeExpression, values)
		}
		if err == nil && value.ColorExpression != "" {
			value.Color, err = evaluateColor(value.ColorExpression, values)
		}
		if err == nil && value.HaloColorExpression != "" {
			value.HaloColor, err = evaluateColor(value.HaloColorExpression, values)
		}
		if err == nil && value.HaloRadiusExpression != "" {
			value.HaloRadius, err = EvaluateNumericExpression(value.HaloRadiusExpression, values)
		}
		if err == nil && value.RotationExpression != "" {
			value.Rotation, err = EvaluateNumericExpression(value.RotationExpression, values)
		}
		if err == nil && value.PriorityExpression != "" {
			value.Priority, err = EvaluateNumericExpression(value.PriorityExpression, values)
		}
		if err != nil {
			return result, err
		}
		if value.FontSize < 0 || value.HaloRadius < 0 {
			return result, fmt.Errorf("text size or halo radius outside supported range")
		}
		result.Text = &value
	}
	return result, nil
}

func resolvePointExpressions(input *PointStyle, values map[string]string) (*PointStyle, error) {
	if input == nil {
		return nil, nil
	}
	value := *input
	var err error
	if value.SizeExpression != "" {
		value.Size, err = EvaluateNumericExpression(value.SizeExpression, values)
	}
	if err == nil && value.RotationExpression != "" {
		value.Rotation, err = EvaluateNumericExpression(value.RotationExpression, values)
	}
	if err == nil && value.OpacityExpression != "" {
		value.Opacity, err = EvaluateNumericExpression(value.OpacityExpression, values)
	}
	if err == nil && value.FillColorExpression != "" {
		value.FillColor, err = evaluateColor(value.FillColorExpression, values)
	}
	if err == nil && value.StrokeColorExpression != "" {
		value.StrokeColor, err = evaluateColor(value.StrokeColorExpression, values)
	}
	if err == nil && value.StrokeWidthExpression != "" {
		value.StrokeWidth, err = EvaluateNumericExpression(value.StrokeWidthExpression, values)
	}
	if err != nil {
		return nil, err
	}
	if value.Size < 0 || value.StrokeWidth < 0 || value.Opacity < 0 || value.Opacity > 1 || math.IsNaN(value.Size) {
		return nil, fmt.Errorf("resolved point portrayal value is outside its valid range")
	}
	return &value, nil
}
func evaluateColor(expression string, values map[string]string) (result color.RGBA, err error) {
	value, err := EvaluateStringExpression(expression, values)
	if err != nil {
		return result, err
	}
	if !validStyleColor(value) {
		return result, fmt.Errorf("unsupported color %q", value)
	}
	return parseColor(value), nil
}

func StyleUsesDynamicExpressions(style *Style) bool {
	if style == nil {
		return false
	}
	for _, rule := range style.Rules {
		for _, symbolizer := range rule.Symbolizers {
			if pointUsesExpressions(symbolizer.Point) || symbolizer.Line != nil && (symbolizer.Line.ColorExpression != "" || symbolizer.Line.WidthExpression != "" || symbolizer.Line.OpacityExpression != "" || symbolizer.Line.DashOffsetExpression != "") || symbolizer.Polygon != nil && (symbolizer.Polygon.FillColorExpression != "" || symbolizer.Polygon.FillOpacityExpression != "" || symbolizer.Polygon.StrokeColorExpression != "" || symbolizer.Polygon.StrokeWidthExpression != "" || symbolizer.Polygon.StrokeOpacityExpression != "") || symbolizer.Text != nil && (symbolizer.Text.LabelExpression != "" || symbolizer.Text.FontFamilyExpression != "" || symbolizer.Text.FontSizeExpression != "" || symbolizer.Text.ColorExpression != "" || symbolizer.Text.HaloColorExpression != "" || symbolizer.Text.HaloRadiusExpression != "" || symbolizer.Text.RotationExpression != "" || symbolizer.Text.PriorityExpression != "") {
				return true
			}
		}
	}
	return false
}
func pointUsesExpressions(value *PointStyle) bool {
	return value != nil && (value.SizeExpression != "" || value.RotationExpression != "" || value.OpacityExpression != "" || value.FillColorExpression != "" || value.StrokeColorExpression != "" || value.StrokeWidthExpression != "")
}
