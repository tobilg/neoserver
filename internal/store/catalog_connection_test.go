package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"testing"
)

func TestCatalogReplacementConnectionsKeepEncryptedDatabaseSelected(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"}
	s, _, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "reconnect"})
	if err != nil {
		t.Fatal(err)
	}
	for _, reopen := range []bool{false, true} {
		if reopen {
			s.Close()
			s, err = Open(cfg)
			if err != nil {
				t.Fatal(err)
			}
		}
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
			t.Fatal(err)
		}
		conn.Close()
		got, err := s.GetWorkspace(ctx, ws.ID)
		if err != nil || got.Name != "reconnect" {
			t.Fatalf("replacement session: %+v %v", got, err)
		}
	}
	s.Close()
}
