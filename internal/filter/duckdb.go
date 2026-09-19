package filter

import (
	"fmt"
	"strconv"
	"strings"
)

// DuckDBOptions contains options for DuckDB CQL2 compilation.
type DuckDBOptions struct {
	StartParamIndex int

	// CRS handling for geometry literals in the filter.
	FilterSRID int
	SourceSRID int

	// Allowed property names (unquoted, case-sensitive) for scalar operations.
	AllowedProperties map[string]struct{}

	// Name of the geometry column (unquoted) that can be used in spatial predicates.
	GeometryProperty string

	// GeometryExpression is a trusted adapter expression, never request text.
	// WKB-backed sources need ST_GeomFromWKB rather than their raw BLOB column.
	GeometryExpression string
}

// CompileForDuckDB parses a CQL2 text filter and returns a DuckDB-compatible SQL expression plus args.
func CompileForDuckDB(cql string, opt DuckDBOptions) (sql string, args []any, nextIndex int, err error) {
	p := newDuckDBParser(cql, opt)
	out, err := p.parse()
	if err != nil {
		return "", nil, opt.StartParamIndex, err
	}
	return out, p.args, p.next, nil
}

type duckdbParser struct {
	l    *lexer
	cur  token
	peek token

	args []any
	next int

	opt DuckDBOptions
}

func newDuckDBParser(input string, opt DuckDBOptions) *duckdbParser {
	if opt.StartParamIndex <= 0 {
		opt.StartParamIndex = 1
	}
	if opt.FilterSRID == 0 {
		opt.FilterSRID = 4326
	}
	if opt.GeometryProperty == "" {
		opt.GeometryProperty = "geom"
	}
	p := &duckdbParser{
		l:    newLexer(input),
		next: opt.StartParamIndex,
		opt:  opt,
	}
	p.cur = p.l.next()
	p.peek = p.l.next()
	return p
}

func (p *duckdbParser) parse() (string, error) {
	if strings.TrimSpace(p.l.s) == "" {
		return "", nil
	}
	sql, err := p.parseOr()
	if err != nil {
		return "", err
	}
	if p.cur.typ != tokEOF {
		return "", fmt.Errorf("unexpected token %q at %d", p.cur.raw, p.cur.pos)
	}
	return sql, nil
}

func (p *duckdbParser) bump() {
	p.cur = p.peek
	p.peek = p.l.next()
}

func (p *duckdbParser) expect(t tokenType) (token, error) {
	if p.cur.typ != t {
		return token{}, fmt.Errorf("expected %v at %d, got %q", t, p.cur.pos, p.cur.raw)
	}
	tok := p.cur
	p.bump()
	return tok, nil
}

func (p *duckdbParser) parseOr() (string, error) {
	left, err := p.parseAnd()
	if err != nil {
		return "", err
	}
	for p.cur.typ == tokOr {
		p.bump()
		right, err := p.parseAnd()
		if err != nil {
			return "", err
		}
		left = "(" + left + " OR " + right + ")"
	}
	return left, nil
}

func (p *duckdbParser) parseAnd() (string, error) {
	left, err := p.parseNot()
	if err != nil {
		return "", err
	}
	for p.cur.typ == tokAnd {
		p.bump()
		right, err := p.parseNot()
		if err != nil {
			return "", err
		}
		left = "(" + left + " AND " + right + ")"
	}
	return left, nil
}

func (p *duckdbParser) parseNot() (string, error) {
	if p.cur.typ == tokNot {
		p.bump()
		expr, err := p.parseNot()
		if err != nil {
			return "", err
		}
		return "(NOT " + expr + ")", nil
	}
	return p.parsePrimary()
}

func (p *duckdbParser) parsePrimary() (string, error) {
	if p.cur.typ == tokLParen {
		p.bump()
		inner, err := p.parseOr()
		if err != nil {
			return "", err
		}
		if _, err := p.expect(tokRParen); err != nil {
			return "", err
		}
		return "(" + inner + ")", nil
	}
	return p.parsePredicate()
}

