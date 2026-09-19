package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCoverageCRUDAndWorkspacePublicIDUniqueness(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "wcs"})
	if err != nil {
		t.Fatal(err)
	}
	service := func(name string) *Service {
		item, err := s.CreateService(ctx, CreateServiceInput{WorkspaceID: ws.ID, Name: name, Type: ServiceTypeRasterFile, ConnectionInfo: json.RawMessage(`{"path":"fixture.tif"}`), Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	first, second := service("first"), service("second")
	coverage, err := s.CreateCoverage(ctx, CreateCoverageInput{ServiceID: first.ID, SourceCoverage: "raster", PublicID: "terrain", Title: "Terrain", Enabled: true, RangeFields: []CoverageRangeField{{Band: 1, Name: "elevation", UOM: "m"}}})
	if err != nil {
		t.Fatal(err)
	}
	if coverage.WorkspaceID != ws.ID {
		t.Fatalf("workspace=%s", coverage.WorkspaceID)
	}
	if coverage.WCS20CoverageSubtype != WCS20CoverageSubtypeRectifiedGrid {
		t.Fatalf("default WCS 2.0 coverage subtype=%q", coverage.WCS20CoverageSubtype)
	}
	if _, err := s.CreateCoverage(ctx, CreateCoverageInput{ServiceID: second.ID, SourceCoverage: "other", PublicID: "terrain", Enabled: true}); err != ErrDuplicateKey {
		t.Fatalf("workspace duplicate err=%v", err)
	}
	listed, err := s.ListCoverages(ctx, first.ID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list=%v err=%v", listed, err)
	}
	byPublic, err := s.GetCoverageByPublicID(ctx, ws.ID, "terrain")
	if err != nil || byPublic.ID != coverage.ID {
		t.Fatalf("lookup=%v err=%v", byPublic, err)
	}
	title := "Updated"
	enabled := false
	subtype := WCS20CoverageSubtypeGrid
	updated, err := s.UpdateCoverage(ctx, coverage.ID, UpdateCoverageInput{Title: &title, Enabled: &enabled, WCS20CoverageSubtype: &subtype})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != title || updated.Enabled || updated.WCS20CoverageSubtype != subtype {
		t.Fatalf("updated=%+v", updated)
	}
	if err := s.DeleteCoverage(ctx, coverage.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCoverage(ctx, coverage.ID); err != ErrNotFound {
		t.Fatalf("get after delete=%v", err)
	}
}

func TestBaselineDefaultsCoveragesToRectifiedGrid(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Path: filepath.Join(t.TempDir(), "migration-v14.db")}
	s, _, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "wcs-migration"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := s.CreateService(ctx, CreateServiceInput{WorkspaceID: ws.ID, Name: "raster", Type: ServiceTypeRasterFile, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	coverage, err := s.CreateCoverage(ctx, CreateCoverageInput{ServiceID: service.ID, SourceCoverage: "raster", PublicID: "legacy", Enabled: true})
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
	got, err := migrated.GetCoverage(ctx, coverage.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WCS20CoverageSubtype != WCS20CoverageSubtypeRectifiedGrid {
		t.Fatalf("migrated subtype=%q", got.WCS20CoverageSubtype)
	}
}

func TestWCSSettingsRoundTrip(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ws, err := s.CreateWorkspace(ctx, CreateWorkspaceInput{Name: "settings"})
	if err != nil {
		t.Fatal(err)
	}
	want := WCSSettings{Enabled: true, Public: true, Title: "WCS", MaxCells: 100, MaxOutputBytes: 2048, ProcessingTimeoutMS: 500}
	if err := s.UpdateWCSSettings(ctx, ws.ID, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWCSSettings(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("got=%+v want=%+v", *got, want)
	}
}
