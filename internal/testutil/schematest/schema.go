// Package schematest compares frozen baselines with current initialization.
package schematest

import (
	"crypto/sha256"
	"database/sql"
	"os"
	"slices"
	"testing"

	"github.com/tobilg/neoserver/internal/dbschema"
)

// Unchanged snapshots a closed database; the returned assertion also checks
// that a refused open left no write-ahead log behind.
func Unchanged(t *testing.T, path string) func() {
	t.Helper()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if sha256.Sum256(before) != sha256.Sum256(after) {
			t.Errorf("refused database was modified: %s", path)
		}
		if _, err := os.Stat(path + ".wal"); !os.IsNotExist(err) {
			t.Errorf("refused database left a WAL: %v", err)
		}
	}
}

func Equal(t *testing.T, baseline, fresh *sql.DB, seeds ...string) {
	t.Helper()
	left, err := dbschema.Shape(baseline)
	if err != nil {
		t.Fatal(err)
	}
	right, err := dbschema.Shape(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(left, right) {
		for _, row := range left {
			if !slices.Contains(right, row) {
				t.Errorf("baseline only: %s", row)
			}
		}
		for _, row := range right {
			if !slices.Contains(left, row) {
				t.Errorf("fresh only: %s", row)
			}
		}
	}
	for _, query := range seeds {
		left, err := dbschema.Rows(baseline, query)
		if err != nil {
			t.Fatal(err)
		}
		right, err := dbschema.Rows(fresh, query)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(left, right) {
			t.Errorf("seed mismatch for %s:\nbaseline: %v\nfresh: %v", query, left, right)
		}
	}
}
