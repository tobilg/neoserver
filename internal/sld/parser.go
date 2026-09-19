package sld

import (
	"encoding/xml"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

// ParseError reports where an SLD document stopped being well-formed.
type ParseError struct {
	Line    int
	Column  int
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("failed to parse SLD: line %d, column %d: %s", e.Line, e.Column, e.Message)
}

// Parse parses an SLD document from a reader. Only whitespace, comments and
// processing instructions may follow the root element.
func Parse(r io.Reader) (*StyledLayerDescriptor, error) {
	var sld StyledLayerDescriptor
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(&sld); err != nil {
		return nil, fmt.Errorf("failed to parse SLD: %w", err)
	}
	for {
		line, column := decoder.InputPos()
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			message := err.Error()
			var syntax *xml.SyntaxError
			if errors.As(err, &syntax) {
				message = syntax.Msg
			}
			return nil, &ParseError{Line: line, Column: column, Message: "unexpected content after the root element (" + message + ")"}
		}
		switch t := token.(type) {
		case xml.Comment, xml.ProcInst:
		case xml.CharData:
			text := string(t)
			if trimmed := strings.TrimLeft(text, " \t\r\n"); trimmed != "" {
				leading := text[:len(text)-len(trimmed)]
				if newlines := strings.Count(leading, "\n"); newlines > 0 {
					line += newlines
					column = len(leading) - strings.LastIndex(leading, "\n")
				} else {
					column += len(leading)
				}
				return nil, &ParseError{Line: line, Column: column, Message: "unexpected text after the root element"}
			}
		default:
			return nil, &ParseError{Line: line, Column: column, Message: "unexpected content after the root element"}
		}
	}
	return &sld, nil
}

// ParseFile parses an SLD document from a file path.
func ParseFile(path string) (*StyledLayerDescriptor, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open SLD file: %w", err)
	}
	defer f.Close()
	return Parse(f)
}

// ParseString parses an SLD document from a string.
func ParseString(s string) (*StyledLayerDescriptor, error) {
	return Parse(strings.NewReader(s))
}

// Validate resolves every user style and rejects constructs that the renderer
// cannot faithfully portray.
func Validate(doc *StyledLayerDescriptor) error {
	if doc == nil {
		return fmt.Errorf("SLD document is nil")
	}
	if len(doc.NamedLayers) == 0 {
		return fmt.Errorf("SLD must contain a NamedLayer")
	}
	for i := range doc.NamedLayers {
		if len(doc.NamedLayers[i].UserStyles) == 0 {
			return fmt.Errorf("NamedLayer %q has no UserStyle", doc.NamedLayers[i].Name)
		}
		for j := range doc.NamedLayers[i].UserStyles {
			if _, err := resolveStyle(&doc.NamedLayers[i].UserStyles[j]); err != nil {
				return fmt.Errorf("style %q: %w", doc.NamedLayers[i].UserStyles[j].Name, err)
			}
		}
	}
	return nil
}

// GetStyle retrieves a style by layer name and optional style name.
func (sld *StyledLayerDescriptor) GetStyle(layerName, styleName string) (*Style, error) {
	for _, nl := range sld.NamedLayers {
		if nl.Name == layerName {
			for _, us := range nl.UserStyles {
				if styleName == "" && us.IsDefault {
					return resolveStyle(&us)
				}
				if us.Name == styleName {
					return resolveStyle(&us)
				}
			}
			// If no matching style found but we have user styles, use the first one
			if len(nl.UserStyles) > 0 {
				return resolveStyle(&nl.UserStyles[0])
			}
		}
	}
	return nil, fmt.Errorf("style not found for layer %q", layerName)
}

// GetDefaultStyle returns the default style from the first layer, if any.
func (sld *StyledLayerDescriptor) GetDefaultStyle() (*Style, error) {
	for _, nl := range sld.NamedLayers {
		for _, us := range nl.UserStyles {
			if us.IsDefault || len(nl.UserStyles) == 1 {
				return resolveStyle(&us)
			}
		}
		if len(nl.UserStyles) > 0 {
			return resolveStyle(&nl.UserStyles[0])
		}
	}
	return nil, fmt.Errorf("no style found in SLD")
}

// resolveStyle converts a UserStyle to a resolved Style.
func resolveStyle(us *UserStyle) (*Style, error) {
	style := &Style{
		Name:             us.Name,
		Title:            us.Title,
		CompositeOpacity: 1,
	}

	for ftsIndex, fts := range us.FeatureTypeStyles {
		if err := resolveFeatureTypeOptions(style, &fts); err != nil {
			return nil, err
		}
		for _, rule := range fts.Rules {
			resolved := ResolvedRule{
				Name:             rule.Name,
				Title:            rule.Title,
				Filter:           rule.Filter,
				MinScale:         rule.MinScaleDenom,
				MaxScale:         rule.MaxScaleDenom,
				ElseFilter:       rule.ElseFilter != nil,
				FeatureTypeStyle: ftsIndex,
			}
			if resolved.ElseFilter && rule.Filter != nil {
				return nil, fmt.Errorf("rule %q cannot contain both Filter and ElseFilter", rule.Name)
			}

			// Pre-compile LIKE patterns in the filter for performance
			if resolved.Filter != nil {
				resolved.Filter.CompilePatterns()
			}

			for _, raw := range orderedRawSymbolizers(&rule) {
				symbolizer := ResolvedSymbolizer{}
				switch {
				case raw.Point != nil:
					if err := validateGraphic(&raw.Point.Graphic); err != nil {
						return nil, err
					}
					symbolizer.Point = resolvePointStyle(raw.Point)
					if resolved.PointStyle == nil {
						resolved.PointStyle = symbolizer.Point
					}
				case raw.Line != nil:
					if raw.Line.Stroke != nil && raw.Line.Stroke.GraphicStroke != nil {
						if err := validateGraphic(&raw.Line.Stroke.GraphicStroke.Graphic); err != nil {
							return nil, err
						}
					}
					symbolizer.Line = resolveLineStyle(raw.Line)
					if resolved.LineStyle == nil {
						resolved.LineStyle = symbolizer.Line
					}
				case raw.Polygon != nil:
					if raw.Polygon.Fill != nil && raw.Polygon.Fill.GraphicFill != nil {
						if err := validateGraphic(&raw.Polygon.Fill.GraphicFill.Graphic); err != nil {
							return nil, err
						}
					}
					if raw.Polygon.Stroke != nil && raw.Polygon.Stroke.GraphicStroke != nil {
						if err := validateGraphic(&raw.Polygon.Stroke.GraphicStroke.Graphic); err != nil {
							return nil, err
						}
					}
					symbolizer.Polygon = resolvePolygonStyle(raw.Polygon)
					if resolved.PolygonStyle == nil {
						resolved.PolygonStyle = symbolizer.Polygon
					}
				case raw.Text != nil:
					symbolizer.Text = resolveTextStyle(raw.Text)
					if resolved.TextStyle == nil {
						resolved.TextStyle = symbolizer.Text
					}
				case raw.Raster != nil:
					var err error
					symbolizer.Raster, err = resolveRasterStyle(raw.Raster)
					if err != nil {
						return nil, err
					}
					if resolved.RasterStyle == nil {
						resolved.RasterStyle = symbolizer.Raster
					}
				}
				resolved.Symbolizers = append(resolved.Symbolizers, symbolizer)
			}
			if len(resolved.Symbolizers) == 0 {
				return nil, fmt.Errorf("rule %q has no supported symbolizers", rule.Name)
			}

			style.Rules = append(style.Rules, resolved)
		}
	}

	return style, nil
}

