package identity

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

// StaticAPIKeyValidator validates a single static API key configured in the server
// configuration (Auth.ApiKey). It is a coarse, server-level credential: a matching
// key is granted the configured default role globally (default "super_admin").
type StaticAPIKeyValidator struct {
	key  string
	role string
	// allowQueryParam permits reading the key from the ?apikey= query parameter.
	allowQueryParam bool
}

// NewStaticAPIKeyValidator creates a validator for the given static key and role.
func NewStaticAPIKeyValidator(key, role string) *StaticAPIKeyValidator {
	if role == "" {
		role = "super_admin"
	}
	return &StaticAPIKeyValidator{key: key, role: role}
}

// ValidateRequest extracts a candidate API key and compares it against the configured
// static key in constant time. It returns (nil, nil) when no key is present or the key
// is not ours, so other validators may run.
func (v *StaticAPIKeyValidator) ValidateRequest(ctx context.Context, r *http.Request) (*Identity, error) {
	if v.key == "" {
		return nil, nil
	}

	key := r.Header.Get("X-API-Key")
	if key == "" {
		auth := r.Header.Get("Authorization")
		switch {
		case strings.HasPrefix(auth, "ApiKey "):
			key = strings.TrimPrefix(auth, "ApiKey ")
		case strings.HasPrefix(auth, "Bearer "):
			key = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if key == "" && v.allowQueryParam {
		key = r.URL.Query().Get("apikey")
	}
	if key == "" {
		return nil, nil
	}

	// Store-issued keys (nsk_) belong to the store validator, not here.
	if strings.HasPrefix(key, "nsk_") {
		return nil, nil
	}

	if subtle.ConstantTimeCompare([]byte(key), []byte(v.key)) != 1 {
		return nil, nil
	}

	return &Identity{
		Subject:    "static-apikey",
		AuthMethod: AuthMethodAPIKey,
		Roles:      map[string]string{"*": v.role},
	}, nil
}
