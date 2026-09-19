package mosaiccatalog

import (
	"context"
	"encoding/binary"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestCatalog(t *testing.T) *catalog {
	t.Helper()
	c, err := openCatalog(filepath.Join(t.TempDir(), "mosaic.duckdb"), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.db.Close() })
	return c
}

func testGranule(serviceID string, generation int64, uri string) *Granule {
	return &Granule{
		WorkspaceID: "ws1", ServiceID: serviceID, Generation: generation, SourceURI: uri,
		CRS: "EPSG:4326", SRID: 4326, BBox: [4]float64{0, 0, 10, 10},
		Width: 100, Height: 100, BandCount: 3, DataType: "Byte",
		ResolutionX: 0.1, ResolutionY: 0.1, FootprintWKB: footprintWKB([4]float64{0, 0, 10, 10}),
	}
}

func TestGenerationLifecycle(t *testing.T) {
	c := newTestCatalog(t)
	ctx := context.Background()

	// First generation for a new service is 1.
	gen, err := c.beginGeneration(ctx, "ws1", "svc", false)
	if err != nil || gen != 1 {
		t.Fatalf("beginGeneration = %d, %v; want 1", gen, err)
	}

	// Activating an empty generation is rejected.
	if _, err := c.activateGeneration(ctx, "ws1", "svc", gen); err == nil || !strings.Contains(err.Error(), "no granules") {
		t.Fatalf("empty activation = %v, want rejection", err)
	}

	if err := c.upsertGranule(ctx, testGranule("svc", gen, "/data/a.tif")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	previous, err := c.activateGeneration(ctx, "ws1", "svc", gen)
	if err != nil || previous != 0 {
		t.Fatalf("activate = previous %d, %v; want 0", previous, err)
	}
	if active, _ := c.activeGeneration(ctx, "svc"); active != 1 {
		t.Fatalf("active generation = %d, want 1", active)
	}

	// A preserve=true begin copies the active granules into the new generation.
	gen2, err := c.beginGeneration(ctx, "ws1", "svc", true)
	if err != nil || gen2 != 2 {
		t.Fatalf("second beginGeneration = %d, %v; want 2", gen2, err)
	}
	if err := c.upsertGranule(ctx, testGranule("svc", gen2, "/data/b.tif")); err != nil {
		t.Fatal(err)
	}
	if previous, err = c.activateGeneration(ctx, "ws1", "svc", gen2); err != nil || previous != 1 {
		t.Fatalf("activate gen2 = previous %d, %v; want 1", previous, err)
	}
	granules, err := c.listGranules(ctx, "ws1", "svc", GranuleFilter{})
	if err != nil || len(granules) != 2 {
		t.Fatalf("gen2 granules = %d, %v; want preserved + new", len(granules), err)
	}

	// Pruning removes every non-active generation's rows.
	if err := c.pruneInactiveGenerations(ctx, "svc", gen2); err != nil {
		t.Fatal(err)
	}
	var stale int
	if err := c.db.QueryRow(`SELECT count(*) FROM mosaic_granules WHERE service_id='svc' AND generation<>2`).Scan(&stale); err != nil || stale != 0 {
		t.Fatalf("stale rows after prune = %d, %v", stale, err)
	}
}

func TestBeginGenerationClearsReusedRows(t *testing.T) {
	c := newTestCatalog(t)
	ctx := context.Background()

	gen, _ := c.beginGeneration(ctx, "ws1", "svc", false)
	// Simulate an interrupted harvest that left partial rows in gen 1.
	if err := c.upsertGranule(ctx, testGranule("svc", gen, "/data/partial.tif")); err != nil {
		t.Fatal(err)
	}
	// A retry reuses active+1 = 1 and must clear the partial rows.
	gen2, err := c.beginGeneration(ctx, "ws1", "svc", false)
	if err != nil || gen2 != gen {
		t.Fatalf("retry generation = %d, %v; want %d", gen2, err, gen)
	}
	var count int
	if err := c.db.QueryRow(`SELECT count(*) FROM mosaic_granules WHERE service_id='svc' AND generation=?`, gen2).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial rows after reuse = %d, %v; want 0", count, err)
	}
}

