package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"golang.org/x/sync/errgroup"
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
		for _, pool := range []*sql.DB{s.db, s.read} {
			conn, err := pool.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
				t.Fatal(err)
			}
			conn.Close()
		}
		if _, err := s.UpdateWorkspace(ctx, ws.ID, UpdateWorkspaceInput{Description: new("after reconnect")}); err != nil {
			t.Fatalf("replacement writer session: %v", err)
		}
		got, err := s.GetWorkspace(ctx, ws.ID)
		if err != nil || got.Name != "reconnect" {
			t.Fatalf("replacement reader session: %+v %v", got, err)
		}
	}
	s.Close()
}

func TestCatalogReadPoolServesConcurrentReadsBesideSerializedWrites(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123", MaxConnections: 4}
	s, _, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.read == s.db {
		t.Fatal("MaxConnections > 1 must provide a separate read pool")
	}
	if got := s.read.Stats().MaxOpenConnections; got != 3 {
		t.Fatalf("reader pool size = %d, want 3", got)
	}
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	group, groupCtx := errgroup.WithContext(ctx)
	for i := range 16 {
		group.Go(func() error {
			if i%4 == 0 {
				_, err := s.UpdateWorkspace(groupCtx, ws.ID, UpdateWorkspaceInput{Description: new(fmt.Sprintf("write %d", i))})
				return err
			}
			got, err := s.GetWorkspace(groupCtx, ws.ID)
			if err == nil && got.Name != "concurrent" {
				err = fmt.Errorf("read %q", got.Name)
			}
			return err
		})
	}
	if err := group.Wait(); err != nil {
		t.Fatal(err)
	}

	single, _, err := Init(Config{Path: filepath.Join(t.TempDir(), "single.db"), EncryptionKey: "abc123", MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	if single.read != single.db {
		t.Fatal("MaxConnections = 1 must share the writer connection")
	}
}
