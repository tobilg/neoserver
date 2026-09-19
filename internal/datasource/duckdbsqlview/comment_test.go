package duckdbsqlview

import (
	"context"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
)

// A query that ends in a line comment must still work once it is embedded as
// a derived table.
func TestSQLViewWithTrailingLineComment(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	helper := NewHelper(db, DefaultTypeMapper)
	ctx := context.Background()
	query := "SELECT id, name, value, geom FROM test_points WHERE id > 1 -- recent points"

	if err := helper.ValidateSQLView(ctx, query); err != nil {
		t.Fatalf("ValidateSQLView failed: %v", err)
	}
	discovery, err := helper.DiscoverSQLViewColumns(ctx, query)
	if err != nil {
		t.Fatalf("DiscoverSQLViewColumns failed: %v", err)
	}
	if discovery.GeometryColumn != "geom" {
		t.Fatalf("geometry column = %q, want geom", discovery.GeometryColumn)
	}

	config := &datasource.SQLViewConfig{
		SQL:            query,
		GeometryColumn: "geom",
		SRID:           4326,
		IDColumn:       "id",
		Properties: []*datasource.SQLViewProperty{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "VARCHAR"},
		},
	}
	count, err := helper.CountSQLView(ctx, config, datasource.QueryParams{})
	if err != nil {
		t.Fatalf("CountSQLView failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	features, err := helper.QuerySQLView(ctx, config, datasource.QueryParams{Limit: 10, OutputSRID: 4326})
	if err != nil {
		t.Fatalf("QuerySQLView failed: %v", err)
	}
	if len(features) != 2 {
		t.Fatalf("features = %d, want 2", len(features))
	}
}
