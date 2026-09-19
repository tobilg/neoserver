package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

func TestLifecycleIntegrityWithOptionalStoresDisabled(t *testing.T) {
	root := t.TempDir()
	cfg := recoveryConfig(t, root, "abc123", filepath.Join(root, "sources"))
	cfg.PersistentCache.Enabled = false
	cfg.MosaicCatalog.Enabled = false
	cfg.Importer.Enabled = false
	cfg.Audit.Enabled = false
	catalog, _, err := store.Init(store.Config{Path: cfg.Store.Path, EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	srv, err := New(context.Background(), cfg, testLogger(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	report, err := srv.lifecycle.AuditIntegrity(context.Background())
	if err != nil || !report.Healthy {
		t.Fatalf("integrity=%+v err=%v", report, err)
	}
}
