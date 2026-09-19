package ast

import (
	"fmt"
	"strings"

	"github.com/tobilg/neoserver/internal/crs"
)

// SQLDialect specifies the target SQL dialect.
type SQLDialect int

const (
	DialectPostgreSQL SQLDialect = iota
	DialectDuckDB
)

// SQLOptions configures SQL generation.
type SQLOptions struct {
	Dialect           SQLDialect
	StartParamIndex   int
	SourceSRID        int    // SRID of the geometry column in the database
	TableAlias        string // Optional table alias prefix (e.g., "t" for FES)
	GeometryProperty  string
	AllowedProperties map[string]struct{}
	IDColumn          string // Column name for ResourceId queries
	CollectionID      string // Collection ID for ResourceId type checking
}

// SQLCompiler compiles an AST to SQL.
type SQLCompiler struct {
	opts     SQLOptions
	args     []interface{}
	paramIdx int
}

// NewSQLCompiler creates a new SQL compiler.
func NewSQLCompiler(opts SQLOptions) *SQLCompiler {
	if opts.StartParamIndex <= 0 {
		opts.StartParamIndex = 1
	}
	if opts.GeometryProperty == "" {
		opts.GeometryProperty = "geom"
	}
	if opts.IDColumn == "" {
		opts.IDColumn = "id"
	}
	return &SQLCompiler{
		opts:     opts,
		paramIdx: opts.StartParamIndex,
	}
}

// Compile compiles an AST node to SQL.
func (c *SQLCompiler) Compile(node *Node) (string, []interface{}, int, error) {
	if node == nil {
		return "", nil, c.paramIdx, nil
	}
	sql, err := c.compile(node)
	if err != nil {
		return "", nil, c.paramIdx, err
	}
	return sql, c.args, c.paramIdx, nil
}

func (c *SQLCompiler) compile(node *Node) (string, error) {
	switch node.Type {
	case NodeAnd:
		return c.compileLogical(node, "AND")
	case NodeOr:
		return c.compileLogical(node, "OR")
	case NodeNot:
		return c.compileNot(node)
	case NodeEqual:
		return c.compileComparison(node, "=")
	case NodeNotEqual:
		return c.compileComparison(node, "<>")
	case NodeLessThan:
		return c.compileComparison(node, "<")
	case NodeLessThanOrEqual:
		return c.compileComparison(node, "<=")
	case NodeGreaterThan:
		return c.compileComparison(node, ">")
	case NodeGreaterThanOrEqual:
		return c.compileComparison(node, ">=")
	case NodeLike:
		return c.compileLike(node)
	case NodeILike:
		return c.compileILike(node)
	case NodeBetween:
		return c.compileBetween(node)
	case NodeIn:
		return c.compileIn(node)
	case NodeIsNull:
		return c.compileIsNull(node)
	case NodeIsNotNull:
		return c.compileIsNotNull(node)
	case NodeBBox:
		return c.compileBBox(node)
	case NodeIntersects:
		return c.compileSpatial(node, "ST_Intersects")
	case NodeWithin:
		return c.compileSpatial(node, "ST_Within")
	case NodeContains:
		return c.compileSpatial(node, "ST_Contains")
	case NodeDisjoint:
		return c.compileSpatial(node, "ST_Disjoint")
	case NodeTouches:
		return c.compileSpatial(node, "ST_Touches")
	case NodeCrosses:
		return c.compileSpatial(node, "ST_Crosses")
	case NodeOverlaps:
		return c.compileSpatial(node, "ST_Overlaps")
	case NodeDWithin:
		return c.compileDWithin(node)
	case NodeAfter:
		return c.compileTemporalAfter(node)
	case NodeBefore:
		return c.compileTemporalBefore(node)
	case NodeDuring:
		return c.compileTemporalDuring(node)
	case NodeBegins:
		return c.compileTemporalBegins(node)
	case NodeBegunBy:
		return c.compileTemporalBegunBy(node)
	case NodeTContains:
		return c.compileTemporalTContains(node)
	case NodeTEquals:
		return c.compileTemporalTEquals(node)
	case NodeTOverlaps:
		return c.compileTemporalTOverlaps(node)
	case NodeMeets:
		return c.compileTemporalMeets(node)
	case NodeOverlappedBy:
		return c.compileTemporalOverlappedBy(node)
	case NodeMetBy:
		return c.compileTemporalMetBy(node)
	case NodeEnds:
		return c.compileTemporalEnds(node)
	case NodeEndedBy:
		return c.compileTemporalEndedBy(node)
	case NodeAnyInteracts:
		return c.compileTemporalAnyInteracts(node)
	case NodeResourceId:
		return c.compileResourceIds(node)
	default:
		return "", fmt.Errorf("unsupported node type: %d", node.Type)
	}
}

