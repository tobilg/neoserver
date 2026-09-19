// Package datasource provides extension preloading for DuckDB-based datasources.
package datasource

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

var (
	extensionsPreloaded bool
	extensionsMu        sync.Mutex
)

// PreloadExtensions installs and loads commonly used DuckDB extensions.
// This should be called once at server startup to avoid slow first requests.
// Extensions are installed to DuckDB's default extension directory and will be
// available to all subsequent DuckDB connections.
func PreloadExtensions(ctx context.Context, logger *slog.Logger) error {
	extensionsMu.Lock()
	defer extensionsMu.Unlock()

	if extensionsPreloaded {
		return nil
	}

	// Create a temporary connection just for extension installation
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return fmt.Errorf("open duckdb for extension preload: %w", err)
	}
	defer db.Close()

	preloadExtensions(ctx, logger, db.ExecContext)
	extensionsPreloaded = true
	return nil
}

// Keep SQL execution injectable so logging and failure handling can be tested
// without installing extensions or opening a real DuckDB database.
func preloadExtensions(ctx context.Context, logger *slog.Logger, execContext func(context.Context, string, ...any) (sql.Result, error)) {
	extensions := []string{"spatial", "httpfs"}

	for _, ext := range extensions {
		started := time.Now()
		logger.Debug("preloading DuckDB extension", "extension", ext)

		// INSTALL downloads the extension to the extension directory
		_, err := execContext(ctx, fmt.Sprintf("INSTALL '%s';", ext))
		if err != nil {
			logger.Warn("failed to install extension", "extension", ext, "error", err)
			// Continue with other extensions - some might not be needed
			continue
		}

		// LOAD verifies the extension works
		_, err = execContext(ctx, fmt.Sprintf("LOAD '%s';", ext))
		if err != nil {
			logger.Warn("failed to load extension", "extension", ext, "error", err)
			continue
		}

		logger.Info("preloaded DuckDB extension", "extension", ext,
			"duration_ms", float64(time.Since(started).Microseconds())/1000)
	}
}

// ExtensionsPreloaded returns true if extensions have been preloaded.
func ExtensionsPreloaded() bool {
	extensionsMu.Lock()
	defer extensionsMu.Unlock()
	return extensionsPreloaded
}
