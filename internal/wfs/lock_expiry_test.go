package wfs

import (
	"testing"
	"time"
)

// WFS 2.0 answers a request carrying an expired lockId with LockHasExpired, and
// ETS wfs20 GetFeatureWithLockTests asserts the 403 that code maps to. Expired
// locks are swept whenever another lock is acquired, so the store has to keep
// the identifier long enough to tell an expired lock from one that never
// existed -- otherwise the answer depends on whether a sweep happened to run.
func TestExpiredLockStaysDistinguishableFromUnknownLock(t *testing.T) {
	locks := NewLockStore(3600, 0, 0, 0)
	lock, _, err := locks.AcquireLock("ws", map[string][]string{"roads": {"1"}}, 60, "ALL")
	if err != nil {
		t.Fatal(err)
	}
	lock.ExpiresAt = time.Now().Add(-time.Minute)

	// Acquiring any other lock sweeps expired ones.
	if _, _, err := locks.AcquireLock("ws", map[string][]string{"roads": {"2"}}, 60, "ALL"); err != nil {
		t.Fatal(err)
	}
	if locks.GetLock(lock.LockID) != nil {
		t.Fatal("expired lock was not swept; the test no longer covers the reported path")
	}

	err = locks.ValidateLock(lock.LockID, "ws", "roads", "1")
	requestErr, ok := err.(*RequestError)
	if !ok {
		t.Fatalf("validating a swept lock returned %T: %v", err, err)
	}
	if requestErr.Code != ExceptionLockHasExpired {
		t.Errorf("swept lock reported as %q, want %q", requestErr.Code, ExceptionLockHasExpired)
	}
	if got := getHTTPStatus(requestErr.Code); got != 403 {
		t.Errorf("status for %q = %d, want 403", requestErr.Code, got)
	}

	// An identifier that never existed is still a bad request, not an expiry.
	err = locks.ValidateLock("never-issued", "ws", "roads", "1")
	requestErr, ok = err.(*RequestError)
	if !ok {
		t.Fatalf("validating an unknown lock returned %T: %v", err, err)
	}
	if requestErr.Code != ExceptionInvalidParameterValue {
		t.Errorf("unknown lock reported as %q, want %q", requestErr.Code, ExceptionInvalidParameterValue)
	}
}
