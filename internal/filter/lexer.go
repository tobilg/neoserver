package filter

import (
	"strings"
	"unicode"
)

type lexer struct {
	s string
	i int
	n int
}

func newLexer(s string) *lexer {
	return &lexer{s: s, n: len(s)}
}

func (l *lexer) next() token {
	l.skipWS()
	if l.i >= l.n {
		return token{typ: tokEOF, pos: l.i}
	}
	ch := l.s[l.i]
	pos := l.i

	switch ch {
	case '(':
		l.i++
		return token{typ: tokLParen, raw: "(", pos: pos}
	case ')':
		l.i++
		return token{typ: tokRParen, raw: ")", pos: pos}
	case ',':
		l.i++
		return token{typ: tokComma, raw: ",", pos: pos}
	case '+':
		l.i++
		return token{typ: tokPlus, raw: "+", pos: pos}
	case '-':
		// could be number sign or minus op; let number parser decide if followed by digit/period
		if l.peekNumberStart() {
			return l.lexNumber()
		}
		l.i++
		return token{typ: tokMinus, raw: "-", pos: pos}
	case '*':
		l.i++
		return token{typ: tokMul, raw: "*", pos: pos}
	case '/':
		l.i++
		return token{typ: tokDiv, raw: "/", pos: pos}
	case '<':
		l.i++
		if l.i < l.n && l.s[l.i] == '=' {
			l.i++
			return token{typ: tokLte, raw: "<=", pos: pos}
		}
		if l.i < l.n && l.s[l.i] == '>' {
			l.i++
			return token{typ: tokNeq, raw: "<>", pos: pos}
		}
		return token{typ: tokLt, raw: "<", pos: pos}
	case '>':
		l.i++
		if l.i < l.n && l.s[l.i] == '=' {
			l.i++
			return token{typ: tokGte, raw: ">=", pos: pos}
		}
		return token{typ: tokGt, raw: ">", pos: pos}
	case '=':
		l.i++
		return token{typ: tokEq, raw: "=", pos: pos}
	case '\'':
		return l.lexString()
	case '"':
		return l.lexQuotedIdent()
	default:
		if isDigit(ch) || ch == '.' {
			if tok, ok := l.lexTemporalCandidate(); ok {
				return tok
			}
			return l.lexNumber()
		}
		if isAlpha(ch) {
			return l.lexWord()
		}
		// Preserve invalid input so parsers reject it instead of silently
		// accepting a valid prefix.
		l.i++
		return token{typ: tokInvalid, raw: string(ch), pos: pos}
	}
}

func (l *lexer) lexTemporalCandidate() (token, bool) {
	start := l.i
	end := start
	for end < l.n {
		ch := l.s[end]
		if !(isDigit(ch) || isAlpha(ch) || ch == '-' || ch == '+' || ch == ':' || ch == '.') {
			break
		}
		end++
	}
	raw := l.s[start:end]
	if !looksLikeDateOrDateTime(raw) {
		return token{}, false
	}
	l.i = end
	return token{typ: tokTemporal, raw: raw, pos: start}, true
}

func (l *lexer) skipWS() {
	for l.i < l.n {
		if !unicode.IsSpace(rune(l.s[l.i])) {
			return
		}
		l.i++
	}
}

func (l *lexer) peekNumberStart() bool {
	if l.i >= l.n {
		return false
	}
	if l.s[l.i] != '-' && l.s[l.i] != '+' {
		return false
	}
	if l.i+1 >= l.n {
		return false
	}
	n := l.s[l.i+1]
	return isDigit(n) || n == '.'
}

func (l *lexer) lexString() token {
	pos := l.i
	// consume opening '
	l.i++
	var sb strings.Builder
	for l.i < l.n {
		ch := l.s[l.i]
		if ch == '\'' {
			// doubled quote => escaped quote
			if l.i+1 < l.n && l.s[l.i+1] == '\'' {
				sb.WriteByte('\'')
				l.i += 2
				continue
			}
			// end
			l.i++
			return token{typ: tokString, raw: sb.String(), pos: pos}
		}
		sb.WriteByte(ch)
		l.i++
	}
	return token{typ: tokInvalid, raw: l.s[pos:], pos: pos}
}

func (l *lexer) lexQuotedIdent() token {
	pos := l.i
	l.i++ // opening "
	var sb strings.Builder
	for l.i < l.n {
		ch := l.s[l.i]
		if ch == '"' {
			// doubled "" => escaped "
			if l.i+1 < l.n && l.s[l.i+1] == '"' {
				sb.WriteByte('"')
				l.i += 2
				continue
			}
			l.i++
			return token{typ: tokIdent, raw: sb.String(), pos: pos}
		}
		sb.WriteByte(ch)
		l.i++
	}
	return token{typ: tokInvalid, raw: l.s[pos:], pos: pos}
}