func (p *duckdbParser) parsePredicate() (string, error) {
	// Spatial predicate: SpatialOperator '(' geomExpr ',' geomExpr ')'
	if p.cur.typ == tokSpatialOp {
		op := strings.ToLower(p.cur.raw)
		p.bump()
		if _, err := p.expect(tokLParen); err != nil {
			return "", err
		}
		g1, err := p.parseGeomExpr()
		if err != nil {
			return "", err
		}
		if _, err := p.expect(tokComma); err != nil {
			return "", err
		}
		g2, err := p.parseGeomExpr()
		if err != nil {
			return "", err
		}
		if _, err := p.expect(tokRParen); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s, %s)", toDuckDBFunction(op), g1, g2), nil
	}

	// Distance predicate: dwithin '(' geomExpr ',' geomExpr ',' number ')'
	if p.cur.typ == tokDWithin {
		p.bump()
		if _, err := p.expect(tokLParen); err != nil {
			return "", err
		}
		g1, err := p.parseGeomExpr()
		if err != nil {
			return "", err
		}
		if _, err := p.expect(tokComma); err != nil {
			return "", err
		}
		g2, err := p.parseGeomExpr()
		if err != nil {
			return "", err
		}
		if _, err := p.expect(tokComma); err != nil {
			return "", err
		}
		if p.cur.typ != tokNumber {
			return "", fmt.Errorf("expected numeric distance at %d", p.cur.pos)
		}
		dist, err := strconv.ParseFloat(p.cur.raw, 64)
		if err != nil {
			return "", fmt.Errorf("invalid numeric distance %q", p.cur.raw)
		}
		ph := p.addArg(dist)
		p.bump()
		if _, err := p.expect(tokRParen); err != nil {
			return "", err
		}
		// DuckDB uses ST_DWithin with same signature
		return fmt.Sprintf("ST_DWithin(%s, %s, %s)", g1, g2, ph), nil
	}

	return p.parseComparison()
}

func (p *duckdbParser) parseComparison() (string, error) {
	if p.cur.typ == tokIdent {
		prop := p.cur.raw
		if err := p.assertAllowedProperty(prop); err != nil {
			return "", err
		}
		p.bump()

		not := false
		if p.cur.typ == tokNot && (p.peek.typ == tokLike || p.peek.typ == tokILike || p.peek.typ == tokIn) {
			not = true
			p.bump()
		}
		if p.cur.typ == tokLike || p.cur.typ == tokILike {
			opTok := p.cur
			p.bump()
			if p.cur.typ != tokString {
				return "", fmt.Errorf("LIKE requires string literal at %d", p.cur.pos)
			}
			ph := p.addArg(p.cur.raw)
			p.bump()
			op := "LIKE"
			if opTok.typ == tokILike {
				op = "ILIKE"
			}
			if not {
				return fmt.Sprintf("%s NOT %s %s", quoteIdent(prop), op, ph), nil
			}
			return fmt.Sprintf("%s %s %s", quoteIdent(prop), op, ph), nil
		}

		if p.cur.typ == tokIs {
			p.bump()
			not := false
			if p.cur.typ == tokNot {
				not = true
				p.bump()
			}
			if p.cur.typ != tokNull {
				return "", fmt.Errorf("expected NULL after IS at %d", p.cur.pos)
			}
			p.bump()
			if not {
				return fmt.Sprintf("%s IS NOT NULL", quoteIdent(prop)), nil
			}
			return fmt.Sprintf("%s IS NULL", quoteIdent(prop)), nil
		}

		if p.cur.typ == tokIn {
			p.bump()
			if _, err := p.expect(tokLParen); err != nil {
				return "", err
			}
			var phs []string
			for {
				if p.cur.typ == tokString {
					phs = append(phs, p.addArg(p.cur.raw))
					p.bump()
				} else if p.cur.typ == tokNumber {
					n, err := strconv.ParseFloat(p.cur.raw, 64)
					if err != nil {
						return "", fmt.Errorf("invalid number %q", p.cur.raw)
					}
					phs = append(phs, p.addArg(n))
					p.bump()
				} else {
					return "", fmt.Errorf("expected literal in IN list at %d", p.cur.pos)
				}
				if p.cur.typ == tokComma {
					p.bump()
					continue
				}
				break
			}
			if _, err := p.expect(tokRParen); err != nil {
				return "", err
			}
			if not {
				return fmt.Sprintf("%s NOT IN (%s)", quoteIdent(prop), strings.Join(phs, ", ")), nil
			}
			return fmt.Sprintf("%s IN (%s)", quoteIdent(prop), strings.Join(phs, ", ")), nil
		}

		left := quoteIdent(prop)

		if p.cur.typ == tokNot && p.peek.typ == tokBetween {
			p.bump()
			p.bump()
			a, err := p.parseScalarExpr(0)
			if err != nil {
				return "", err
			}
			if p.cur.typ != tokAnd {
				return "", fmt.Errorf("expected AND in BETWEEN at %d", p.cur.pos)
			}
			p.bump()
			b, err := p.parseScalarExpr(0)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s NOT BETWEEN %s AND %s", left, a, b), nil
		}
		if p.cur.typ == tokBetween {
			p.bump()
			a, err := p.parseScalarExpr(0)
			if err != nil {
				return "", err
			}
			if p.cur.typ != tokAnd {
				return "", fmt.Errorf("expected AND in BETWEEN at %d", p.cur.pos)
			}
			p.bump()
			b, err := p.parseScalarExpr(0)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s BETWEEN %s AND %s", left, a, b), nil
		}
		switch p.cur.typ {
		case tokEq, tokNeq, tokLt, tokGt, tokLte, tokGte:
			op := p.cur.raw
			p.bump()
			right, err := p.parseScalarExpr(0)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s %s %s", left, op, right), nil
		default:
			return "", fmt.Errorf("expected comparison operator at %d", p.cur.pos)
		}
	}

	left, err := p.parseScalarExpr(0)
	if err != nil {
		return "", err
	}
	if p.cur.typ == tokNot && p.peek.typ == tokBetween {
		p.bump()
		p.bump()
		a, err := p.parseScalarExpr(0)
		if err != nil {
			return "", err
		}
		if p.cur.typ != tokAnd {
			return "", fmt.Errorf("expected AND in BETWEEN at %d", p.cur.pos)
		}
		p.bump()
		b, err := p.parseScalarExpr(0)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s NOT BETWEEN %s AND %s", left, a, b), nil
	}
	if p.cur.typ == tokBetween {
		p.bump()
		a, err := p.parseScalarExpr(0)
		if err != nil {
			return "", err
		}
		if p.cur.typ != tokAnd {
			return "", fmt.Errorf("expected AND in BETWEEN at %d", p.cur.pos)
		}
		p.bump()
		b, err := p.parseScalarExpr(0)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s BETWEEN %s AND %s", left, a, b), nil
	}

	switch p.cur.typ {
	case tokEq, tokNeq, tokLt, tokGt, tokLte, tokGte:
		op := p.cur.raw
		p.bump()
		right, err := p.parseScalarExpr(0)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s", left, op, right), nil
	default:
		return "", fmt.Errorf("expected comparison operator at %d", p.cur.pos)
	}
}

