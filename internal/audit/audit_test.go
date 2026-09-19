package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
)

func auditTestConfig(root string) conf.Audit {
	return conf.Audit{Enabled: true, DatabasePath: filepath.Join(root, "audit.duckdb"), RetentionDays: 90,
		MaxFieldBytes: 256, CleanupIntervalSec: 86400, ShutdownTimeoutSec: 5, WriteTimeoutSec: 1,
		RetryIntervalSec: 60, RetryBatchSize: 100}
}

func TestMiddlewareRecordsChangesSecurityAndRetention(t *testing.T) {
	cfg := auditTestConfig(t.TempDir())
	manager, err := Open(context.Background(), cfg, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/denied" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/demo/services", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/denied?secret=not-recorded", nil))
	events, err := manager.List(context.Background(), Query{Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %+v, %v", events, err)
	}
	for _, event := range events {
		if event.Path == "" || event.Path == "/denied?secret=not-recorded" {
			t.Fatalf("unsafe audited path: %q", event.Path)
		}
	}
	if err := manager.Record(context.Background(), Event{Timestamp: time.Now().AddDate(0, 0, -100), Method: "POST", Path: "/old", Action: "change"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, _ = manager.List(context.Background(), Query{Limit: 10})
	if len(events) != 2 {
		t.Fatalf("retention left %d events", len(events))
	}
}

func TestWriteFailureQueuesWithoutChangingResponseAndRecovers(t *testing.T) {
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.duckdb"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	manager, err := Open(context.Background(), auditTestConfig(root), "abc123", slog.New(slog.NewTextHandler(io.Discard, nil)), catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	var failing atomic.Bool
	failing.Store(true)
	manager.setWriteEvent(func(ctx context.Context, event Event) error {
		if failing.Load() {
			return errors.New("injected audit write failure")
		}
		return manager.insertEvent(ctx, event)
	})
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
	request = request.WithContext(identity.WithIdentity(request.Context(), &identity.Identity{APIKeyID: "outbox-key", AuthMethod: identity.AuthMethodAPIKey, SessionID: "browser-session"}))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("response changed to %d", recorder.Code)
	}
	if count, countErr := catalog.CountAuditOutbox(context.Background()); countErr != nil || count != 1 {
		t.Fatalf("outbox count = %d, %v", count, countErr)
	}
	if err := manager.Health(context.Background()); err == nil {
		t.Fatal("degraded audit manager reported healthy")
	}
	failing.Store(false)
	if err := manager.drainBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Health(context.Background()); err != nil {
		t.Fatalf("audit health did not recover: %v", err)
	}
	events, err := manager.List(context.Background(), Query{Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if events[0].CredentialID != "outbox-key" {
		t.Fatalf("outbox lost credential attribution: %+v", events[0])
	}
	if count, _ := catalog.CountAuditOutbox(context.Background()); count != 0 {
		t.Fatalf("outbox retained %d events", count)
	}
}

type rejectEnqueueOutbox struct{ Outbox }

func (r rejectEnqueueOutbox) EnqueueAuditEvent(context.Context, string, time.Time, []byte) error {
	return errors.New("injected outbox failure")
}

func TestAuditAndOutboxFailureLatchesReadiness(t *testing.T) {
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.duckdb"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	manager, err := Open(context.Background(), auditTestConfig(root), "abc123", slog.New(slog.NewTextHandler(io.Discard, nil)), rejectEnqueueOutbox{Outbox: catalog})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	manager.setWriteEvent(func(context.Context, Event) error { return errors.New("injected audit failure") })
	if err := manager.Record(context.Background(), Event{Method: http.MethodPost, Path: "/api/v1/roles", Action: "change"}); err == nil {
		t.Fatal("lost audit handoff returned nil")
	}
	if !manager.lost.Load() {
		t.Fatal("audit loss was not latched")
	}
	if err := manager.Health(context.Background()); err == nil {
		t.Fatal("latched audit loss reported healthy")
	}
}

func TestShutdownPreservesUndeliveredOutbox(t *testing.T) {
	root := t.TempDir()
	catalog, _, err := store.Init(store.Config{Path: filepath.Join(root, "catalog.duckdb"), EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	manager, err := Open(context.Background(), auditTestConfig(root), "abc123", slog.New(slog.NewTextHandler(io.Discard, nil)), catalog)
	if err != nil {
		t.Fatal(err)
	}
	manager.setWriteEvent(func(context.Context, Event) error { return errors.New("still unavailable") })
	if err := manager.Record(context.Background(), Event{Method: http.MethodDelete, Path: "/api/v1/workspaces/demo", Action: "change"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := manager.Close(ctx); err == nil {
		t.Fatal("shutdown unexpectedly drained unavailable audit storage")
	}
	if count, countErr := catalog.CountAuditOutbox(context.Background()); countErr != nil || count != 1 {
		t.Fatalf("outbox after shutdown = %d, %v", count, countErr)
	}
}

func TestRetentionFailureDegradesUntilSuccessfulRetry(t *testing.T) {
	root := t.TempDir()
	manager, err := Open(context.Background(), auditTestConfig(root), "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	manager.setPurgeOld(func(context.Context) error { return errors.New("injected retention failure") })
	if err := manager.Purge(context.Background()); err == nil {
		t.Fatal("retention fault returned nil")
	}
	if err := manager.Health(context.Background()); err == nil {
		t.Fatal("retention fault did not degrade health")
	}
	manager.setPurgeOld(manager.purgeEvents)
	if err := manager.Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Health(context.Background()); err != nil {
		t.Fatalf("retention health did not recover: %v", err)
	}
}
