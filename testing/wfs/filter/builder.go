// Package filter provides FES 2.0 filter building utilities.
package filter

import (
	"fmt"
	"strings"
)

// FilterBuilder builds FES 2.0 filter XML.
type FilterBuilder struct {
	sb strings.Builder
}

// NewFilterBuilder creates a new filter builder.
func NewFilterBuilder() *FilterBuilder {
	return &FilterBuilder{}
}

// Start begins a Filter element.
func (b *FilterBuilder) Start() *FilterBuilder {
	b.sb.WriteString(`<fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0">
`)
	return b
}

// End closes the Filter element and returns the XML.
func (b *FilterBuilder) End() string {
	b.sb.WriteString(`</fes:Filter>
`)
	return b.sb.String()
}

// PropertyIsEqualTo adds a PropertyIsEqualTo comparison.
func (b *FilterBuilder) PropertyIsEqualTo(property, value string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:PropertyIsEqualTo>
    <fes:ValueReference>%s</fes:ValueReference>
    <fes:Literal>%s</fes:Literal>
  </fes:PropertyIsEqualTo>
`, property, value)
	return b
}

// PropertyIsNotEqualTo adds a PropertyIsNotEqualTo comparison.
func (b *FilterBuilder) PropertyIsNotEqualTo(property, value string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:PropertyIsNotEqualTo>
    <fes:ValueReference>%s</fes:ValueReference>
    <fes:Literal>%s</fes:Literal>
  </fes:PropertyIsNotEqualTo>
`, property, value)
	return b
}

// PropertyIsLessThan adds a PropertyIsLessThan comparison.
func (b *FilterBuilder) PropertyIsLessThan(property, value string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:PropertyIsLessThan>
    <fes:ValueReference>%s</fes:ValueReference>
    <fes:Literal>%s</fes:Literal>
  </fes:PropertyIsLessThan>
`, property, value)
	return b
}

// PropertyIsGreaterThan adds a PropertyIsGreaterThan comparison.
func (b *FilterBuilder) PropertyIsGreaterThan(property, value string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:PropertyIsGreaterThan>
    <fes:ValueReference>%s</fes:ValueReference>
    <fes:Literal>%s</fes:Literal>
  </fes:PropertyIsGreaterThan>
`, property, value)
	return b
}

// PropertyIsLessThanOrEqualTo adds a PropertyIsLessThanOrEqualTo comparison.
func (b *FilterBuilder) PropertyIsLessThanOrEqualTo(property, value string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:PropertyIsLessThanOrEqualTo>
    <fes:ValueReference>%s</fes:ValueReference>
    <fes:Literal>%s</fes:Literal>
  </fes:PropertyIsLessThanOrEqualTo>
`, property, value)
	return b
}

// PropertyIsGreaterThanOrEqualTo adds a PropertyIsGreaterThanOrEqualTo comparison.
func (b *FilterBuilder) PropertyIsGreaterThanOrEqualTo(property, value string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:PropertyIsGreaterThanOrEqualTo>
    <fes:ValueReference>%s</fes:ValueReference>
    <fes:Literal>%s</fes:Literal>
  </fes:PropertyIsGreaterThanOrEqualTo>
`, property, value)
	return b
}

// PropertyIsLike adds a PropertyIsLike comparison.
func (b *FilterBuilder) PropertyIsLike(property, pattern, wildCard, singleChar, escapeChar string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:PropertyIsLike wildCard="%s" singleChar="%s" escapeChar="%s">
    <fes:ValueReference>%s</fes:ValueReference>
    <fes:Literal>%s</fes:Literal>
  </fes:PropertyIsLike>
`, wildCard, singleChar, escapeChar, property, pattern)
	return b
}

// PropertyIsNull adds a PropertyIsNull check.
func (b *FilterBuilder) PropertyIsNull(property string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:PropertyIsNull>
    <fes:ValueReference>%s</fes:ValueReference>
  </fes:PropertyIsNull>
`, property)
	return b
}

