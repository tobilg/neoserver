package duckdbsqlview

import (
	"context"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
)

func TestSQLViewIdentityValidation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	helper := NewHelper(db, DefaultTypeMapper)
	for _, tc := range []struct {
		sql, id string
		valid   bool
	}{
		{"SELECT id, geom FROM test_points", "id", true},
		{"SELECT id AS custom_id, geom FROM test_points WHERE id > 10", "custom_id", true},
		{"SELECT NULL AS id, geom FROM test_points", "id", false},
		{"SELECT 1 AS id, geom FROM test_points", "id", false},
		{"SELECT id, geom FROM test_points", "", false},
	} {
		err := helper.ValidateSQLViewIdentity(context.Background(), &datasource.SQLViewConfig{SQL: tc.sql, IDColumn: tc.id})
		if (err == nil) != tc.valid {
			t.Fatalf("%s id=%s: %v", tc.sql, tc.id, err)
		}
	}
}
