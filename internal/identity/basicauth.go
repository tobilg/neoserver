package identity

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"
)

// BasicAuthValidator validates HTTP Basic credentials against a configured
// username -> password map (Auth.Users). Matching users are granted the configured
// default role globally (default "super_admin").
type BasicAuthValidator struct {
	users   map[string]string
	role    string
	limiter *FailureLimiter // throttles password guessing on every endpoint
}

// NewBasicAuthValidator creates a validator for the given users and role.
func NewBasicAuthValidator(users map[string]string, role string) *BasicAuthValidator {
	if role == "" {
		role = "super_admin"
	}
	return &BasicAuthValidator{users: users, role: role, limiter: NewFailureLimiter()}
}

// ValidateRequest validates Basic credentials. It returns (nil, nil) when no Basic
// header is present so other validators may run, and an AuthError when credentials
// are present but invalid. Comparison is constant time to limit timing signals.
func (v *BasicAuthValidator) ValidateRequest(ctx context.Context, r *http.Request) (*Identity, error) {
	if len(v.users) == 0 {
		return nil, nil
	}

	username, password, ok := r.BasicAuth()
	if !ok {
		return nil, nil
	}

	keys, now := CredentialRateKeys(r, username), time.Now()
	if wait := v.limiter.Wait(keys, now); wait > 0 {
		return nil, &AuthError{Message: "Too many failed sign-in attempts", StatusCode: http.StatusTooManyRequests, RetryAfter: wait}
	}
	expected, exists := v.users[username]
	// Run the compare unconditionally to avoid leaking whether the username exists.
	match := subtle.ConstantTimeCompare([]byte(password), []byte(expected)) == 1
	if !exists || !match {
		return nil, &AuthError{Message: "Invalid credentials", RetryAfter: v.limiter.Fail(keys, now)}
	}
	v.limiter.Succeed(keys)

	return &Identity{
		Subject:    username,
		AuthMethod: AuthMethodBasic,
		Roles:      map[string]string{"*": v.role},
	}, nil
}
