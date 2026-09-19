package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

// JWTValidator validates self-signed JWT tokens.
type JWTValidator struct {
	store store.Store
}

// NewJWTValidator creates a new JWT validator.
func NewJWTValidator(s store.Store) *JWTValidator {
	return &JWTValidator{store: s}
}

// ValidateRequest extracts and validates a JWT from the request.
func (v *JWTValidator) ValidateRequest(ctx context.Context, r *http.Request) (*Identity, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		return nil, nil // No Bearer token
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	if token == "" {
		return nil, nil
	}

	// Check if it's an API key (starts with nsk_)
	if strings.HasPrefix(token, "nsk_") {
		return nil, nil // Not a JWT, let API key validator handle it
	}

	return v.Validate(ctx, token)
}

// Validate validates a self-signed JWT token.
func (v *JWTValidator) Validate(ctx context.Context, tokenString string) (*Identity, error) {
	// Parse the token
	token, err := parseJWT(tokenString)
	if err != nil {
		return nil, &AuthError{Message: "Invalid token format"}
	}

	// Check issuer - only validate tokens issued by neoserver
	if token.Payload.Iss != "neoserver" {
		return nil, nil // Not a self-signed token, let OIDC handle it
	}

	// Get the signing key from store
	signingKey, err := v.store.GetActiveSigningKey(ctx)
	if err != nil {
		return nil, &AuthError{Message: "Failed to get signing key"}
	}

	// Parse public key
	publicKey, err := x509.ParsePKIXPublicKey(signingKey.PublicKey)
	if err != nil {
		return nil, &AuthError{Message: "Failed to parse signing key"}
	}

	ecdsaKey, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, &AuthError{Message: "Invalid signing key type"}
	}

	// Verify signature
	if err := token.Verify(ecdsaKey); err != nil {
		return nil, &AuthError{Message: "Invalid token signature"}
	}

	// Check expiration
	if time.Now().Unix() > token.Payload.Exp {
		return nil, &AuthError{Message: "Token expired"}
	}

	// Build identity
	roles := make(map[string]string)
	if token.Payload.Role != "" {
		// Self-signed tokens have a direct role claim
		if token.Payload.Role == "super_admin" {
			roles["*"] = "super_admin"
		} else {
			// For workspace-scoped tokens, the workspace would be in claims
			// For now, treat non-super_admin roles as global
			roles["*"] = token.Payload.Role
		}
	}

	return &Identity{
		Subject:    token.Payload.Sub,
		AuthMethod: AuthMethodJWT,
		Roles:      roles,
		Claims: map[string]interface{}{
			"iss":  token.Payload.Iss,
			"sub":  token.Payload.Sub,
			"role": token.Payload.Role,
			"iat":  token.Payload.Iat,
			"exp":  token.Payload.Exp,
		},
	}, nil
}

// JWT token structures

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtPayload struct {
	Iss  string `json:"iss"`
	Sub  string `json:"sub"`
	Role string `json:"role,omitempty"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

type jwtToken struct {
	Header  jwtHeader
	Payload jwtPayload
	Raw     string
}

func parseJWT(tokenString string) (*jwtToken, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("failed to decode header")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("failed to decode payload")
	}

	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, errors.New("failed to parse header")
	}

	var payload jwtPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, errors.New("failed to parse payload")
	}

	return &jwtToken{
		Header:  header,
		Payload: payload,
		Raw:     tokenString,
	}, nil
}

func (t *jwtToken) Verify(publicKey *ecdsa.PublicKey) error {
	parts := strings.Split(t.Raw, ".")
	if len(parts) != 3 {
		return errors.New("invalid token format")
	}

	// Pin the algorithm to ES256. This rejects "alg: none" and any attempt to
	// present a token with an unexpected algorithm header.
	if t.Header.Alg != "ES256" {
		return errors.New("unexpected JWT algorithm")
	}

	message := parts[0] + "." + parts[1]
	hash := sha256.Sum256([]byte(message))

	signatureBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return errors.New("failed to decode signature")
	}

	if len(signatureBytes) != 64 {
		return errors.New("invalid signature length")
	}

	r := new(big.Int).SetBytes(signatureBytes[:32])
	s := new(big.Int).SetBytes(signatureBytes[32:])

	if !ecdsa.Verify(publicKey, hash[:], r, s) {
		return errors.New("invalid signature")
	}

	return nil
}
