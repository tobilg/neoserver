package filter

import (
	"strings"
	"testing"
)

func pgOpts(props ...string) Options {
	allowed := map[string]struct{}{}
	for _, p := range props {
		allowed[p] = struct{}{}
	}
	return Options{StartParamIndex: 1, FilterSRID: 4326, SourceSRID: 4326, AllowedProperties: allowed, GeometryProperty: "geom"}
}

func duckOpts(props ...string) DuckDBOptions {
	allowed := map[string]struct{}{}
	for _, p := range props {
		allowed[p] = struct{}{}
	}
	return DuckDBOptions{StartParamIndex: 1, FilterSRID: 4326, SourceSRID: 4326, AllowedProperties: allowed, GeometryProperty: "geom"}
}

func TestQuoteIdentEscaping(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"name", `"name"`},
		{`na"me`, `"na""me"`},
		{`a""b`, `"a""""b"`},
		{`x;DROP TABLE y--`, `"x;DROP TABLE y--"`},
	}
	for _, tt := range tests {
		if got := quoteIdent(tt.in); got != tt.want {
			t.Errorf("quoteIdent(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

// A hostile identifier admitted through the allowlist must stay inside its
// quotes: the doubled-quote escaping prevents breaking out of the identifier.
func TestHostileAllowlistedIdentifierIsQuoted(t *testing.T) {
	hostile := `a" = '' ; DROP TABLE users; --`
	sql, _, _, err := Compile(hostile+" = 'x'", pgOpts(hostile))
	if err != nil {
		// The lexer may reject such identifiers outright, which is equally safe.
		return
	}
	if !strings.Contains(sql, `"a"" = '' ; DROP TABLE users; --"`) && strings.Contains(sql, "DROP TABLE") {
		t.Fatalf("hostile identifier escaped its quotes: %s", sql)
	}
}

// Literal values must always compile to $N placeholders, never inline SQL.
func TestLiteralsAlwaysParameterized(t *testing.T) {
	injection := `'; DROP TABLE users; --`
	// Inside a CQL string literal a single quote is escaped by doubling.
	quoted := strings.ReplaceAll(injection, `'`, `''`)
	filters := []string{
		"name = '" + quoted + "'",
		"name LIKE '" + quoted + "'",
		"name IN ('" + quoted + "', 'b')",
		"name BETWEEN '" + quoted + "' AND 'z'",
	}
	for _, f := range filters {
		sql, args, _, err := Compile(f, pgOpts("name"))
		if err != nil {
			t.Errorf("Compile(%q) error: %v", f, err)
			continue
		}
		if strings.Contains(sql, "DROP TABLE") {
			t.Errorf("literal leaked into SQL for %q: %s", f, sql)
		}
		found := false
		for _, a := range args {
			if s, ok := a.(string); ok && strings.Contains(s, "DROP TABLE") {
				found = true
			}
		}
		if !found {
			t.Errorf("literal missing from args for %q: %v", f, args)
		}

		dsql, dargs, _, err := CompileForDuckDB(f, duckOpts("name"))
		if err != nil {
			t.Errorf("CompileForDuckDB(%q) error: %v", f, err)
			continue
		}
		if strings.Contains(dsql, "DROP TABLE") {
			t.Errorf("literal leaked into DuckDB SQL for %q: %s", f, dsql)
		}
		if len(dargs) == 0 {
			t.Errorf("no args for DuckDB %q", f)
		}
	}
}

func TestTemporalLiteralCasts(t *testing.T) {
	tests := []struct {
		name     string
		filter   string
		pgWant   string
		duckWant string
	}{
		{"date only", "ts >= 2024-01-15", "::timestamp", "CAST($1 AS TIMESTAMP)"},
		{"datetime utc", "ts >= 2024-01-15T10:30:00Z", "::timestamptz", "CAST($1 AS TIMESTAMPTZ)"},
		{"datetime offset", "ts >= 2024-01-15T10:30:00+02:00", "::timestamptz", "CAST($1 AS TIMESTAMPTZ)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, _, err := Compile(tt.filter, pgOpts("ts"))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if !strings.Contains(sql, tt.pgWant) {
				t.Errorf("pg sql = %s, want containing %s", sql, tt.pgWant)
			}
			if len(args) != 1 {
				t.Errorf("pg args = %v, want the literal as one arg", args)
			}

			dsql, dargs, _, err := CompileForDuckDB(tt.filter, duckOpts("ts"))
			if err != nil {
				t.Fatalf("CompileForDuckDB: %v", err)
			}
			if !strings.Contains(dsql, tt.duckWant) {
				t.Errorf("duckdb sql = %s, want containing %s", dsql, tt.duckWant)
			}
			if len(dargs) != 1 {
				t.Errorf("duckdb args = %v", dargs)
			}
		})
	}
}

func TestLooksLikeDateOrDateTime(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"2024-01-15", true},
		{"2024-01-15T10:30:00Z", true},
		{"2024-1-15", false},
		{"20240115", false},
		{"abcd-ef-gh", false},
		{"2024-01", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := looksLikeDateOrDateTime(tt.in); got != tt.want {
			t.Errorf("looksLikeDateOrDateTime(%q) = %t, want %t", tt.in, got, tt.want)
		}
	}
}

func TestLexerEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		filter string
	}{
		{"unterminated string", "name = 'oops"},
		{"unterminated quoted ident", `"name = 'x'`},
		{"stray operator", "name = = 'x'"},
		{"lone at sign", "name = 'x' @"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, _, err := Compile(tt.filter, pgOpts("name")); err == nil {
				t.Errorf("Compile(%q) should fail", tt.filter)
			}
			if _, _, _, err := CompileForDuckDB(tt.filter, duckOpts("name")); err == nil {
				t.Errorf("CompileForDuckDB(%q) should fail", tt.filter)
			}
		})
	}
}

func TestLexerNumberForms(t *testing.T) {
	tests := []struct {
		filter string
		arg    float64
	}{
		{"age = -5", -5},
		{"age = 1e3", 1000},
		{"age = 1.5E-2", 0.015},
		{"age = .5", 0.5},
	}
	for _, tt := range tests {
		_, args, _, err := Compile(tt.filter, pgOpts("age"))
		if err != nil {
			t.Errorf("Compile(%q): %v", tt.filter, err)
			continue
		}
		if len(args) != 1 || args[0].(float64) != tt.arg {
			t.Errorf("Compile(%q) args = %v, want [%v]", tt.filter, args, tt.arg)
		}
	}

	// A leading + is always the binary operator, never a numeric sign, so a
	// unary-plus literal is a parse error.
	if _, _, _, err := Compile("age = +3.5", pgOpts("age")); err == nil {
		t.Error("unary plus must be rejected")
	}
}

func TestDoubledQuoteEscapesInLiterals(t *testing.T) {
	sql, args, _, err := Compile("name = 'O''Brien'", pgOpts("name"))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(args) != 1 || args[0] != "O'Brien" {
		t.Fatalf("args = %v, want [O'Brien]", args)
	}
	if strings.Contains(sql, "O'Brien") {
		t.Fatalf("literal leaked into SQL: %s", sql)
	}

	// Doubled double-quote inside a quoted identifier resolves to one quote.
	sqlIdent, _, _, err := Compile(`"na""me" = 'x'`, pgOpts(`na"me`))
	if err != nil {
		t.Fatalf("quoted ident: %v", err)
	}
	if !strings.Contains(sqlIdent, `"na""me"`) {
		t.Fatalf("identifier not re-escaped: %s", sqlIdent)
	}
}

