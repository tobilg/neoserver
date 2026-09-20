package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCapabilitiesRevisionMigrationMutationRollbackAndRestart(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Path: filepath.Join(t.TempDir(), "catalog.db"), EncryptionKey: "abc123"}
	db, attached, err := newCatalogConnection()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("ATTACH '" + escapeSQLLiteral(cfg.Path) + "' AS store (ENCRYPTION_KEY 'abc123'); USE store"); err != nil {
		t.Fatal(err)
	}
	attached()
	ddl, err := os.ReadFile("testdata/baseline-v25.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO workspaces(id,name,description) VALUES ('existing','existing','Keep me')`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	check := func(want int64) {
		t.Helper()
		if got, err := s.GetCapabilitiesRevision(ctx, "existing"); err != nil || got != want {
			t.Fatalf("revision=%d want=%d err=%v", got, want, err)
		}
	}
	check(1)
	ws, err := s.GetWorkspace(ctx, "existing")
	if err != nil || ws.Description != "Keep me" {
		t.Fatalf("migration lost existing workspace: %+v %v", ws, err)
	}
	if err := s.UpdateWMTSSettings(ctx, ws.ID, WMTSSettings{Enabled: true, TileMatrixLimitsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	check(2)
	service, err := s.CreateService(ctx, CreateServiceInput{WorkspaceID: ws.ID, Name: "features", Type: ServiceTypePostGIS, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	check(3)
	input := CreateLayerInput{ServiceID: service.ID, SourceLayer: "roads", PublicID: "roads", Enabled: true}
	layer, err := s.CreateLayer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	check(4)
	if _, err = s.CreateLayer(ctx, input); err == nil {
		t.Fatal("duplicate publication succeeded")
	}
	check(4)
	if err = s.DeleteLayer(ctx, layer.ID); err != nil {
		t.Fatal(err)
	}
	check(5)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	check(5)
	settings, err := s.GetWMTSSettings(ctx, ws.ID)
	if err != nil || !settings.TileMatrixLimitsEnabled {
		t.Fatalf("settings lost across restart: %+v %v", settings, err)
	}
}
