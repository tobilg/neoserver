package datasource

import (
	"fmt"
	"strconv"
	"strings"
)

type SQLDialect string

const (
	SQLPostGIS SQLDialect = "postgis"
	SQLDuckDB  SQLDialect = "duckdb"
)

type PredicateOptions struct {
	Dialect            SQLDialect
	TableAlias         string
	GeometryExpression string // trusted adapter expression, including WKB decoding
	StartParamIndex    int
}

// SQLPredicate defers compilation until the adapter knows its SQL dialect,
// geometry representation, table alias, and preceding parameter count.
type SQLPredicate interface {
	CompilePredicate(PredicateOptions) (string, []any, int, error)
}

// AppendQueryPredicate is shared by feature, count, render, and SQL-view
// builders. Legacy precompiled predicates are rebased instead of dropped.
func AppendQueryPredicate(p QueryParams, opts PredicateOptions, where *[]string, args *[]any, next *int) error {
	opts.StartParamIndex = *next
	var sql string
	var values []any
	var end int
	var err error
	if p.Predicate != nil {
		sql, values, end, err = p.Predicate.CompilePredicate(opts)
	} else if strings.TrimSpace(p.CompiledFilter) != "" {
		sql, err = rebaseParameters(p.CompiledFilter, p.CompiledFilterParamOffset, *next, len(p.CompiledFilterArgs))
		values, end = p.CompiledFilterArgs, *next+len(p.CompiledFilterArgs)
	} else {
		return nil
	}
	if err != nil {
		return err
	}
	if sql != "" {
		*where = append(*where, sql)
		*args = append(*args, values...)
		*next = end
	}
	return nil
}

func (p QueryParams) HasCompiledPredicate() bool {
	return p.Predicate != nil || strings.TrimSpace(p.CompiledFilter) != ""
}

// Compiler literals are bound separately. Skip quoted SQL identifiers/literals
// so "$1" as an identifier is not confused with the first bind parameter.
func rebaseParameters(sql string, from, to, count int) (string, error) {
	if from <= 0 {
		from = 1
	}
	var out strings.Builder
	for i := 0; i < len(sql); {
		ch := sql[i]
		if ch == '\'' || ch == '"' {
			start := i
			i++
			for i < len(sql) {
				if sql[i] != ch {
					i++
					continue
				}
				i++
				if i < len(sql) && sql[i] == ch {
					i++
					continue
				}
				break
			}
			out.WriteString(sql[start:i])
		} else if ch == '$' && i+1 < len(sql) && sql[i+1] >= '0' && sql[i+1] <= '9' {
			end := i + 1
			for end < len(sql) && sql[end] >= '0' && sql[end] <= '9' {
				end++
			}
			number, err := strconv.Atoi(sql[i+1 : end])
			if err != nil || number < from || number-from >= count {
				return "", fmt.Errorf("compiled predicate has an invalid parameter reference")
			}
			fmt.Fprintf(&out, "$%d", to+number-from)
			i = end
		} else {
			out.WriteByte(ch)
			i++
		}
	}
	return out.String(), nil
}