func (l *lexer) lexWord() token {
	pos := l.i
	start := l.i
	l.i++
	for l.i < l.n {
		ch := l.s[l.i]
		if isAlpha(ch) || isDigit(ch) || ch == '_' || ch == ':' || ch == '$' {
			l.i++
			continue
		}
		break
	}
	raw := l.s[start:l.i]
	up := strings.ToUpper(raw)

	switch up {
	case "AND":
		return token{typ: tokAnd, raw: raw, pos: pos}
	case "OR":
		return token{typ: tokOr, raw: raw, pos: pos}
	case "NOT":
		return token{typ: tokNot, raw: raw, pos: pos}
	case "LIKE":
		return token{typ: tokLike, raw: raw, pos: pos}
	case "ILIKE":
		return token{typ: tokILike, raw: raw, pos: pos}
	case "BETWEEN":
		return token{typ: tokBetween, raw: raw, pos: pos}
	case "IN":
		return token{typ: tokIn, raw: raw, pos: pos}
	case "IS":
		return token{typ: tokIs, raw: raw, pos: pos}
	case "NULL":
		return token{typ: tokNull, raw: raw, pos: pos}
	case "TRUE", "FALSE":
		return token{typ: tokBool, raw: strings.ToLower(up), pos: pos}
	case "NOW":
		// NOW() is handled in parser as temporal function
		return token{typ: tokTemporal, raw: "NOW", pos: pos}
	// spatial/distance operators
	case "EQUALS", "DISJOINT", "TOUCHES", "WITHIN", "OVERLAPS", "CROSSES", "INTERSECTS", "CONTAINS", "S_INTERSECTS":
		return token{typ: tokSpatialOp, raw: strings.ToLower(up), pos: pos}
	case "DWITHIN":
		return token{typ: tokDWithin, raw: strings.ToLower(up), pos: pos}
	// geometry literals
	case "POINT":
		return token{typ: tokPoint, raw: "POINT", pos: pos}
	case "LINESTRING":
		return token{typ: tokLineString, raw: "LINESTRING", pos: pos}
	case "POLYGON":
		return token{typ: tokPolygon, raw: "POLYGON", pos: pos}
	case "MULTIPOINT":
		return token{typ: tokMultiPoint, raw: "MULTIPOINT", pos: pos}
	case "MULTILINESTRING":
		return token{typ: tokMultiLineString, raw: "MULTILINESTRING", pos: pos}
	case "MULTIPOLYGON":
		return token{typ: tokMultiPolygon, raw: "MULTIPOLYGON", pos: pos}
	case "GEOMETRYCOLLECTION":
		return token{typ: tokGeometryCollection, raw: "GEOMETRYCOLLECTION", pos: pos}
	case "ENVELOPE", "BBOX":
		return token{typ: tokEnvelope, raw: "ENVELOPE", pos: pos}
	}

	// Temporal literals like 2020-01-01 or 2020-01-01T10:11:12Z are tokenized as identifier-like,
	// but we can treat them as temporal if they match simple patterns.
	if looksLikeDateOrDateTime(raw) {
		return token{typ: tokTemporal, raw: raw, pos: pos}
	}

	return token{typ: tokIdent, raw: raw, pos: pos}
}

func (l *lexer) lexNumber() token {
	pos := l.i
	start := l.i
	// optional sign
	if l.i < l.n && (l.s[l.i] == '+' || l.s[l.i] == '-') {
		l.i++
	}
	// digits / period
	for l.i < l.n {
		ch := l.s[l.i]
		if isDigit(ch) || ch == '.' {
			l.i++
			continue
		}
		break
	}
	// exponent
	if l.i < l.n && (l.s[l.i] == 'e' || l.s[l.i] == 'E') {
		l.i++
		if l.i < l.n && (l.s[l.i] == '+' || l.s[l.i] == '-') {
			l.i++
		}
		for l.i < l.n && isDigit(l.s[l.i]) {
			l.i++
		}
	}
	return token{typ: tokNumber, raw: l.s[start:l.i], pos: pos}
}

func isAlpha(b byte) bool { return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') }
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func looksLikeDateOrDateTime(s string) bool {
	// Extremely lightweight: YYYY-MM-DD or YYYY-MM-DDTHH:MM...
	if len(s) < 10 {
		return false
	}
	if s[4] != '-' || s[7] != '-' {
		return false
	}
	for _, idx := range []int{0, 1, 2, 3, 5, 6, 8, 9} {
		if idx >= len(s) || (s[idx] < '0' || s[idx] > '9') {
			return false
		}
	}
	return true
}
