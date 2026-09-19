// Package sld provides parsing and evaluation of OGC Styled Layer Descriptor (SLD) documents.
package sld

import (
	"encoding/xml"
	"image/color"
	"regexp"
	"strconv"
	"strings"
)

// StyledLayerDescriptor is the root element of an SLD document.
type StyledLayerDescriptor struct {
	XMLName     xml.Name     `xml:"StyledLayerDescriptor"`
	Version     string       `xml:"version,attr"`
	NamedLayers []NamedLayer `xml:"NamedLayer"`
}

// NamedLayer represents a named layer in an SLD document.
type NamedLayer struct {
	Name       string      `xml:"Name"`
	UserStyles []UserStyle `xml:"UserStyle"`
}

// UserStyle represents a user-defined style.
type UserStyle struct {
	Name              string             `xml:"Name"`
	Title             string             `xml:"Title"`
	IsDefault         bool               `xml:"IsDefault"`
	FeatureTypeStyles []FeatureTypeStyle `xml:"FeatureTypeStyle"`
}

// FeatureTypeStyle contains styling rules for a feature type.
type FeatureTypeStyle struct {
	Name           string          `xml:"Name"`
	Transformation *Transformation `xml:"Transformation"`
	VendorOptions  []VendorOption  `xml:"VendorOption"`
	Rules          []Rule          `xml:"Rule"`
}

type Transformation struct {
	Function OGCFunction `xml:"Function"`
}

// Rule represents a styling rule with optional filter and symbolizers.
type Rule struct {
	Name              string              `xml:"Name"`
	Title             string              `xml:"Title"`
	Filter            *Filter             `xml:"Filter"`
	MinScaleDenom     float64             `xml:"MinScaleDenominator"`
	MaxScaleDenom     float64             `xml:"MaxScaleDenominator"`
	PointSymbolizers  []PointSymbolizer   `xml:"PointSymbolizer"`
	LineSymbolizers   []LineSymbolizer    `xml:"LineSymbolizer"`
	PolygonSymbolizer []PolygonSymbolizer `xml:"PolygonSymbolizer"`
	TextSymbolizers   []TextSymbolizer    `xml:"TextSymbolizer"`
	RasterSymbolizers []RasterSymbolizer  `xml:"RasterSymbolizer"`
	ElseFilter        *struct{}           `xml:"ElseFilter"`
	// Symbolizers preserves the XML painter order. The typed slices above are
	// retained for compatibility with callers that build rules directly.
	Symbolizers []RawSymbolizer `xml:"-"`
}

// RawSymbolizer is one parsed SLD symbolizer in document order.
type RawSymbolizer struct {
	Point   *PointSymbolizer
	Line    *LineSymbolizer
	Polygon *PolygonSymbolizer
	Text    *TextSymbolizer
	Raster  *RasterSymbolizer
}

// Filter represents an OGC filter expression.
type Filter struct {
	// Comparison operators
	PropertyIsEqualTo              []PropertyComparison `xml:"PropertyIsEqualTo"`
	PropertyIsNotEqualTo           []PropertyComparison `xml:"PropertyIsNotEqualTo"`
	PropertyIsLessThan             []PropertyComparison `xml:"PropertyIsLessThan"`
	PropertyIsLessThanOrEqualTo    []PropertyComparison `xml:"PropertyIsLessThanOrEqualTo"`
	PropertyIsGreaterThan          []PropertyComparison `xml:"PropertyIsGreaterThan"`
	PropertyIsGreaterThanOrEqualTo []PropertyComparison `xml:"PropertyIsGreaterThanOrEqualTo"`
	PropertyIsLike                 []PropertyIsLike     `xml:"PropertyIsLike"`
	PropertyIsNull                 []PropertyIsNull     `xml:"PropertyIsNull"`
	PropertyIsBetween              []PropertyIsBetween  `xml:"PropertyIsBetween"`

	// Logical operators
	And []Filter `xml:"And"`
	Or  []Filter `xml:"Or"`
	Not *Filter  `xml:"Not"`
}

// PropertyComparison represents a property comparison filter.
type PropertyComparison struct {
	PropertyName string `xml:"PropertyName"`
	Literal      string `xml:"Literal"`
}

// PropertyIsLike represents a LIKE comparison.
type PropertyIsLike struct {
	PropertyName  string         `xml:"PropertyName"`
	Literal       string         `xml:"Literal"`
	WildCard      string         `xml:"wildCard,attr"`
	SingleChar    string         `xml:"singleChar,attr"`
	EscapeChar    string         `xml:"escapeChar,attr"`
	CompiledRegex *regexp.Regexp `xml:"-"` // Pre-compiled regex pattern (not serialized)
}

