package datasource

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrSQLViewIdentity = errors.New("SQL view requires a unique, non-null id_column")

type SQLViewIdentityValidator interface {
	ValidateSQLViewIdentity(context.Context, *SQLViewConfig) error
}

// SQLViewIdentitySQL is used only after the adapter validates the authored SQL.
// Match the string form used by published feature IDs, not just native equality.
func SQLViewIdentitySQL(config *SQLViewConfig) (string, error) {
	if config == nil || config.IDColumn == "" {
		return "", ErrSQLViewIdentity
	}
	id := `"` + strings.ReplaceAll(config.IDColumn, `"`, `""`) + `"`
	return fmt.Sprintf("SELECT COUNT(*) = COUNT(%s) AND COUNT(*) = COUNT(DISTINCT CAST(%s AS VARCHAR)) FROM (%s) AS v", id, id, config.SQL), nil
}
