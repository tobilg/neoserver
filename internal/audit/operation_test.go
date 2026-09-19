package audit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/identity"
)

func TestAuditCanonicalOperationsAndCredentialAttribution(t *testing.T) {
	ctx := context.Background()
	m, err := Open(ctx, auditTestConfig(t.TempDir()), "abc123", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, tc := range []struct{ method, query, body, key, subject string }{
		{"GET", "request=DropStoredQuery", "", "key-1", ""},
		{"GET", "request=LockFeature", "", "key-2", "same owner"},
		{"POST", "request=GetCapabilities", `<wfs:Transaction xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS"/>`, "key-3", "same owner"},
		{"POST", "request=Transaction", `<wfs:GetFeature xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS"/>`, "read-key", ""},
		{"GET", "request=GetFeature", "", "read-key", ""},
	} {
		r := httptest.NewRequest(tc.method, "/maps/workspaces/demo/wfs?"+tc.query, strings.NewReader(tc.body))
		r = r.WithContext(identity.WithIdentity(r.Context(), &identity.Identity{Subject: tc.subject, APIKeyID: tc.key, AuthMethod: identity.AuthMethodAPIKey}))
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	events, err := m.List(ctx, Query{})
	if err != nil || len(events) != 3 {
		t.Fatalf("events=%+v error=%v", events, err)
	}
	for key, operation := range map[string]string{"key-1": "DROPSTOREDQUERY", "key-2": "LOCKFEATURE", "key-3": "TRANSACTION"} {
		filtered, err := m.List(ctx, Query{CredentialID: key})
		if err != nil || len(filtered) != 1 || filtered[0].Operation != operation || filtered[0].Action != "change" {
			t.Fatalf("%s: %+v %v", key, filtered, err)
		}
		payload, _ := json.Marshal(filtered[0])
		var restored Event
		if err := json.Unmarshal(payload, &restored); err != nil || restored.CredentialID != key {
			t.Fatalf("outbox roundtrip: %+v %v", restored, err)
		}
	}
}

func TestAuditCredentialMigrationPreservesHistoricalEvents(t *testing.T) {
	ctx := context.Background()
	cfg := auditTestConfig(t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := Open(ctx, cfg, "abc123", logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Record(ctx, Event{ID: "historical", Method: "POST", Action: "change", Path: "/old", Principal: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec("DROP INDEX audit_events_time; DROP INDEX audit_events_workspace; ALTER TABLE audit_events DROP COLUMN credential_id"); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	m, err = Open(ctx, cfg, "abc123", logger)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)
	events, err := m.List(ctx, Query{})
	if err != nil || len(events) != 1 || events[0].CredentialID != "" || events[0].Principal != "owner" {
		t.Fatalf("historical events: %+v %v", events, err)
	}
	var legacy Event
	if err := json.Unmarshal([]byte(`{"id":"old-outbox","method":"POST","action":"change","path":"/old-outbox"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if err := m.Record(ctx, legacy); err != nil {
		t.Fatal(err)
	}
}
