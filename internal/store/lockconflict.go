package store

import (
	"fmt"
	"strings"
)

// lockConflictPatterns are substrings DuckDB emits when another OS process
// holds the exclusive file lock on an attached database. Observed with
// duckdb-go v2.5.4; matching is deliberately broad and case-insensitive so a
// wording change degrades to the raw driver error instead of a wrong match.
var lockConflictPatterns = []string{
	"could not set lock on file",
	"conflicting lock",
	"file is already open in",
	"database is locked",
}

// IsLockConflict reports whether err looks like a DuckDB file-lock conflict,
// i.e. another live process has the database attached read-write.
func IsLockConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, pattern := range lockConflictPatterns {
		if strings.Contains(message, pattern) {
			return true
		}
	}
	return false
}

// WrapAttachError decorates a DuckDB attach failure with single-active-node
// guidance when it is a lock conflict; any other error is returned unchanged.
func WrapAttachError(err error, path string) error {
	if err == nil || !IsLockConflict(err) {
		return err
	}
	return fmt.Errorf("another process is holding %s; neoserver requires a single active node per store — stop the other process (or the running server, for admin commands) and retry: %w", path, err)
}
