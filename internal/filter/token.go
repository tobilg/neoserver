package filter

import "fmt"

type tokenType int

const (
	tokEOF tokenType = iota
	tokInvalid
	tokIdent
	tokString
	tokNumber
	tokBool
	tokTemporal

	tokLParen
	tokRParen
	tokComma

	// operators / keywords
	tokAnd
	tokOr
	tokNot

	tokEq
	tokNeq
	tokLt
	tokGt
	tokLte
	tokGte

	tokPlus
	tokMinus
	tokMul
	tokDiv

	tokLike
	tokILike
	tokBetween
	tokIn
	tokIs
	tokNull

	// spatial / distance
	tokSpatialOp
	tokDWithin

	// geometry literals
	tokPoint
	tokLineString
	tokPolygon
	tokMultiPoint
	tokMultiLineString
	tokMultiPolygon
	tokGeometryCollection
	tokEnvelope
)

type token struct {
	typ tokenType
	raw string
	pos int
}

func (t token) String() string {
	return fmt.Sprintf("%v(%q)", t.typ, t.raw)
}
