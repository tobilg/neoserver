package pathpolicy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
)

// remoteOrigin serves every path with body and status, reachable as
// https://example.com through the fetcher's transport seam.
func remoteOrigin(t *testing.T, status *atomic.Int32, body string, opts Options) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := int(status.Load())
		w.WriteHeader(code)
		if code == http.StatusOK {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(server.Close)
	previous := remoteTransport
	remoteTransport = func() http.RoundTripper {
		transport := server.Client().Transport.(*http.Transport).Clone()
		transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}
		return transport
	}
	opts.RemoteCachePath = t.TempDir()
	if err := ConfigureWithOptions([]string{"https://example.com/**"}, opts); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		remoteTransport = previous
		Configure(nil)
	})
}

func TestAcquireServesCachedCopyWhenOriginFails(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	remoteOrigin(t, &status, "parquet-v1", Options{})
	ctx := context.Background()
	first, err := Acquire(ctx, "https://example.com/roads.parquet")
	if err != nil {
		t.Fatal(err)
	}
	first.Release()
	if !first.Cached {
		t.Fatal("HTTPS source was not cached")
	}

	status.Store(http.StatusServiceUnavailable)
	stale, err := Acquire(ctx, "https://example.com/roads.parquet")
	if err != nil {
		t.Fatalf("5xx with a cached copy: %v", err)
	}
	stale.Release()
	if body, _ := os.ReadFile(stale.Path); string(body) != "parquet-v1" {
		t.Fatalf("stale body %q", body)
	}

	status.Store(http.StatusNotFound)
	if _, err := Acquire(ctx, "https://example.com/roads.parquet"); err == nil {
		t.Fatal("a 404 must not be masked by the cached copy")
	}
	status.Store(http.StatusServiceUnavailable)
	if _, err := Acquire(ctx, "https://example.com/never-fetched.parquet"); err == nil {
		t.Fatal("a 5xx without a cached copy must fail")
	}
}

func TestRemoteCacheQuotaEvictsOnlyUnleasedDownloads(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	remoteOrigin(t, &status, "12345678", Options{RemoteCacheMaxBytes: 10})
	ctx := context.Background()
	acquire := func(path string) *Lease {
		t.Helper()
		lease, err := Acquire(ctx, "https://example.com/"+path)
		if err != nil {
			t.Fatal(err)
		}
		return lease
	}
	exists := func(lease *Lease) bool { _, err := os.Stat(lease.Path); return err == nil }

	a := acquire("a.parquet")
	a.Release()
	// Keep the LRU order deterministic on coarse file-system clocks.
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(a.Path, old, old)
	b := acquire("b.parquet")
	defer b.Release()
	if exists(a) || !exists(b) {
		t.Fatalf("over quota: released a kept=%v, leased b kept=%v", exists(a), exists(b))
	}

	c := acquire("c.parquet")
	defer c.Release()
	if !exists(b) || !exists(c) {
		t.Fatal("a leased download was evicted to satisfy the quota")
	}
}

func TestRemoteCacheEvictsDownloadsUnusedPastMaxAge(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	remoteOrigin(t, &status, "x", Options{RemoteCacheMaxAge: time.Hour})
	ctx := context.Background()
	stale, err := Acquire(ctx, "https://example.com/stale.parquet")
	if err != nil {
		t.Fatal(err)
	}
	stale.Release()
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale.Path, old, old); err != nil {
		t.Fatal(err)
	}
	fresh, err := Acquire(ctx, "https://example.com/fresh.parquet")
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Release()
	if _, err := os.Stat(stale.Path); err == nil {
		t.Fatal("download unused past RemoteCacheMaxAge was kept")
	}
	if _, err := os.Stat(stale.Path + ".json"); err == nil {
		t.Fatal("evicted download left its metadata behind")
	}
}

func TestConcurrentLeasesKeepHeldDownloads(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	remoteOrigin(t, &status, "12345678", Options{RemoteCacheMaxBytes: 10})
	var group errgroup.Group
	for i := range 16 {
		group.Go(func() error {
			lease, err := Acquire(context.Background(), fmt.Sprintf("https://example.com/%d.parquet", i%4))
			if err != nil {
				return err
			}
			defer lease.Release()
			// A held download must survive quota passes from other acquisitions.
			if _, err := os.Stat(lease.Path); err != nil {
				return fmt.Errorf("held download evicted: %w", err)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		t.Fatal(err)
	}
	remoteCache.Lock()
	defer remoteCache.Unlock()
	if len(remoteCache.pins) != 0 {
		t.Fatalf("leases leaked: %v", remoteCache.pins)
	}
}
