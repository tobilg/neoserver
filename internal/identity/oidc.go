package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/tobilg/neoserver/internal/store"
)

// OIDCConfig holds OpenID Connect configuration.
type OIDCConfig struct {
	IssuerURL       string
	ClientID        string
	RequiredScopes  []string
	SkipIssuerCheck bool
	GroupClaims     []string
}

// OIDCValidator validates OIDC tokens and resolves claims to roles.
type OIDCValidator struct {
	store    store.Store
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	config   OIDCConfig
}

// NewOIDCValidator creates a new OIDC validator.
func NewOIDCValidator(ctx context.Context, s store.Store, cfg OIDCConfig) (*OIDCValidator, error) {
	// Bound both discovery and subsequent JWKS requests, including browser
	// authentication paths that construct a validator independently.
	ctx = oidc.ClientContext(ctx, &http.Client{Timeout: 5 * time.Second})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if cfg.IssuerURL == "" {
		return nil, fmt.Errorf("OIDC issuer URL is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("OIDC client ID is required")
	}

	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	verifierConfig := &oidc.Config{
		ClientID:          cfg.ClientID,
		SkipClientIDCheck: false,
		SkipIssuerCheck:   cfg.SkipIssuerCheck,
	}

	verifier := provider.Verifier(verifierConfig)

	return &OIDCValidator{
		store:    s,
		provider: provider,
		verifier: verifier,
		config:   cfg,
	}, nil
}

// ValidateRequest extracts and validates an OIDC token from the request.
func (v *OIDCValidator) ValidateRequest(ctx context.Context, r *http.Request) (*Identity, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		return nil, nil // No Bearer token
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	if token == "" {
		return nil, nil
	}

	// Skip if it's an API key
	if strings.HasPrefix(token, "nsk_") {
		return nil, nil
	}

	// Try to parse as JWT to check issuer
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		// Check if it's a self-signed neoserver token
		payload, err := decodeJWTPayload(parts[1])
		if err == nil && payload["iss"] == "neoserver" {
			return nil, nil // Let JWT validator handle it
		}
	}

	return v.Validate(ctx, token)
}

// Validate validates an OIDC token and returns the identity.
func (v *OIDCValidator) Validate(ctx context.Context, tokenString string) (*Identity, error) {
	return v.validateToken(ctx, tokenString, true)
}

// ValidateIDToken authenticates the browser login. ID tokens need not carry
// access-token scopes; API bearer requests must use Validate instead.
func (v *OIDCValidator) ValidateIDToken(ctx context.Context, tokenString string) (*Identity, error) {
	return v.validateToken(ctx, tokenString, false)
}

func (v *OIDCValidator) validateToken(ctx context.Context, tokenString string, requireScopes bool) (*Identity, error) {
	// Verify the token
	token, err := v.verifier.Verify(ctx, tokenString)
	if err != nil {
		return nil, &AuthError{Message: "Token verification failed"}
	}

	// Extract claims
	var claims map[string]interface{}
	if err := token.Claims(&claims); err != nil {
		return nil, &AuthError{Message: "Failed to parse claims"}
	}

	// Validate required scopes if configured
	if requireScopes && len(v.config.RequiredScopes) > 0 {
		if err := v.validateScopes(claims); err != nil {
			return nil, &AuthError{Message: err.Error()}
		}
	}

	// Extract user info
	subject, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	displayName, _ := claims["name"].(string)
	if subject == "" {
		return nil, &AuthError{Message: "Token subject is missing"}
	}

	// Extract claims that can be mapped to roles
	claimsMap := extractMappableClaims(claims, v.config.GroupClaims)
	if _, hasOverage := claims["_claim_names"]; hasOverage && len(claimsMap["groups"]) == 0 {
		return nil, &AuthError{Message: "Token group claims require provider overage resolution"}
	}

	// Resolve claims to roles using the store
	roles, err := v.store.ResolveClaimsToRoles(ctx, claimsMap)
	if err != nil {
		return nil, &AuthError{Message: "Failed to resolve roles"}
	}

	return &Identity{
		Subject:     subject,
		Email:       email,
		DisplayName: displayName,
		AuthMethod:  AuthMethodOIDC,
		Roles:       roles,
		Claims:      safePresentedClaims(claims, v.config.GroupClaims),
	}, nil
}

// validateScopes checks if the token has the required scopes.
func (v *OIDCValidator) validateScopes(claims map[string]interface{}) error {
	var tokenScopes []string
	presented := false

	// Try different scope claim names
	if scopeStr, ok := claims["scope"].(string); ok {
		presented = true
		tokenScopes = strings.Fields(scopeStr)
	} else if scopeArr, ok := claims["scopes"].([]interface{}); ok {
		presented = true
		for _, s := range scopeArr {
			if str, ok := s.(string); ok {
				tokenScopes = append(tokenScopes, str)
			}
		}
	} else if scopeArr, ok := claims["scp"].([]interface{}); ok {
		presented = true
		for _, s := range scopeArr {
			if str, ok := s.(string); ok {
				tokenScopes = append(tokenScopes, str)
			}
		}
	} else if scopeStr, ok := claims["scp"].(string); ok {
		presented = true
		tokenScopes = strings.Fields(scopeStr)
	}
	if !presented {
		if len(v.config.RequiredScopes) > 0 {
			return fmt.Errorf("required scope claim is missing or malformed")
		}
		return nil
	}

	scopeSet := make(map[string]bool)
	for _, s := range tokenScopes {
		scopeSet[s] = true
	}

	for _, required := range v.config.RequiredScopes {
		if !scopeSet[required] {
			return fmt.Errorf("missing required scope: %s", required)
		}
	}

	return nil
}

// extractMappableClaims extracts claims that can be mapped to roles.
func extractMappableClaims(claims map[string]interface{}, configured ...[]string) map[string][]string {
	result := make(map[string][]string)

	var claimNames []string
	if len(configured) > 0 {
		claimNames = configured[0]
	}
	if len(claimNames) == 0 {
		claimNames = []string{"groups", "roles", "group", "role", "realm_access.roles", "cognito:groups"}
	}

	for _, name := range claimNames {
		values := extractClaimValues(claims, name)
		if len(values) > 0 {
			result[name] = values
		}
	}

	return result
}

// extractClaimValues extracts values from a claim, handling nested paths.
func extractClaimValues(claims map[string]interface{}, path string) []string {
	if exact, ok := claims[path]; ok {
		return extractStringSlice(exact)
	}
	parts := strings.Split(path, ".")
	current := claims

	for i, part := range parts {
		if i == len(parts)-1 {
			// Last part - extract values
			return extractStringSlice(current[part])
		}

		// Navigate to nested object
		nested, ok := current[part].(map[string]interface{})
		if !ok {
			return nil
		}
		current = nested
	}

	return nil
}

func safePresentedClaims(claims map[string]interface{}, groupClaims []string) map[string]interface{} {
	safe := make(map[string]interface{})
	for _, name := range []string{"iss", "sub", "email", "name", "exp"} {
		if value, ok := claims[name]; ok {
			safe[name] = value
		}
	}
	for name, values := range extractMappableClaims(claims, groupClaims) {
		safe[name] = values
	}
	return safe
}

// extractStringSlice converts a claim value to a string slice.
func extractStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case string:
		return []string{val}
	case []string:
		return val
	case []interface{}:
		var result []string
		for _, item := range val {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

// decodeJWTPayload decodes the payload portion of a JWT without verification.
func decodeJWTPayload(payload string) (map[string]interface{}, error) {
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, err
	}

	return claims, nil
}