func resolveFeatureTypeOptions(style *Style, fts *FeatureTypeStyle) error {
	for _, option := range fts.VendorOptions {
		name, value := strings.ToLower(strings.TrimSpace(option.Name)), strings.TrimSpace(option.Expression())
		switch name {
		case "composite":
			value = strings.ToLower(value)
			switch value {
			case "source-over", "source-in", "source-out", "source-atop", "destination-over", "destination-in", "destination-out", "destination-atop", "xor", "copy", "multiply", "screen", "overlay", "darken", "lighten", "color-dodge", "color-burn", "hard-light", "soft-light", "difference", "exclusion":
				style.Composite = value
			default:
				return fmt.Errorf("unsupported composite mode %q", value)
			}
		case "composite-opacity":
			opacity, err := strconv.ParseFloat(value, 64)
			if err != nil || opacity < 0 || opacity > 1 {
				return fmt.Errorf("composite-opacity must be between 0 and 1")
			}
			style.CompositeOpacity = opacity
		case "composite-base":
			style.CompositeBase = strings.EqualFold(value, "true") || value == "1"
		case "sortby", "z-order":
			fields := strings.Fields(value)
			if len(fields) == 0 {
				return fmt.Errorf("sortBy requires a property")
			}
			style.SortBy = fields[0]
			style.SortDescending = len(fields) > 1 && (strings.EqualFold(fields[1], "D") || strings.EqualFold(fields[1], "DESC"))
		}
	}
	if fts.Transformation == nil {
		return nil
	}
	name := strings.ToLower(strings.TrimSpace(fts.Transformation.Function.Name))
	if colon := strings.LastIndex(name, ":"); colon >= 0 {
		name = name[colon+1:]
	}
	transformation := &ResolvedTransformation{Name: name, Radius: 20}
	parameters := transformationParameters(fts.Transformation.Function)
	transformation.Parameters = parameters
	if _, ok := LookupProcess(name); !ok {
		return fmt.Errorf("unsupported rendering transformation %q", name)
	}
	switch name {
	case "heatmap", "barnes":
		if value := parameters["radiuspixels"]; value != "" {
			var err error
			transformation.Radius, err = strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(transformation.Radius) || math.IsInf(transformation.Radius, 0) || transformation.Radius < 1 || transformation.Radius > 256 {
				return fmt.Errorf("heatmap radiusPixels must be between 1 and 256")
			}
		}
		transformation.WeightProperty = parameters["weightattr"]
	case "pointstacker":
		if value := parameters["cellsize"]; value != "" {
			cell, err := strconv.ParseFloat(value, 64)
			if err != nil || cell < 2 || cell > 256 {
				return fmt.Errorf("PointStacker cellSize must be between 2 and 256")
			}
			transformation.Radius = cell
		} else {
			transformation.Radius = 32
		}
	case "groupcandidateselection":
		transformation.WeightProperty = parameters["groupby"]
	case "contour":
		for _, value := range strings.Split(parameters["levels"], ",") {
			if number, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				transformation.Levels = append(transformation.Levels, number)
			}
		}
	case "rasterize", "rasteraspointcollections":
	case "rasteralgebra":
		expression := parameters["expression"]
		if expression == "" || len(expression) > 256 {
			return fmt.Errorf("RasterAlgebra requires a bounded expression parameter")
		}
		for _, character := range expression {
			if !(character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || strings.ContainsRune("_ +-*()./", character)) {
				return fmt.Errorf("RasterAlgebra expression contains an unsupported character")
			}
		}
	}
	style.Transformation = transformation
	return nil
}

func transformationParameters(function OGCFunction) map[string]string {
	result := make(map[string]string)
	for _, child := range function.Functions {
		name := strings.ToLower(strings.TrimSpace(child.Name))
		if colon := strings.LastIndex(name, ":"); colon >= 0 {
			name = name[colon+1:]
		}
		if name != "parameter" || len(child.Literals) < 2 {
			continue
		}
		result[strings.ToLower(strings.TrimSpace(child.Literals[0].Value))] = strings.TrimSpace(child.Literals[1].Value)
	}
	return result
}

func StyleUsesCompositing(style *Style) bool {
	return style != nil && (style.Composite != "" && style.Composite != "source-over" || style.CompositeOpacity != 1 || style.CompositeBase)
}

