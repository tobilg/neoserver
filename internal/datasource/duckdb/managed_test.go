package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
)

func TestManagedEncryptedDatabaseUsesReadOnlySecretResolver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.duckdb")
	key := strings.Repeat("ab", 32)
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSTALL spatial; LOAD spatial"); err != nil {
		t.Fatal(err)
	}
	attach := fmt.Sprintf("ATTACH '%s' AS managed (ENCRYPTION_KEY '%s')", strings.ReplaceAll(path, "'", "''"), key)
	if _, err = db.Exec(attach); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE managed.roads AS SELECT 1::BIGINT AS id, ST_Point(1,2)::GEOMETRY AS geom; CHECKPOINT managed`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	ConfigureManagedResolver(func(_ context.Context, workspaceID, serviceID, importID string) (string, string, map[string]int, error) {
		if workspaceID != "workspace-1" || serviceID != "service-1" || importID != "import-1" {
			return "", "", nil, store.ErrNotFound
		}
		return path, key, map[string]int{"roads": 4326}, nil
	})
	t.Cleanup(func() { ConfigureManagedResolver(nil) })
	connection, _ := json.Marshal(Config{ManagedImportID: "import-1"})
	for _, owner := range []string{"", "another-workspace"} {
		if source, err := NewFromService(&store.Service{ID: "service-1", WorkspaceID: owner, Type: store.ServiceTypeDuckDB, ConnectionInfo: connection}); err == nil {
			source.Close()
			t.Fatal("foreign managed asset accepted")
		}
	}
	ds, err := NewFromService(&store.Service{ID: "service-1", WorkspaceID: "workspace-1", Type: store.ServiceTypeDuckDB, ConnectionInfo: connection})
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	layers, err := ds.DiscoverLayers(context.Background())
	if err != nil || len(layers) != 1 || layers[0].Name != "roads" {
		t.Fatalf("managed layers=%+v err=%v", layers, err)
	}
	queryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	features, err := ds.Query(queryCtx, "roads", datasource.QueryParams{Limit: 10})
	if err != nil || len(features) != 1 {
		t.Fatalf("managed query=%+v err=%v", features, err)
	}
}
