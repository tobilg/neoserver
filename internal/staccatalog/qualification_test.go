package staccatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/stacmodel"
)

// Opt-in qualification exercises the real validator, staged writer and search
// SQL at the requested inventory size. It records timing, memory and index use.
func TestSTACInventoryQualification(t *testing.T) {
	raw := os.Getenv("NEOSRV_STAC_BENCH_ITEMS")
	if raw == "" {
		t.Skip("set NEOSRV_STAC_BENCH_ITEMS=1000000 to run inventory qualification")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1000 {
		t.Fatal("qualification requires at least 1000 Items")
	}
	c, err := Open(filepath.Join(t.TempDir(), "stac.duckdb"), "abc123", int64(n))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	if err = c.PutCollection(ctx, "benchmark", Collection{Document: stacmodel.Collection("scenes", "Scenes", "Synthetic benchmark grid", "other", nil)}, true); err != nil {
		t.Fatal(err)
	}
	j, err := c.CreateJob(ctx, "benchmark", "scenes", "import", "uploading", nil)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	batch := make([]stacmodel.Document, 0, 200)
	for i := 0; i < n; i++ {
		d := testItem(fmt.Sprintf("scene-%07d", i), float64(i%1000)/10-50)
		y := float64(i/1000)/10 - 50
		d["geometry"] = stacmodel.Document{"type": "Point", "coordinates": []float64{float64(i%1000)/10 - 50, y}}
		d["bbox"] = []float64{float64(i%1000)/10 - 50, y, float64(i%1000)/10 - 50, y}
		batch = append(batch, d)
		if len(batch) == cap(batch) || i == n-1 {
			if err = c.Stage(ctx, j, batch); err != nil {
				t.Fatal(err)
			}
			batch = batch[:0]
		}
		if i > 0 && i%100000 == 0 {
			t.Logf("staged %d Items in %s", i, time.Since(started))
		}
	}
	if err = c.SetJobStatus(ctx, "benchmark", j.ID, "ready", ""); err != nil {
		t.Fatal(err)
	}
	if err = c.Publish(ctx, "benchmark", j.ID, false, nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("published %d Items in %s", n, time.Since(started))
	q := Search{BBox: []float64{-49.05, -49.05, -48.05, -48.05}, Limit: 100}
	if n < 20000 {
		q.BBox = []float64{-49.05, -50, -39.05, -49.95}
	}
	statement, args := searchStatement("benchmark", q, []string{"scenes"}, cursor{})
	rows, err := c.read.QueryContext(ctx, "EXPLAIN "+statement, args...)
	if err != nil {
		t.Fatal(err)
	}
	plan := ""
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			t.Fatal(err)
		}
		plan += value
	}
	rows.Close()
	t.Log(plan)
	if !strings.Contains(strings.ToUpper(plan), "RTREE_INDEX_SCAN") {
		t.Fatal("selective search does not use the spatial index")
	}
	for i := 0; i < 5; i++ {
		if _, err = c.Search(ctx, "benchmark", q, []string{"scenes"}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	durations := []time.Duration{}
	failures := make(chan error, 4)
	for client := 0; client < 4; client++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				start := time.Now()
				page, err := c.Search(ctx, "benchmark", q, []string{"scenes"})
				if err != nil {
					failures <- err
					return
				}
				if len(page.Items) != 100 {
					failures <- fmt.Errorf("selective query returned %d Items, expected 100", len(page.Items))
					return
				}
				elapsed := time.Since(start)
				mu.Lock()
				durations = append(durations, elapsed)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[len(durations)*95/100]
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	t.Logf("Go=%s CPUs=%d GOMAXPROCS=%d Items=%d clients=4 requests=%d p95=%s GoHeapMiB=%.1f", runtime.Version(), runtime.NumCPU(), runtime.GOMAXPROCS(0), n, len(durations), p95, float64(memory.HeapAlloc)/(1<<20))
	if p95 > time.Second {
		t.Fatalf("warm selective search p95 %s exceeds one second", p95)
	}
}