func StyleUsesZOrder(style *Style) bool { return style != nil && style.SortBy != "" }

func StyleUsesTransformation(style *Style) bool { return style != nil && style.Transformation != nil }

func StyleUsesRemoteGraphics(style *Style) bool {
	if style == nil {
		return false
	}
	isRemote := func(point *PointStyle) bool {
		return point != nil && point.ExternalGraphic != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(point.ExternalGraphic.Href)), "https://")
	}
	for _, rule := range style.Rules {
		for _, symbolizer := range rule.Symbolizers {
			if isRemote(symbolizer.Point) || symbolizer.Line != nil && symbolizer.Line.GraphicStroke != nil && isRemote(symbolizer.Line.GraphicStroke.Point) || symbolizer.Polygon != nil && (symbolizer.Polygon.GraphicFill != nil && isRemote(symbolizer.Polygon.GraphicFill.Point) || symbolizer.Polygon.GraphicStroke != nil && isRemote(symbolizer.Polygon.GraphicStroke.Point)) {
				return true
			}
		}
	}
	return false
}

func orderedRawSymbolizers(rule *Rule) []RawSymbolizer {
	if len(rule.Symbolizers) > 0 {
		return rule.Symbolizers
	}
	var result []RawSymbolizer
	for i := range rule.PointSymbolizers {
		result = append(result, RawSymbolizer{Point: &rule.PointSymbolizers[i]})
	}
	for i := range rule.LineSymbolizers {
		result = append(result, RawSymbolizer{Line: &rule.LineSymbolizers[i]})
	}
	for i := range rule.PolygonSymbolizer {
		result = append(result, RawSymbolizer{Polygon: &rule.PolygonSymbolizer[i]})
	}
	for i := range rule.TextSymbolizers {
		result = append(result, RawSymbolizer{Text: &rule.TextSymbolizers[i]})
	}
	for i := range rule.RasterSymbolizers {
		result = append(result, RawSymbolizer{Raster: &rule.RasterSymbolizers[i]})
	}
	return result
}

func resolveRasterStyle(raw *RasterSymbolizer) (*RasterStyle, error) {
	if raw.ShadedRelief != nil {
		return nil, fmt.Errorf("ShadedRelief is not supported")
	}
	if raw.OverlapBehavior != nil {
		return nil, fmt.Errorf("OverlapBehavior is not supported")
	}
	if raw.ImageOutline != nil {
		return nil, fmt.Errorf("ImageOutline is not supported")
	}
	result := &RasterStyle{Opacity: 1}
	if strings.TrimSpace(raw.Opacity.Value()) != "" {
		rawValue := strings.TrimSpace(raw.Opacity.Value())
		value, err := numericStyleValue(rawValue)
		if err != nil || value < 0 || value > 1 {
			return nil, fmt.Errorf("RasterSymbolizer Opacity must be between 0 and 1")
		}
		result.Opacity = value
		if IsDynamicExpression(rawValue) {
			result.OpacityExpression = rawValue
		}
	}
	if raw.ChannelSelection != nil {
		selection := raw.ChannelSelection
		rgb := selection.Red != nil || selection.Green != nil || selection.Blue != nil
		if selection.Gray != nil && rgb {
			return nil, fmt.Errorf("ChannelSelection cannot mix GrayChannel and RGB channels")
		}
		if rgb && (selection.Red == nil || selection.Green == nil || selection.Blue == nil) {
			return nil, fmt.Errorf("ChannelSelection requires RedChannel, GreenChannel, and BlueChannel")
		}
		var err error
		if selection.Gray != nil {
			result.Channels.Gray, err = resolveChannel(selection.Gray)
		}
		if err == nil && selection.Red != nil {
			result.Channels.Red, err = resolveChannel(selection.Red)
		}
		if err == nil && selection.Green != nil {
			result.Channels.Green, err = resolveChannel(selection.Green)
		}
		if err == nil && selection.Blue != nil {
			result.Channels.Blue, err = resolveChannel(selection.Blue)
		}
		if err != nil {
			return nil, err
		}
	}
	var err error
	result.ContrastEnhancement, err = resolveContrast(raw.ContrastEnhancement)
	if err != nil {
		return nil, err
	}
	if raw.ColorMap != nil {
		kind := strings.ToLower(strings.TrimSpace(raw.ColorMap.Type))
		if kind == "" {
			kind = "ramp"
		}
		if kind != "ramp" && kind != "intervals" && kind != "values" {
			return nil, fmt.Errorf("ColorMap type must be ramp, intervals, or values")
		}
		cm := &ResolvedColorMap{Type: kind, Extended: raw.ColorMap.Extended}
		for _, entry := range raw.ColorMap.Entries {
			quantityRaw := strings.TrimSpace(entry.Quantity)
			quantity, e := numericStyleValue(quantityRaw)
			if e != nil {
				return nil, fmt.Errorf("ColorMapEntry quantity %q is invalid", entry.Quantity)
			}
			opacity := 1.0
			opacityRaw := strings.TrimSpace(entry.Opacity)
			if strings.TrimSpace(entry.Opacity) != "" {
				opacity, e = numericStyleValue(opacityRaw)
				if e != nil || opacity < 0 || opacity > 1 {
					return nil, fmt.Errorf("ColorMapEntry opacity must be between 0 and 1")
				}
			}
			colorRaw := strings.TrimSpace(entry.Color)
			labelRaw := strings.TrimSpace(entry.Label)
			resolvedColor := colorRaw
			if IsDynamicExpression(colorRaw) {
				resolvedColor, e = EvaluateStringExpression(colorRaw, nil)
				if e != nil {
					return nil, fmt.Errorf("ColorMapEntry color expression: %w", e)
				}
			}
			resolvedLabel := entry.Label
			if IsDynamicExpression(labelRaw) {
				resolvedLabel, e = EvaluateStringExpression(labelRaw, nil)
				if e != nil {
					return nil, fmt.Errorf("ColorMapEntry label expression: %w", e)
				}
			}
			resolved := ResolvedColorMapEntry{Color: parseColor(resolvedColor), Quantity: quantity, Opacity: opacity, Label: resolvedLabel}
			if IsDynamicExpression(colorRaw) {
				resolved.ColorExpression = colorRaw
			}
			if IsDynamicExpression(quantityRaw) {
				resolved.QuantityExpression = quantityRaw
			}
			if IsDynamicExpression(opacityRaw) {
				resolved.OpacityExpression = opacityRaw
			}
			if IsDynamicExpression(labelRaw) {
				resolved.LabelExpression = labelRaw
			}
			cm.Entries = append(cm.Entries, resolved)
		}
		if len(cm.Entries) == 0 {
			return nil, fmt.Errorf("ColorMap requires at least one ColorMapEntry")
		}
		for i := 1; i < len(cm.Entries); i++ {
			if cm.Entries[i].Quantity <= cm.Entries[i-1].Quantity {
				return nil, fmt.Errorf("ColorMapEntry quantities must be strictly increasing")
			}
		}
		result.ColorMap = cm
	}
	return result, nil
}

