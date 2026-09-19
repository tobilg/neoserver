package tiles

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/jpeg"
	"strconv"
	"sync"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

// The WMTS 1.0 suite intermittently received a JPEG tile that answered 200 with
// image/jpeg and a plausible length, but no decoder could read it. It failed on
// a different layer and tile each run, roughly once per 160 requests, which
// looked like concurrent renders interfering rather than one bad tile.
//
// Metatiling was the suspect: one region is rendered and every tile in it is
// cropped from that render, so concurrent requests for neighbouring tiles share
// work. This drives that path hard, decodes every response and requires every
// empty tile to be byte-identical. It passes under -race, which with the suite's
// own evidence (the rejected tile matched 161 accepted ones in length and every
// header) places the fault in the suite's image parser rather than the server.
func TestConcurrentJPEGTilesAreAllDecodable(t *testing.T) {
	cfg := newTestConfig()
	cfg.Tiles.MaxMetatileFactor = 4
	cfg.Tiles.MaxGutterPixels = 32
	// This is about what renders produce, not admission control: let requests
	// wait for a render slot instead of being shed as "render queue is full",
	// which a race-instrumented build on a shared runner otherwise triggers.
	cfg.Tiles.RenderQueueTimeoutMS = 120_000
	ws := newTestWorkspace()
	layer := addTestLayer(ws, "roads", &fakeDataSource{})
	layer.TileCacheParameters = &store.TileCacheParameterPolicy{MetatileFactor: 2, GutterPixels: 8}
	engine := NewEngine(cfg, newTestLogger(), nil, nil)
	resource := ws.GetResource("roads")

	// Neighbouring tiles at one zoom, so requests land in the same metatile.
	type tile struct{ column, row int }
	var tiles []tile
	for column := range 4 {
		for row := range 4 {
			tiles = append(tiles, tile{column, row})
		}
	}

	var wg sync.WaitGroup
	failures := make(chan string, len(tiles)*8)
	// The fake source has no features, so every tile is empty, and an empty tile
	// always encodes to the same bytes. Any second digest means a render or the
	// encoder was disturbed by a concurrent one -- which is what a JPEG that
	// decodes on 161 requests and not on the 162nd would require.
	var digestsMu sync.Mutex
	digests := map[[32]byte]int{}
	for range 8 {
		for _, target := range tiles {
			wg.Add(1)
			go func(target tile) {
				defer wg.Done()
				result, err := engine.Fetch(t.Context(), EngineRequest{
					Workspace: ws, Resource: resource, TileType: "map",
					MatrixSet: TMSWebMercatorQuad, Zoom: 2,
					Column: target.column, Row: target.row, Format: MediaTypeJPEG,
				})
				if err != nil {
					failures <- "fetch " + err.Error()
					return
				}
				digestsMu.Lock()
				digests[sha256.Sum256(result.Data)]++
				digestsMu.Unlock()
				decoded, err := jpeg.Decode(bytes.NewReader(result.Data))
				if err != nil {
					failures <- "undecodable JPEG of " + strconv.Itoa(len(result.Data)) + " bytes: " + err.Error()
					return
				}
				if decoded.Bounds() != image.Rect(0, 0, 256, 256) {
					failures <- "tile bounds " + decoded.Bounds().String()
				}
			}(target)
		}
	}
	wg.Wait()
	close(failures)

	seen := 0
	for failure := range failures {
		seen++
		if seen <= 5 {
			t.Errorf("tile %d: %s", seen, failure)
		}
	}
	if seen > 5 {
		t.Errorf("... and %d more", seen-5)
	}
	if len(digests) != 1 {
		t.Errorf("empty tiles rendered to %d distinct byte sequences, want 1", len(digests))
	}
}
