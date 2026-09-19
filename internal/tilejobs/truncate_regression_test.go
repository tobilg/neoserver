package tilejobs

import (
	"context"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/tilecache"
	"github.com/tobilg/neoserver/internal/tiles"
)

func TestStyleTruncateDeletesCanonicalVariantsAndPreservesOtherStyles(t *testing.T) {
	// This test claims and runs the job itself, so it must be the only claimant:
	// a background worker would otherwise take the job first and claimNext would
	// return nothing. Race builds lose that race far more often.
	env := newJobTestEnv(t, true, func(c *conf.PersistentCacheJobs) { c.MaxConcurrentJobs = 0 })
	ctx := context.Background()
	ws, release, _ := env.registry.AcquireByID(env.workspaceID)
	defer release()
	request := tiles.EngineRequest{Workspace: ws, Resource: ws.GetResource("roads"), TileType: "map", MatrixSet: tiles.TMSWebMercatorQuad, Format: tiles.MediaTypePNG, Style: "default"}
	id, err := env.manager.engine.ResolveIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "#dim=fixture", "#deps=fixture"} {
		variant := id
		variant.StyleDigest += suffix
		if ok, err := env.persistent.Put(ctx, variant, []byte("matching"), tilecache.Policy{}); err != nil || !ok {
			t.Fatalf("put=%v,%v", ok, err)
		}
	}
	unrelated := id
	unrelated.StyleName = "other"
	unrelated.StyleDigest = "other@hash#tms=fixture"
	if _, err := env.persistent.Put(ctx, unrelated, []byte("preserve"), tilecache.Policy{}); err != nil {
		t.Fatal(err)
	}
	job, err := env.manager.Create(ctx, ws.ID, "test", tilecache.JobRequest{Operation: tilecache.OperationTruncate, Resource: "roads", TileType: "map", TileMatrixSet: tiles.TMSWebMercatorQuad, Format: tiles.MediaTypePNG, Style: "default"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := env.manager.claimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim=%v,%v", claimed, err)
	}
	env.manager.run(claimed)
	finished, err := env.persistent.JobStore().GetTileCacheJob(ctx, job.ID)
	if err != nil || finished.Status != tilecache.JobSucceeded || finished.ProcessedTiles != 3 {
		t.Fatalf("result=%+v err=%v", finished, err)
	}
	if _, _, found, err := env.persistent.Get(ctx, id); err != nil || found {
		t.Fatal("matching canonical tile survived")
	}
	if _, _, found, err := env.persistent.Get(ctx, unrelated); err != nil || !found {
		t.Fatal("unrelated style removed")
	}
}