// PropertyIsNull represents a NULL check.
type PropertyIsNull struct {
	PropertyName string `xml:"PropertyName"`
}

// CompilePatterns pre-compiles all LIKE patterns in the filter tree.
// This should be called once after parsing to avoid repeated regex compilation.
func (f *Filter) CompilePatterns() {
	if f == nil {
		return
	}

	// Compile LIKE patterns
	for i := range f.PropertyIsLike {
		f.PropertyIsLike[i].Compile()
	}

	// Recurse into logical operators
	for i := range f.And {
		f.And[i].CompilePatterns()
	}
	for i := range f.Or {
		f.Or[i].CompilePatterns()
	}
	if f.Not != nil {
		f.Not.CompilePatterns()
	}
}

// Compile pre-compiles the LIKE pattern into a regex.
func (p *PropertyIsLike) Compile() {
	wildCard := p.WildCard
	if wildCard == "" {
		wildCard = "*"
	}
	singleChar := p.SingleChar
	if singleChar == "" {
		singleChar = "?"
	}

	// Escape regex special characters
	regexPattern := regexp.QuoteMeta(p.Literal)

	// Replace wildcard with .*
	regexPattern = stringReplaceAll(regexPattern, regexp.QuoteMeta(wildCard), ".*")

	// Replace single char with .
	regexPattern = stringReplaceAll(regexPattern, regexp.QuoteMeta(singleChar), ".")

	// Anchor the pattern
	regexPattern = "^" + regexPattern + "$"

	re, err := regexp.Compile(regexPattern)
	if err == nil {
		p.CompiledRegex = re
	}
}

// stringReplaceAll replaces all occurrences of old with new in s.
// This is a simple implementation to avoid importing strings package.
func stringReplaceAll(s, old, new string) string {
	if old == "" {
		return s
	}
	var result []byte
	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result = append(result, new...)
			i += len(old)
		} else {
			result = append(result, s[i])
			i++
		}
	}
	return string(result)
}

// PropertyIsBetween represents a BETWEEN comparison.
type PropertyIsBetween struct {
	PropertyName string `xml:"PropertyName"`
	LowerBound   string `xml:"LowerBoundary>Literal"`
	UpperBound   string `xml:"UpperBoundary>Literal"`
}

// PointSymbolizer defines how to render point features.
type PointSymbolizer struct {
	Graphic       Graphic        `xml:"Graphic"`
	VendorOptions []VendorOption `xml:"VendorOption"`
}

// Graphic represents a graphic symbol.
type Graphic struct {
	Mark            *Mark              `xml:"Mark"`
	ExternalGraphic *ExternalGraphic   `xml:"ExternalGraphic"`
	Size            SourceChannelValue `xml:"Size"`
	Rotation        SourceChannelValue `xml:"Rotation"`
	Opacity         SourceChannelValue `xml:"Opacity"`
}

// Mark represents a well-known mark symbol.
type Mark struct {
	WellKnownName string  `xml:"WellKnownName"`
	Fill          *Fill   `xml:"Fill"`
	Stroke        *Stroke `xml:"Stroke"`
}

// ExternalGraphic represents an external image.
type ExternalGraphic struct {
	OnlineResource OnlineResource `xml:"OnlineResource"`
	Format         string         `xml:"Format"`
}

// OnlineResource represents an external resource reference.
type OnlineResource struct {
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr"`
}

// LineSymbolizer defines how to render line features.
type LineSymbolizer struct {
	Stroke              *Stroke        `xml:"Stroke"`
	PerpendicularOffset string         `xml:"PerpendicularOffset"`
	VendorOptions       []VendorOption `xml:"VendorOption"`
}

// PolygonSymbolizer defines how to render polygon features.
type PolygonSymbolizer struct {
	Fill          *Fill          `xml:"Fill"`
	Stroke        *Stroke        `xml:"Stroke"`
	VendorOptions []VendorOption `xml:"VendorOption"`
}

// TextSymbolizer defines how to render text labels.
type TextSymbolizer struct {
	Label          Label           `xml:"Label"`
	Font           *Font           `xml:"Font"`
	Fill           *Fill           `xml:"Fill"`
	Halo           *Halo           `xml:"Halo"`
	LabelPlacement *LabelPlacement `xml:"LabelPlacement"`
	Priority       string          `xml:"Priority"`
	VendorOptions  []VendorOption  `xml:"VendorOption"`
}