func (c *SQLCompiler) compileLogical(node *Node, op string) (string, error) {
	if len(node.Children) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		sql, err := c.compile(child)
		if err != nil {
			return "", err
		}
		if sql != "" {
			parts = append(parts, sql)
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return "(" + strings.Join(parts, " "+op+" ") + ")", nil
}

func (c *SQLCompiler) compileNot(node *Node) (string, error) {
	if len(node.Children) == 0 {
		return "", nil
	}
	sql, err := c.compile(node.Children[0])
	if err != nil {
		return "", err
	}
	if sql == "" {
		return "", nil
	}
	return "(NOT " + sql + ")", nil
}

func (c *SQLCompiler) compileComparison(node *Node, op string) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph := c.addArg(node.Value)
	return fmt.Sprintf("%s %s %s", prop, op, ph), nil
}

func (c *SQLCompiler) compileLike(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	pattern := c.convertLikePattern(node.Value.(string), node.WildCard, node.SingleChar, node.EscapeChar)
	ph := c.addArg(pattern)
	op := "LIKE"
	if !node.MatchCase {
		op = "ILIKE"
	}
	return fmt.Sprintf("%s %s %s", prop, op, ph), nil
}

func (c *SQLCompiler) compileILike(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph := c.addArg(node.Value)
	return fmt.Sprintf("%s ILIKE %s", prop, ph), nil
}

func (c *SQLCompiler) compileBetween(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	if len(node.Children) != 2 {
		return "", fmt.Errorf("BETWEEN requires exactly 2 children")
	}
	ph1 := c.addArg(node.Children[0].Value)
	ph2 := c.addArg(node.Children[1].Value)
	return fmt.Sprintf("%s BETWEEN %s AND %s", prop, ph1, ph2), nil
}

func (c *SQLCompiler) compileIn(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	phs := make([]string, len(node.Children))
	for i, child := range node.Children {
		phs[i] = c.addArg(child.Value)
	}
	return fmt.Sprintf("%s IN (%s)", prop, strings.Join(phs, ",")), nil
}

func (c *SQLCompiler) compileIsNull(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	return fmt.Sprintf("%s IS NULL", prop), nil
}

func (c *SQLCompiler) compileIsNotNull(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	return fmt.Sprintf("%s IS NOT NULL", prop), nil
}

func (c *SQLCompiler) compileBBox(node *Node) (string, error) {
	prop := c.quoteGeometryProperty(node.Property)
	if c.opts.Dialect == DialectDuckDB {
		return c.compileBBoxDuckDB(node, prop)
	}
	return c.compileBBoxPostgreSQL(node, prop)
}

func (c *SQLCompiler) compileBBoxPostgreSQL(node *Node, prop string) (string, error) {
	// Use ST_MakeEnvelope for PostgreSQL
	ph1 := c.addArg(node.MinX)
	ph2 := c.addArg(node.MinY)
	ph3 := c.addArg(node.MaxX)
	ph4 := c.addArg(node.MaxY)
	srid := node.BBoxSRID
	if srid == 0 {
		srid = 4326
	}
	envelope := fmt.Sprintf("ST_MakeEnvelope(%s,%s,%s,%s,%d)", ph1, ph2, ph3, ph4, srid)
	if c.opts.SourceSRID != 0 && c.opts.SourceSRID != srid {
		envelope = fmt.Sprintf("ST_Transform(%s,%d)", envelope, c.opts.SourceSRID)
	}
	return fmt.Sprintf("ST_Intersects(%s,%s)", prop, envelope), nil
}

func (c *SQLCompiler) compileBBoxDuckDB(node *Node, prop string) (string, error) {
	// DuckDB uses ST_GeomFromText with POLYGON WKT
	wkt := fmt.Sprintf("POLYGON((%f %f,%f %f,%f %f,%f %f,%f %f))",
		node.MinX, node.MinY,
		node.MaxX, node.MinY,
		node.MaxX, node.MaxY,
		node.MinX, node.MaxY,
		node.MinX, node.MinY,
	)
	ph := c.addArg(wkt)
	srid := node.BBoxSRID
	if srid == 0 {
		srid = 4326
	}
	geomExpr := fmt.Sprintf("ST_GeomFromText(%s)", ph)
	if c.opts.SourceSRID != 0 && c.opts.SourceSRID != srid {
		geomExpr = fmt.Sprintf("ST_Transform(%s,'EPSG:%d','EPSG:%d')", geomExpr, srid, c.opts.SourceSRID)
	}
	return fmt.Sprintf("ST_Intersects(%s,%s)", prop, geomExpr), nil
}

