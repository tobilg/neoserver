package ast

import (
	"fmt"
	"strconv"
	"strings"
)

// FESConverter converts FES filter types to AST nodes.
// This is designed to work with the existing FES XML structures in internal/wfs.
type FESConverter struct {
	opts FESConvertOptions
}

// FESConvertOptions configures FES to AST conversion.
type FESConvertOptions struct {
	GeometryProperty  string
	AllowedProperties map[string]struct{}
	CollectionID      string
}

// NewFESConverter creates a new FES to AST converter.
func NewFESConverter(opts FESConvertOptions) *FESConverter {
	if opts.GeometryProperty == "" {
		opts.GeometryProperty = "geom"
	}
	return &FESConverter{opts: opts}
}

// ConvertComparison converts a comparison predicate to an AST node.
func (c *FESConverter) ConvertComparison(prop, literal, op string) (*Node, error) {
	prop = c.stripNSPrefix(prop)
	if err := c.validateProperty(prop); err != nil {
		return nil, err
	}

	var nodeType NodeType
	switch op {
	case "=":
		nodeType = NodeEqual
	case "<>":
		nodeType = NodeNotEqual
	case "<":
		nodeType = NodeLessThan
	case "<=":
		nodeType = NodeLessThanOrEqual
	case ">":
		nodeType = NodeGreaterThan
	case ">=":
		nodeType = NodeGreaterThanOrEqual
	default:
		return nil, fmt.Errorf("unknown comparison operator: %s", op)
	}

	return &Node{
		Type:     nodeType,
		Property: prop,
		Value:    literal,
	}, nil
}

// ConvertLike converts a LIKE predicate to an AST node.
func (c *FESConverter) ConvertLike(prop, literal, wildCard, singleChar, escapeChar string, matchCase bool) (*Node, error) {
	prop = c.stripNSPrefix(prop)
	if err := c.validateProperty(prop); err != nil {
		return nil, err
	}

	return &Node{
		Type:       NodeLike,
		Property:   prop,
		Value:      literal,
		WildCard:   wildCard,
		SingleChar: singleChar,
		EscapeChar: escapeChar,
		MatchCase:  matchCase,
	}, nil
}

// ConvertIsNull converts an IS NULL predicate to an AST node.
func (c *FESConverter) ConvertIsNull(prop string) (*Node, error) {
	prop = c.stripNSPrefix(prop)
	// For unknown properties, return TRUE (they are always NULL)
	if c.opts.AllowedProperties != nil {
		if _, ok := c.opts.AllowedProperties[prop]; !ok {
			// Check if it's a GML property that should be treated as always NULL
			if c.isGMLProperty(prop) {
				return nil, nil // Return nil to indicate "always true"
			}
		}
	}
	return &Node{
		Type:     NodeIsNull,
		Property: prop,
	}, nil
}

// ConvertBetween converts a BETWEEN predicate to an AST node.
func (c *FESConverter) ConvertBetween(prop, lower, upper string) (*Node, error) {
	prop = c.stripNSPrefix(prop)
	if err := c.validateProperty(prop); err != nil {
		return nil, err
	}

	return &Node{
		Type:     NodeBetween,
		Property: prop,
		Children: []*Node{
			{Type: NodeLiteral, Value: lower},
			{Type: NodeLiteral, Value: upper},
		},
	}, nil
}

// ConvertBBox converts a BBOX predicate to an AST node.
func (c *FESConverter) ConvertBBox(prop string, minX, minY, maxX, maxY float64, srsName string) (*Node, error) {
	prop = c.stripNSPrefix(prop)
	if prop == "" {
		prop = c.opts.GeometryProperty
	}
	srid := ParseSRIDFromCRS(srsName)
	if srid == 0 {
		srid = 4326
	}

	return &Node{
		Type:     NodeBBox,
		Property: prop,
		MinX:     minX,
		MinY:     minY,
		MaxX:     maxX,
		MaxY:     maxY,
		BBoxSRID: srid,
	}, nil
}

// ConvertSpatial converts a spatial predicate to an AST node.
func (c *FESConverter) ConvertSpatial(nodeType NodeType, prop, wkt string, srid int) (*Node, error) {
	prop = c.stripNSPrefix(prop)
	if prop == "" {
		prop = c.opts.GeometryProperty
	}
	// Validate that this is the geometry property
	if prop != c.opts.GeometryProperty {
		return nil, fmt.Errorf("spatial operations only allowed on geometry property %q, got %q", c.opts.GeometryProperty, prop)
	}
	if srid == 0 {
		srid = 4326
	}

	return &Node{
		Type:     nodeType,
		Property: prop,
		WKT:      wkt,
		SRID:     srid,
	}, nil
}

// ConvertDWithin converts a DWithin predicate to an AST node.
func (c *FESConverter) ConvertDWithin(prop, wkt string, srid int, distance float64, unit string) (*Node, error) {
	prop = c.stripNSPrefix(prop)
	if prop == "" {
		prop = c.opts.GeometryProperty
	}
	if prop != c.opts.GeometryProperty {
		return nil, fmt.Errorf("spatial operations only allowed on geometry property %q, got %q", c.opts.GeometryProperty, prop)
	}
	if srid == 0 {
		srid = 4326
	}

	return &Node{
		Type:     NodeDWithin,
		Property: prop,
		WKT:      wkt,
		SRID:     srid,
		Distance: distance,
		DistUnit: unit,
	}, nil
}

