package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/tobilg/neoserver/internal/store"
)

// APIKeyValidator validates API keys against the store.
type APIKeyValidator struct {
	store store.Store
	// allowQueryParam permits reading the key from the ?apikey= query parameter.
	// Off by default to avoid leaking keys into logs/proxies/history.
	allowQueryParam bool
}

// NewAPIKeyValidator creates a new API key validator.
func NewAPIKeyValidator(s store.Store) *APIKeyValidator {
	return &APIKeyValidator{store: s}
}

// ValidateRequest extracts and validates an API key from the request.
func (v *APIKeyValidator) ValidateRequest(ctx context.Context, r *http.Request) (*Identity, error) {
	// Extract API key from header or query parameter
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = r.Header.Get("Authorization")
		if strings.HasPrefix(key, "Bearer nsk_") {
			key = strings.TrimPrefix(key, "Bearer ")
		} else if strings.HasPrefix(key, "ApiKey ") {
			key = strings.TrimPrefix(key, "ApiKey ")
		} else {
			key = ""
		}
	}
	if key == "" && v.allowQueryParam {
		key = r.URL.Query().Get("apikey")
	}

	if key == "" {
		return nil, nil // No API key provided
	}

	// Validate the key starts with our prefix
	if !strings.HasPrefix(key, "nsk_") {
		return nil, &AuthError{Message: "Invalid API key format"}
	}

	return v.Validate(ctx, key)
}

// Validate validates an API key and returns the identity.
func (v *APIKeyValidator) Validate(ctx context.Context, key string) (*Identity, error) {
	// Hash the key
	hash := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(hash[:])

	// Look up the key in the store
	apiKey, err := v.store.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		if err == store.ErrNotFound || err == store.ErrInvalidCredentials {
			return nil, &AuthError{Message: "Invalid API key"}
		}
		return nil, &AuthError{Message: "Failed to validate API key"}
	}

	// Build identity
	roles := make(map[string]string)
	if apiKey.WorkspaceID != nil {
		roles[*apiKey.WorkspaceID] = apiKey.RoleID
	} else {
		// Global API key (e.g., super_admin)
		roles["*"] = apiKey.RoleID
	}

	identity := &Identity{
		Subject:    apiKey.OwnerName,
		Email:      apiKey.OwnerEmail,
		AuthMethod: AuthMethodAPIKey,
		Roles:      roles,
		APIKeyID:   apiKey.ID,
	}
	return identity, nil
}

// AuthError represents an authentication error.
type AuthError struct {
	Message    string
	StatusCode int
}

func (e *AuthError) Error() string {
	return e.Message
}
