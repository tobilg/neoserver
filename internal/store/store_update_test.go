package store

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

// TestDuckDBVarcharPrimaryKeyUpdate tests whether DuckDB v2 supports
// UPDATE on tables with VARCHAR PRIMARY KEY (previously bugged).
func TestDuckDBVarcharPrimaryKeyUpdate(t *testing.T) {
	// Create a temporary path for the test database
	tmpFile, err := os.CreateTemp("", "duckdb_update_test_*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	// Remove the empty file - DuckDB will create it fresh
	os.Remove(tmpPath)
	defer os.Remove(tmpPath)

	// Open in-memory DuckDB and attach the file
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Attach the database file
	_, err = db.Exec("ATTACH '" + tmpPath + "' AS test_db; USE test_db;")
	if err != nil {
		t.Fatalf("failed to attach database: %v", err)
	}

	// Create a test table with VARCHAR PRIMARY KEY (mimics workspaces table)
	_, err = db.Exec(`
		CREATE TABLE test_workspaces (
			id VARCHAR PRIMARY KEY,
			name VARCHAR NOT NULL UNIQUE,
			description VARCHAR,
			settings JSON,
			updated_at TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	ctx := context.Background()

	// Insert a test row
	testID := "test-workspace-id-123"
	_, err = db.ExecContext(ctx, `
		INSERT INTO test_workspaces (id, name, description, settings, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, testID, "original_name", "original description", `{"enabled": false}`)
	if err != nil {
		t.Fatalf("failed to insert test row: %v", err)
	}

	// Test 1: Simple UPDATE on VARCHAR column
	t.Run("UpdateVarcharColumn", func(t *testing.T) {
		result, err := db.ExecContext(ctx, `
			UPDATE test_workspaces SET description = ? WHERE id = ?
		`, "updated description", testID)
		if err != nil {
			t.Errorf("UPDATE on VARCHAR column failed: %v", err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows != 1 {
			t.Errorf("expected 1 row affected, got %d", rows)
		}

		// Verify the update
		var desc string
		err = db.QueryRowContext(ctx, "SELECT description FROM test_workspaces WHERE id = ?", testID).Scan(&desc)
		if err != nil {
			t.Errorf("failed to verify update: %v", err)
			return
		}
		if desc != "updated description" {
			t.Errorf("expected 'updated description', got '%s'", desc)
		}
	})

	// Test 2: UPDATE on UNIQUE VARCHAR column (name)
	t.Run("UpdateUniqueVarcharColumn", func(t *testing.T) {
		result, err := db.ExecContext(ctx, `
			UPDATE test_workspaces SET name = ? WHERE id = ?
		`, "updated_name", testID)
		if err != nil {
			t.Errorf("UPDATE on UNIQUE VARCHAR column failed: %v", err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows != 1 {
			t.Errorf("expected 1 row affected, got %d", rows)
		}

		// Verify the update
		var name string
		err = db.QueryRowContext(ctx, "SELECT name FROM test_workspaces WHERE id = ?", testID).Scan(&name)
		if err != nil {
			t.Errorf("failed to verify update: %v", err)
			return
		}
		if name != "updated_name" {
			t.Errorf("expected 'updated_name', got '%s'", name)
		}
	})

	// Test 3: UPDATE multiple columns at once
	t.Run("UpdateMultipleColumns", func(t *testing.T) {
		result, err := db.ExecContext(ctx, `
			UPDATE test_workspaces
			SET name = ?, description = ?, settings = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, "final_name", "final description", `{"enabled": true}`, testID)
		if err != nil {
			t.Errorf("UPDATE multiple columns failed: %v", err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows != 1 {
			t.Errorf("expected 1 row affected, got %d", rows)
		}

		// Verify all updates
		var name, desc string
		var settings any
		err = db.QueryRowContext(ctx, "SELECT name, description, settings FROM test_workspaces WHERE id = ?", testID).Scan(&name, &desc, &settings)
		if err != nil {
			t.Errorf("failed to verify updates: %v", err)
			return
		}
		if name != "final_name" {
			t.Errorf("expected 'final_name', got '%s'", name)
		}
		if desc != "final description" {
			t.Errorf("expected 'final description', got '%s'", desc)
		}
	})

	// Test 4: UPDATE using VARCHAR PRIMARY KEY in WHERE clause with other operations
	t.Run("UpdateWithVarcharPKWhere", func(t *testing.T) {
		// Insert another row to test
		_, err = db.ExecContext(ctx, `
			INSERT INTO test_workspaces (id, name, description, updated_at)
			VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		`, "second-id", "second_workspace", "second description")
		if err != nil {
			t.Fatalf("failed to insert second row: %v", err)
		}

		// Update both rows with different criteria
		result, err := db.ExecContext(ctx, `
			UPDATE test_workspaces SET description = 'batch updated' WHERE id IN (?, ?)
		`, testID, "second-id")
		if err != nil {
			t.Errorf("UPDATE with IN clause failed: %v", err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows != 2 {
			t.Errorf("expected 2 rows affected, got %d", rows)
		}
	})

	t.Log("All UPDATE tests passed! The DuckDB v2 driver supports UPDATE on VARCHAR PRIMARY KEY tables.")
}
