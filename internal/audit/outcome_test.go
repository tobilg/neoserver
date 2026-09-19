package audit

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestListFiltersByOutcome(t *testing.T) {
	ctx := context.Background()
	m, err := Open(ctx, auditTestConfig(t.TempDir()), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)
	stamp := time.Now().UTC()
	for i, status := range []int{200, 201, 302, 400, 401, 500} {
		if _, err := m.db.ExecContext(ctx, `INSERT INTO audit_events(id,occurred_at,principal,credential_id,workspace,path,operation,status,request_id,auth_method,protocol,action,method,duration_ms,security_event) VALUES(?,?,'','','','/p','op',?,'','','','read','GET',0,false)`, fmt.Sprintf("e%d", i), stamp, status); err != nil {
			t.Fatal(err)
		}
	}
	count := func(outcome string) int {
		t.Helper()
		events, err := m.List(ctx, Query{Outcome: outcome})
		if err != nil {
			t.Fatal(err)
		}
		return len(events)
	}
	if got := count(""); got != 6 {
		t.Fatalf("all = %d, want 6", got)
	}
	if got := count(OutcomeFailed); got != 3 {
		t.Fatalf("failed = %d, want 3", got)
	}
	if got := count(OutcomeSucceeded); got != 3 {
		t.Fatalf("succeeded = %d, want 3", got)
	}
}