// PropertyIsBetween adds a PropertyIsBetween comparison.
func (b *FilterBuilder) PropertyIsBetween(property, lower, upper string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:PropertyIsBetween>
    <fes:ValueReference>%s</fes:ValueReference>
    <fes:LowerBoundary>
      <fes:Literal>%s</fes:Literal>
    </fes:LowerBoundary>
    <fes:UpperBoundary>
      <fes:Literal>%s</fes:Literal>
    </fes:UpperBoundary>
  </fes:PropertyIsBetween>
`, property, lower, upper)
	return b
}

// ResourceId adds a ResourceId filter.
func (b *FilterBuilder) ResourceId(id string) *FilterBuilder {
	fmt.Fprintf(&b.sb, `  <fes:ResourceId rid="%s"/>
`, id)
	return b
}

// BBOX adds a BBOX spatial filter.
func (b *FilterBuilder) BBOX(property string, minx, miny, maxx, maxy float64, srsName string) *FilterBuilder {
	srs := ""
	if srsName != "" {
		srs = fmt.Sprintf(` srsName="%s"`, srsName)
	}
	fmt.Fprintf(&b.sb, `  <fes:BBOX>
    <fes:ValueReference>%s</fes:ValueReference>
    <gml:Envelope xmlns:gml="http://www.opengis.net/gml/3.2"%s>
      <gml:lowerCorner>%.6f %.6f</gml:lowerCorner>
      <gml:upperCorner>%.6f %.6f</gml:upperCorner>
    </gml:Envelope>
  </fes:BBOX>
`, property, srs, minx, miny, maxx, maxy)
	return b
}

// Intersects adds an Intersects spatial filter with a point.
func (b *FilterBuilder) IntersectsPoint(property string, x, y float64, srsName string) *FilterBuilder {
	srs := ""
	if srsName != "" {
		srs = fmt.Sprintf(` srsName="%s"`, srsName)
	}
	fmt.Fprintf(&b.sb, `  <fes:Intersects>
    <fes:ValueReference>%s</fes:ValueReference>
    <gml:Point xmlns:gml="http://www.opengis.net/gml/3.2"%s>
      <gml:pos>%.6f %.6f</gml:pos>
    </gml:Point>
  </fes:Intersects>
`, property, srs, x, y)
	return b
}

// IntersectsPolygon adds an Intersects spatial filter with a polygon.
func (b *FilterBuilder) IntersectsPolygon(property string, coords [][2]float64, srsName string) *FilterBuilder {
	srs := ""
	if srsName != "" {
		srs = fmt.Sprintf(` srsName="%s"`, srsName)
	}

	var posList strings.Builder
	for i, c := range coords {
		if i > 0 {
			posList.WriteString(" ")
		}
		fmt.Fprintf(&posList, "%.6f %.6f", c[0], c[1])
	}

	fmt.Fprintf(&b.sb, `  <fes:Intersects>
    <fes:ValueReference>%s</fes:ValueReference>
    <gml:Polygon xmlns:gml="http://www.opengis.net/gml/3.2"%s>
      <gml:exterior>
        <gml:LinearRing>
          <gml:posList>%s</gml:posList>
        </gml:LinearRing>
      </gml:exterior>
    </gml:Polygon>
  </fes:Intersects>
`, property, srs, posList.String())
	return b
}

// Within adds a Within spatial filter.
func (b *FilterBuilder) WithinPolygon(property string, coords [][2]float64, srsName string) *FilterBuilder {
	srs := ""
	if srsName != "" {
		srs = fmt.Sprintf(` srsName="%s"`, srsName)
	}

	var posList strings.Builder
	for i, c := range coords {
		if i > 0 {
			posList.WriteString(" ")
		}
		fmt.Fprintf(&posList, "%.6f %.6f", c[0], c[1])
	}

	fmt.Fprintf(&b.sb, `  <fes:Within>
    <fes:ValueReference>%s</fes:ValueReference>
    <gml:Polygon xmlns:gml="http://www.opengis.net/gml/3.2"%s>
      <gml:exterior>
        <gml:LinearRing>
          <gml:posList>%s</gml:posList>
        </gml:LinearRing>
      </gml:exterior>
    </gml:Polygon>
  </fes:Within>
`, property, srs, posList.String())
	return b
}

// DWithin adds a DWithin (distance within) spatial filter.
func (b *FilterBuilder) DWithin(property string, x, y, distance float64, units, srsName string) *FilterBuilder {
	srs := ""
	if srsName != "" {
		srs = fmt.Sprintf(` srsName="%s"`, srsName)
	}
	fmt.Fprintf(&b.sb, `  <fes:DWithin>
    <fes:ValueReference>%s</fes:ValueReference>
    <gml:Point xmlns:gml="http://www.opengis.net/gml/3.2"%s>
      <gml:pos>%.6f %.6f</gml:pos>
    </gml:Point>
    <fes:Distance uom="%s">%.6f</fes:Distance>
  </fes:DWithin>
`, property, srs, x, y, units, distance)
	return b
}

// And starts an And logical operator.
func (b *FilterBuilder) And() *FilterBuilder {
	b.sb.WriteString("  <fes:And>\n")
	return b
}

// EndAnd ends the And logical operator.
func (b *FilterBuilder) EndAnd() *FilterBuilder {
	b.sb.WriteString("  </fes:And>\n")
	return b
}

// Or starts an Or logical operator.
func (b *FilterBuilder) Or() *FilterBuilder {
	b.sb.WriteString("  <fes:Or>\n")
	return b
}

// EndOr ends the Or logical operator.
func (b *FilterBuilder) EndOr() *FilterBuilder {
	b.sb.WriteString("  </fes:Or>\n")
	return b
}

// Not starts a Not logical operator.
func (b *FilterBuilder) Not() *FilterBuilder {
	b.sb.WriteString("  <fes:Not>\n")
	return b
}

// EndNot ends the Not logical operator.
func (b *FilterBuilder) EndNot() *FilterBuilder {
	b.sb.WriteString("  </fes:Not>\n")
	return b
}

// BuildBBOXFilter builds a simple BBOX filter.
func BuildBBOXFilter(property string, minx, miny, maxx, maxy float64, srsName string) string {
	return NewFilterBuilder().
		Start().
		BBOX(property, minx, miny, maxx, maxy, srsName).
		End()
}

// BuildResourceIdFilter builds a ResourceId filter.
func BuildResourceIdFilter(ids ...string) string {
	b := NewFilterBuilder().Start()
	for _, id := range ids {
		b.ResourceId(id)
	}
	return b.End()
}

// BuildPropertyIsEqualToFilter builds a PropertyIsEqualTo filter.
func BuildPropertyIsEqualToFilter(property, value string) string {
	return NewFilterBuilder().
		Start().
		PropertyIsEqualTo(property, value).
		End()
}