func TestScalarArithmeticPrecedence(t *testing.T) {
	sql, args, _, err := Compile("total = a + b * c", pgOpts("total", "a", "b", "c"))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// * binds tighter than +.
	if !strings.Contains(sql, `("a" + ("b" * "c"))`) {
		t.Fatalf("precedence wrong: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("args = %v, want none", args)
	}

	sql, _, _, err = Compile("total = (a + b) * c", pgOpts("total", "a", "b", "c"))
	if err != nil {
		t.Fatalf("Compile parens: %v", err)
	}
	if !strings.Contains(sql, `* "c"`) || !strings.Contains(sql, `"a" + "b"`) {
		t.Fatalf("parenthesized grouping wrong: %s", sql)
	}
}

func TestNegatedOperators(t *testing.T) {
	tests := []struct {
		filter string
		want   string
	}{
		{"name NOT LIKE 'a%'", "NOT LIKE"},
		{"name NOT IN ('a', 'b')", "NOT IN"},
		{"age NOT BETWEEN 1 AND 9", "NOT BETWEEN"},
		{"name IS NOT NULL", "IS NOT NULL"},
	}
	for _, tt := range tests {
		sql, _, _, err := Compile(tt.filter, pgOpts("name", "age"))
		if err != nil {
			t.Errorf("Compile(%q): %v", tt.filter, err)
			continue
		}
		if !strings.Contains(sql, tt.want) {
			t.Errorf("Compile(%q) = %s, want containing %s", tt.filter, sql, tt.want)
		}

		dsql, _, _, err := CompileForDuckDB(tt.filter, duckOpts("name", "age"))
		if err != nil {
			t.Errorf("CompileForDuckDB(%q): %v", tt.filter, err)
			continue
		}
		if !strings.Contains(dsql, tt.want) {
			t.Errorf("CompileForDuckDB(%q) = %s, want containing %s", tt.filter, dsql, tt.want)
		}
	}
}

func TestEnvelopePlainVsWrapped(t *testing.T) {
	// West < east: a single polygon.
	_, args, _, err := Compile("INTERSECTS(geom, ENVELOPE(-10, -5, 10, 5))", pgOpts())
	if err != nil {
		t.Fatalf("Compile plain: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v", args)
	}
	// Geometry args are EWKT with an SRID prefix.
	wkt := args[0].(string)
	if !strings.Contains(wkt, "POLYGON((") || strings.Contains(wkt, "MULTIPOLYGON") {
		t.Fatalf("expected plain polygon, got %s", wkt)
	}

	// West > east: antimeridian crossing becomes two polygons.
	_, args, _, err = Compile("INTERSECTS(geom, ENVELOPE(170, -10, -170, 10))", pgOpts())
	if err != nil {
		t.Fatalf("Compile wrapped: %v", err)
	}
	if wkt := args[0].(string); !strings.Contains(wkt, "MULTIPOLYGON") {
		t.Fatalf("expected wrapped multipolygon, got %s", wkt)
	}

	// Wrong arity fails.
	if _, _, _, err := Compile("INTERSECTS(geom, ENVELOPE(1, 2, 3))", pgOpts()); err == nil {
		t.Fatal("expected error for three-coordinate ENVELOPE")
	}
}

func TestSpatialFunctionMapping(t *testing.T) {
	if got := toPostGISFunction("intersects"); got != "ST_Intersects" {
		t.Errorf("toPostGISFunction(intersects) = %s", got)
	}
	if got := toPostGISFunction("bogus"); got != "UNKNOWN_bogus" {
		t.Errorf("unknown op fallback = %s", got)
	}
	if got := toDuckDBFunction("bogus"); got != "UNKNOWN_bogus" {
		t.Errorf("duckdb unknown op fallback = %s", got)
	}
}

func TestGeometryPropertyRestriction(t *testing.T) {
	// Only the configured geometry column may appear in spatial predicates,
	// even when the identifier is allowlisted for scalar use.
	_, _, _, err := Compile("INTERSECTS(other_geom, POINT(1 2))", pgOpts("other_geom"))
	if err == nil {
		t.Fatal("expected non-configured geometry property to be rejected")
	}
	if _, _, _, err := CompileForDuckDB("INTERSECTS(other_geom, POINT(1 2))", duckOpts("other_geom")); err == nil {
		t.Fatal("expected DuckDB rejection as well")
	}
}

func TestNowFunction(t *testing.T) {
	sql, args, _, err := Compile("ts >= NOW()", pgOpts("ts"))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(sql, "NOW()") {
		t.Fatalf("sql = %s, want NOW()", sql)
	}
	if len(args) != 0 {
		t.Fatalf("NOW() must not produce args, got %v", args)
	}
}

func TestPostgresLogicalAndSpatialOperators(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		want    []string
		wantArg string // geometry literals travel as parameterized EWKT args
	}{
		{"not", "NOT name = 'a'", []string{"NOT "}, ""},
		{"or", "name = 'a' OR name = 'b'", []string{" OR "}, ""},
		{"nested logic", "NOT (name = 'a' AND (age > 1 OR age < 9))", []string{"NOT ", " OR ", " AND "}, ""},
		{"dwithin", "DWITHIN(geom, POINT(1 2), 100)", []string{"ST_DWithin"}, "POINT"},
		{"within", "WITHIN(geom, POLYGON((0 0,1 0,1 1,0 0)))", []string{"ST_Within"}, "POLYGON"},
		{"touches multipoint", "TOUCHES(geom, MULTIPOINT(1 2, 3 4))", []string{"ST_Touches"}, "MULTIPOINT"},
		{"crosses linestring", "CROSSES(geom, LINESTRING(0 0, 1 1))", []string{"ST_Crosses"}, "LINESTRING"},
		{"ilike", "name ILIKE 'A%'", []string{"ILIKE"}, ""},
		{"boolean literal", "active = TRUE", []string{"true"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, _, err := Compile(tt.filter, pgOpts("name", "age", "active"))
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.filter, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(sql, want) {
					t.Errorf("Compile(%q) = %s, want containing %s", tt.filter, sql, want)
				}
			}
			if tt.wantArg != "" {
				found := false
				for _, a := range args {
					if s, ok := a.(string); ok && strings.Contains(s, tt.wantArg) {
						found = true
					}
				}
				if !found {
					t.Errorf("Compile(%q) args = %v, want an arg containing %s", tt.filter, args, tt.wantArg)
				}
			}
		})
	}
}

