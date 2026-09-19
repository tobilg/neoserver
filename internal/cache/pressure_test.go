package cache

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPressureEvictsWithoutExceedingBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Features.MaxMemoryMB = 1
	cfg.Features.MaxEntries = 1000
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	value := bytes.Repeat([]byte{'x'}, 16<<10)
	for i := 0; i < 200; i++ {
		m.SetFeatures("ws:layer:"+string(rune(i+1000)), value, time.Minute)
	}
	m.features.Wait()
	metrics := m.GetMetrics().Features
	if metrics.SizeBytes > metrics.MaxSizeBytes {
		t.Fatalf("cache used %d bytes with maximum %d", metrics.SizeBytes, metrics.MaxSizeBytes)
	}
	if metrics.KeysEvicted == 0 && metrics.SetsRejected == 0 {
		t.Fatal("expected eviction or admission rejection under pressure")
	}
	if metrics.InvalidationKeys > metrics.EntryCount+1 {
		t.Fatalf("invalidation index retained %d keys for %d cache entries", metrics.InvalidationKeys, metrics.EntryCount)
	}
}

func TestOversizedAdmissionIsObservable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tiles.MaxEntrySize = 8
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetTile("ws:large", make([]byte, 9), time.Minute)
	if _, ok := m.GetTile("ws:large"); ok {
		t.Fatal("oversized tile was cached")
	}
	if got := m.GetMetrics().Tiles.OversizedSkipped; got != 1 {
		t.Fatalf("oversized skips = %d", got)
	}
}

func TestNativeTTLAndDeletionCleanTracking(t *testing.T) {
	m, err := NewManager(DefaultConfig())
	if err != nil { t.Fatal(err) }
	defer m.Close()
	m.SetFeatures("ws:native-ttl", []byte("value"), time.Minute)
	m.features.Wait()
	remaining, ok := m.features.GetTTL("ws:native-ttl")
	if !ok || remaining <= 0 || remaining > time.Minute { t.Fatalf("native TTL = %v, %v", remaining, ok) }
	if got := m.GetMetrics().Features.InvalidationKeys; got != 1 { t.Fatalf("tracked keys = %d", got) }
	m.features.Del("ws:native-ttl")
	m.features.Wait()
	if got := m.GetMetrics().Features.InvalidationKeys; got != 0 { t.Fatalf("tracked keys after delete = %d", got) }
}

func TestLoadBytesCoalescesConcurrentMisses(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(context.Context) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return []byte("result"), nil
	}
	const workers = 12
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			value, _, err := m.LoadBytes(context.Background(), CacheTypeFeatures, "ws:same", time.Minute, loader)
			if err == nil && string(value) != "result" {
				t.Errorf("value = %q", value)
			}
			errs <- err
		}()
	}
	<-started
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("loader calls = %d", got)
	}
	if m.GetMetrics().Features.LoadsShared == 0 {
		t.Fatal("shared load was not recorded")
	}
}

func TestLoadBytesWaiterCanCancelIndependently(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	started, release := make(chan struct{}), make(chan struct{})
	loader := func(ctx context.Context) ([]byte, error) {
		close(started)
		select {
		case <-release:
			return []byte("ok"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	leaderDone := make(chan error, 1)
	go func() {
		_, _, err := m.LoadBytes(context.Background(), CacheTypeFeatures, "ws:cancel", time.Minute, loader)
		leaderDone <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := m.LoadBytes(ctx, CacheTypeFeatures, "ws:cancel", time.Minute, loader); err == nil {
		t.Fatal("cancelled waiter succeeded")
	}
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatal(err)
	}
}
