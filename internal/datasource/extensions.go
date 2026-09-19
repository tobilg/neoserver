// Package datasource provides extension preloading for DuckDB-based datasources.
package datasource

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

// requiredExtensions are the DuckDB extensions neoserver loads. The container image
// bundles them so the server starts without network access.
var requiredExtensions = [...]string{"spatial", "httpfs"}

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
	for _, ext := range requiredExtensions {
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

// InstalledExtension describes an extension in DuckDB's extension directory.
type InstalledExtension struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

// InstallExtensions installs and loads every extension neoserver uses, then
// checks that each was installed for the embedded DuckDB's own version and
// platform. Unlike PreloadExtensions it fails on the first error: it prepares
// an image, where a missing extension must stop the build.
func InstallExtensions(ctx context.Context) (duckdbVersion, platform string, installed []InstalledExtension, err error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return "", "", nil, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return "", "", nil, err
	}
	defer conn.Close()
	return installExtensions(ctx, conn)
}

func installExtensions(ctx context.Context, conn *sql.Conn) (duckdbVersion, platform string, installed []InstalledExtension, err error) {
	if err = conn.QueryRowContext(ctx, "SELECT library_version FROM pragma_version()").Scan(&duckdbVersion); err != nil {
		return "", "", nil, fmt.Errorf("read DuckDB version: %w", err)
	}
	if err = conn.QueryRowContext(ctx, "SELECT platform FROM pragma_platform()").Scan(&platform); err != nil {
		return "", "", nil, fmt.Errorf("read DuckDB platform: %w", err)
	}
	for _, ext := range requiredExtensions {
		if _, err = conn.ExecContext(ctx, fmt.Sprintf("INSTALL '%s'; LOAD '%s';", ext, ext)); err != nil {
			return "", "", nil, fmt.Errorf("install extension %s: %w", ext, err)
		}
		var entry InstalledExtension
		if err = conn.QueryRowContext(ctx, `SELECT extension_name, coalesce(extension_version, ''), coalesce(install_path, '')
			FROM duckdb_extensions() WHERE extension_name = ? AND installed AND loaded`, ext).Scan(&entry.Name, &entry.Version, &entry.Path); err != nil {
			return "", "", nil, fmt.Errorf("inspect extension %s: %w", ext, err)
		}
		if err = checkExtensionPath(entry.Path, duckdbVersion, platform); err != nil {
			return "", "", nil, fmt.Errorf("extension %s: %w", ext, err)
		}
		installed = append(installed, entry)
	}
	return duckdbVersion, platform, installed, nil
}

// checkExtensionPath requires the extension file to sit in the directory
// DuckDB uses for this exact version and platform, <dir>/<version>/<platform>/.
func checkExtensionPath(path, duckdbVersion, platform string) error {
	if path == "" {
		return fmt.Errorf("no install path reported")
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	if want := "/" + duckdbVersion + "/" + platform; !strings.HasSuffix(dir, want) {
		return fmt.Errorf("installed at %s, want a directory ending in %s", path, want)
	}
	return nil
}
