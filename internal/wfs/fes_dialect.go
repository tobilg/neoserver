package wfs

import (
	"fmt"

	"github.com/tobilg/neoserver/internal/datasource"
)

type featurePredicate struct {
	Filter  *FESFilter
	Options FESCompileOptions
}

func (p *featurePredicate) CompilePredicate(opts datasource.PredicateOptions) (string, []any, int, error) {
	options := p.Options
	options.Dialect, options.TableAlias, options.GeometryExpression, options.StartParamIndex = opts.Dialect, opts.TableAlias, opts.GeometryExpression, opts.StartParamIndex
	return CompileFES(p.Filter, options)
}

func (c *fesCompiler) column(name string) string {
	if name == c.geomProp && c.geometryExpression != "" {
		return c.geometryExpression
	}
	alias := c.tableAlias
	if alias == "" {
		alias = "t"
	}
	if alias != "t" && alias != "v" {
		alias = quoteIdent(alias)
	}
	return alias + "." + quoteIdent(name)
}

func (c *fesCompiler) transformGeometry(expr string, srid int) string {
	if srid == c.sourceSRID {
		return expr
	}
	if c.dialect == datasource.SQLDuckDB {
		return fmt.Sprintf("ST_Transform(%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", expr, srid, c.sourceSRID)
	}
	return fmt.Sprintf("ST_Transform(%s, %d)", expr, c.sourceSRID)
}

func (c *fesCompiler) geometryLiteral(index, srid int) string {
	expr := fmt.Sprintf("ST_GeomFromText($%d, %d)", index, srid)
	if c.dialect == datasource.SQLDuckDB {
		// DuckDB spatial resolves the WKT argument during statement binding.
		// An untyped parameter fails before the driver supplies its string value.
		expr = fmt.Sprintf("ST_GeomFromText($%d::VARCHAR)", index)
	}
	return c.transformGeometry(expr, srid)
}
