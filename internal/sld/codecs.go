package sld

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The alternative-format codecs intentionally compile to the same typed SLD
// document used by XML styles. They do not have a second rendering model.

func canonicalDocument(name string, rules []Rule) *StyledLayerDescriptor {
	if strings.TrimSpace(name) == "" {
		name = "style"
	}
	return &StyledLayerDescriptor{Version: "1.1.0", NamedLayers: []NamedLayer{{Name: name, UserStyles: []UserStyle{{Name: name, IsDefault: true, FeatureTypeStyles: []FeatureTypeStyle{{Rules: rules}}}}}}}
}

func appendPoint(rule *Rule, value PointSymbolizer) {
	rule.PointSymbolizers = append(rule.PointSymbolizers, value)
	rule.Symbolizers = append(rule.Symbolizers, RawSymbolizer{Point: &rule.PointSymbolizers[len(rule.PointSymbolizers)-1]})
}

func appendLine(rule *Rule, value LineSymbolizer) {
	rule.LineSymbolizers = append(rule.LineSymbolizers, value)
	rule.Symbolizers = append(rule.Symbolizers, RawSymbolizer{Line: &rule.LineSymbolizers[len(rule.LineSymbolizers)-1]})
}

func appendPolygon(rule *Rule, value PolygonSymbolizer) {
	rule.PolygonSymbolizer = append(rule.PolygonSymbolizer, value)
	rule.Symbolizers = append(rule.Symbolizers, RawSymbolizer{Polygon: &rule.PolygonSymbolizer[len(rule.PolygonSymbolizer)-1]})
}

func appendText(rule *Rule, value TextSymbolizer) {
	rule.TextSymbolizers = append(rule.TextSymbolizers, value)
	rule.Symbolizers = append(rule.Symbolizers, RawSymbolizer{Text: &rule.TextSymbolizers[len(rule.TextSymbolizers)-1]})
}

func appendRaster(rule *Rule, value RasterSymbolizer) {
	rule.RasterSymbolizers = append(rule.RasterSymbolizers, value)
	rule.Symbolizers = append(rule.Symbolizers, RawSymbolizer{Raster: &rule.RasterSymbolizers[len(rule.RasterSymbolizers)-1]})
}

func cssParam(name, value string) CssParameter { return CssParameter{Name: name, Value: value} }

func compileCSS(body string) (*StyledLayerDescriptor, error) {
	var rules []Rule
	for cursor := 0; ; {
		open := strings.Index(body[cursor:], "{")
		if open < 0 {
			if strings.TrimSpace(body[cursor:]) != "" {
				return nil, fmt.Errorf("CSS content outside a rule at byte %d", cursor)
			}
			break
		}
		open += cursor
		close := strings.Index(body[open+1:], "}")
		if close < 0 {
			return nil, fmt.Errorf("CSS rule at byte %d is missing a closing brace", open)
		}
		close += open + 1
		selector := strings.TrimSpace(body[cursor:open])
		if selector == "" {
			return nil, fmt.Errorf("CSS rule at byte %d has no selector", open)
		}
		ruleName, filter, selectorErr := parseCSSSelector(selector)
		if selectorErr != nil {
			return nil, selectorErr
		}
		declarations, err := parseCSSDeclarations(body[open+1 : close])
		if err != nil {
			return nil, err
		}
		rule, err := ruleFromFlatProperties(ruleName, declarations)
		if err != nil {
			return nil, err
		}
		rule.Filter = filter
		rules = append(rules, rule)
		cursor = close + 1
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("CSS style has no rules")
	}
	return canonicalDocument("style", rules), nil
}

