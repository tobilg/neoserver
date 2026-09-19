package datasource

import (
	"fmt"
	"strings"
)

// FeatureIDPredicate compares the published scalar identifier as text. The
// column expression is trusted, quoted SQL from the adapter, never user SQL.
// IDs themselves remain parameters, including numeric-looking or quoted IDs.
func FeatureIDPredicate(ids []string, column string, start int) (string, []any, int) {
	markers := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		markers[i], args[i] = fmt.Sprintf("$%d", start+i), id
	}
	return fmt.Sprintf("CAST(%s AS VARCHAR) IN (%s)", column, strings.Join(markers, ", ")), args, start + len(ids)
}
