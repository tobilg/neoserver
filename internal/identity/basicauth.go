package identity

import (
	"context"
	"crypto/subtle"
	"net/http"
)

// BasicAuthValidator validates HTTP Basic credentials against a configured
// username -> password map (Auth.Users). Matching users are granted the configured
// default role globally (default "super_admin").
type BasicAuthValidator struct {
	users map[string]string
	role  string
}

// NewBasicAuthValidator creates a validator for the given users and role.
func NewBasicAuthValidator(users map[string]string, role string) *BasicAuthValidator {
	if role == "" {
		role = "super_admin"
	}
	return &BasicAuthValidator{users: users, role: role}
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

	expected, exists := v.users[username]
	// Run the compare unconditionally to avoid leaking whether the username exists.
	match := subtle.ConstantTimeCompare([]byte(password), []byte(expected)) == 1
	if !exists || !match {
		return nil, &AuthError{Message: "Invalid credentials"}
	}

	return &Identity{
		Subject:    username,
		AuthMethod: AuthMethodBasic,
		Roles:      map[string]string{"*": v.role},
	}, nil
}