func (c *SQLCompiler) compileSpatial(node *Node, fn string) (string, error) {
	prop := c.quoteGeometryProperty(node.Property)
	if c.opts.Dialect == DialectDuckDB {
		return c.compileSpatialDuckDB(node, fn, prop)
	}
	return c.compileSpatialPostgreSQL(node, fn, prop)
}

func (c *SQLCompiler) compileSpatialPostgreSQL(node *Node, fn, prop string) (string, error) {
	srid := node.SRID
	if srid == 0 {
		srid = 4326
	}
	ewkt := fmt.Sprintf("SRID=%d;%s", srid, node.WKT)
	ph := c.addArg(ewkt)
	geomExpr := ph + "::geometry"
	if c.opts.SourceSRID != 0 && c.opts.SourceSRID != srid {
		geomExpr = fmt.Sprintf("ST_Transform(%s,%d)", geomExpr, c.opts.SourceSRID)
	}
	return fmt.Sprintf("%s(%s,%s)", fn, prop, geomExpr), nil
}

func (c *SQLCompiler) compileSpatialDuckDB(node *Node, fn, prop string) (string, error) {
	srid := node.SRID
	if srid == 0 {
		srid = 4326
	}
	ph := c.addArg(node.WKT)
	geomExpr := fmt.Sprintf("ST_GeomFromText(%s)", ph)
	if c.opts.SourceSRID != 0 && c.opts.SourceSRID != srid {
		geomExpr = fmt.Sprintf("ST_Transform(%s,'EPSG:%d','EPSG:%d')", geomExpr, srid, c.opts.SourceSRID)
	}
	return fmt.Sprintf("%s(%s,%s)", fn, prop, geomExpr), nil
}

func (c *SQLCompiler) compileDWithin(node *Node) (string, error) {
	prop := c.quoteGeometryProperty(node.Property)
	if c.opts.Dialect == DialectDuckDB {
		return c.compileDWithinDuckDB(node, prop)
	}
	return c.compileDWithinPostgreSQL(node, prop)
}

func (c *SQLCompiler) compileDWithinPostgreSQL(node *Node, prop string) (string, error) {
	srid := node.SRID
	if srid == 0 {
		srid = 4326
	}
	ewkt := fmt.Sprintf("SRID=%d;%s", srid, node.WKT)
	ph := c.addArg(ewkt)
	geomExpr := ph + "::geometry"
	if c.opts.SourceSRID != 0 && c.opts.SourceSRID != srid {
		geomExpr = fmt.Sprintf("ST_Transform(%s,%d)", geomExpr, c.opts.SourceSRID)
	}
	distPh := c.addArg(node.Distance)
	return fmt.Sprintf("ST_DWithin(%s,%s,%s)", prop, geomExpr, distPh), nil
}

func (c *SQLCompiler) compileDWithinDuckDB(node *Node, prop string) (string, error) {
	srid := node.SRID
	if srid == 0 {
		srid = 4326
	}
	ph := c.addArg(node.WKT)
	geomExpr := fmt.Sprintf("ST_GeomFromText(%s)", ph)
	if c.opts.SourceSRID != 0 && c.opts.SourceSRID != srid {
		geomExpr = fmt.Sprintf("ST_Transform(%s,'EPSG:%d','EPSG:%d')", geomExpr, srid, c.opts.SourceSRID)
	}
	distPh := c.addArg(node.Distance)
	return fmt.Sprintf("ST_DWithin(%s,%s,%s)", prop, geomExpr, distPh), nil
}

// Temporal predicates