var simpleFilterPattern = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_.-]*)\s*(<=|>=|!=|=|<|>)\s*(.*?)\s*$`)

func parseCSSSelector(selector string) (string, *Filter, error) {
	selector = strings.TrimSpace(selector)
	name := selector
	condition := ""
	if open := strings.Index(selector, "["); open >= 0 {
		close := strings.LastIndex(selector, "]")
		if close < open {
			return "", nil, fmt.Errorf("CSS selector %q has an unclosed filter", selector)
		}
		condition = selector[open+1 : close]
		name = strings.TrimSpace(selector[:open])
	}
	if name != "*" && !strings.HasPrefix(name, "#") {
		return "", nil, fmt.Errorf("unsupported CSS selector %q; use *, #layer, and an optional property comparison", selector)
	}
	filter, err := parseSimpleFilter(condition)
	return name, filter, err
}
func parseSimpleFilter(expression string) (*Filter, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, nil
	}
	matches := simpleFilterPattern.FindStringSubmatch(expression)
	if len(matches) != 4 {
		return nil, fmt.Errorf("unsupported style filter %q", expression)
	}
	comparison := PropertyComparison{PropertyName: matches[1], Literal: trimStyleScalar(matches[3])}
	filter := &Filter{}
	switch matches[2] {
	case "=":
		filter.PropertyIsEqualTo = []PropertyComparison{comparison}
	case "!=":
		filter.PropertyIsNotEqualTo = []PropertyComparison{comparison}
	case "<":
		filter.PropertyIsLessThan = []PropertyComparison{comparison}
	case "<=":
		filter.PropertyIsLessThanOrEqualTo = []PropertyComparison{comparison}
	case ">":
		filter.PropertyIsGreaterThan = []PropertyComparison{comparison}
	case ">=":
		filter.PropertyIsGreaterThanOrEqualTo = []PropertyComparison{comparison}
	}
	return filter, nil
}

func parseCSSDeclarations(body string) (map[string]string, error) {
	result := make(map[string]string)
	for _, declaration := range strings.Split(body, ";") {
		declaration = strings.TrimSpace(declaration)
		if declaration == "" {
			continue
		}
		parts := strings.SplitN(declaration, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid CSS declaration %q", declaration)
		}
		result[strings.ToLower(strings.TrimSpace(parts[0]))] = trimStyleScalar(parts[1])
	}
	return result, nil
}

func ruleFromFlatProperties(name string, properties map[string]string) (Rule, error) {
	rule := Rule{Name: name}
	known := map[string]bool{}
	use := func(keys ...string) {
		for _, key := range keys {
			known[key] = true
		}
	}
	if hasAny(properties, "mark", "mark-size", "mark-color", "mark-opacity", "mark-stroke", "mark-stroke-width") {
		use("mark", "mark-size", "mark-color", "mark-opacity", "mark-stroke", "mark-stroke-width")
		appendPoint(&rule, PointSymbolizer{Graphic: Graphic{Mark: &Mark{WellKnownName: valueOr(properties, "mark", "circle"), Fill: &Fill{CssParameters: []CssParameter{cssParam("fill", valueOr(properties, "mark-color", "#808080"))}}, Stroke: &Stroke{CssParameters: []CssParameter{cssParam("stroke", valueOr(properties, "mark-stroke", "#000000")), cssParam("stroke-width", valueOr(properties, "mark-stroke-width", "1"))}}}, Size: SourceChannelValue{Text: valueOr(properties, "mark-size", "10")}, Opacity: SourceChannelValue{Text: valueOr(properties, "mark-opacity", "1")}}})
	}
	if hasAny(properties, "fill", "fill-opacity") {
		use("fill", "fill-opacity")
		fill := &Fill{CssParameters: []CssParameter{cssParam("fill", valueOr(properties, "fill", "#808080")), cssParam("fill-opacity", valueOr(properties, "fill-opacity", "1"))}}
		var stroke *Stroke
		if hasAny(properties, "stroke", "stroke-width", "stroke-opacity") {
			use("stroke", "stroke-width", "stroke-opacity")
			stroke = flatStroke(properties)
		}
		appendPolygon(&rule, PolygonSymbolizer{Fill: fill, Stroke: stroke})
	} else if hasAny(properties, "stroke", "stroke-width", "stroke-opacity", "stroke-dasharray") {
		use("stroke", "stroke-width", "stroke-opacity", "stroke-dasharray")
		appendLine(&rule, LineSymbolizer{Stroke: flatStroke(properties)})
	}
	if hasAny(properties, "label", "font-family", "font-size", "font-style", "font-weight", "text-fill", "halo-color", "halo-radius") {
		use("label", "font-family", "font-size", "font-style", "font-weight", "text-fill", "halo-color", "halo-radius")
		label := valueOr(properties, "label", "")
		text := TextSymbolizer{Label: Label{Literal: label}, Font: &Font{CssParameters: []CssParameter{cssParam("font-family", valueOr(properties, "font-family", "sans-serif")), cssParam("font-size", valueOr(properties, "font-size", "12")), cssParam("font-style", valueOr(properties, "font-style", "normal")), cssParam("font-weight", valueOr(properties, "font-weight", "normal"))}}, Fill: &Fill{CssParameters: []CssParameter{cssParam("fill", valueOr(properties, "text-fill", "#000000"))}}}
		if property, ok := propertyReference(label); ok {
			text.Label = Label{PropertyName: property}
		}
		if hasAny(properties, "halo-color", "halo-radius") {
			text.Halo = &Halo{Radius: valueOr(properties, "halo-radius", "1"), Fill: &Fill{CssParameters: []CssParameter{cssParam("fill", valueOr(properties, "halo-color", "#ffffff"))}}}
		}
		appendText(&rule, text)
	}
	if opacity, ok := properties["raster-opacity"]; ok {
		use("raster-opacity")
		appendRaster(&rule, RasterSymbolizer{Opacity: SourceChannelValue{Text: opacity}})
	}
	for key := range properties {
		if !known[key] {
			return Rule{}, fmt.Errorf("unsupported style property %q", key)
		}
	}
	if len(rule.Symbolizers) == 0 {
		return Rule{}, fmt.Errorf("rule %q has no symbolizer properties", name)
	}
	return rule, nil
}

func flatStroke(properties map[string]string) *Stroke {
	params := []CssParameter{cssParam("stroke", valueOr(properties, "stroke", "#000000")), cssParam("stroke-width", valueOr(properties, "stroke-width", "1")), cssParam("stroke-opacity", valueOr(properties, "stroke-opacity", "1"))}
	if value := properties["stroke-dasharray"]; value != "" {
		params = append(params, cssParam("stroke-dasharray", value))
	}
	return &Stroke{CssParameters: params}
}

func hasAny(values map[string]string, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}
func valueOr(values map[string]string, key, fallback string) string {
	if value, ok := values[key]; ok {
		return value
	}
	return fallback
}
func trimStyleScalar(value string) string { return strings.Trim(strings.TrimSpace(value), "\"'") }
func propertyReference(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") && len(value) > 2 {
		return strings.TrimSpace(value[1 : len(value)-1]), true
	}
	return "", false
}

func compileYSLD(body string) (*StyledLayerDescriptor, error) {
	var root map[string]any
	if err := yaml.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("invalid YSLD: %w", err)
	}
	name := stringValue(root["name"])
	featureStyles, ok := sliceValue(root["feature-styles"])
	if !ok {
		featureStyles, ok = sliceValue(root["featureStyles"])
	}
	if !ok {
		return nil, fmt.Errorf("YSLD requires feature-styles")
	}
	var rules []Rule
	for _, featureStyleValue := range featureStyles {
		featureStyle, ok := mapValue(featureStyleValue)
		if !ok {
			return nil, fmt.Errorf("YSLD feature-style must be an object")
		}
		ruleValues, ok := sliceValue(featureStyle["rules"])
		if !ok {
			return nil, fmt.Errorf("YSLD feature-style requires rules")
		}
		for _, ruleValue := range ruleValues {
			ruleMap, ok := mapValue(ruleValue)
			if !ok {
				return nil, fmt.Errorf("YSLD rule must be an object")
			}
			rule := Rule{Name: stringValue(ruleMap["name"]), Title: stringValue(ruleMap["title"])}
			if filterText := stringValue(ruleMap["filter"]); filterText != "" {
				parsedFilter, filterErr := parseSimpleFilter(strings.Trim(filterText, "[]"))
				if filterErr != nil {
					return nil, filterErr
				}
				rule.Filter = parsedFilter
			}
			if scale, ok := mapValue(ruleMap["scale"]); ok {
				if value, valid := floatValue(scale["min"]); valid {
					rule.MinScaleDenom = value
				}
				if value, valid := floatValue(scale["max"]); valid {
					rule.MaxScaleDenom = value
				}
			}
			symbolizers, ok := sliceValue(ruleMap["symbolizers"])
			if !ok {
				return nil, fmt.Errorf("YSLD rule %q requires symbolizers", rule.Name)
			}
			for _, symbolizerValue := range symbolizers {
				symbolizer, ok := mapValue(symbolizerValue)
				if !ok || len(symbolizer) != 1 {
					return nil, fmt.Errorf("YSLD symbolizer must contain exactly one type")
				}
				for symbolizerType, propertiesValue := range symbolizer {
					propertiesAny, ok := mapValue(propertiesValue)
					if !ok {
						return nil, fmt.Errorf("YSLD %s symbolizer must be an object", symbolizerType)
					}
					properties := make(map[string]string)
					for key, value := range propertiesAny {
						properties[ysldProperty(symbolizerType, key)] = stringValue(value)
					}
					translated, err := ruleFromFlatProperties(rule.Name, properties)
					if err != nil {
						return nil, err
					}
					for _, raw := range translated.Symbolizers {
						switch {
						case raw.Point != nil:
							appendPoint(&rule, *raw.Point)
						case raw.Line != nil:
							appendLine(&rule, *raw.Line)
						case raw.Polygon != nil:
							appendPolygon(&rule, *raw.Polygon)
						case raw.Text != nil:
							appendText(&rule, *raw.Text)
						case raw.Raster != nil:
							appendRaster(&rule, *raw.Raster)
						}
					}
				}
			}
			rules = append(rules, rule)
		}
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("YSLD style has no rules")
	}
	return canonicalDocument(name, rules), nil
}

func ysldProperty(symbolizerType, key string) string {
	symbolizerType, key = strings.ToLower(symbolizerType), strings.ToLower(key)
	mapping := map[string]map[string]string{
		"mark":   {"well-known-name": "mark", "size": "mark-size", "color": "mark-color", "fill-color": "mark-color", "opacity": "mark-opacity", "stroke-color": "mark-stroke", "stroke-width": "mark-stroke-width"},
		"line":   {"color": "stroke", "width": "stroke-width", "opacity": "stroke-opacity", "dasharray": "stroke-dasharray"},
		"fill":   {"color": "fill", "opacity": "fill-opacity", "outline-color": "stroke", "outline-width": "stroke-width", "outline-opacity": "stroke-opacity"},
		"text":   {"label": "label", "font-family": "font-family", "font-size": "font-size", "font-style": "font-style", "font-weight": "font-weight", "color": "text-fill", "halo-color": "halo-color", "halo-radius": "halo-radius"},
		"raster": {"opacity": "raster-opacity"},
	}
	if value := mapping[symbolizerType][key]; value != "" {
		return value
	}
	return symbolizerType + "." + key
}

type mapboxStyle struct {
	Name   string        `json:"name"`
	Layers []mapboxLayer `json:"layers"`
}
type mapboxLayer struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	SourceLayer string         `json:"source-layer"`
	MinZoom     *float64       `json:"minzoom"`
	MaxZoom     *float64       `json:"maxzoom"`
	Paint       map[string]any `json:"paint"`
	Layout      map[string]any `json:"layout"`
	Filter      any            `json:"filter"`
}

func compileMapbox(body string) (*StyledLayerDescriptor, error) {
	var style mapboxStyle
	decoder := json.NewDecoder(strings.NewReader(body))
	if err := decoder.Decode(&style); err != nil {
		return nil, fmt.Errorf("invalid Mapbox style: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("Mapbox style must contain exactly one JSON document")
	}
	if len(style.Layers) == 0 {
		return nil, fmt.Errorf("Mapbox style has no layers")
	}
	var rules []Rule
	for index, layer := range style.Layers {
		if visibility, ok := layer.Layout["visibility"].(string); ok && visibility == "none" {
			continue
		}
		properties := make(map[string]string)
		set := func(target, source string, values map[string]any) error {
			if value, ok := values[source]; ok {
				scalar, err := mapboxProperty(source, value)
				if err != nil {
					section := "paint"
					if source == "text-field" || source == "text-size" {
						section = "layout"
					}
					return &stylePathError{Path: fmt.Sprintf("layers[%d].%s.%s", index, section, source), Err: err}
				}
				properties[target] = scalar
			}
			return nil
		}
		switch strings.ToLower(layer.Type) {
		case "circle":
			if err := validateMapboxKeys(layer.Paint, "circle-radius", "circle-color", "circle-opacity", "circle-stroke-color", "circle-stroke-width"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			if err := validateMapboxKeys(layer.Layout, "visibility"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			properties["mark"] = "circle"
			for target, source := range map[string]string{"mark-size": "circle-radius", "mark-color": "circle-color", "mark-opacity": "circle-opacity", "mark-stroke": "circle-stroke-color", "mark-stroke-width": "circle-stroke-width"} {
				if err := set(target, source, layer.Paint); err != nil {
					return nil, err
				}
			}
			if radius, err := strconv.ParseFloat(properties["mark-size"], 64); err == nil {
				properties["mark-size"] = strconv.FormatFloat(radius*2, 'g', -1, 64)
			} else if radius := properties["mark-size"]; IsDynamicExpression(radius) {
				properties["mark-size"] = "${2 * (" + radius[2:len(radius)-1] + ")}"
			}
		case "line":
			if err := validateMapboxKeys(layer.Paint, "line-color", "line-width", "line-opacity", "line-dasharray"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			if err := validateMapboxKeys(layer.Layout, "visibility"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			for target, source := range map[string]string{"stroke": "line-color", "stroke-width": "line-width", "stroke-opacity": "line-opacity", "stroke-dasharray": "line-dasharray"} {
				if err := set(target, source, layer.Paint); err != nil {
					return nil, err
				}
			}
		case "fill":
			if err := validateMapboxKeys(layer.Paint, "fill-color", "fill-opacity", "fill-outline-color"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			if err := validateMapboxKeys(layer.Layout, "visibility"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			for target, source := range map[string]string{"fill": "fill-color", "fill-opacity": "fill-opacity", "stroke": "fill-outline-color"} {
				if err := set(target, source, layer.Paint); err != nil {
					return nil, err
				}
			}
		case "symbol":
			if err := validateMapboxKeys(layer.Paint, "text-color", "text-halo-color", "text-halo-width"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			if err := validateMapboxKeys(layer.Layout, "visibility", "text-field", "text-size"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			for target, source := range map[string]string{"label": "text-field", "font-size": "text-size"} {
				if err := set(target, source, layer.Layout); err != nil {
					return nil, err
				}
			}
			for target, source := range map[string]string{"text-fill": "text-color", "halo-color": "text-halo-color", "halo-radius": "text-halo-width"} {
				if err := set(target, source, layer.Paint); err != nil {
					return nil, err
				}
			}
			if field := properties["label"]; strings.HasPrefix(field, "{") && strings.HasSuffix(field, "}") {
				properties["label"] = "[" + strings.TrimSpace(field[1:len(field)-1]) + "]"
			}
		case "raster":
			if err := validateMapboxKeys(layer.Paint, "raster-opacity"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			if err := validateMapboxKeys(layer.Layout, "visibility"); err != nil {
				return nil, fmt.Errorf("Mapbox layer %q: %w", layer.ID, err)
			}
			if err := set("raster-opacity", "raster-opacity", layer.Paint); err != nil {
				return nil, err
			}
			if len(properties) == 0 {
				properties["raster-opacity"] = "1"
			}
		case "background", "hillshade", "heatmap", "fill-extrusion":
			return nil, fmt.Errorf("Mapbox layer type %q is not supported", layer.Type)
		default:
			return nil, fmt.Errorf("unknown Mapbox layer type %q", layer.Type)
		}
		rule, err := ruleFromFlatProperties(layer.ID, properties)
		if err != nil {
			return nil, err
		}
		if layer.Filter != nil {
			rule.Filter, err = compileMapboxFilter(layer.Filter)
			if err != nil {
				return nil, fmt.Errorf("Mapbox layer %q filter: %w", layer.ID, err)
			}
		}
		if layer.MinZoom != nil {
			rule.MaxScaleDenom = zoomScale(*layer.MinZoom)
		}
		if layer.MaxZoom != nil {
			rule.MinScaleDenom = zoomScale(*layer.MaxZoom)
		}
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("Mapbox style has no visible layers")
	}
	return canonicalDocument(style.Name, rules), nil
}

func mapboxScalar(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case []any:
		if len(typed) > 0 {
			if operator, ok := typed[0].(string); ok && operator == "get" && len(typed) == 2 {
				property, ok := typed[1].(string)
				if ok && property != "" {
					property = strings.ReplaceAll(property, `\`, `\\`)
					return "${property('" + strings.ReplaceAll(property, "'", `\'`) + "')}", nil
				}
			}
		}
		return "", fmt.Errorf("unsupported expression: only [\"get\", \"property\"] is supported")
	default:
		return "", fmt.Errorf("value must be a scalar or supported get expression")
	}
}

