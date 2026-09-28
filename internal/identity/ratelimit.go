package identity

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Failure thresholds for CredentialRateKeys. A single client is throttled
// quickly; a principal only after failures from many clients, so one client
// cannot lock another user out while distributed guessing is still slowed.
const (
	clientFailureThreshold    = 5
	principalFailureThreshold = 50
)

// RateKey is one dimension a FailureLimiter counts failures in.
type RateKey struct {
	Key       string
	Threshold int
}

// CredentialRateKeys returns the limiter keys for a credential guess by a client.
// The principal is hashed so secrets presented as principals are not retained.
func CredentialRateKeys(r *http.Request, principal string) []RateKey {
	return []RateKey{
		{Key: "ip:" + ClientAddress(r), Threshold: clientFailureThreshold},
		{Key: "principal:" + HashSessionToken(principal), Threshold: principalFailureThreshold},
	}
}

// ClientAddress is the request's client IP. TransportSecurity has already
// replaced RemoteAddr with the forwarded client when the peer is a trusted proxy.
func ClientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

type failureAttempt struct {
	failures     int
	blockedUntil time.Time
	updatedAt    time.Time
}

// FailureLimiter backs off repeated authentication failures. Once a key reaches
// its threshold it is blocked for 30 seconds, doubling per further failure up
// to 15 minutes. A success clears the keys. It is safe for concurrent use.
type FailureLimiter struct {
	mu       sync.Mutex
	attempts map[string]failureAttempt
}

func NewFailureLimiter() *FailureLimiter {
	return &FailureLimiter{attempts: make(map[string]failureAttempt)}
}

// Wait reports how long the most restrictive key remains blocked.
func (l *FailureLimiter) Wait(keys []RateKey, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	var wait time.Duration
	for _, key := range keys {
		if blocked := l.attempts[key.Key].blockedUntil; blocked.After(now) {
			wait = max(wait, blocked.Sub(now))
		}
	}
	return wait
}

// Fail records a failure against every key and returns the resulting wait.
func (l *FailureLimiter) Fail(keys []RateKey, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	var wait time.Duration
	for _, key := range keys {
		attempt := l.attempts[key.Key]
		attempt.failures++
		attempt.updatedAt = now
		if over := attempt.failures - key.Threshold; over >= 0 {
			delay := 30 * time.Second << min(over, 5)
			attempt.blockedUntil = now.Add(min(delay, 15*time.Minute))
		}
		l.attempts[key.Key] = attempt
		if attempt.blockedUntil.After(now) {
			wait = max(wait, attempt.blockedUntil.Sub(now))
		}
	}
	return wait
}

// Succeed clears the failure history of every key.
func (l *FailureLimiter) Succeed(keys []RateKey) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		delete(l.attempts, key.Key)
	}
}

func (l *FailureLimiter) prune(now time.Time) {
	if len(l.attempts) < 1024 {
		return
	}
	for key, attempt := range l.attempts {
		if !attempt.blockedUntil.After(now) && now.Sub(attempt.updatedAt) > time.Hour {
			delete(l.attempts, key)
		}
	}
}