func (c *SQLCompiler) compileTemporalAfter(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	if node.TimeEnd != "" {
		// After a period - compare to end time
		ph := c.addArg(node.TimeEnd)
		return fmt.Sprintf("%s > %s::timestamptz", prop, ph), nil
	}
	ph := c.addArg(node.TimeStart)
	return fmt.Sprintf("%s > %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalBefore(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph := c.addArg(node.TimeStart)
	return fmt.Sprintf("%s < %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalDuring(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph1 := c.addArg(node.TimeStart)
	ph2 := c.addArg(node.TimeEnd)
	return fmt.Sprintf("(%s >= %s::timestamptz AND %s <= %s::timestamptz)", prop, ph1, prop, ph2), nil
}

func (c *SQLCompiler) compileTemporalBegins(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph := c.addArg(node.TimeStart)
	return fmt.Sprintf("%s = %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalBegunBy(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph := c.addArg(node.TimeStart)
	return fmt.Sprintf("%s = %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalTContains(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	if node.TimeEnd != "" {
		ph1 := c.addArg(node.TimeStart)
		ph2 := c.addArg(node.TimeEnd)
		return fmt.Sprintf("(%s <= %s::timestamptz AND %s >= %s::timestamptz)", prop, ph1, prop, ph2), nil
	}
	ph := c.addArg(node.TimeStart)
	return fmt.Sprintf("%s = %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalTEquals(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	if node.TimeEnd != "" {
		ph1 := c.addArg(node.TimeStart)
		ph2 := c.addArg(node.TimeEnd)
		return fmt.Sprintf("(%s = %s::timestamptz AND %s = %s::timestamptz)", prop, ph1, prop, ph2), nil
	}
	ph := c.addArg(node.TimeStart)
	return fmt.Sprintf("%s = %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalTOverlaps(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph1 := c.addArg(node.TimeStart)
	ph2 := c.addArg(node.TimeEnd)
	return fmt.Sprintf("(%s < %s::timestamptz AND %s > %s::timestamptz)", prop, ph2, prop, ph1), nil
}

func (c *SQLCompiler) compileTemporalMeets(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph := c.addArg(node.TimeStart)
	return fmt.Sprintf("%s = %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalOverlappedBy(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph1 := c.addArg(node.TimeStart)
	ph2 := c.addArg(node.TimeEnd)
	return fmt.Sprintf("(%s > %s::timestamptz AND %s < %s::timestamptz)", prop, ph1, prop, ph2), nil
}

func (c *SQLCompiler) compileTemporalMetBy(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph := c.addArg(node.TimeEnd)
	return fmt.Sprintf("%s = %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalEnds(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph := c.addArg(node.TimeEnd)
	return fmt.Sprintf("%s = %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalEndedBy(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	ph := c.addArg(node.TimeEnd)
	return fmt.Sprintf("%s = %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileTemporalAnyInteracts(node *Node) (string, error) {
	prop := c.quoteProperty(node.Property)
	if node.TimeEnd != "" {
		ph1 := c.addArg(node.TimeStart)
		ph2 := c.addArg(node.TimeEnd)
		return fmt.Sprintf("(%s >= %s::timestamptz AND %s <= %s::timestamptz)", prop, ph1, prop, ph2), nil
	}
	ph := c.addArg(node.TimeStart)
	return fmt.Sprintf("%s = %s::timestamptz", prop, ph), nil
}

func (c *SQLCompiler) compileResourceIds(node *Node) (string, error) {
	if len(node.ResourceIDs) == 0 {
		return "", nil
	}
	idCol := c.opts.IDColumn
	if c.opts.TableAlias != "" {
		idCol = c.opts.TableAlias + "." + idCol
	}
	phs := make([]string, len(node.ResourceIDs))
	for i, id := range node.ResourceIDs {
		phs[i] = c.addArg(id)
	}
	return fmt.Sprintf("%s::text IN (%s)", idCol, strings.Join(phs, ",")), nil
}

func (c *SQLCompiler) quoteProperty(name string) string {
	quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	if c.opts.TableAlias != "" {
		return c.opts.TableAlias + "." + quoted
	}
	return quoted
}

func (c *SQLCompiler) quoteGeometryProperty(name string) string {
	// If the property is empty, use the configured geometry property
	if name == "" {
		name = c.opts.GeometryProperty
	}
	return c.quoteProperty(name)
}

func (c *SQLCompiler) addArg(v interface{}) string {
	ph := fmt.Sprintf("$%d", c.paramIdx)
	c.paramIdx++
	c.args = append(c.args, v)
	return ph
}

func (c *SQLCompiler) convertLikePattern(pattern, wildCard, singleChar, escapeChar string) string {
	if wildCard == "" {
		wildCard = "*"
	}
	if singleChar == "" {
		singleChar = "?"
	}
	// Convert FES wildcards to SQL LIKE wildcards
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

// ParseSRIDFromCRS extracts SRID from a CRS string like "EPSG:4326" or "urn:ogc:def:crs:EPSG::4326".
// Delegates to the unified crs.MustParse function.
func ParseSRIDFromCRS(crsStr string) int {
	return crs.MustParse(crsStr)
}