func TestDuckDBScalarArithmeticAndTemporal(t *testing.T) {
	sql, _, _, err := CompileForDuckDB("total = a + b * c", duckOpts("total", "a", "b", "c"))
	if err != nil {
		t.Fatalf("CompileForDuckDB arithmetic: %v", err)
	}
	if !strings.Contains(sql, `("a" + ("b" * "c"))`) {
		t.Fatalf("duckdb precedence wrong: %s", sql)
	}

	sql, args, _, err := CompileForDuckDB("ts >= NOW()", duckOpts("ts"))
	if err != nil {
		t.Fatalf("CompileForDuckDB NOW: %v", err)
	}
	if !strings.Contains(sql, "NOW()") || len(args) != 0 {
		t.Fatalf("duckdb NOW() wrong: %s args=%v", sql, args)
	}

	// Parenthesized scalar and boolean literal paths.
	sql, _, _, err = CompileForDuckDB("total = (a + b) / c AND active = FALSE", duckOpts("total", "a", "b", "c", "active"))
	if err != nil {
		t.Fatalf("CompileForDuckDB parens: %v", err)
	}
	if !strings.Contains(sql, "/") || !strings.Contains(sql, "false") {
		t.Fatalf("duckdb parenthesized/boolean output wrong: %s", sql)
	}
}

func TestMalformedPredicatesRejected(t *testing.T) {
	filters := []string{
		"name BETWEEN 'a' 'z'",           // missing AND
		"name NOT BETWEEN 'a' 'z'",       // missing AND in negated form
		"name IN (name)",                 // non-literal in IN list
		"name IN ()",                     // empty IN list
		"name IN ('a' 'b')",              // missing comma
		"name LIKE 42",                   // non-string LIKE pattern
		"name IS 'x'",                    // IS without NULL
		"secret = 'x'",                   // property not in allowlist
		"name =",                         // missing right operand
		"INTERSECTS(geom)",               // missing second geometry
		"DWITHIN(geom, POINT(1 2))",      // missing distance
		"INTERSECTS(geom, ENVELOPE(a, b, c, d))", // non-numeric envelope
		"AND name = 'x'",                 // dangling operator
		"(name = 'x'",                    // unbalanced paren
	}
	for _, f := range filters {
		if _, _, _, err := Compile(f, pgOpts("name")); err == nil {
			t.Errorf("Compile(%q) should fail", f)
		}
		if _, _, _, err := CompileForDuckDB(f, duckOpts("name")); err == nil {
			t.Errorf("CompileForDuckDB(%q) should fail", f)
		}
	}

	// Documented current behavior: an incomplete coordinate pair such as
	// POINT(1) is not validated at compile time. The malformed WKT travels as
	// a parameter and fails in the database rather than as an HTTP 400.
	if _, _, _, err := Compile("INTERSECTS(geom, POINT(1))", pgOpts("name")); err != nil {
		t.Logf("note: POINT(1) is now rejected at compile time: %v", err)
	}
}

