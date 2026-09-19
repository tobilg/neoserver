package tilecache

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testOwnershipConfig(hostname string) OwnershipConfig {
	return OwnershipConfig{
		Prefix:        "neoserver-tiles",
		CheckInterval: 20 * time.Millisecond,
		Hostname:      hostname,
		Logger:        discardLogger(),
	}
}

func mustGuard(t *testing.T, backend BlobStore, cfg OwnershipConfig) *ownershipGuard {
	t.Helper()
	guard, err := newOwnershipGuard(backend, cfg)
	if err != nil {
		t.Fatalf("new ownership guard: %v", err)
	}
	return guard
}

func markerFrom(t *testing.T, backend *fakeBlobStore, key string) ownerMarker {
	t.Helper()
	body, ok := backend.object(key)
	if !ok {
		t.Fatalf("marker %s missing", key)
	}
	var marker ownerMarker
	if err := json.Unmarshal(body, &marker); err != nil {
		t.Fatalf("unmarshal marker: %v", err)
	}
	return marker
}

func putOwner(t *testing.T, backend *fakeBlobStore, marker ownerMarker) string {
	t.Helper()
	body, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	key := "neoserver-tiles/.neoserver-owner.json"
	if err := backend.Put(context.Background(), key, body); err != nil {
		t.Fatal(err)
	}
	return key
}

func existingMarker(id, host string) ownerMarker {
	return ownerMarker{SchemaVersion: ownerMarkerVersion, InstanceID: id, Hostname: host, PID: 1,
		StartedAt: time.Now().Add(-time.Hour)}
}

func TestOwnershipAcquireOnEmptyPrefix(t *testing.T) {
	backend := newFakeBlobStore()
	guard := mustGuard(t, backend, testOwnershipConfig("node-a"))
	if err := guard.acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer guard.release()

	marker := markerFrom(t, backend, guard.key)
	if marker.SchemaVersion != ownerMarkerVersion || marker.Hostname != "node-a" || marker.PID != os.Getpid() || marker.InstanceID == "" {
		t.Fatalf("marker = %+v", marker)
	}
}

func TestOwnershipAlwaysRefusesExistingMarker(t *testing.T) {
	for _, host := range []string{"node-a", "node-b"} {
		t.Run(host, func(t *testing.T) {
			backend := newFakeBlobStore()
			putOwner(t, backend, existingMarker("old-owner", host))
			guard := mustGuard(t, backend, testOwnershipConfig("node-a"))
			err := guard.acquire(context.Background())
			if err == nil {
				guard.release()
				t.Fatal("existing marker must require manual takeover")
			}
			for _, want := range []string{"old-owner", "--take-over-tile-cache-owner"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q missing %q", err, want)
				}
			}
		})
	}
}

func TestOwnershipManualTakeoverRequiresExactOwner(t *testing.T) {
	backend := newFakeBlobStore()
	putOwner(t, backend, existingMarker("old-owner", "node-a"))

	wrongCfg := testOwnershipConfig("node-b")
	wrongCfg.TakeoverOwnerID = "different-owner"
	if err := mustGuard(t, backend, wrongCfg).acquire(context.Background()); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong owner error = %v", err)
	}

	cfg := testOwnershipConfig("node-b")
	cfg.TakeoverOwnerID = "old-owner"
	guard := mustGuard(t, backend, cfg)
	if err := guard.acquire(context.Background()); err != nil {
		t.Fatalf("manual takeover: %v", err)
	}
	defer guard.release()
	if marker := markerFrom(t, backend, guard.key); marker.InstanceID != guard.instanceID || marker.Hostname != "node-b" {
		t.Fatalf("marker not replaced: %+v", marker)
	}
}

func TestOwnershipTakeoverRejectsAbsentMarker(t *testing.T) {
	cfg := testOwnershipConfig("node-b")
	cfg.TakeoverOwnerID = "old-owner"
	err := mustGuard(t, newFakeBlobStore(), cfg).acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "marker is absent") {
		t.Fatalf("error = %v", err)
	}
}

func TestOwnershipCorruptMarkerBlocksStartup(t *testing.T) {
	backend := newFakeBlobStore()
	_ = backend.Put(context.Background(), "neoserver-tiles/.neoserver-owner.json", []byte("{not json"))
	err := mustGuard(t, backend, testOwnershipConfig("node-b")).acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("error = %v", err)
	}
}

