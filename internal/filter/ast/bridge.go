// Package ast provides bridge functions for integrating the shared filter AST
// with existing filter implementations (CQL2 and FES).
package ast

import (
	"fmt"
	"strings"
)

// CompileOptions provides common options for compiling filters.
type CompileOptions struct {
	StartParamIndex   int
	SourceSRID        int
	FilterSRID        int
	GeometryProperty  string
	AllowedProperties map[string]struct{}
	TableAlias        string // For FES: "t", for CQL2: ""
	IDColumn          string
	CollectionID      string
}

// CompileToPostgreSQL compiles an AST to PostgreSQL SQL.
func CompileToPostgreSQL(node *Node, opts CompileOptions) (string, []interface{}, int, error) {
	compiler := NewSQLCompiler(SQLOptions{
		Dialect:           DialectPostgreSQL,
		StartParamIndex:   opts.StartParamIndex,
		SourceSRID:        opts.SourceSRID,
		TableAlias:        opts.TableAlias,
		GeometryProperty:  opts.GeometryProperty,
		AllowedProperties: opts.AllowedProperties,
		IDColumn:          opts.IDColumn,
		CollectionID:      opts.CollectionID,
	})
	return compiler.Compile(node)
}

// CompileToDuckDB compiles an AST to DuckDB SQL.
func CompileToDuckDB(node *Node, opts CompileOptions) (string, []interface{}, int, error) {
	compiler := NewSQLCompiler(SQLOptions{
		Dialect:           DialectDuckDB,
		StartParamIndex:   opts.StartParamIndex,
		SourceSRID:        opts.SourceSRID,
		TableAlias:        opts.TableAlias,
		GeometryProperty:  opts.GeometryProperty,
		AllowedProperties: opts.AllowedProperties,
		IDColumn:          opts.IDColumn,
		CollectionID:      opts.CollectionID,
	})
	return compiler.Compile(node)
}

// TemporalOp defines temporal operation types for the simplified interface.
type TemporalOp string

const (
	TemporalAfter        TemporalOp = "After"
	TemporalBefore       TemporalOp = "Before"
	TemporalDuring       TemporalOp = "During"
	TemporalBegins       TemporalOp = "Begins"
	TemporalBegunBy      TemporalOp = "BegunBy"
	TemporalTContains    TemporalOp = "TContains"
	TemporalTEquals      TemporalOp = "TEquals"
	TemporalTOverlaps    TemporalOp = "TOverlaps"
	TemporalMeets        TemporalOp = "Meets"
	TemporalOverlappedBy TemporalOp = "OverlappedBy"
	TemporalMetBy        TemporalOp = "MetBy"
	TemporalEnds         TemporalOp = "Ends"
	TemporalEndedBy      TemporalOp = "EndedBy"
	TemporalAnyInteracts TemporalOp = "AnyInteracts"
)

// TemporalNodeType returns the AST NodeType for a temporal operation.
func TemporalNodeType(op TemporalOp) NodeType {
	switch op {
	case TemporalAfter:
		return NodeAfter
	case TemporalBefore:
		return NodeBefore
	case TemporalDuring:
		return NodeDuring
	case TemporalBegins:
		return NodeBegins
	case TemporalBegunBy:
		return NodeBegunBy
	case TemporalTContains:
		return NodeTContains
	case TemporalTEquals:
		return NodeTEquals
	case TemporalTOverlaps:
		return NodeTOverlaps
	case TemporalMeets:
		return NodeMeets
	case TemporalOverlappedBy:
		return NodeOverlappedBy
	case TemporalMetBy:
		return NodeMetBy
	case TemporalEnds:
		return NodeEnds
	case TemporalEndedBy:
		return NodeEndedBy
	case TemporalAnyInteracts:
		return NodeAnyInteracts
	default:
		return NodeAfter
	}
}

// SpatialOp defines spatial operation types.
type SpatialOp string

const (
	SpatialIntersects SpatialOp = "Intersects"
	SpatialWithin     SpatialOp = "Within"
	SpatialContains   SpatialOp = "Contains"
	SpatialDisjoint   SpatialOp = "Disjoint"
	SpatialTouches    SpatialOp = "Touches"
	SpatialCrosses    SpatialOp = "Crosses"
	SpatialOverlaps   SpatialOp = "Overlaps"
)

// SpatialNodeType returns the AST NodeType for a spatial operation.
func SpatialNodeType(op SpatialOp) NodeType {
	switch op {
	case SpatialIntersects:
		return NodeIntersects
	case SpatialWithin:
		return NodeWithin
	case SpatialContains:
		return NodeContains
	case SpatialDisjoint:
		return NodeDisjoint
	case SpatialTouches:
		return NodeTouches
	case SpatialCrosses:
		return NodeCrosses
	case SpatialOverlaps:
		return NodeOverlaps
	default:
		return NodeIntersects
	}
}

// ComparisonOp defines comparison operation types.
type ComparisonOp string

const (
	CompEqual              ComparisonOp = "="
	CompNotEqual           ComparisonOp = "<>"
	CompLessThan           ComparisonOp = "<"
	CompLessThanOrEqual    ComparisonOp = "<="
	CompGreaterThan        ComparisonOp = ">"
	CompGreaterThanOrEqual ComparisonOp = ">="
)

// ComparisonNodeType returns the AST NodeType for a comparison operation.
func ComparisonNodeType(op ComparisonOp) NodeType {
	switch op {
	case CompEqual:
		return NodeEqual
	case CompNotEqual:
		return NodeNotEqual
	case CompLessThan:
		return NodeLessThan
	case CompLessThanOrEqual:
		return NodeLessThanOrEqual
	case CompGreaterThan:
		return NodeGreaterThan
	case CompGreaterThanOrEqual:
		return NodeGreaterThanOrEqual
	default:
		return NodeEqual
	}
}

// QuoteIdentifier quotes an SQL identifier.
func QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// QuoteIdentifierWithAlias quotes an SQL identifier with optional table alias.
func QuoteIdentifierWithAlias(name, alias string) string {
	quoted := QuoteIdentifier(name)
	if alias != "" {
		return alias + "." + quoted
	}
	return quoted
}

// StripNamespacePrefix removes XML namespace prefix from a property name.
func StripNamespacePrefix(s string) string {
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// ValidateNumericCoordinates checks if all coordinate strings are valid numbers.
func ValidateNumericCoordinates(coords []string) bool {
	for _, c := range coords {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		// Check for valid number format
		_, err := fmt.Sscanf(c, "%f", new(float64))
		if err != nil {
			return false
		}
	}
	return true
}

// ConvertFESLikePattern converts FES LIKE pattern to SQL LIKE pattern.
func ConvertFESLikePattern(pattern, wildCard, singleChar, escapeChar string) string {
	if wildCard == "" {
		wildCard = "*"
	}
	if singleChar == "" {
		singleChar = "?"
	}

	result := pattern
	// First escape existing SQL wildcards if escape char is set
	if escapeChar != "" {
		result = strings.ReplaceAll(result, "%", escapeChar+"%")
		result = strings.ReplaceAll(result, "_", escapeChar+"_")
	}
	// Then convert FES wildcards to SQL wildcards
	result = strings.ReplaceAll(result, wildCard, "%")
	result = strings.ReplaceAll(result, singleChar, "_")
	return result
}