func TestDuckDBNumberAndEdgeLiterals(t *testing.T) {
	// Exercise the DuckDB scalar-primary paths: signed numbers, exponents,
	// parenthesized identifiers, and temporal literals in BETWEEN.
	sql, args, _, err := CompileForDuckDB("age = -2.5 OR age = 1e2", duckOpts("age"))
	if err != nil {
		t.Fatalf("CompileForDuckDB numbers: %v", err)
	}
	if len(args) != 2 || args[0].(float64) != -2.5 || args[1].(float64) != 100.0 {
		t.Fatalf("args = %v", args)
	}
	if !strings.Contains(sql, " OR ") {
		t.Fatalf("sql = %s", sql)
	}

	sql, args, _, err = CompileForDuckDB("ts BETWEEN 2024-01-01 AND 2024-12-31", duckOpts("ts"))
	if err != nil {
		t.Fatalf("CompileForDuckDB temporal between: %v", err)
	}
	if len(args) != 2 || !strings.Contains(sql, "BETWEEN") {
		t.Fatalf("sql = %s args = %v", sql, args)
	}
}

func TestComparisonOperatorForms(t *testing.T) {
	filters := []struct {
		filter string
		want   string
	}{
		{"name NOT ILIKE 'a%'", "NOT ILIKE"},
		{"name <> 'x'", "<>"},
		{"age <= 5", "<="},
		{"age >= 5", ">="},
		{"age < 5 AND age > 1", "<"},
		{"age IN (1, 2.5)", "IN ("},
	}
	for _, tt := range filters {
		sql, _, _, err := Compile(tt.filter, pgOpts("name", "age"))
		if err != nil {
			t.Errorf("Compile(%q): %v", tt.filter, err)
			continue
		}
		if !strings.Contains(sql, tt.want) {
			t.Errorf("Compile(%q) = %s, want containing %s", tt.filter, sql, tt.want)
		}
		if _, _, _, err := CompileForDuckDB(tt.filter, duckOpts("name", "age")); err != nil {
			t.Errorf("CompileForDuckDB(%q): %v", tt.filter, err)
		}
	}
}

func TestPostgresTemporalBetween(t *testing.T) {
	sql, args, _, err := Compile("ts BETWEEN 2024-01-01 AND 2024-12-31T23:59:59Z", pgOpts("ts"))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("args = %v", args)
	}
	if !strings.Contains(sql, "::timestamp") || !strings.Contains(sql, "::timestamptz") {
		t.Fatalf("expected mixed casts, got %s", sql)
	}
}

func TestTokenString(t *testing.T) {
	// Smoke-test the token stringer used in parser error messages.
	tok := token{typ: tokIdent, raw: "name", pos: 3}
	if s := tok.String(); !strings.Contains(s, "name") {
		t.Errorf("token String() = %q, want to contain the raw text", s)
	}
}

func TestStartParamIndexOffset(t *testing.T) {
	sql, args, next, err := Compile("name = 'a' AND age > 2", Options{
		StartParamIndex:   5,
		AllowedProperties: map[string]struct{}{"name": {}, "age": {}},
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(sql, "$5") || !strings.Contains(sql, "$6") {
		t.Fatalf("offset placeholders missing: %s", sql)
	}
	if next != 7 || len(args) != 2 {
		t.Fatalf("next = %d args = %v, want 7 and 2 args", next, args)
	}
}