func TestOwnershipLegacyMarkerRemainsTakeoverCompatible(t *testing.T) {
	for version := legacyOwnerMarkerVersion; version < ownerMarkerVersion; version++ {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			backend := newFakeBlobStore()
			legacy := existingMarker("legacy-owner", "old-node")
			legacy.SchemaVersion = version
			putOwner(t, backend, legacy)
			cfg := testOwnershipConfig("new-node")
			cfg.TakeoverOwnerID = legacy.InstanceID
			guard := mustGuard(t, backend, cfg)
			if err := guard.acquire(context.Background()); err != nil {
				t.Fatalf("take over legacy marker: %v", err)
			}
			defer guard.release()
			if marker := markerFrom(t, backend, guard.key); marker.SchemaVersion != ownerMarkerVersion {
				t.Fatalf("replacement marker schema = %d, want %d", marker.SchemaVersion, ownerMarkerVersion)
			}
		})
	}
}

func TestOwnershipVerificationIsReadOnly(t *testing.T) {
	backend := newFakeBlobStore()
	guard := mustGuard(t, backend, testOwnershipConfig("node-a"))
	if err := guard.acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer guard.release()
	before, err := backend.GetLease(context.Background(), guard.key)
	if err != nil {
		t.Fatal(err)
	}
	putsBefore, getsBefore := backend.ownershipCallCounts()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, gets := backend.ownershipCallCounts()
		if gets > getsBefore {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("periodic ownership verification did not run")
		}
		time.Sleep(time.Millisecond)
	}
	putsAfter, _ := backend.ownershipCallCounts()
	after, err := backend.GetLease(context.Background(), guard.key)
	if err != nil {
		t.Fatal(err)
	}
	if putsAfter != putsBefore {
		t.Fatalf("ownership verification performed a write: before=%d after=%d", putsBefore, putsAfter)
	}
	if before.ETag != after.ETag || string(before.Body) != string(after.Body) {
		t.Fatal("ownership marker changed during read-only verification")
	}
}

func TestOwnershipVerificationFailurePermanentlyFences(t *testing.T) {
	backend := newFakeBlobStore()
	guard := mustGuard(t, backend, testOwnershipConfig("node-a"))
	if err := guard.acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	backend.setGetErr(errors.New("s3 unavailable"))
	select {
	case <-guard.lostSignal():
	case <-time.After(2 * time.Second):
		t.Fatal("ownership was not fenced after verification failure")
	}
	backend.setGetErr(nil)
	time.Sleep(2 * guard.cfg.CheckInterval)
	if err := guard.health(); !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("health = %v", err)
	}
	if _, err := guard.beginMutation(); !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("mutation gate = %v", err)
	}
	guard.release()
	if _, ok := backend.object(guard.key); !ok {
		t.Fatal("a process that lost ownership must not delete the marker")
	}
}

func TestOwnershipConditionalReleaseCannotDeleteReplacement(t *testing.T) {
	backend := newFakeBlobStore()
	cfg := testOwnershipConfig("node-a")
	cfg.CheckInterval = time.Hour
	guard := mustGuard(t, backend, cfg)
	if err := guard.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	object, err := backend.GetLease(context.Background(), guard.key)
	if err != nil {
		t.Fatal(err)
	}
	replacement, _ := json.Marshal(existingMarker("replacement", "node-b"))
	if _, err := backend.ReplaceLease(context.Background(), guard.key, replacement, object.ETag); err != nil {
		t.Fatal(err)
	}
	guard.release()
	if marker := markerFrom(t, backend, guard.key); marker.InstanceID != "replacement" {
		t.Fatalf("replacement marker was deleted: %+v", marker)
	}
}

type unsafeConditionalStore struct{ *fakeBlobStore }

func (s *unsafeConditionalStore) CreateLease(ctx context.Context, key string, content []byte) (string, error) {
	if err := s.Put(ctx, key, content); err != nil {
		return "", err
	}
	object, err := s.GetLease(ctx, key)
	return object.ETag, err
}

func TestOwnershipRejectsEndpointWithoutConditionalSemantics(t *testing.T) {
	backend := &unsafeConditionalStore{newFakeBlobStore()}
	err := mustGuard(t, backend, testOwnershipConfig("node-a")).acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "If-None-Match") {
		t.Fatalf("error = %v", err)
	}
}

func TestOwnershipEmptyPrefixMarkerKey(t *testing.T) {
	guard := mustGuard(t, newFakeBlobStore(), OwnershipConfig{Prefix: "", Logger: discardLogger()})
	if guard.key != ".neoserver-owner.json" {
		t.Fatalf("key = %q", guard.key)
	}
	guard = mustGuard(t, newFakeBlobStore(), OwnershipConfig{Prefix: "/p/x/", Logger: discardLogger()})
	if guard.key != "p/x/.neoserver-owner.json" {
		t.Fatalf("key = %q", guard.key)
	}
}

