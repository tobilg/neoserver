package wfs

import (
	"encoding/xml"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// FESFilter represents a parsed FES 2.0 filter.
type FESFilter struct {
	XMLName xml.Name // Accept any root element name (Filter with or without namespace)
	And     *FESAnd  `xml:"And"`
	Or      *FESOr   `xml:"Or"`
	Not     *FESNot  `xml:"Not"`
	// Comparison predicates
	PropertyIsEqualTo              *FESComparison        `xml:"PropertyIsEqualTo"`
	PropertyIsNotEqualTo           *FESComparison        `xml:"PropertyIsNotEqualTo"`
	PropertyIsLessThan             *FESComparison        `xml:"PropertyIsLessThan"`
	PropertyIsGreaterThan          *FESComparison        `xml:"PropertyIsGreaterThan"`
	PropertyIsLessThanOrEqualTo    *FESComparison        `xml:"PropertyIsLessThanOrEqualTo"`
	PropertyIsGreaterThanOrEqualTo *FESComparison        `xml:"PropertyIsGreaterThanOrEqualTo"`
	PropertyIsLike                 *FESPropertyIsLike    `xml:"PropertyIsLike"`
	PropertyIsNull                 *FESPropertyIsNull    `xml:"PropertyIsNull"`
	PropertyIsNil                  *FESPropertyIsNil     `xml:"PropertyIsNil"`
	PropertyIsBetween              *FESPropertyIsBetween `xml:"PropertyIsBetween"`
	// Spatial predicates
	BBOX       *FESBBOX    `xml:"BBOX"`
	Intersects *FESSpatial `xml:"Intersects"`
	Within     *FESSpatial `xml:"Within"`
	Contains   *FESSpatial `xml:"Contains"`
	Disjoint   *FESSpatial `xml:"Disjoint"`
	Touches    *FESSpatial `xml:"Touches"`
	Crosses    *FESSpatial `xml:"Crosses"`
	Overlaps   *FESSpatial `xml:"Overlaps"`
	DWithin    *FESDWithin `xml:"DWithin"`
	// Temporal predicates
	After        *FESTemporalAfter        `xml:"After"`
	Before       *FESTemporalBefore       `xml:"Before"`
	During       *FESTemporalDuring       `xml:"During"`
	Begins       *FESTemporalBegins       `xml:"Begins"`
	BegunBy      *FESTemporalBegunBy      `xml:"BegunBy"`
	TContains    *FESTemporalTContains    `xml:"TContains"`
	TEquals      *FESTemporalTEquals      `xml:"TEquals"`
	TOverlaps    *FESTemporalTOverlaps    `xml:"TOverlaps"`
	Meets        *FESTemporalMeets        `xml:"Meets"`
	OverlappedBy *FESTemporalOverlappedBy `xml:"OverlappedBy"`
	MetBy        *FESTemporalMetBy        `xml:"MetBy"`
	Ends         *FESTemporalEnds         `xml:"Ends"`
	EndedBy      *FESTemporalEndedBy      `xml:"EndedBy"`
	AnyInteracts *FESTemporalAnyInteracts `xml:"AnyInteracts"`
	// Resource ID
	ResourceId []FESResourceId `xml:"ResourceId"`
}

// FESAnd represents an AND logical operator.
// Each field represents a possible child predicate type.
type FESAnd struct {
	And                            []*FESAnd               `xml:"And"`
	Or                             []*FESOr                `xml:"Or"`
	Not                            []*FESNot               `xml:"Not"`
	PropertyIsEqualTo              []*FESComparison        `xml:"PropertyIsEqualTo"`
	PropertyIsNotEqualTo           []*FESComparison        `xml:"PropertyIsNotEqualTo"`
	PropertyIsLessThan             []*FESComparison        `xml:"PropertyIsLessThan"`
	PropertyIsGreaterThan          []*FESComparison        `xml:"PropertyIsGreaterThan"`
	PropertyIsLessThanOrEqualTo    []*FESComparison        `xml:"PropertyIsLessThanOrEqualTo"`
	PropertyIsGreaterThanOrEqualTo []*FESComparison        `xml:"PropertyIsGreaterThanOrEqualTo"`
	PropertyIsLike                 []*FESPropertyIsLike    `xml:"PropertyIsLike"`
	PropertyIsNull                 []*FESPropertyIsNull    `xml:"PropertyIsNull"`
	PropertyIsNil                  []*FESPropertyIsNil     `xml:"PropertyIsNil"`
	PropertyIsBetween              []*FESPropertyIsBetween `xml:"PropertyIsBetween"`
	BBOX                           []*FESBBOX              `xml:"BBOX"`
	Intersects                     []*FESSpatial           `xml:"Intersects"`
	Within                         []*FESSpatial           `xml:"Within"`
	Contains                       []*FESSpatial           `xml:"Contains"`
	Disjoint                       []*FESSpatial           `xml:"Disjoint"`
	Touches                        []*FESSpatial           `xml:"Touches"`
	Crosses                        []*FESSpatial           `xml:"Crosses"`
	Overlaps                       []*FESSpatial           `xml:"Overlaps"`
	DWithin                        []*FESDWithin           `xml:"DWithin"`
	// Temporal predicates
	After        []*FESTemporalAfter        `xml:"After"`
	Before       []*FESTemporalBefore       `xml:"Before"`
	During       []*FESTemporalDuring       `xml:"During"`
	Begins       []*FESTemporalBegins       `xml:"Begins"`
	BegunBy      []*FESTemporalBegunBy      `xml:"BegunBy"`
	TContains    []*FESTemporalTContains    `xml:"TContains"`
	TEquals      []*FESTemporalTEquals      `xml:"TEquals"`
	TOverlaps    []*FESTemporalTOverlaps    `xml:"TOverlaps"`
	Meets        []*FESTemporalMeets        `xml:"Meets"`
	OverlappedBy []*FESTemporalOverlappedBy `xml:"OverlappedBy"`
	MetBy        []*FESTemporalMetBy        `xml:"MetBy"`
	Ends         []*FESTemporalEnds         `xml:"Ends"`
	EndedBy      []*FESTemporalEndedBy      `xml:"EndedBy"`
	AnyInteracts []*FESTemporalAnyInteracts `xml:"AnyInteracts"`
	ResourceId   []FESResourceId            `xml:"ResourceId"`
}

// FESOr represents an OR logical operator.
type FESOr struct {
	And                            []*FESAnd               `xml:"And"`
	Or                             []*FESOr                `xml:"Or"`
	Not                            []*FESNot               `xml:"Not"`
	PropertyIsEqualTo              []*FESComparison        `xml:"PropertyIsEqualTo"`
	PropertyIsNotEqualTo           []*FESComparison        `xml:"PropertyIsNotEqualTo"`
	PropertyIsLessThan             []*FESComparison        `xml:"PropertyIsLessThan"`
	PropertyIsGreaterThan          []*FESComparison        `xml:"PropertyIsGreaterThan"`
	PropertyIsLessThanOrEqualTo    []*FESComparison        `xml:"PropertyIsLessThanOrEqualTo"`
	PropertyIsGreaterThanOrEqualTo []*FESComparison        `xml:"PropertyIsGreaterThanOrEqualTo"`
	PropertyIsLike                 []*FESPropertyIsLike    `xml:"PropertyIsLike"`
	PropertyIsNull                 []*FESPropertyIsNull    `xml:"PropertyIsNull"`
	PropertyIsNil                  []*FESPropertyIsNil     `xml:"PropertyIsNil"`
	PropertyIsBetween              []*FESPropertyIsBetween `xml:"PropertyIsBetween"`
	BBOX                           []*FESBBOX              `xml:"BBOX"`
	Intersects                     []*FESSpatial           `xml:"Intersects"`
	Within                         []*FESSpatial           `xml:"Within"`
	Contains                       []*FESSpatial           `xml:"Contains"`
	Disjoint                       []*FESSpatial           `xml:"Disjoint"`
	Touches                        []*FESSpatial           `xml:"Touches"`
	Crosses                        []*FESSpatial           `xml:"Crosses"`
	Overlaps                       []*FESSpatial           `xml:"Overlaps"`
	DWithin                        []*FESDWithin           `xml:"DWithin"`
	// Temporal predicates
	After        []*FESTemporalAfter        `xml:"After"`
	Before       []*FESTemporalBefore       `xml:"Before"`
	During       []*FESTemporalDuring       `xml:"During"`
	Begins       []*FESTemporalBegins       `xml:"Begins"`
	BegunBy      []*FESTemporalBegunBy      `xml:"BegunBy"`
	TContains    []*FESTemporalTContains    `xml:"TContains"`
	TEquals      []*FESTemporalTEquals      `xml:"TEquals"`
	TOverlaps    []*FESTemporalTOverlaps    `xml:"TOverlaps"`
	Meets        []*FESTemporalMeets        `xml:"Meets"`
	OverlappedBy []*FESTemporalOverlappedBy `xml:"OverlappedBy"`
	MetBy        []*FESTemporalMetBy        `xml:"MetBy"`
	Ends         []*FESTemporalEnds         `xml:"Ends"`
	EndedBy      []*FESTemporalEndedBy      `xml:"EndedBy"`
	AnyInteracts []*FESTemporalAnyInteracts `xml:"AnyInteracts"`
	ResourceId   []FESResourceId            `xml:"ResourceId"`
}

// FESNot represents a NOT logical operator.
type FESNot struct {
	And                            *FESAnd               `xml:"And"`
	Or                             *FESOr                `xml:"Or"`
	Not                            *FESNot               `xml:"Not"`
	PropertyIsEqualTo              *FESComparison        `xml:"PropertyIsEqualTo"`
	PropertyIsNotEqualTo           *FESComparison        `xml:"PropertyIsNotEqualTo"`
	PropertyIsLessThan             *FESComparison        `xml:"PropertyIsLessThan"`
	PropertyIsGreaterThan          *FESComparison        `xml:"PropertyIsGreaterThan"`
	PropertyIsLessThanOrEqualTo    *FESComparison        `xml:"PropertyIsLessThanOrEqualTo"`
	PropertyIsGreaterThanOrEqualTo *FESComparison        `xml:"PropertyIsGreaterThanOrEqualTo"`
	PropertyIsLike                 *FESPropertyIsLike    `xml:"PropertyIsLike"`
	PropertyIsNull                 *FESPropertyIsNull    `xml:"PropertyIsNull"`
	PropertyIsNil                  *FESPropertyIsNil     `xml:"PropertyIsNil"`
	PropertyIsBetween              *FESPropertyIsBetween `xml:"PropertyIsBetween"`
	BBOX                           *FESBBOX              `xml:"BBOX"`
	Intersects                     *FESSpatial           `xml:"Intersects"`
	Within                         *FESSpatial           `xml:"Within"`
	Contains                       *FESSpatial           `xml:"Contains"`
	Disjoint                       *FESSpatial           `xml:"Disjoint"`
	Touches                        *FESSpatial           `xml:"Touches"`
	Crosses                        *FESSpatial           `xml:"Crosses"`
	Overlaps                       *FESSpatial           `xml:"Overlaps"`
	DWithin                        *FESDWithin           `xml:"DWithin"`
	// Temporal predicates
	After        *FESTemporalAfter        `xml:"After"`
	Before       *FESTemporalBefore       `xml:"Before"`
	During       *FESTemporalDuring       `xml:"During"`
	Begins       *FESTemporalBegins       `xml:"Begins"`
	BegunBy      *FESTemporalBegunBy      `xml:"BegunBy"`
	TContains    *FESTemporalTContains    `xml:"TContains"`
	TEquals      *FESTemporalTEquals      `xml:"TEquals"`
	TOverlaps    *FESTemporalTOverlaps    `xml:"TOverlaps"`
	Meets        *FESTemporalMeets        `xml:"Meets"`
	OverlappedBy *FESTemporalOverlappedBy `xml:"OverlappedBy"`
	MetBy        *FESTemporalMetBy        `xml:"MetBy"`
	Ends         *FESTemporalEnds         `xml:"Ends"`
	EndedBy      *FESTemporalEndedBy      `xml:"EndedBy"`
	AnyInteracts *FESTemporalAnyInteracts `xml:"AnyInteracts"`
	ResourceId   []FESResourceId          `xml:"ResourceId"`
}

// FESComparison represents a comparison predicate.
type FESComparison struct {
	MatchCase      string `xml:"matchCase,attr"`
	ValueReference string `xml:"ValueReference"`
	Literal        string `xml:"Literal"`
}

// FESPropertyIsLike represents a LIKE comparison.
type FESPropertyIsLike struct {
	WildCard       string `xml:"wildCard,attr"`
	SingleChar     string `xml:"singleChar,attr"`
	EscapeChar     string `xml:"escapeChar,attr"`
	MatchCase      string `xml:"matchCase,attr"`
	ValueReference string `xml:"ValueReference"`
	Literal        string `xml:"Literal"`
}

// FESPropertyIsNull represents an IS NULL check.
type FESPropertyIsNull struct {
	ValueReference string `xml:"ValueReference"`
}

// FESPropertyIsNil represents an IS NIL check (xsi:nil="true").
// In practice, this is equivalent to IS NULL for database purposes.
type FESPropertyIsNil struct {
	ValueReference string `xml:"ValueReference"`
}

// FESPropertyIsBetween represents a BETWEEN check.
type FESPropertyIsBetween struct {
	ValueReference string             `xml:"ValueReference"`
	LowerBoundary  FESBetweenBoundary `xml:"LowerBoundary"`
	UpperBoundary  FESBetweenBoundary `xml:"UpperBoundary"`
}

// FESBetweenBoundary represents a boundary in BETWEEN.
type FESBetweenBoundary struct {
	Literal string `xml:"Literal"`
}

// FESBBOX represents a BBOX spatial predicate.
type FESBBOX struct {
	ValueReference string      `xml:"ValueReference"`
	Envelope       FESEnvelope `xml:"Envelope"`
}

// FESEnvelope represents a GML envelope.
type FESEnvelope struct {
	SrsName     string `xml:"srsName,attr"`
	LowerCorner string `xml:"lowerCorner"`
	UpperCorner string `xml:"upperCorner"`
}

// FESSpatial represents a spatial predicate.
type FESSpatial struct {
	ValueReference string `xml:"ValueReference"`
	// Geometry can be various types
	Point      *FESPoint      `xml:"Point"`
	Polygon    *FESPolygon    `xml:"Polygon"`
	LineString *FESLineString `xml:"LineString"`
	Envelope   *FESEnvelope   `xml:"Envelope"`
}

// FESDWithin represents a distance within predicate.
type FESDWithin struct {
	ValueReference string      `xml:"ValueReference"`
	Point          *FESPoint   `xml:"Point"`
	Distance       FESDistance `xml:"Distance"`
}

// FESDistance represents a distance value.
type FESDistance struct {
	UOM   string `xml:"uom,attr"`
	Value string `xml:",chardata"`
}

// FESPoint represents a GML point.
type FESPoint struct {
	SrsName string `xml:"srsName,attr"`
	Pos     string `xml:"pos"`
}

// FESPolygon represents a GML polygon.
type FESPolygon struct {
	SrsName  string             `xml:"srsName,attr"`
	Exterior FESPolygonExterior `xml:"exterior"`
}

// FESPolygonExterior represents polygon exterior ring.
type FESPolygonExterior struct {
	LinearRing FESLinearRing `xml:"LinearRing"`
}

// FESLinearRing represents a linear ring.
type FESLinearRing struct {
	PosList string `xml:"posList"`
}

// FESLineString represents a GML line string.
type FESLineString struct {
	SrsName string `xml:"srsName,attr"`
	PosList string `xml:"posList"`
}

// FESResourceId represents a resource identifier.
type FESResourceId struct {
	Rid string `xml:"rid,attr"`
}

// GML Temporal Types for FES 2.0 temporal filters

// GMLTimeInstant represents gml:TimeInstant
type GMLTimeInstant struct {
	GmlID        string `xml:"id,attr"`
	TimePosition string `xml:"timePosition"`
}

// GMLTimePeriod represents gml:TimePeriod
type GMLTimePeriod struct {
	GmlID         string `xml:"id,attr"`
	BeginPosition string `xml:"beginPosition"`
	EndPosition   string `xml:"endPosition"`
}

// FES Temporal Predicates

// FESTemporalAfter represents fes:After temporal predicate
type FESTemporalAfter struct {
	ValueReference string          `xml:"ValueReference"`
	TimeInstant    *GMLTimeInstant `xml:"TimeInstant"`
	TimePeriod     *GMLTimePeriod  `xml:"TimePeriod"`
}

// FESTemporalBefore represents fes:Before temporal predicate
type FESTemporalBefore struct {
	ValueReference string          `xml:"ValueReference"`
	TimeInstant    *GMLTimeInstant `xml:"TimeInstant"`
	TimePeriod     *GMLTimePeriod  `xml:"TimePeriod"`
}

// FESTemporalDuring represents fes:During temporal predicate
type FESTemporalDuring struct {
	ValueReference string         `xml:"ValueReference"`
	TimePeriod     *GMLTimePeriod `xml:"TimePeriod"`
}

// FESTemporalBegins represents fes:Begins temporal predicate
type FESTemporalBegins struct {
	ValueReference string         `xml:"ValueReference"`
	TimePeriod     *GMLTimePeriod `xml:"TimePeriod"`
}

// FESTemporalBegunBy represents fes:BegunBy temporal predicate
type FESTemporalBegunBy struct {
	ValueReference string         `xml:"ValueReference"`
	TimePeriod     *GMLTimePeriod `xml:"TimePeriod"`
}

// FESTemporalTContains represents fes:TContains temporal predicate
type FESTemporalTContains struct {
	ValueReference string          `xml:"ValueReference"`
	TimeInstant    *GMLTimeInstant `xml:"TimeInstant"`
	TimePeriod     *GMLTimePeriod  `xml:"TimePeriod"`
}

// FESTemporalTEquals represents fes:TEquals temporal predicate
type FESTemporalTEquals struct {
	ValueReference string          `xml:"ValueReference"`
	TimeInstant    *GMLTimeInstant `xml:"TimeInstant"`
	TimePeriod     *GMLTimePeriod  `xml:"TimePeriod"`
}

// FESTemporalTOverlaps represents fes:TOverlaps temporal predicate
type FESTemporalTOverlaps struct {
	ValueReference string         `xml:"ValueReference"`
	TimePeriod     *GMLTimePeriod `xml:"TimePeriod"`
}

// FESTemporalMeets represents fes:Meets temporal predicate
type FESTemporalMeets struct {
	ValueReference string         `xml:"ValueReference"`
	TimePeriod     *GMLTimePeriod `xml:"TimePeriod"`
}

// FESTemporalOverlappedBy represents fes:OverlappedBy temporal predicate
type FESTemporalOverlappedBy struct {
	ValueReference string         `xml:"ValueReference"`
	TimePeriod     *GMLTimePeriod `xml:"TimePeriod"`
}

// FESTemporalMetBy represents fes:MetBy temporal predicate
type FESTemporalMetBy struct {
	ValueReference string         `xml:"ValueReference"`
	TimePeriod     *GMLTimePeriod `xml:"TimePeriod"`
}

// FESTemporalEnds represents fes:Ends temporal predicate
type FESTemporalEnds struct {
	ValueReference string         `xml:"ValueReference"`
	TimePeriod     *GMLTimePeriod `xml:"TimePeriod"`
}

// FESTemporalEndedBy represents fes:EndedBy temporal predicate
type FESTemporalEndedBy struct {
	ValueReference string         `xml:"ValueReference"`
	TimePeriod     *GMLTimePeriod `xml:"TimePeriod"`
}

// FESTemporalAnyInteracts represents fes:AnyInteracts temporal predicate
type FESTemporalAnyInteracts struct {
	ValueReference string          `xml:"ValueReference"`
	TimeInstant    *GMLTimeInstant `xml:"TimeInstant"`
	TimePeriod     *GMLTimePeriod  `xml:"TimePeriod"`
}

// ParseFESFilter parses an FES 2.0 XML filter string.
func ParseFESFilter(filterXML string) (*FESFilter, error) {
	if err := validateFilterStructure(filterXML); err != nil {
		return nil, err
	}
	var filter FESFilter
	if err := xml.Unmarshal([]byte(filterXML), &filter); err != nil {
		return nil, fmt.Errorf("failed to parse FES filter: %w", err)
	}

	// Validate that the root element is a Filter element
	localName := strings.ToLower(filter.XMLName.Local)
	if localName != "filter" {
		return nil, fmt.Errorf("expected element type <Filter> but have <%s>", filter.XMLName.Local)
	}

	return &filter, nil
}

// The XML model deliberately supports many predicate types. Reject unknown,
// empty or ambiguous predicate containers before decoding: otherwise encoding/xml
// can ignore a child and turn a restrictive filter into a broader predicate.
func validateFilterStructure(raw string) error {
	type container struct {
		name      string
		children  int
		resources bool
	}
	d := xml.NewDecoder(strings.NewReader(raw))
	var stack []container
	roots := 0
	isContainer := func(name string) bool { return name == "Filter" || name == "And" || name == "Or" || name == "Not" }
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch v := token.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				roots++
				if roots != 1 || v.Name.Local != "Filter" {
					return fmt.Errorf("expected element type <Filter> as the single root")
				}
			}
			predicate := len(stack) == 0 || isContainer(stack[len(stack)-1].name)
			if predicate && v.Name.Space != "" && v.Name.Space != NSFes && v.Name.Space != "fes" {
				return fmt.Errorf("invalid FES predicate namespace %q", v.Name.Space)
			}
			if len(stack) > 0 && isContainer(stack[len(stack)-1].name) {
				parent := &stack[len(stack)-1]
				if _, ok := reflect.TypeFor[FESFilter]().FieldByName(v.Name.Local); !ok || v.Name.Local == "XMLName" {
					return fmt.Errorf("unsupported filter predicate %q", v.Name.Local)
				}
				resource := v.Name.Local == "ResourceId"
				if (parent.name == "Filter" && parent.children > 0 && (!resource || !parent.resources)) || (parent.name == "Not" && parent.children > 0) {
					return fmt.Errorf("ambiguous %s predicates", parent.name)
				}
				parent.children++
				parent.resources = resource
			}
			stack = append(stack, container{name: v.Name.Local})
		case xml.EndElement:
			if len(stack) == 0 {
				return fmt.Errorf("unexpected filter end")
			}
			last := stack[len(stack)-1]
			if isContainer(last.name) && last.children == 0 {
				return fmt.Errorf("empty %s predicate", last.name)
			}
			stack = stack[:len(stack)-1]
		case xml.Directive:
			return fmt.Errorf("XML directives are not supported")
		}
	}
	if roots != 1 {
		return fmt.Errorf("missing Filter element")
	}
	return nil
}