// ConvertTemporal converts a temporal predicate to an AST node.
func (c *FESConverter) ConvertTemporal(nodeType NodeType, prop, timeStart, timeEnd string) (*Node, error) {
	prop = c.stripNSPrefix(prop)
	if err := c.validateProperty(prop); err != nil {
		return nil, err
	}

	return &Node{
		Type:      nodeType,
		Property:  prop,
		TimeStart: timeStart,
		TimeEnd:   timeEnd,
	}, nil
}

// ConvertResourceIds converts resource IDs to an AST node.
// It validates that the resource IDs match the collection being queried.
func (c *FESConverter) ConvertResourceIds(rids []string) (*Node, error) {
	if len(rids) == 0 {
		return nil, nil
	}

	// Extract and validate IDs
	ids := make([]string, 0, len(rids))
	for _, rid := range rids {
		// ResourceId format is "collectionId.featureId" or just "featureId"
		parts := strings.SplitN(rid, ".", 2)
		var id string
		if len(parts) == 2 {
			// Validate collection prefix matches
			if c.opts.CollectionID != "" && parts[0] != c.opts.CollectionID {
				return nil, &ResourceIdMismatchError{
					ExpectedType: c.opts.CollectionID,
					ActualType:   parts[0],
					ResourceId:   rid,
				}
			}
			id = parts[1]
		} else {
			id = rid
		}
		ids = append(ids, id)
	}

	return &Node{
		Type:        NodeResourceId,
		ResourceIDs: ids,
	}, nil
}

// ResourceIdMismatchError indicates a resource ID type mismatch.
type ResourceIdMismatchError struct {
	ExpectedType string
	ActualType   string
	ResourceId   string
}

func (e *ResourceIdMismatchError) Error() string {
	return fmt.Sprintf("resource ID %q specifies type %q but query is for type %q", e.ResourceId, e.ActualType, e.ExpectedType)
}

func (c *FESConverter) validateProperty(prop string) error {
	// Check if it's the geometry property (not allowed in comparisons)
	if prop == c.opts.GeometryProperty {
		return fmt.Errorf("geometry property %q cannot be used in comparison operators", prop)
	}
	// GML properties are always permitted (they may not be mapped as columns).
	if c.isGMLProperty(prop) {
		return nil
	}
	// Fail closed: a nil allowlist means no properties were declared, so deny
	// every non-GML property reference rather than passing arbitrary identifiers
	// through. Callers must supply the set of queryable columns explicitly.
	if _, ok := c.opts.AllowedProperties[prop]; !ok {
		return fmt.Errorf("unknown property %q", prop)
	}
	return nil
}

func (c *FESConverter) isGMLProperty(prop string) bool {
	// Check for common GML properties that may not be mapped
	gmlProps := []string{"gml:name", "gml:description", "gml:identifier", "name", "description"}
	for _, gp := range gmlProps {
		if prop == gp || strings.HasSuffix(prop, ":"+gp) {
			return true
		}
	}
	return false
}

func (c *FESConverter) stripNSPrefix(s string) string {
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// ParseCoordinates parses a coordinate string like "10 20" or "10,20" into floats.
func ParseCoordinates(s string) ([]float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	// Split by spaces or commas
	parts := strings.Fields(s)
	coords := make([]float64, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid coordinate %q: %w", p, err)
		}
		coords = append(coords, f)
	}
	return coords, nil
}

// ParseEnvelope parses envelope corners into a bounding box.
func ParseEnvelope(lowerCorner, upperCorner string) (minX, minY, maxX, maxY float64, err error) {
	lower, err := ParseCoordinates(lowerCorner)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid lower corner %s: %w", lowerCorner, err)
	}
	if len(lower) < 2 {
		return 0, 0, 0, 0, fmt.Errorf("invalid lower corner %s: need at least 2 coordinates", lowerCorner)
	}
	upper, err := ParseCoordinates(upperCorner)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid upper corner %s: %w", upperCorner, err)
	}
	if len(upper) < 2 {
		return 0, 0, 0, 0, fmt.Errorf("invalid upper corner %s: need at least 2 coordinates", upperCorner)
	}
	return lower[0], lower[1], upper[0], upper[1], nil
}

// PointToWKT converts a GML point to WKT.
func PointToWKT(pos string) (string, int, error) {
	coords, err := ParseCoordinates(pos)
	if err != nil {
		return "", 0, fmt.Errorf("invalid point coordinates %s: %w", pos, err)
	}
	if len(coords) < 2 {
		return "", 0, fmt.Errorf("invalid point coordinates %s: need at least 2 coordinates", pos)
	}
	return fmt.Sprintf("POINT(%f %f)", coords[0], coords[1]), 0, nil
}

// PolygonToWKT converts a GML polygon (posList) to WKT.
func PolygonToWKT(posList string) (string, error) {
	coords, err := ParseCoordinates(posList)
	if err != nil {
		return "", err
	}
	if len(coords) < 6 || len(coords)%2 != 0 {
		return "", fmt.Errorf("invalid polygon coordinates: need at least 3 points")
	}

	var sb strings.Builder
	sb.WriteString("POLYGON((")
	for i := 0; i < len(coords); i += 2 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%f %f", coords[i], coords[i+1]))
	}
	sb.WriteString("))")
	return sb.String(), nil
}

// LineStringToWKT converts a GML line string (posList) to WKT.
func LineStringToWKT(posList string) (string, error) {
	coords, err := ParseCoordinates(posList)
	if err != nil {
		return "", err
	}
	if len(coords) < 4 || len(coords)%2 != 0 {
		return "", fmt.Errorf("invalid linestring coordinates: need at least 2 points")
	}

	var sb strings.Builder
	sb.WriteString("LINESTRING(")
	for i := 0; i < len(coords); i += 2 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%f %f", coords[i], coords[i+1]))
	}
	sb.WriteString(")")
	return sb.String(), nil
}