func resolveChannel(raw *SelectedChannel) (*ResolvedChannel, error) {
	rawName := raw.SourceChannel.Value()
	name := rawName
	if IsDynamicExpression(rawName) {
		var err error
		name, err = EvaluateStringExpression(rawName, nil)
		if err != nil {
			return nil, fmt.Errorf("SourceChannelName expression: %w", err)
		}
	}
	if name == "" {
		return nil, fmt.Errorf("SourceChannelName is required")
	}
	contrast, err := resolveContrast(raw.ContrastEnhancement)
	if err != nil {
		return nil, err
	}
	resolved := &ResolvedChannel{Name: name, ContrastEnhancement: contrast}
	if IsDynamicExpression(rawName) {
		resolved.NameExpression = rawName
	}
	return resolved, nil
}

func resolveContrast(raw *ContrastEnhancement) (*ResolvedContrastEnhancement, error) {
	if raw == nil {
		return nil, nil
	}
	if raw.Histogram != nil && raw.Normalize != nil {
		return nil, fmt.Errorf("ContrastEnhancement cannot contain both Normalize and Histogram")
	}
	result := &ResolvedContrastEnhancement{Normalize: raw.Normalize != nil, Histogram: raw.Histogram != nil, Gamma: 1}
	if raw.Normalize != nil {
		for _, option := range raw.Normalize.VendorOptions {
			value := option.Expression()
			switch strings.ToLower(strings.TrimSpace(option.Name)) {
			case "algorithm", "algorithm_name":
				resolved, err := stringStyleValue(value)
				if err != nil {
					return nil, fmt.Errorf("Normalize algorithm: %w", err)
				}
				result.Algorithm = resolved
				if IsDynamicExpression(value) {
					result.AlgorithmExpression = value
				}
			case "minvalue", "min_value":
				parsed, err := numericStyleValue(value)
				if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
					return nil, fmt.Errorf("Normalize minValue must be finite")
				}
				result.MinValue = &parsed
				if IsDynamicExpression(value) {
					result.MinValueExpression = value
				}
			case "maxvalue", "max_value":
				parsed, err := numericStyleValue(value)
				if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
					return nil, fmt.Errorf("Normalize maxValue must be finite")
				}
				result.MaxValue = &parsed
				if IsDynamicExpression(value) {
					result.MaxValueExpression = value
				}
			default:
				return nil, fmt.Errorf("unsupported Normalize VendorOption %q", option.Name)
			}
		}
		if result.Algorithm != "" {
			switch strings.ToLower(result.Algorithm) {
			case "stretchtominimummaximum":
				result.Algorithm = "StretchToMinimumMaximum"
			case "cliptominimummaximum":
				result.Algorithm = "ClipToMinimumMaximum"
			case "cliptozero":
				result.Algorithm = "ClipToZero"
			default:
				return nil, fmt.Errorf("unsupported Normalize algorithm %q", result.Algorithm)
			}
			if result.MinValue == nil || result.MaxValue == nil || *result.MinValue >= *result.MaxValue {
				return nil, fmt.Errorf("Normalize algorithm requires minValue < maxValue")
			}
		}
	}
	if strings.TrimSpace(raw.GammaValue) != "" {
		gamma, err := strconv.ParseFloat(strings.TrimSpace(raw.GammaValue), 64)
		if err != nil || gamma <= 0 {
			return nil, fmt.Errorf("GammaValue must be greater than zero")
		}
		result.Gamma = gamma
	}
	return result, nil
}

func numericStyleValue(value string) (float64, error) {
	if IsDynamicExpression(value) {
		return EvaluateNumericExpression(value, nil)
	}
	return strconv.ParseFloat(strings.TrimSpace(value), 64)
}

func stringStyleValue(value string) (string, error) {
	if IsDynamicExpression(value) {
		return EvaluateStringExpression(value, nil)
	}
	return strings.TrimSpace(value), nil
}

func RasterStyleUsesEnvironment(style *RasterStyle) bool {
	if style == nil {
		return false
	}
	if style.OpacityExpression != "" {
		return true
	}
	for _, channel := range []*ResolvedChannel{style.Channels.Gray, style.Channels.Red, style.Channels.Green, style.Channels.Blue} {
		if channel != nil && (channel.NameExpression != "" || contrastUsesEnvironment(channel.ContrastEnhancement)) {
			return true
		}
	}
	if contrastUsesEnvironment(style.ContrastEnhancement) {
		return true
	}
	if style.ColorMap != nil {
		for _, entry := range style.ColorMap.Entries {
			if entry.ColorExpression != "" || entry.QuantityExpression != "" || entry.OpacityExpression != "" || entry.LabelExpression != "" {
				return true
			}
		}
	}
	return false
}

