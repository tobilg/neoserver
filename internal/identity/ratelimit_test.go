package identity

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func requestFrom(address string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = address + ":1234"
	return request
}

func TestFailureLimiterBacksOffAndResets(t *testing.T) {
	limiter, now := NewFailureLimiter(), time.Now()
	keys := CredentialRateKeys(requestFrom("198.51.100.1"), "ada")
	for attempt := 1; attempt < clientFailureThreshold; attempt++ {
		if wait := limiter.Fail(keys, now); wait > 0 {
			t.Fatalf("attempt %d blocked for %s", attempt, wait)
		}
	}
	if wait := limiter.Fail(keys, now); wait < 29*time.Second || wait > 31*time.Second {
		t.Fatalf("threshold failure wait=%s, want about 30s", wait)
	}
	if wait := limiter.Fail(keys, now); wait < 59*time.Second {
		t.Fatalf("backoff did not double: %s", wait)
	}
	limiter.Succeed(keys)
	if wait := limiter.Wait(keys, now); wait != 0 {
		t.Fatalf("success did not reset: %s", wait)
	}
}

func TestFailureLimiterDoesNotLetOneClientLockOutAUser(t *testing.T) {
	limiter, now := NewFailureLimiter(), time.Now()
	for range 20 {
		limiter.Fail(CredentialRateKeys(requestFrom("198.51.100.1"), "ada"), now)
	}
	if wait := limiter.Wait(CredentialRateKeys(requestFrom("198.51.100.1"), "ada"), now); wait == 0 {
		t.Fatal("the guessing client was not throttled")
	}
	if wait := limiter.Wait(CredentialRateKeys(requestFrom("203.0.113.9"), "ada"), now); wait != 0 {
		t.Fatalf("another client was locked out of the user for %s", wait)
	}
	// Distributed guessing still throttles the principal.
	for client := range principalFailureThreshold {
		limiter.Fail(CredentialRateKeys(requestFrom(fmt.Sprintf("192.0.2.%d", client)), "bob"), now)
	}
	if wait := limiter.Wait(CredentialRateKeys(requestFrom("203.0.113.9"), "bob"), now); wait == 0 {
		t.Fatal("distributed guessing did not throttle the principal")
	}
}
