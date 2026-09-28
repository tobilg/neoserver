// Package sqlutil holds the quoting shared by every generated SQL statement.
// PostgreSQL and DuckDB use the same rules for identifiers and literals.
package sqlutil

import "strings"

// QuoteIdent quotes an identifier, doubling embedded double quotes.
func QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// QuoteLiteral returns value as a single-quoted string literal. Prefer bound
// parameters; use this only where a statement cannot take them.
func QuoteLiteral(value string) string {
	return "'" + EscapeLiteral(value) + "'"
}

// EscapeLiteral escapes value for a position already inside single quotes,
// such as the path and key of an ATTACH statement.
func EscapeLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