func (p *duckdbParser) parseScalarExpr(minPrec int) (string, error) {
	left, err := p.parseScalarPrimary()
	if err != nil {
		return "", err
	}
	for {
		prec, ok := scalarPrec(p.cur.typ)
		if !ok || prec < minPrec {
			break
		}
		op := p.cur.raw
		p.bump()
		right, err := p.parseScalarExpr(prec + 1)
		if err != nil {
			return "", err
		}
		left = "(" + left + " " + op + " " + right + ")"
	}
	return left, nil
}

func (p *duckdbParser) parseScalarPrimary() (string, error) {
	switch p.cur.typ {
	case tokLParen:
		p.bump()
		inner, err := p.parseScalarExpr(0)
		if err != nil {
			return "", err
		}
		if _, err := p.expect(tokRParen); err != nil {
			return "", err
		}
		return "(" + inner + ")", nil
	case tokIdent:
		prop := p.cur.raw
		if err := p.assertAllowedProperty(prop); err != nil {
			return "", err
		}
		p.bump()
		return quoteIdent(prop), nil
	case tokString:
		ph := p.addArg(p.cur.raw)
		p.bump()
		return ph, nil
	case tokNumber:
		n, err := strconv.ParseFloat(p.cur.raw, 64)
		if err != nil {
			return "", fmt.Errorf("invalid number %q", p.cur.raw)
		}
		ph := p.addArg(n)
		p.bump()
		return ph, nil
	case tokBool:
		v := strings.ToLower(p.cur.raw)
		p.bump()
		return v, nil
	case tokTemporal:
		val := p.cur.raw
		if strings.EqualFold(val, "NOW") && p.peek.typ == tokLParen {
			p.bump()
			p.bump()
			if _, err := p.expect(tokRParen); err != nil {
				return "", err
			}
			return "NOW()", nil
		}
		ph := p.addArg(val)
		p.bump()
		// DuckDB uses CAST for timestamp conversion
		if strings.ContainsAny(val, "Z+") || (strings.Contains(val, "T") && strings.Contains(val, ":")) {
			return fmt.Sprintf("CAST(%s AS TIMESTAMPTZ)", ph), nil
		}
		return fmt.Sprintf("CAST(%s AS TIMESTAMP)", ph), nil
	default:
		return "", fmt.Errorf("unexpected scalar token %q at %d", p.cur.raw, p.cur.pos)
	}
}

