package filter

import (
	"fmt"
	"strconv"
	"strings"
)

type Options struct {
	StartParamIndex int

	// CRS handling for geometry literals in the filter.
	FilterSRID int
	SourceSRID int

	// Allowed property names (unquoted, case-sensitive) for scalar operations.
	AllowedProperties map[string]struct{}

	// Name of the geometry column (unquoted) that can be used in spatial predicates.
	GeometryProperty string
}

// Compile parses a CQL2 text filter and returns a parameterized SQL expression plus args.
// The resulting SQL is suitable for inclusion inside a WHERE clause.
func Compile(cql string, opt Options) (sql string, args []any, nextIndex int, err error) {
	p := newParser(cql, opt)
	out, err := p.parse()
	if err != nil {
		return "", nil, opt.StartParamIndex, err
	}
	return out, p.args, p.next, nil
}

type parser struct {
	l    *lexer
	cur  token
	peek token

	args []any
	next int

	opt Options
}

func newParser(input string, opt Options) *parser {
	if opt.StartParamIndex <= 0 {
		opt.StartParamIndex = 1
	}
	if opt.FilterSRID == 0 {
		opt.FilterSRID = 4326
	}
	if opt.GeometryProperty == "" {
		opt.GeometryProperty = "geom"
	}
	p := &parser{
		l:    newLexer(input),
		next: opt.StartParamIndex,
		opt:  opt,
	}
	p.cur = p.l.next()
	p.peek = p.l.next()
	return p
}

