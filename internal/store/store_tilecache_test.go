package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWMTSSettingsAndTileRevisionPersist(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()

	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "tile-cache-store"})
	if err != nil {
		t.Fatal(err)
	}
	settings := WMTSSettings{
		Enabled: true, Public: true, Title: "Cached maps", FeatureInfoEnabled: true,
		VectorTilesEnabled: true, ProviderName: "Map operator", ProviderSite: "https://example.test/maps",
		ContactName: "Admin", ContactPosition: "Operator", ContactEmail: "admin@example.test",
	}
	if err := s.UpdateWMTSSettings(ctx, ws.ID, settings); err != nil {
		t.Fatal(err)
	}
	gotSettings, err := s.GetWMTSSettings(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotSettings.Enabled || gotSettings.Title != settings.Title || !gotSettings.VectorTilesEnabled ||
		gotSettings.ProviderName != settings.ProviderName || gotSettings.ProviderSite != settings.ProviderSite ||
		gotSettings.ContactName != settings.ContactName || gotSettings.ContactPosition != settings.ContactPosition || gotSettings.ContactEmail != settings.ContactEmail {
		t.Fatalf("WMTS settings = %+v", gotSettings)
	}
	if revision, err := s.GetTileRevision(ctx, ws.ID); err != nil || revision != 1 {
		t.Fatalf("tile revision = %d, err = %v", revision, err)
	}
	tileSettings := DefaultOGCTilesAPISettings()
	if err := s.UpdateOGCTilesAPISettings(ctx, ws.ID, tileSettings); err != nil {
		t.Fatal(err)
	}
	if revision, err := s.GetTileRevision(ctx, ws.ID); err != nil || revision != 2 {
		t.Fatalf("tile revision after rendering update = %d, err = %v", revision, err)
	}
	var cacheTables int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables
		WHERE table_name IN ('tile_cache_jobs', 'tile_cache_job_chunks')`).Scan(&cacheTables); err != nil {
		t.Fatal(err)
	}
	if cacheTables != 0 {
		t.Fatalf("catalog contains %d persistent cache tables", cacheTables)
	}
}

func TestLegacyWMTSSettingsReceiveNewDefaults(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "legacy-wmts-settings"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE workspaces SET wmts_settings = ? WHERE id = ?`, []byte(`{"enabled":true,"feature_info_enabled":true}`), ws.ID); err != nil {
		t.Fatal(err)
	}
	settings, err := s.GetWMTSSettings(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ProviderName != "neoserver" || settings.VectorTilesEnabled {
		t.Fatalf("legacy WMTS defaults = %+v", settings)
	}
}

func TestBaselineKeepsPersistentTileCacheStateOutOfCatalog(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Path: filepath.Join(t.TempDir(), "migration-v11.db")}
	s, _, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "pre-v11"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(cfg)
	if err != nil {
		t.Fatalf("open and migrate: %v", err)
	}
	defer migrated.Close()
	var version int
	if err := migrated.db.QueryRowContext(ctx, "SELECT max(version) FROM schema_info").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
	settings, err := migrated.GetWMTSSettings(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Enabled || !settings.FeatureInfoEnabled {
		t.Fatalf("migrated WMTS defaults = %+v", settings)
	}
	var cacheTables int
	if err := migrated.db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables
		WHERE table_name IN ('tile_cache_jobs', 'tile_cache_job_chunks')`).Scan(&cacheTables); err != nil {
		t.Fatal(err)
	}
	if cacheTables != 0 {
		t.Fatalf("catalog contains %d persistent cache tables after v12 migration", cacheTables)
	}
}
