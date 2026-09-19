// Package ast defines a shared filter AST (Abstract Syntax Tree) for both
// CQL2 text filters and FES 2.0 XML filters. This enables a single SQL
// generation path for both filter types, reducing code duplication.
package ast

// NodeType identifies the type of an AST node.
type NodeType int

const (
	// Logical operators
	NodeAnd NodeType = iota
	NodeOr
	NodeNot

	// Comparison operators
	NodeEqual
	NodeNotEqual
	NodeLessThan
	NodeLessThanOrEqual
	NodeGreaterThan
	NodeGreaterThanOrEqual
	NodeLike
	NodeILike
	NodeBetween
	NodeIn
	NodeIsNull
	NodeIsNotNull

	// Spatial operators
	NodeBBox
	NodeIntersects
	NodeWithin
	NodeContains
	NodeDisjoint
	NodeTouches
	NodeCrosses
	NodeOverlaps
	NodeDWithin

	// Temporal operators
	NodeAfter
	NodeBefore
	NodeDuring
	NodeBegins
	NodeBegunBy
	NodeTContains
	NodeTEquals
	NodeTOverlaps
	NodeMeets
	NodeOverlappedBy
	NodeMetBy
	NodeEnds
	NodeEndedBy
	NodeAnyInteracts

	// Resource ID (for FES)
	NodeResourceId

	// Literals and identifiers
	NodeProperty
	NodeLiteral
	NodeGeometry
	NodeTimestamp
	NodeTimePeriod
)

// Node represents a node in the filter AST.
type Node struct {
	Type     NodeType
	Children []*Node

	// For property references
	Property string

	// For literal values
	Value interface{}

	// For geometry nodes
	WKT  string
	SRID int

	// For temporal nodes
	TimeStart string
	TimeEnd   string

	// For LIKE operator
	WildCard   string
	SingleChar string
	EscapeChar string
	MatchCase  bool

	// For DWithin
	Distance float64
	DistUnit string

	// For ResourceId
	ResourceIDs []string

	// For BBOX
	MinX, MinY, MaxX, MaxY float64
	BBoxSRID               int
}

// And creates an AND node with the given children.
func And(children ...*Node) *Node {
	return &Node{Type: NodeAnd, Children: children}
}

// Or creates an OR node with the given children.
func Or(children ...*Node) *Node {
	return &Node{Type: NodeOr, Children: children}
}

// Not creates a NOT node.
func Not(child *Node) *Node {
	return &Node{Type: NodeNot, Children: []*Node{child}}
}

// Equal creates an equality comparison node.
func Equal(property string, value interface{}) *Node {
	return &Node{
		Type:     NodeEqual,
		Property: property,
		Value:    value,
	}
}

// NotEqual creates a not-equal comparison node.
func NotEqual(property string, value interface{}) *Node {
	return &Node{
		Type:     NodeNotEqual,
		Property: property,
		Value:    value,
	}
}

// LessThan creates a less-than comparison node.
func LessThan(property string, value interface{}) *Node {
	return &Node{
		Type:     NodeLessThan,
		Property: property,
		Value:    value,
	}
}

// LessThanOrEqual creates a less-than-or-equal comparison node.
func LessThanOrEqual(property string, value interface{}) *Node {
	return &Node{
		Type:     NodeLessThanOrEqual,
		Property: property,
		Value:    value,
	}
}

// GreaterThan creates a greater-than comparison node.
func GreaterThan(property string, value interface{}) *Node {
	return &Node{
		Type:     NodeGreaterThan,
		Property: property,
		Value:    value,
	}
}

// GreaterThanOrEqual creates a greater-than-or-equal comparison node.
func GreaterThanOrEqual(property string, value interface{}) *Node {
	return &Node{
		Type:     NodeGreaterThanOrEqual,
		Property: property,
		Value:    value,
	}
}

// Like creates a LIKE comparison node.
func Like(property string, pattern string, wildCard, singleChar, escapeChar string, matchCase bool) *Node {
	return &Node{
		Type:       NodeLike,
		Property:   property,
		Value:      pattern,
		WildCard:   wildCard,
		SingleChar: singleChar,
		EscapeChar: escapeChar,
		MatchCase:  matchCase,
	}
}

// ILike creates a case-insensitive LIKE comparison node.
func ILike(property string, pattern string) *Node {
	return &Node{
		Type:     NodeILike,
		Property: property,
		Value:    pattern,
	}
}

// Between creates a BETWEEN comparison node.
func Between(property string, lower, upper interface{}) *Node {
	return &Node{
		Type:     NodeBetween,
		Property: property,
		Children: []*Node{
			{Type: NodeLiteral, Value: lower},
			{Type: NodeLiteral, Value: upper},
		},
	}
}

// In creates an IN comparison node.
func In(property string, values []interface{}) *Node {
	children := make([]*Node, len(values))
	for i, v := range values {
		children[i] = &Node{Type: NodeLiteral, Value: v}
	}
	return &Node{
		Type:     NodeIn,
		Property: property,
		Children: children,
	}
}

// IsNull creates an IS NULL node.
func IsNull(property string) *Node {
	return &Node{
		Type:     NodeIsNull,
		Property: property,
	}
}

// IsNotNull creates an IS NOT NULL node.
func IsNotNull(property string) *Node {
	return &Node{
		Type:     NodeIsNotNull,
		Property: property,
	}
}

// BBox creates a BBOX spatial node.
func BBox(property string, minX, minY, maxX, maxY float64, srid int) *Node {
	return &Node{
		Type:     NodeBBox,
		Property: property,
		MinX:     minX,
		MinY:     minY,
		MaxX:     maxX,
		MaxY:     maxY,
		BBoxSRID: srid,
	}
}

// Spatial creates a spatial operation node.
func Spatial(nodeType NodeType, property, wkt string, srid int) *Node {
	return &Node{
		Type:     nodeType,
		Property: property,
		WKT:      wkt,
		SRID:     srid,
	}
}

// DWithin creates a distance-within spatial node.
func DWithin(property, wkt string, srid int, distance float64, unit string) *Node {
	return &Node{
		Type:     NodeDWithin,
		Property: property,
		WKT:      wkt,
		SRID:     srid,
		Distance: distance,
		DistUnit: unit,
	}
}

// Temporal creates a temporal operation node with a time instant.
func Temporal(nodeType NodeType, property, timestamp string) *Node {
	return &Node{
		Type:      nodeType,
		Property:  property,
		TimeStart: timestamp,
	}
}

// TemporalPeriod creates a temporal operation node with a time period.
func TemporalPeriod(nodeType NodeType, property, start, end string) *Node {
	return &Node{
		Type:      nodeType,
		Property:  property,
		TimeStart: start,
		TimeEnd:   end,
	}
}

// ResourceIds creates a resource ID filter node.
func ResourceIds(ids []string) *Node {
	return &Node{
		Type:        NodeResourceId,
		ResourceIDs: ids,
	}
}

// Geometry creates a geometry literal node.
func Geometry(wkt string, srid int) *Node {
	return &Node{
		Type: NodeGeometry,
		WKT:  wkt,
		SRID: srid,
	}
}

// Literal creates a literal value node.
func Literal(value interface{}) *Node {
	return &Node{
		Type:  NodeLiteral,
		Value: value,
	}
}

// Property creates a property reference node.
func PropertyNode(name string) *Node {
	return &Node{
		Type:     NodeProperty,
		Property: name,
	}
}