// RasterSymbolizer defines portrayal for a grid coverage. This implementation
// supports the standards-focused subset used by WMS and map tiles.
type RasterSymbolizer struct {
	Opacity             SourceChannelValue   `xml:"Opacity"`
	ChannelSelection    *ChannelSelection    `xml:"ChannelSelection"`
	ColorMap            *ColorMap            `xml:"ColorMap"`
	ContrastEnhancement *ContrastEnhancement `xml:"ContrastEnhancement"`
	ShadedRelief        *struct{}            `xml:"ShadedRelief"`
	OverlapBehavior     *struct{}            `xml:"OverlapBehavior"`
	ImageOutline        *struct{}            `xml:"ImageOutline"`
}

type ChannelSelection struct {
	Red   *SelectedChannel `xml:"RedChannel"`
	Green *SelectedChannel `xml:"GreenChannel"`
	Blue  *SelectedChannel `xml:"BlueChannel"`
	Gray  *SelectedChannel `xml:"GrayChannel"`
}

type SelectedChannel struct {
	SourceChannel       SourceChannelValue   `xml:"SourceChannelName"`
	ContrastEnhancement *ContrastEnhancement `xml:"ContrastEnhancement"`
}

type SourceChannelValue struct {
	Text     string       `xml:",chardata"`
	Function *OGCFunction `xml:"Function"`
}

func (v SourceChannelValue) Value() string {
	if v.Function != nil {
		return v.Function.Expression()
	}
	return strings.TrimSpace(v.Text)
}

type ContrastEnhancement struct {
	Normalize  *Normalize `xml:"Normalize"`
	Histogram  *struct{}  `xml:"Histogram"`
	GammaValue string     `xml:"GammaValue"`
}

type Normalize struct {
	VendorOptions []VendorOption `xml:"VendorOption"`
}

// VendorOption captures optional renderer extensions without making them part
// of the standards-only execution path.
type VendorOption struct {
	Name     string       `xml:"name,attr"`
	Value    string       `xml:",chardata"`
	Function *OGCFunction `xml:"Function"`
}

func (v VendorOption) Expression() string {
	if v.Function != nil {
		return v.Function.Expression()
	}
	return strings.TrimSpace(v.Value)
}

type OGCFunction struct {
	Name       string        `xml:"name,attr"`
	Literals   []OGCLiteral  `xml:"Literal"`
	Properties []string      `xml:"PropertyName"`
	Functions  []OGCFunction `xml:"Function"`
	Arguments  []OGCArgument `xml:"-"`
}

type OGCArgument struct {
	Literal, Property string
	Function          *OGCFunction
}

type OGCLiteral struct {
	Value string `xml:",chardata"`
}

func (f OGCFunction) Expression() string {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", `\'`) + "'" }
	var arguments []string
	if len(f.Arguments) > 0 {
		for _, argument := range f.Arguments {
			switch {
			case argument.Function != nil:
				expression := strings.TrimSuffix(strings.TrimPrefix(argument.Function.Expression(), "${"), "}")
				arguments = append(arguments, expression)
			case argument.Property != "":
				arguments = append(arguments, "property("+quote(strings.TrimSpace(argument.Property))+")")
			default:
				value := strings.TrimSpace(argument.Literal)
				if _, err := strconv.ParseFloat(value, 64); err != nil {
					value = quote(value)
				}
				arguments = append(arguments, value)
			}
		}
		return "${" + strings.ToLower(strings.TrimSpace(f.Name)) + "(" + strings.Join(arguments, ", ") + ")}"
	}
	for _, literal := range f.Literals {
		value := strings.TrimSpace(literal.Value)
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			value = quote(value)
		}
		arguments = append(arguments, value)
	}
	for _, property := range f.Properties {
		arguments = append(arguments, "property("+quote(strings.TrimSpace(property))+")")
	}
	for _, child := range f.Functions {
		expression := strings.TrimSuffix(strings.TrimPrefix(child.Expression(), "${"), "}")
		arguments = append(arguments, expression)
	}
	if strings.TrimSpace(f.Name) == "" {
		return ""
	}
	return "${" + strings.ToLower(strings.TrimSpace(f.Name)) + "(" + strings.Join(arguments, ", ") + ")}"
}

type ColorMap struct {
	Type     string          `xml:"type,attr"`
	Extended bool            `xml:"extended,attr"`
	Entries  []ColorMapEntry `xml:"ColorMapEntry"`
}

type ColorMapEntry struct {
	Color    string `xml:"color,attr"`
	Quantity string `xml:"quantity,attr"`
	Opacity  string `xml:"opacity,attr"`
	Label    string `xml:"label,attr"`
}