func validateMapboxKeys(values map[string]any, allowed ...string) error {
	accepted := map[string]bool{}
	for _, key := range allowed {
		accepted[key] = true
	}
	for key := range values {
		if !accepted[key] {
			return fmt.Errorf("unsupported property %q", key)
		}
	}
	return nil
}
func compileMapboxFilter(value any) (*Filter, error) {
	parts, ok := value.([]any)
	if !ok || len(parts) != 3 {
		return nil, fmt.Errorf("only three-part comparison filters are supported")
	}
	operator, ok := parts[0].(string)
	if !ok {
		return nil, fmt.Errorf("filter operator must be a string")
	}
	if operator == "==" {
		operator = "="
	}
	property := ""
	switch left := parts[1].(type) {
	case string:
		property = left
	case []any:
		if len(left) == 2 && left[0] == "get" {
			property, _ = left[1].(string)
		}
	}
	if property == "" {
		return nil, fmt.Errorf("filter left operand must name a property")
	}
	literal, err := mapboxScalar(parts[2])
	if err != nil {
		return nil, err
	}
	return parseSimpleFilter(property + " " + operator + " " + literal)
}

func zoomScale(zoom float64) float64 { return 559082264.0287178 / math.Pow(2, math.Max(0, zoom)) }
func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}
func mapValue(value any) (map[string]any, bool) {
	typed, ok := value.(map[string]any)
	return typed, ok
}
func sliceValue(value any) ([]any, bool) { typed, ok := value.([]any); return typed, ok }
func floatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case string:
		result, err := strconv.ParseFloat(typed, 64)
		return result, err == nil
	}
	return 0, false
}

// StableKeys is useful to callers which want deterministic diagnostics.
func StableKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
