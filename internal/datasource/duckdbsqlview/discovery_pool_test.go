package duckdbsqlview

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDiscoveryReleasesSingleConnection(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	db.SetMaxOpenConns(1)
	helper := NewHelper(db, DefaultTypeMapper)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for _, query := range []string{"SELECT 1 AS id, ST_Point(0,0) AS geom", "SELECT id, geom FROM test_points WHERE false"} {
				metadata, err := helper.DiscoverSQLViewColumns(ctx, query)
				if err != nil || metadata == nil {
					t.Errorf("discovery: %+v %v", metadata, err)
				}
			}
		})
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledSRIDDetectionIsNotUnknownCRS(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	helper := NewHelper(db, DefaultTypeMapper)
	if _, err := helper.detectSQLViewSRID(ctx, "SELECT ST_Point(0,0) AS geom", "geom"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled SRID error: %v", err)
	}
	if _, err := helper.DiscoverSQLViewColumns(ctx, "SELECT ST_Point(0,0) AS geom"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled discovery error: %v", err)
	}
}