func (p *parser) parse() (string, error) {
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

func (p *parser) bump() {
	p.cur = p.peek
	p.peek = p.l.next()
}

func (p *parser) expect(t tokenType) (token, error) {
	if p.cur.typ != t {
		return token{}, fmt.Errorf("expected %v at %d, got %q", t, p.cur.pos, p.cur.raw)
	}
	tok := p.cur
	p.bump()
	return tok, nil
}

// booleanExpression: OR lowest precedence, AND, NOT, primary.
func (p *parser) parseOr() (string, error) {
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

func (p *parser) parseAnd() (string, error) {
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

func (p *parser) parseNot() (string, error) {
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

func (p *parser) parsePrimary() (string, error) {
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

func (p *parser) parsePredicate() (string, error) {
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
		return fmt.Sprintf("%s(%s,%s)", toPostGISFunction(op), g1, g2), nil
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
		return fmt.Sprintf("ST_DWithin(%s,%s,%s)", g1, g2, ph), nil
	}

	// Comparison predicate (including like/between/in/is null)
	return p.parseComparison()
}

func (p *parser) parseComparison() (string, error) {
	// LIKE / ILIKE / IN / IS NULL require starting with property name.
	if p.cur.typ == tokIdent {
		prop := p.cur.raw
		if err := p.assertAllowedProperty(prop); err != nil {
			return "", err
		}
		p.bump() // consume ident

		// propertyName (NOT)? (LIKE|ILIKE) string
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

		// propertyName IS (NOT)? NULL
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

		// propertyName (NOT)? IN (...)
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
				return fmt.Sprintf("%s NOT IN (%s)", quoteIdent(prop), strings.Join(phs, ",")), nil
			}
			return fmt.Sprintf("%s IN (%s)", quoteIdent(prop), strings.Join(phs, ",")), nil
		}

		// If we got here, this wasn't a special property-starting predicate.
		// Rewind by treating the property as a scalar expression head.
		// We return the quoted identifier and let the generic scalar parser continue.
		// Note: to keep the parser simple, we don't actually rewind tokens; instead,
		// we construct `left` here and continue with binary comparison parsing below.
		left := quoteIdent(prop)

		// BETWEEN / binary comparison follow.
		if p.cur.typ == tokNot && p.peek.typ == tokBetween {
			p.bump() // NOT
			p.bump() // BETWEEN
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

	// BETWEEN is handled as: scalarExpr (NOT)? BETWEEN scalarExpr AND scalarExpr
	// We'll parse as left scalar expression first, then see if BETWEEN follows.
	left, err := p.parseScalarExpr(0)
	if err != nil {
		return "", err
	}
	if p.cur.typ == tokNot && p.peek.typ == tokBetween {
		p.bump() // NOT
		p.bump() // BETWEEN
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

	// Binary comparison: left op right
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

// Scalar expression precedence: * / higher than + -
func (p *parser) parseScalarExpr(minPrec int) (string, error) {
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

func scalarPrec(t tokenType) (int, bool) {
	switch t {
	case tokMul, tokDiv:
		return 20, true
	case tokPlus, tokMinus:
		return 10, true
	default:
		return 0, false
	}
}

func (p *parser) parseScalarPrimary() (string, error) {
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
		// parse to float64 to ensure it's a real number
		n, err := strconv.ParseFloat(p.cur.raw, 64)
		if err != nil {
			return "", fmt.Errorf("invalid number %q", p.cur.raw)
		}
		ph := p.addArg(n)
		p.bump()
		return ph, nil
	case tokBool:
		// bool literal, no arg required
		v := strings.ToLower(p.cur.raw)
		p.bump()
		return v, nil
	case tokTemporal:
		// NOW() or timestamp/date literal
		val := p.cur.raw
		if strings.EqualFold(val, "NOW") && p.peek.typ == tokLParen {
			p.bump() // NOW
			p.bump() // (
			if _, err := p.expect(tokRParen); err != nil {
				return "", err
			}
			return "NOW()", nil
		}
		ph := p.addArg(val)
		p.bump()
		// Cast is handled by Postgres; use timestamptz when timezone present.
		if strings.ContainsAny(val, "Z+") || (strings.Contains(val, "T") && strings.Contains(val, ":") && (strings.Contains(val, "Z") || strings.Contains(val, "+") || strings.Contains(val, "-"))) {
			return ph + "::timestamptz", nil
		}
		return ph + "::timestamp", nil
	default:
		return "", fmt.Errorf("unexpected scalar token %q at %d", p.cur.raw, p.cur.pos)
	}
}

func (p *parser) parseGeomExpr() (string, error) {
	// either property name or geom literal.
	if p.cur.typ == tokIdent {
		name := p.cur.raw
		// Only allow the configured geometry property in geom expressions.
		if name != p.opt.GeometryProperty {
			return "", fmt.Errorf("unsupported geometry property %q (allowed: %q)", name, p.opt.GeometryProperty)
		}
		p.bump()
		return quoteIdent(name), nil
	}

	wkt, err := p.parseGeomLiteralWKT()
	if err != nil {
		return "", err
	}
	ewkt := fmt.Sprintf("SRID=%d;%s", p.opt.FilterSRID, wkt)
	ph := p.addArg(ewkt)
	sql := ph + "::geometry"
	if p.opt.SourceSRID != 0 && p.opt.SourceSRID != p.opt.FilterSRID {
		sql = fmt.Sprintf("ST_Transform(%s,%d)", sql, p.opt.SourceSRID)
	}
	return sql, nil
}

func (p *parser) parseGeomLiteralWKT() (string, error) {
	switch p.cur.typ {
	case tokEnvelope:
		// ENVELOPE(xmin,ymin,xmax,ymax)
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
		// We represent envelope as WKT polygon to keep things simple and portable.
		// POLYGON((xmin ymin, xmax ymin, xmax ymax, xmin ymax, xmin ymin))
		return envelopeWKT(coords)
	case tokPoint, tokLineString, tokPolygon, tokMultiPoint, tokMultiLineString, tokMultiPolygon, tokGeometryCollection:
		// Reconstruct WKT by consuming tokens while balancing parentheses.
		kw := p.cur.raw
		p.bump()
		if p.cur.typ != tokLParen {
			return "", fmt.Errorf("expected '(' after %s at %d", kw, p.cur.pos)
		}
		var sb strings.Builder
		sb.WriteString(strings.ToUpper(kw))
		// capture everything from the first '(' through its matching ')', including nested parens.
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

func (p *parser) addArg(v any) string {
	ph := fmt.Sprintf("$%d", p.next)
	p.next++
	p.args = append(p.args, v)
	return ph
}

func (p *parser) assertAllowedProperty(name string) error {
	// Fail closed: a nil allowlist means no properties were declared, so deny
	// every property reference rather than passing arbitrary identifiers through.
	// Callers must supply the set of queryable columns explicitly.
	if _, ok := p.opt.AllowedProperties[name]; ok {
		return nil
	}
	return fmt.Errorf("unknown property %q", name)
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func envelopeWKT(coords []string) (string, error) {
	if len(coords) != 4 {
		return "", fmt.Errorf("BBOX requires four coordinates")
	}
	minX, err := strconv.ParseFloat(coords[0], 64)
	if err != nil {
		return "", fmt.Errorf("invalid BBOX minimum x: %w", err)
	}
	maxX, err := strconv.ParseFloat(coords[2], 64)
	if err != nil {
		return "", fmt.Errorf("invalid BBOX maximum x: %w", err)
	}
	if minX <= maxX {
		return fmt.Sprintf("POLYGON((%s %s,%s %s,%s %s,%s %s,%s %s))",
			coords[0], coords[1], coords[2], coords[1], coords[2], coords[3],
			coords[0], coords[3], coords[0], coords[1]), nil
	}
	// A west value greater than east denotes a CRS84 antimeridian crossing.
	// Represent it as two polygons so spatial predicates preserve the intended
	// short wrapped envelope.
	return fmt.Sprintf("MULTIPOLYGON(((%s %s,180 %s,180 %s,%s %s,%s %s)),((-180 %s,%s %s,%s %s,-180 %s,-180 %s)))",
		coords[0], coords[1], coords[1], coords[3], coords[0], coords[3], coords[0], coords[1],
		coords[1], coords[2], coords[1], coords[2], coords[3], coords[3], coords[1]), nil
}

var pgFunctionForCql = map[string]string{
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

func toPostGISFunction(cqlFunName string) string {
	if fun, ok := pgFunctionForCql[strings.ToLower(cqlFunName)]; ok {
		return fun
	}
	return "UNKNOWN_" + cqlFunName
}