func contrastUsesEnvironment(value *ResolvedContrastEnhancement) bool {
	return value != nil && (value.AlgorithmExpression != "" || value.MinValueExpression != "" || value.MaxValueExpression != "")
}

// ResolveRasterEnvironment returns a request-local copy with safe environment
// expressions evaluated. The compiled catalog style remains immutable.
func ResolveRasterEnvironment(input *RasterStyle, environment map[string]string) (*RasterStyle, error) {
	if input == nil {
		return nil, nil
	}
	result := *input
	if input.OpacityExpression != "" {
		value, err := EvaluateNumericExpression(input.OpacityExpression, environment)
		if err != nil || value < 0 || value > 1 {
			return nil, fmt.Errorf("RasterSymbolizer Opacity expression is invalid")
		}
		result.Opacity = value
	}
	resolveChannelEnvironment := func(channel *ResolvedChannel) (*ResolvedChannel, error) {
		if channel == nil {
			return nil, nil
		}
		copyValue := *channel
		if channel.NameExpression != "" {
			value, err := EvaluateStringExpression(channel.NameExpression, environment)
			if err != nil || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("SourceChannelName expression is invalid")
			}
			copyValue.Name = value
		}
		contrast, err := resolveContrastEnvironment(channel.ContrastEnhancement, environment)
		if err != nil {
			return nil, err
		}
		copyValue.ContrastEnhancement = contrast
		return &copyValue, nil
	}
	var err error
	if result.Channels.Gray, err = resolveChannelEnvironment(input.Channels.Gray); err != nil {
		return nil, err
	}
	if result.Channels.Red, err = resolveChannelEnvironment(input.Channels.Red); err != nil {
		return nil, err
	}
	if result.Channels.Green, err = resolveChannelEnvironment(input.Channels.Green); err != nil {
		return nil, err
	}
	if result.Channels.Blue, err = resolveChannelEnvironment(input.Channels.Blue); err != nil {
		return nil, err
	}
	if result.ContrastEnhancement, err = resolveContrastEnvironment(input.ContrastEnhancement, environment); err != nil {
		return nil, err
	}
	if input.ColorMap != nil {
		cm := *input.ColorMap
		cm.Entries = append([]ResolvedColorMapEntry(nil), input.ColorMap.Entries...)
		for i := range cm.Entries {
			entry := &cm.Entries[i]
			if entry.ColorExpression != "" {
				value, e := EvaluateStringExpression(entry.ColorExpression, environment)
				if e != nil {
					return nil, e
				}
				entry.Color = parseColor(value)
			}
			if entry.QuantityExpression != "" {
				value, e := EvaluateNumericExpression(entry.QuantityExpression, environment)
				if e != nil {
					return nil, e
				}
				entry.Quantity = value
			}
			if entry.OpacityExpression != "" {
				value, e := EvaluateNumericExpression(entry.OpacityExpression, environment)
				if e != nil || value < 0 || value > 1 {
					return nil, fmt.Errorf("ColorMapEntry opacity expression is invalid")
				}
				entry.Opacity = value
			}
			if entry.LabelExpression != "" {
				value, e := EvaluateStringExpression(entry.LabelExpression, environment)
				if e != nil {
					return nil, e
				}
				entry.Label = value
			}
			if i > 0 && entry.Quantity <= cm.Entries[i-1].Quantity {
				return nil, fmt.Errorf("evaluated ColorMapEntry quantities must be strictly increasing")
			}
		}
		result.ColorMap = &cm
	}
	return &result, nil
}

func resolveContrastEnvironment(input *ResolvedContrastEnhancement, environment map[string]string) (*ResolvedContrastEnhancement, error) {
	if input == nil {
		return nil, nil
	}
	result := *input
	if input.AlgorithmExpression != "" {
		value, err := EvaluateStringExpression(input.AlgorithmExpression, environment)
		if err != nil {
			return nil, err
		}
		result.Algorithm = value
	}
	if input.MinValueExpression != "" {
		value, err := EvaluateNumericExpression(input.MinValueExpression, environment)
		if err != nil {
			return nil, err
		}
		result.MinValue = &value
	}
	if input.MaxValueExpression != "" {
		value, err := EvaluateNumericExpression(input.MaxValueExpression, environment)
		if err != nil {
			return nil, err
		}
		result.MaxValue = &value
	}
	if result.Algorithm != "" {
		switch strings.ToLower(result.Algorithm) {
		case "stretchtominimummaximum":
			result.Algorithm = "StretchToMinimumMaximum"
		case "cliptominimummaximum":
			result.Algorithm = "ClipToMinimumMaximum"
		case "cliptozero":
			result.Algorithm = "ClipToZero"
		default:
			return nil, fmt.Errorf("unsupported Normalize algorithm %q", result.Algorithm)
		}
		if result.MinValue == nil || result.MaxValue == nil || *result.MinValue >= *result.MaxValue {
			return nil, fmt.Errorf("Normalize algorithm requires minValue < maxValue")
		}
	}
	return &result, nil
}