type noConditionalDeleteStore struct{ *fakeBlobStore }

func (s *noConditionalDeleteStore) DeleteLease(context.Context, string, string) error {
	panic("ownership must never depend on conditional delete")
}

func TestOwnershipReleaseReacquireWithoutConditionalDelete(t *testing.T) {
	backend := &noConditionalDeleteStore{newFakeBlobStore()}
	cfg := testOwnershipConfig("node-a")
	first := mustGuard(t, backend, cfg)
	if err := first.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.release()
	marker := markerFrom(t, backend.fakeBlobStore, first.key)
	if !marker.Released || marker.InstanceID != first.instanceID {
		t.Fatalf("released marker = %+v", marker)
	}
	staleTakeover := cfg
	staleTakeover.TakeoverOwnerID = first.instanceID
	if err := mustGuard(t, backend, staleTakeover).acquire(context.Background()); err == nil {
		t.Fatal("stale takeover option must not automatically reclaim a released prefix")
	}
	second := mustGuard(t, backend, cfg)
	if err := second.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer second.release()
	marker = markerFrom(t, backend.fakeBlobStore, second.key)
	if marker.Released || marker.InstanceID != second.instanceID {
		t.Fatalf("reacquired marker = %+v", marker)
	}
	if second.etag == first.etag {
		t.Fatal("owner generations reused an ETag")
	}
}

type unsafeReplacementStore struct{ *fakeBlobStore }

func (s *unsafeReplacementStore) ReplaceLease(ctx context.Context, key string, content []byte, _ string) (string, error) {
	if err := s.Put(ctx, key, content); err != nil {
		return "", err
	}
	object, err := s.GetLease(ctx, key)
	return object.ETag, err
}

func TestOwnershipRejectsIgnoredConditionalReplacement(t *testing.T) {
	backend := &unsafeReplacementStore{newFakeBlobStore()}
	err := mustGuard(t, backend, testOwnershipConfig("node-a")).acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "If-Match replacement") {
		t.Fatalf("error = %v", err)
	}
}

func TestOwnershipRequiresConditionalBackend(t *testing.T) {
	backend, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newOwnershipGuard(backend, testOwnershipConfig("node-a")); err == nil {
		t.Fatal("expected conditional backend error")
	}
}

func TestManagerAcquiresReleasesAndFencesOwnership(t *testing.T) {
	backend := newFakeBlobStore()
	cfg := testOwnershipConfig("node-a")
	manager, err := NewManager(context.Background(), Config{
		DatabasePath: t.TempDir() + "/cache.duckdb", EncryptionKey: "abc123", ObjectPrefix: "neoserver-tiles",
		MaxBytes: 1024, MaintenanceInterval: time.Hour, Ownership: &cfg,
	}, backend)
	if err != nil {
		t.Fatalf("manager with ownership: %v", err)
	}
	identity := testIdentity(0)
	if stored, err := manager.Put(context.Background(), identity, []byte("payload"), Policy{}); err != nil || !stored {
		t.Fatalf("initial put = %t, %v", stored, err)
	}
	backend.setGetErr(errors.New("verification failed"))
	select {
	case <-manager.ownership.lostSignal():
	case <-time.After(2 * time.Second):
		t.Fatal("ownership loss timeout")
	}
	backend.setGetErr(nil)
	if _, err := manager.Put(context.Background(), identity, []byte("new"), Policy{}); !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("fenced put error = %v", err)
	}
	if _, err := manager.JobStore().CreateTileCacheJob(context.Background(), CreateJobInput{WorkspaceID: "w"}); !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("fenced job error = %v", err)
	}
	if data, _, hit, err := manager.Get(context.Background(), identity); err != nil || !hit || string(data) != "payload" {
		t.Fatalf("read after fencing = %q, %t, %v", data, hit, err)
	}
	if err := manager.Health(context.Background()); !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("health = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestManagerRefusedWhenPrefixOwned(t *testing.T) {
	backend := newFakeBlobStore()
	putOwner(t, backend, existingMarker("other", "node-z"))
	cfg := testOwnershipConfig("node-a")
	_, err := NewManager(context.Background(), Config{
		DatabasePath: t.TempDir() + "/cache.duckdb", EncryptionKey: "abc123", ObjectPrefix: "neoserver-tiles",
		MaxBytes: 1024, MaintenanceInterval: time.Hour, Ownership: &cfg,
	}, backend)
	if err == nil || !strings.Contains(err.Error(), "node-z") {
		t.Fatalf("expected ownership refusal naming the owner, got %v", err)
	}
}