// Label represents the text content for a label.
type Label struct {
	PropertyName string       `xml:"PropertyName"`
	Literal      string       `xml:",chardata"`
	Function     *OGCFunction `xml:"Function"`
}

func (l Label) Expression() string {
	if l.Function != nil {
		return l.Function.Expression()
	}
	if strings.TrimSpace(l.PropertyName) != "" {
		return "${property('" + strings.ReplaceAll(strings.TrimSpace(l.PropertyName), "'", `\'`) + "')}"
	}
	return strings.TrimSpace(l.Literal)
}

// Font represents font styling.
type Font struct {
	CssParameters []CssParameter `xml:"CssParameter"`
}

// Halo represents a text halo/outline.
type Halo struct {
	Radius string `xml:"Radius"`
	Fill   *Fill  `xml:"Fill"`
}

// LabelPlacement defines label positioning.
type LabelPlacement struct {
	PointPlacement *PointPlacement `xml:"PointPlacement"`
	LinePlacement  *LinePlacement  `xml:"LinePlacement"`
}

// PointPlacement defines label positioning for points.
type PointPlacement struct {
	AnchorPoint  *AnchorPoint  `xml:"AnchorPoint"`
	Displacement *Displacement `xml:"Displacement"`
	Rotation     string        `xml:"Rotation"`
}

// LinePlacement defines label positioning along lines.
type LinePlacement struct {
	PerpendicularOffset string `xml:"PerpendicularOffset"`
}

// AnchorPoint defines the anchor point for labels.
type AnchorPoint struct {
	AnchorPointX string `xml:"AnchorPointX"`
	AnchorPointY string `xml:"AnchorPointY"`
}

// Displacement defines label displacement.
type Displacement struct {
	DisplacementX string `xml:"DisplacementX"`
	DisplacementY string `xml:"DisplacementY"`
}

// Fill represents fill styling.
type Fill struct {
	CssParameters []CssParameter `xml:"CssParameter"`
	GraphicFill   *GraphicFill   `xml:"GraphicFill"`
}

// GraphicFill represents a pattern fill.
type GraphicFill struct {
	Graphic Graphic `xml:"Graphic"`
}

// Stroke represents stroke/line styling.
type Stroke struct {
	CssParameters []CssParameter `xml:"CssParameter"`
	GraphicStroke *GraphicStroke `xml:"GraphicStroke"`
}

// GraphicStroke represents a pattern stroke.
type GraphicStroke struct {
	Graphic    Graphic `xml:"Graphic"`
	InitialGap string  `xml:"InitialGap"`
	Gap        string  `xml:"Gap"`
}

// CssParameter represents a CSS-like styling parameter.
type CssParameter struct {
	Name         string       `xml:"name,attr"`
	Value        string       `xml:",chardata"`
	PropertyName string       `xml:"PropertyName"`
	Function     *OGCFunction `xml:"Function"`
}

func (p CssParameter) Expression() string {
	if p.Function != nil {
		return p.Function.Expression()
	}
	if strings.TrimSpace(p.PropertyName) != "" {
		return "${property('" + strings.ReplaceAll(strings.TrimSpace(p.PropertyName), "'", `\'`) + "')}"
	}
	return strings.TrimSpace(p.Value)
}

// Style represents a resolved, ready-to-use style for rendering.
type Style struct {
	Name             string
	Title            string
	Rules            []ResolvedRule
	Composite        string
	CompositeOpacity float64
	CompositeBase    bool
	SortBy           string
	SortDescending   bool
	Transformation   *ResolvedTransformation
}

type ResolvedTransformation struct {
	Name           string
	Radius         float64
	WeightProperty string
	Levels         []float64
	Parameters     map[string]string
}

// ResolvedRule is a rule with resolved styling values.
type ResolvedRule struct {
	Name             string
	Title            string
	Filter           *Filter
	MinScale         float64
	MaxScale         float64
	PointStyle       *PointStyle
	LineStyle        *LineStyle
	PolygonStyle     *PolygonStyle
	TextStyle        *TextStyle
	RasterStyle      *RasterStyle
	ElseFilter       bool
	Symbolizers      []ResolvedSymbolizer
	FeatureTypeStyle int
}

// ResolvedSymbolizer is a tagged union that preserves painter order.
type ResolvedSymbolizer struct {
	Point   *PointStyle
	Line    *LineStyle
	Polygon *PolygonStyle
	Text    *TextStyle
	Raster  *RasterStyle
}

type RasterStyle struct {
	Opacity             float64
	OpacityExpression   string
	Channels            RasterChannels
	ColorMap            *ResolvedColorMap
	ContrastEnhancement *ResolvedContrastEnhancement
}