// resolvePointStyle converts a PointSymbolizer to a PointStyle.
func resolvePointStyle(ps *PointSymbolizer) *PointStyle {
	style := &PointStyle{
		Shape:       "circle",
		Size:        6.0,
		Rotation:    0,
		Opacity:     1.0,
		FillColor:   color.RGBA{R: 128, G: 128, B: 128, A: 255},
		StrokeColor: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth: 1.0,
	}

	if value := ps.Graphic.Size.Value(); value != "" {
		if size, err := numericStyleValue(value); err == nil {
			style.Size = size
		}
		if IsDynamicExpression(value) {
			style.SizeExpression = value
		}
	}

	if value := ps.Graphic.Rotation.Value(); value != "" {
		if rot, err := numericStyleValue(value); err == nil {
			style.Rotation = rot
		}
		if IsDynamicExpression(value) {
			style.RotationExpression = value
		}
	}

	if value := ps.Graphic.Opacity.Value(); value != "" {
		if opacity, err := numericStyleValue(value); err == nil {
			style.Opacity = opacity
		}
		if IsDynamicExpression(value) {
			style.OpacityExpression = value
		}
	}

	if ps.Graphic.Mark != nil {
		if ps.Graphic.Mark.WellKnownName != "" {
			style.Shape = strings.ToLower(ps.Graphic.Mark.WellKnownName)
		}
		if ps.Graphic.Mark.Fill != nil {
			style.FillColor = resolveFillColor(ps.Graphic.Mark.Fill)
			for _, param := range ps.Graphic.Mark.Fill.CssParameters {
				if param.Name == "fill" && IsDynamicExpression(param.Expression()) {
					style.FillColorExpression = param.Expression()
				}
			}
		}
		if ps.Graphic.Mark.Stroke != nil {
			style.StrokeColor, style.StrokeWidth = resolveStroke(ps.Graphic.Mark.Stroke)
			for _, param := range ps.Graphic.Mark.Stroke.CssParameters {
				if !IsDynamicExpression(param.Expression()) {
					continue
				}
				if param.Name == "stroke" {
					style.StrokeColorExpression = param.Expression()
				}
				if param.Name == "stroke-width" {
					style.StrokeWidthExpression = param.Expression()
				}
			}
		}
	}
	if external := ps.Graphic.ExternalGraphic; external != nil {
		style.ExternalGraphic = &ResolvedExternalGraphic{Href: strings.TrimSpace(external.OnlineResource.Href), Format: strings.ToLower(strings.TrimSpace(external.Format))}
	}
	applyObstacleOptions(ps.VendorOptions, &style.LabelObstacle, &style.ObstacleMargin)

	return style
}

// resolveLineStyle converts a LineSymbolizer to a LineStyle.
func resolveLineStyle(ls *LineSymbolizer) *LineStyle {
	style := &LineStyle{
		Color:    color.RGBA{R: 0, G: 0, B: 0, A: 255},
		Width:    1.0,
		Opacity:  1.0,
		LineCap:  "round",
		LineJoin: "round",
	}

	if ls.Stroke != nil {
		for _, param := range ls.Stroke.CssParameters {
			value := param.Expression()
			switch param.Name {
			case "stroke":
				if resolved, err := stringStyleValue(value); err == nil && resolved != "" {
					style.Color = parseColor(resolved)
				}
				if IsDynamicExpression(value) {
					style.ColorExpression = value
				}
			case "stroke-width":
				if w, err := numericStyleValue(value); err == nil {
					style.Width = w
				}
				if IsDynamicExpression(value) {
					style.WidthExpression = value
				}
			case "stroke-opacity":
				if o, err := numericStyleValue(value); err == nil {
					style.Opacity = o
				}
				if IsDynamicExpression(value) {
					style.OpacityExpression = value
				}
			case "stroke-linecap":
				style.LineCap = value
			case "stroke-linejoin":
				style.LineJoin = value
			case "stroke-dasharray":
				style.DashArray = parseDashArray(value)
			case "stroke-dashoffset":
				if o, err := numericStyleValue(value); err == nil {
					style.DashOffset = o
				}
				if IsDynamicExpression(value) {
					style.DashOffsetExpression = value
				}
			}
		}
		if raw := ls.Stroke.GraphicStroke; raw != nil {
			style.GraphicStroke = resolveGraphicPattern(&raw.Graphic)
			style.GraphicStroke.Gap, _ = strconv.ParseFloat(strings.TrimSpace(raw.Gap), 64)
			style.GraphicStroke.InitialGap, _ = strconv.ParseFloat(strings.TrimSpace(raw.InitialGap), 64)
		}
	}
	applyObstacleOptions(ls.VendorOptions, &style.LabelObstacle, &style.ObstacleMargin)

	return style
}

// resolvePolygonStyle converts a PolygonSymbolizer to a PolygonStyle.
func resolvePolygonStyle(pgs *PolygonSymbolizer) *PolygonStyle {
	style := &PolygonStyle{
		FillColor:     color.RGBA{R: 128, G: 128, B: 128, A: 255},
		FillOpacity:   1.0,
		StrokeColor:   color.RGBA{R: 0, G: 0, B: 0, A: 255},
		StrokeWidth:   1.0,
		StrokeOpacity: 1.0,
	}

	if pgs.Fill != nil {
		for _, param := range pgs.Fill.CssParameters {
			value := param.Expression()
			switch param.Name {
			case "fill":
				if resolved, err := stringStyleValue(value); err == nil && resolved != "" {
					style.FillColor = parseColor(resolved)
				}
				if IsDynamicExpression(value) {
					style.FillColorExpression = value
				}
			case "fill-opacity":
				if o, err := numericStyleValue(value); err == nil {
					style.FillOpacity = o
				}
				if IsDynamicExpression(value) {
					style.FillOpacityExpression = value
				}
			}
		}
		if raw := pgs.Fill.GraphicFill; raw != nil {
			style.GraphicFill = resolveGraphicPattern(&raw.Graphic)
		}
	}

	if pgs.Stroke != nil {
		for _, param := range pgs.Stroke.CssParameters {
			value := param.Expression()
			switch param.Name {
			case "stroke":
				if resolved, err := stringStyleValue(value); err == nil && resolved != "" {
					style.StrokeColor = parseColor(resolved)
				}
				if IsDynamicExpression(value) {
					style.StrokeColorExpression = value
				}
			case "stroke-width":
				if w, err := numericStyleValue(value); err == nil {
					style.StrokeWidth = w
				}
				if IsDynamicExpression(value) {
					style.StrokeWidthExpression = value
				}
			case "stroke-opacity":
				if o, err := numericStyleValue(value); err == nil {
					style.StrokeOpacity = o
				}
				if IsDynamicExpression(value) {
					style.StrokeOpacityExpression = value
				}
			}
		}
		if raw := pgs.Stroke.GraphicStroke; raw != nil {
			style.GraphicStroke = resolveGraphicPattern(&raw.Graphic)
			style.GraphicStroke.Gap, _ = strconv.ParseFloat(strings.TrimSpace(raw.Gap), 64)
			style.GraphicStroke.InitialGap, _ = strconv.ParseFloat(strings.TrimSpace(raw.InitialGap), 64)
		}
	}
	applyObstacleOptions(pgs.VendorOptions, &style.LabelObstacle, &style.ObstacleMargin)

	return style
}