func (p *duckdbParser) parseGeomExpr() (string, error) {
	if p.cur.typ == tokIdent {
		name := p.cur.raw
		if name != p.opt.GeometryProperty {
			return "", fmt.Errorf("unsupported geometry property %q (allowed: %q)", name, p.opt.GeometryProperty)
		}
		p.bump()
		if p.opt.GeometryExpression != "" {
			return p.opt.GeometryExpression, nil
		}
		return quoteIdent(name), nil
	}

	wkt, err := p.parseGeomLiteralWKT()
	if err != nil {
		return "", err
	}
	ph := p.addArg(wkt)
	sql := fmt.Sprintf("ST_GeomFromText(CAST(%s AS VARCHAR))", ph)
	if p.opt.SourceSRID != 0 && p.opt.SourceSRID != p.opt.FilterSRID {
		sql = fmt.Sprintf("ST_Transform(%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", sql, p.opt.FilterSRID, p.opt.SourceSRID)
	}
	return sql, nil
}

func (p *duckdbParser) parseGeomLiteralWKT() (string, error) {
	switch p.cur.typ {
	case tokEnvelope:
		p.bump()
		if _, err := p.expect(tokLParen); err != nil {
			return "", err
		}
		coords := make([]string, 0, 4)
		for i := 0; i < 4; i++ {
			if p.cur.typ != tokNumber {
				return "", fmt.Errorf("expected number in ENVELOPE at %d", p.cur.pos)
			}
			coords = append(coords, p.cur.raw)
			p.bump()
			if i < 3 {
				if _, err := p.expect(tokComma); err != nil {
					return "", err
				}
			}
		}
		if _, err := p.expect(tokRParen); err != nil {
			return "", err
		}
		return envelopeWKT(coords)
	case tokPoint, tokLineString, tokPolygon, tokMultiPoint, tokMultiLineString, tokMultiPolygon, tokGeometryCollection:
		kw := p.cur.raw
		p.bump()
		if p.cur.typ != tokLParen {
			return "", fmt.Errorf("expected '(' after %s at %d", kw, p.cur.pos)
		}
		var sb strings.Builder
		sb.WriteString(strings.ToUpper(kw))
		depth := 0
		prevNum := false
		for {
			if p.cur.typ == tokLParen {
				depth++
			} else if p.cur.typ == tokRParen {
				depth--
			}
			if prevNum && p.cur.typ == tokNumber {
				sb.WriteByte(' ')
			}
			sb.WriteString(p.cur.raw)
			prevNum = p.cur.typ == tokNumber
			p.bump()
			if depth == 0 {
				break
			}
			if p.cur.typ == tokEOF {
				return "", fmt.Errorf("unterminated geometry literal")
			}
		}
		return sb.String(), nil
	default:
		return "", fmt.Errorf("expected geometry literal at %d", p.cur.pos)
	}
}

func (p *duckdbParser) addArg(v any) string {
	ph := fmt.Sprintf("$%d", p.next)
	p.next++
	p.args = append(p.args, v)
	return ph
}

func (p *duckdbParser) assertAllowedProperty(name string) error {
	// Fail closed: a nil allowlist means no properties were declared, so deny
	// every property reference rather than passing arbitrary identifiers through.
	// Callers must supply the set of queryable columns explicitly.
	if _, ok := p.opt.AllowedProperties[name]; ok {
		return nil
	}
	return fmt.Errorf("unknown property %q", name)
}

var duckdbFunctionForCql = map[string]string{
	"crosses":      "ST_Crosses",
	"contains":     "ST_Contains",
	"disjoint":     "ST_Disjoint",
	"equals":       "ST_Equals",
	"intersects":   "ST_Intersects",
	"s_intersects": "ST_Intersects",
	"overlaps":     "ST_Overlaps",
	"touches":      "ST_Touches",
	"within":       "ST_Within",
	"dwithin":      "ST_DWithin",
}

func toDuckDBFunction(cqlFunName string) string {
	if fun, ok := duckdbFunctionForCql[strings.ToLower(cqlFunName)]; ok {
		return fun
	}
	return "UNKNOWN_" + cqlFunName
}
