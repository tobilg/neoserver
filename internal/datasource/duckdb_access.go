package datasource

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/tobilg/neoserver/internal/sqlutil"
)

// RestrictDuckDBAccess confines a DuckDB database to the files and directories
// its datasource was configured with, then locks the configuration so that no
// later statement can widen access again. The settings are database-wide, so
// they cover every pooled connection. Load extensions first: extension
// autoloading counts as external access too.
func RestrictDuckDBAccess(db *sql.DB, paths, directories []string) error {
	var statements []string
	if len(paths) > 0 {
		statements = append(statements, "SET allowed_paths = "+duckDBList(paths))
	}
	if len(directories) > 0 {
		statements = append(statements, "SET allowed_directories = "+duckDBList(directories))
	}
	statements = append(statements, "SET enable_external_access = false", "SET lock_configuration = true")
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("restrict DuckDB access: %w", err)
		}
	}
	return nil
}

func duckDBList(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = sqlutil.QuoteLiteral(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