func applyObstacleOptions(options []VendorOption, enabled *bool, margin *float64) {
	for _, option := range options {
		name := strings.ToLower(strings.TrimSpace(option.Name))
		value := strings.TrimSpace(option.Expression())
		switch name {
		case "labelobstacle", "obstacle":
			*enabled = strings.EqualFold(value, "true") || value == "1"
		case "obstaclemargin":
			*margin, _ = strconv.ParseFloat(value, 64)
		}
	}
}

func validateGraphic(graphic *Graphic) error {
	if graphic == nil || graphic.Mark == nil && graphic.ExternalGraphic == nil {
		return fmt.Errorf("Graphic requires Mark or ExternalGraphic")
	}
	if graphic.Mark != nil && graphic.ExternalGraphic != nil {
		return fmt.Errorf("Graphic cannot contain both Mark and ExternalGraphic")
	}
	if external := graphic.ExternalGraphic; external != nil {
		if strings.TrimSpace(external.OnlineResource.Href) == "" {
			return fmt.Errorf("ExternalGraphic requires OnlineResource href")
		}
		format := strings.ToLower(strings.TrimSpace(external.Format))
		switch format {
		case "", "image/png", "image/jpeg", "image/gif", "image/svg+xml":
		default:
			return fmt.Errorf("unsupported ExternalGraphic format %q", external.Format)
		}
	}
	return nil
}

func resolveGraphicPattern(graphic *Graphic) *GraphicPattern {
	point := resolvePointStyle(&PointSymbolizer{Graphic: *graphic})
	return &GraphicPattern{Point: point, Gap: point.Size}
}

// resolveTextStyle converts a TextSymbolizer to a TextStyle.
func resolveTextStyle(ts *TextSymbolizer) *TextStyle {
	labelExpression := ts.Label.Expression()
	style := &TextStyle{
		PropertyName:       ts.Label.PropertyName,
		Literal:            strings.TrimSpace(ts.Label.Literal),
		FontFamily:         "sans-serif",
		FontSize:           12.0,
		FontStyle:          "normal",
		FontWeight:         "normal",
		Color:              color.RGBA{R: 0, G: 0, B: 0, A: 255},
		HaloColor:          color.RGBA{R: 255, G: 255, B: 255, A: 255},
		HaloRadius:         0,
		AnchorX:            0.5,
		AnchorY:            0.5,
		ConflictResolution: true,
		SpaceAround:        2,
		ForceLeftToRight:   true,
		GoodnessOfFit:      0.5,
	}
	if IsDynamicExpression(labelExpression) && (ts.Label.Function != nil || strings.TrimSpace(ts.Label.PropertyName) == "") {
		style.LabelExpression = labelExpression
	}

	if ts.Font != nil {
		for _, param := range ts.Font.CssParameters {
			value := param.Expression()
			switch param.Name {
			case "font-family":
				if resolved, err := stringStyleValue(value); err == nil && resolved != "" {
					style.FontFamily = resolved
				}
				if IsDynamicExpression(value) {
					style.FontFamilyExpression = value
				}
			case "font-size":
				if s, err := numericStyleValue(value); err == nil {
					style.FontSize = s
				}
				if IsDynamicExpression(value) {
					style.FontSizeExpression = value
				}
			case "font-style":
				style.FontStyle = value
			case "font-weight":
				style.FontWeight = value
			}
		}
	}

	if ts.Fill != nil {
		for _, param := range ts.Fill.CssParameters {
			if param.Name == "fill" {
				value := param.Expression()
				if resolved, err := stringStyleValue(value); err == nil && resolved != "" {
					style.Color = parseColor(resolved)
				}
				if IsDynamicExpression(value) {
					style.ColorExpression = value
				}
			}
		}
	}

	if ts.Halo != nil {
		if ts.Halo.Radius != "" {
			if r, err := numericStyleValue(ts.Halo.Radius); err == nil {
				style.HaloRadius = r
			}
			if IsDynamicExpression(ts.Halo.Radius) {
				style.HaloRadiusExpression = ts.Halo.Radius
			}
		}
		if ts.Halo.Fill != nil {
			for _, param := range ts.Halo.Fill.CssParameters {
				if param.Name == "fill" {
					value := param.Expression()
					if resolved, err := stringStyleValue(value); err == nil && resolved != "" {
						style.HaloColor = parseColor(resolved)
					}
					if IsDynamicExpression(value) {
						style.HaloColorExpression = value
					}
				}
			}
		}
	}

	if ts.LabelPlacement != nil && ts.LabelPlacement.PointPlacement != nil {
		pp := ts.LabelPlacement.PointPlacement
		if pp.AnchorPoint != nil {
			if x, err := strconv.ParseFloat(pp.AnchorPoint.AnchorPointX, 64); err == nil {
				style.AnchorX = x
			}
			if y, err := strconv.ParseFloat(pp.AnchorPoint.AnchorPointY, 64); err == nil {
				style.AnchorY = y
			}
		}
		if pp.Displacement != nil {
			if x, err := strconv.ParseFloat(pp.Displacement.DisplacementX, 64); err == nil {
				style.DisplacementX = x
			}
			if y, err := strconv.ParseFloat(pp.Displacement.DisplacementY, 64); err == nil {
				style.DisplacementY = y
			}
		}
		if pp.Rotation != "" {
			if r, err := strconv.ParseFloat(pp.Rotation, 64); err == nil {
				style.Rotation = r
			}
		}
	}
	if ts.LabelPlacement != nil && ts.LabelPlacement.LinePlacement != nil {
		style.FollowLine = true
		style.Advanced = true
	}
	if value, err := numericStyleValue(strings.TrimSpace(ts.Priority)); err == nil {
		style.Priority = value
		if value != 0 {
			style.Advanced = true
		}
	}
	if IsDynamicExpression(strings.TrimSpace(ts.Priority)) {
		style.PriorityExpression = strings.TrimSpace(ts.Priority)
		style.Advanced = true
	}
	for _, option := range ts.VendorOptions {
		name, value := strings.ToLower(strings.TrimSpace(option.Name)), strings.TrimSpace(option.Expression())
		boolean := strings.EqualFold(value, "true") || value == "1"
		switch name {
		case "conflictresolution":
			style.ConflictResolution = boolean
		case "spacearound":
			style.SpaceAround, _ = strconv.ParseFloat(value, 64)
		case "followline":
			style.FollowLine = boolean
		case "repeat":
			style.Repeat, _ = strconv.ParseFloat(value, 64)
		case "maxdisplacement":
			style.MaxDisplacement, _ = strconv.ParseFloat(value, 64)
		case "maxangledelta":
			style.MaxAngleDelta, _ = strconv.ParseFloat(value, 64)
		case "partials":
			style.Partials = boolean
		case "group":
			style.Group = value
		case "labelallgroup":
			style.LabelAllGroup = boolean
		case "autowrap":
			style.AutoWrap, _ = strconv.Atoi(value)
		case "forcelefttoright":
			style.ForceLeftToRight = boolean
		case "goodnessoffit":
			style.GoodnessOfFit, _ = strconv.ParseFloat(value, 64)
		case "polygonalign":
			style.PolygonAlign = strings.ToLower(value)
		default:
			continue
		}
		style.Advanced = true
	}

	return style
}

