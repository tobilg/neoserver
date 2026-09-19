package audit

import (
	"context"
	"database/sql"
	"github.com/tobilg/neoserver/internal/testutil/schematest"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func openAuditFile(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("ATTACH '" + sqlLiteral(path) + "' AS audit (ENCRYPTION_KEY 'abc123'); USE audit"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

func auditSchemaVersion(t *testing.T, path string) int {
	t.Helper()
	db := openAuditFile(t, path)
	defer db.Close()
	var version int
	if err := db.QueryRow("SELECT max(version) FROM audit_schema").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func TestAuditSchemaStampsNewAndUnversionedLogs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name  string
		setup string
	}{
		{name: "new"},
		// The shape 0.1.0 wrote: no version table, credential_id added by ALTER.
		{name: "unversioned", setup: `CREATE TABLE audit_events (
			id VARCHAR PRIMARY KEY, occurred_at TIMESTAMP NOT NULL, request_id VARCHAR, principal VARCHAR,
			auth_method VARCHAR, workspace VARCHAR, protocol VARCHAR, operation VARCHAR, action VARCHAR,
			method VARCHAR, path VARCHAR, status INTEGER, duration_ms BIGINT, security_event BOOLEAN);
			ALTER TABLE audit_events ADD COLUMN credential_id VARCHAR DEFAULT '';
			CREATE INDEX audit_events_time ON audit_events(occurred_at);
CREATE INDEX audit_events_workspace ON audit_events(workspace,occurred_at);
INSERT INTO audit_events(id, occurred_at) VALUES ('kept', now())`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := auditTestConfig(t.TempDir())
			if tc.setup != "" {
				db := openAuditFile(t, cfg.DatabasePath)
				if _, err := db.Exec(tc.setup); err != nil {
					t.Fatal(err)
				}
				db.Close()
			}
			for i := 0; i < 2; i++ {
				manager, err := Open(context.Background(), cfg, "abc123", logger)
				if err != nil {
					t.Fatal(err)
				}
				if err = manager.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if version := auditSchemaVersion(t, cfg.DatabasePath); version != schemaVersion {
				t.Fatalf("version=%d", version)
			}
			if tc.setup != "" {
				db := openAuditFile(t, cfg.DatabasePath)
				defer db.Close()
				var count int
				if err := db.QueryRow("SELECT count(*) FROM audit_events WHERE id='kept'").Scan(&count); err != nil || count != 1 {
					t.Fatalf("existing event lost: %d %v", count, err)
				}
			}
		})
	}
}

func TestAuditSchemaRefusesUnsupportedLogs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name, setup, want string
	}{
		{name: "older", setup: "CREATE TABLE audit_schema (version INTEGER PRIMARY KEY, applied_at TIMESTAMP); INSERT INTO audit_schema(version) VALUES (0)", want: "older than the oldest supported version"},
		{name: "newer", setup: "CREATE TABLE audit_schema (version INTEGER PRIMARY KEY, applied_at TIMESTAMP); INSERT INTO audit_schema(version) VALUES (2)", want: "newer than this binary supports"},
		{name: "missing column", setup: "CREATE TABLE audit_events (id VARCHAR PRIMARY KEY, occurred_at TIMESTAMP NOT NULL)", want: "predates schema version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := auditTestConfig(t.TempDir())
			db := openAuditFile(t, cfg.DatabasePath)
			if _, err := db.Exec(tc.setup); err != nil {
				t.Fatal(err)
			}
			db.Close()
			defer schematest.Unchanged(t, cfg.DatabasePath)()
			manager, err := Open(context.Background(), cfg, "abc123", logger)
			if manager != nil {
				manager.Close(context.Background())
				t.Fatal("opened an unsupported audit log")
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