func TestRestoreGenerationConflict(t *testing.T) {
	c := newTestCatalog(t)
	ctx := context.Background()

	gen, _ := c.beginGeneration(ctx, "ws1", "svc", false)
	_ = c.upsertGranule(ctx, testGranule("svc", gen, "/data/a.tif"))
	if _, err := c.activateGeneration(ctx, "ws1", "svc", gen); err != nil {
		t.Fatal(err)
	}

	// Restoring from the active generation back to 0 succeeds.
	if err := c.restoreGeneration(ctx, "svc", gen, 0); err != nil {
		t.Fatalf("restore: %v", err)
	}
	// A second restore no longer matches (active_generation is now 0) and
	// must report the concurrent change.
	if err := c.restoreGeneration(ctx, "svc", gen, 0); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale restore = %v, want conflict", err)
	}
}

func TestDeleteGranuleCreatesNewGeneration(t *testing.T) {
	c := newTestCatalog(t)
	ctx := context.Background()

	gen, _ := c.beginGeneration(ctx, "ws1", "svc", false)
	keep := testGranule("svc", gen, "/data/keep.tif")
	drop := testGranule("svc", gen, "/data/drop.tif")
	for _, g := range []*Granule{keep, drop} {
		if err := c.upsertGranule(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.activateGeneration(ctx, "ws1", "svc", gen); err != nil {
		t.Fatal(err)
	}

	next, previous, err := c.deleteGranule(ctx, "ws1", "svc", drop.ID)
	if err != nil {
		t.Fatalf("deleteGranule: %v", err)
	}
	if next != gen+1 || previous != gen {
		t.Fatalf("generations = next %d previous %d, want %d/%d", next, previous, gen+1, gen)
	}
	granules, err := c.listGranules(ctx, "ws1", "svc", GranuleFilter{})
	if err != nil || len(granules) != 1 || granules[0].SourceURI != "/data/keep.tif" {
		t.Fatalf("granules after delete = %+v, %v", granules, err)
	}

	// Deleting an unknown granule reports ErrNotFound.
	if _, _, err := c.deleteGranule(ctx, "ws1", "svc", "no-such-id"); err != ErrNotFound {
		t.Fatalf("unknown granule delete = %v, want ErrNotFound", err)
	}
}

func TestListGranulesFilters(t *testing.T) {
	c := newTestCatalog(t)
	ctx := context.Background()

	gen, _ := c.beginGeneration(ctx, "ws1", "svc", false)
	west := testGranule("svc", gen, "/data/west.tif")
	west.BBox = [4]float64{-20, 0, -10, 10}
	west.Time = "2024-01-01"
	east := testGranule("svc", gen, "/data/east.tif")
	east.BBox = [4]float64{10, 0, 20, 10}
	east.Time = "2024-06-01"
	for _, g := range []*Granule{west, east} {
		if err := c.upsertGranule(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.activateGeneration(ctx, "ws1", "svc", gen); err != nil {
		t.Fatal(err)
	}

	// BBox filter selects only intersecting granules.
	bbox := [4]float64{-15, 2, -12, 8}
	granules, err := c.listGranules(ctx, "ws1", "svc", GranuleFilter{BBox: &bbox})
	if err != nil || len(granules) != 1 || granules[0].SourceURI != "/data/west.tif" {
		t.Fatalf("bbox filter = %+v, %v", granules, err)
	}

	// Time filter.
	granules, err = c.listGranules(ctx, "ws1", "svc", GranuleFilter{Time: "2024-06-01"})
	if err != nil || len(granules) != 1 || granules[0].SourceURI != "/data/east.tif" {
		t.Fatalf("time filter = %+v, %v", granules, err)
	}

	// Limit and offset page through the ordered set.
	granules, err = c.listGranules(ctx, "ws1", "svc", GranuleFilter{Limit: 1, Offset: 1})
	if err != nil || len(granules) != 1 {
		t.Fatalf("paged list = %+v, %v", granules, err)
	}

	// Unknown service yields no granules and no error.
	granules, err = c.listGranules(ctx, "ws1", "other", GranuleFilter{})
	if err != nil || granules != nil {
		t.Fatalf("unknown service list = %+v, %v", granules, err)
	}
}

func TestResetInterruptedTwoPhase(t *testing.T) {
	c := newTestCatalog(t)
	ctx := context.Background()

	running, err := c.createJob(ctx, "ws1", "svc", "tester", HarvestRequest{Mode: HarvestSynchronize})
	if err != nil {
		t.Fatal(err)
	}
	running.Status = JobRunning
	if err := c.updateJob(ctx, running); err != nil {
		t.Fatal(err)
	}

	cancelling, err := c.createJob(ctx, "ws1", "svc2", "tester", HarvestRequest{Mode: HarvestSynchronize})
	if err != nil {
		t.Fatal(err)
	}
	cancelling.Status = JobRunning
	if err := c.updateJob(ctx, cancelling); err != nil {
		t.Fatal(err)
	}
	if err := c.requestCancel(ctx, cancelling.ID); err != nil {
		t.Fatal(err)
	}

	if err := c.resetInterrupted(ctx); err != nil {
		t.Fatal(err)
	}

	// Interrupted running jobs are re-queued; cancel-requested jobs finalize.
	requeued, _ := c.getJob(ctx, running.ID)
	if requeued.Status != JobQueued {
		t.Fatalf("running job after reset = %s, want queued", requeued.Status)
	}
	finalized, _ := c.getJob(ctx, cancelling.ID)
	if finalized.Status != JobCancelled {
		t.Fatalf("cancelling job after reset = %s, want cancelled", finalized.Status)
	}
}

func TestFootprintWKB(t *testing.T) {
	bbox := [4]float64{1, 2, 3, 4}
	wkb := footprintWKB(bbox)
	if len(wkb) != 1+4+4+4+5*16 {
		t.Fatalf("wkb length = %d", len(wkb))
	}
	if wkb[0] != 1 {
		t.Fatal("expected little-endian byte order marker")
	}
	if geomType := binary.LittleEndian.Uint32(wkb[1:5]); geomType != 3 {
		t.Fatalf("geometry type = %d, want polygon (3)", geomType)
	}
	if rings := binary.LittleEndian.Uint32(wkb[5:9]); rings != 1 {
		t.Fatalf("ring count = %d, want 1", rings)
	}
	if points := binary.LittleEndian.Uint32(wkb[9:13]); points != 5 {
		t.Fatalf("point count = %d, want 5 (closed ring)", points)
	}
	firstX := math.Float64frombits(binary.LittleEndian.Uint64(wkb[13:21]))
	firstY := math.Float64frombits(binary.LittleEndian.Uint64(wkb[21:29]))
	lastX := math.Float64frombits(binary.LittleEndian.Uint64(wkb[13+4*16 : 21+4*16]))
	if firstX != 1 || firstY != 2 || lastX != firstX {
		t.Fatalf("ring not closed at bbox min: first (%v,%v) last x %v", firstX, firstY, lastX)
	}
}

func TestFootprintGeoJSON(t *testing.T) {
	geo := footprintGeoJSON([4]float64{0, 0, 2, 2})
	if geo["type"] != "Polygon" {
		t.Fatalf("type = %v", geo["type"])
	}
	rings := geo["coordinates"].([][][]float64)
	if len(rings) != 1 || len(rings[0]) != 5 {
		t.Fatalf("coordinates = %+v", rings)
	}
}

func TestUpsertGranuleConflictUpdates(t *testing.T) {
	c := newTestCatalog(t)
	ctx := context.Background()

	gen, _ := c.beginGeneration(ctx, "ws1", "svc", false)
	first := testGranule("svc", gen, "/data/a.tif")
	if err := c.upsertGranule(ctx, first); err != nil {
		t.Fatal(err)
	}
	// Same (service, generation, source_uri) updates in place.
	second := testGranule("svc", gen, "/data/a.tif")
	second.BandCount = 4
	if err := c.upsertGranule(ctx, second); err != nil {
		t.Fatal(err)
	}
	_ = c.upsertGranule(ctx, testGranule("svc", gen, "/data/b.tif"))
	if _, err := c.activateGeneration(ctx, "ws1", "svc", gen); err != nil {
		t.Fatal(err)
	}
	granules, _ := c.listGranules(ctx, "ws1", "svc", GranuleFilter{})
	if len(granules) != 2 {
		t.Fatalf("granule count = %d, want 2", len(granules))
	}
	for _, g := range granules {
		if g.SourceURI == "/data/a.tif" && g.BandCount != 4 {
			t.Fatalf("conflict update lost: %+v", g)
		}
	}

	// Timestamps are set on insert.
	if granules[0].CreatedAt.IsZero() || granules[0].UpdatedAt.Before(granules[0].CreatedAt.Add(-time.Second)) {
		t.Fatalf("timestamps not maintained: %+v", granules[0])
	}
}