func StyleUsesAdvancedLabels(style *Style) bool {
	if style == nil {
		return false
	}
	for _, rule := range style.Rules {
		for _, symbolizer := range rule.Symbolizers {
			if symbolizer.Text != nil && symbolizer.Text.Advanced {
				return true
			}
		}
		if rule.TextStyle != nil && rule.TextStyle.Advanced {
			return true
		}
	}
	return false
}

// resolveFillColor extracts fill color from a Fill element.
func resolveFillColor(fill *Fill) color.RGBA {
	for _, param := range fill.CssParameters {
		if param.Name == "fill" {
			value := param.Expression()
			if resolved, err := stringStyleValue(value); err == nil && resolved != "" {
				return parseColor(resolved)
			}
		}
	}
	return color.RGBA{R: 128, G: 128, B: 128, A: 255}
}

// resolveStroke extracts stroke color and width from a Stroke element.
func resolveStroke(stroke *Stroke) (color.RGBA, float64) {
	c := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	width := 1.0

	for _, param := range stroke.CssParameters {
		value := param.Expression()
		switch param.Name {
		case "stroke":
			if resolved, err := stringStyleValue(value); err == nil && resolved != "" {
				c = parseColor(resolved)
			}
		case "stroke-width":
			if w, err := numericStyleValue(value); err == nil {
				width = w
			}
		}
	}

	return c, width
}

// parseColor parses a CSS color string (hex format).
func parseColor(s string) color.RGBA {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		// Try named colors
		if c, ok := namedColors[strings.ToLower(s)]; ok {
			return c
		}
		return color.RGBA{R: 0, G: 0, B: 0, A: 255}
	}

	s = strings.TrimPrefix(s, "#")
	var r, g, b uint8

	switch len(s) {
	case 3:
		// Short format: #RGB
		if v, err := strconv.ParseUint(s[0:1]+s[0:1], 16, 8); err == nil {
			r = uint8(v)
		}
		if v, err := strconv.ParseUint(s[1:2]+s[1:2], 16, 8); err == nil {
			g = uint8(v)
		}
		if v, err := strconv.ParseUint(s[2:3]+s[2:3], 16, 8); err == nil {
			b = uint8(v)
		}
	case 6:
		// Full format: #RRGGBB
		if v, err := strconv.ParseUint(s[0:2], 16, 8); err == nil {
			r = uint8(v)
		}
		if v, err := strconv.ParseUint(s[2:4], 16, 8); err == nil {
			g = uint8(v)
		}
		if v, err := strconv.ParseUint(s[4:6], 16, 8); err == nil {
			b = uint8(v)
		}
	}

	return color.RGBA{R: r, G: g, B: b, A: 255}
}

// parseDashArray parses a dash array string.
func parseDashArray(s string) []float64 {
	parts := strings.Fields(strings.ReplaceAll(s, ",", " "))
	var result []float64
	for _, p := range parts {
		if v, err := strconv.ParseFloat(p, 64); err == nil {
			result = append(result, v)
		}
	}
	return result
}

// namedColors maps CSS color names to RGBA values.
var namedColors = map[string]color.RGBA{
	"black":   {R: 0, G: 0, B: 0, A: 255},
	"white":   {R: 255, G: 255, B: 255, A: 255},
	"red":     {R: 255, G: 0, B: 0, A: 255},
	"green":   {R: 0, G: 128, B: 0, A: 255},
	"blue":    {R: 0, G: 0, B: 255, A: 255},
	"yellow":  {R: 255, G: 255, B: 0, A: 255},
	"cyan":    {R: 0, G: 255, B: 255, A: 255},
	"magenta": {R: 255, G: 0, B: 255, A: 255},
	"gray":    {R: 128, G: 128, B: 128, A: 255},
	"grey":    {R: 128, G: 128, B: 128, A: 255},
	"orange":  {R: 255, G: 165, B: 0, A: 255},
	"purple":  {R: 128, G: 0, B: 128, A: 255},
	"brown":   {R: 165, G: 42, B: 42, A: 255},
	"pink":    {R: 255, G: 192, B: 203, A: 255},
}