type RasterChannels struct {
	Gray  *ResolvedChannel
	Red   *ResolvedChannel
	Green *ResolvedChannel
	Blue  *ResolvedChannel
}

type ResolvedChannel struct {
	Name                string
	NameExpression      string
	ContrastEnhancement *ResolvedContrastEnhancement
}

type ResolvedContrastEnhancement struct {
	Normalize           bool
	Histogram           bool
	Gamma               float64
	Algorithm           string
	AlgorithmExpression string
	MinValue            *float64
	MaxValue            *float64
	MinValueExpression  string
	MaxValueExpression  string
}

type ResolvedColorMap struct {
	Type     string
	Extended bool
	Entries  []ResolvedColorMapEntry
}

type ResolvedColorMapEntry struct {
	Color              color.RGBA
	Quantity           float64
	Opacity            float64
	Label              string
	ColorExpression    string
	QuantityExpression string
	OpacityExpression  string
	LabelExpression    string
}

// PointStyle holds resolved point styling.
type PointStyle struct {
	Shape                                                             string // circle, square, triangle, star, cross, x
	Size                                                              float64
	Rotation                                                          float64
	Opacity                                                           float64
	FillColor                                                         color.RGBA
	StrokeColor                                                       color.RGBA
	StrokeWidth                                                       float64
	ExternalGraphic                                                   *ResolvedExternalGraphic
	SizeExpression, RotationExpression, OpacityExpression             string
	FillColorExpression, StrokeColorExpression, StrokeWidthExpression string
	LabelObstacle                                                     bool
	ObstacleMargin                                                    float64
}

type ResolvedExternalGraphic struct {
	Href   string
	Format string
}

// GraphicPattern is used by GraphicStroke and GraphicFill. Point contains
// either a well-known mark or an ExternalGraphic reference.
type GraphicPattern struct {
	Point      *PointStyle
	Gap        float64
	InitialGap float64
}

// LineStyle holds resolved line styling.
type LineStyle struct {
	Color                                                                     color.RGBA
	Width                                                                     float64
	Opacity                                                                   float64
	DashArray                                                                 []float64
	DashOffset                                                                float64
	LineCap                                                                   string // butt, round, square
	LineJoin                                                                  string // miter, round, bevel
	GraphicStroke                                                             *GraphicPattern
	ColorExpression, WidthExpression, OpacityExpression, DashOffsetExpression string
	LabelObstacle                                                             bool
	ObstacleMargin                                                            float64
}

// PolygonStyle holds resolved polygon styling.
type PolygonStyle struct {
	FillColor                                                             color.RGBA
	FillOpacity                                                           float64
	StrokeColor                                                           color.RGBA
	StrokeWidth                                                           float64
	StrokeOpacity                                                         float64
	GraphicFill                                                           *GraphicPattern
	GraphicStroke                                                         *GraphicPattern
	FillColorExpression, FillOpacityExpression                            string
	StrokeColorExpression, StrokeWidthExpression, StrokeOpacityExpression string
	LabelObstacle                                                         bool
	ObstacleMargin                                                        float64
}

// TextStyle holds resolved text styling.
type TextStyle struct {
	PropertyName                                               string
	Literal                                                    string
	FontFamily                                                 string
	FontSize                                                   float64
	FontStyle                                                  string // normal, italic, oblique
	FontWeight                                                 string // normal, bold
	Color                                                      color.RGBA
	HaloColor                                                  color.RGBA
	HaloRadius                                                 float64
	AnchorX                                                    float64
	AnchorY                                                    float64
	DisplacementX                                              float64
	DisplacementY                                              float64
	Rotation                                                   float64
	Priority                                                   float64
	ConflictResolution                                         bool
	SpaceAround                                                float64
	FollowLine                                                 bool
	Repeat                                                     float64
	MaxDisplacement                                            float64
	MaxAngleDelta                                              float64
	Partials                                                   bool
	Group                                                      string
	LabelAllGroup                                              bool
	AutoWrap                                                   int
	ForceLeftToRight                                           bool
	GoodnessOfFit                                              float64
	PolygonAlign                                               string
	Advanced                                                   bool
	LabelExpression, FontFamilyExpression, FontSizeExpression  string
	ColorExpression, HaloColorExpression, HaloRadiusExpression string
	RotationExpression, PriorityExpression                     string
}

// WellKnownMarks defines the standard SLD mark shapes.
var WellKnownMarks = map[string]bool{
	"circle":   true,
	"square":   true,
	"triangle": true,
	"star":     true,
	"cross":    true,
	"x":        true,
}
