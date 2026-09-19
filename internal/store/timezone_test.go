package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestColumnDefaultTimestampsAreUTC(t *testing.T) {
	catalog, _, err := Init(Config{Path: filepath.Join(t.TempDir(), "catalog.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	var created time.Time
	if err := catalog.db.QueryRowContext(context.Background(), `SELECT created_at FROM roles WHERE id = 'viewer'`).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if drift := time.Since(created); drift < -time.Minute || drift > time.Minute {
		t.Fatalf("seeded role created_at %s is %s away from now (UTC %s)", created, drift, time.Now().UTC())
	}
}
