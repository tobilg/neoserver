package store

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// jwtHeader represents the JWT header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// jwtPayload represents the JWT payload for self-signed tokens.
type jwtPayload struct {
	Iss  string `json:"iss"`            // Issuer (always "neoserver" for self-signed)
	Sub  string `json:"sub"`            // Subject (user identifier)
	Role string `json:"role,omitempty"` // Role for self-signed tokens
	Iat  int64  `json:"iat"`            // Issued at
	Exp  int64  `json:"exp"`            // Expiration time
}

// jwtToken represents a complete JWT.
type jwtToken struct {
	Header  jwtHeader
	Payload jwtPayload
	Raw     string
}

// Sign signs the token with the given ECDSA private key.
func (t *jwtToken) Sign(privateKey *ecdsa.PrivateKey) (string, error) {
	headerBytes, err := json.Marshal(t.Header)
	if err != nil {
		return "", fmt.Errorf("failed to marshal header: %w", err)
	}

	payloadBytes, err := json.Marshal(t.Payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	message := headerB64 + "." + payloadB64
	hash := sha256.Sum256([]byte(message))

	r, s, err := ecdsa.Sign(rand.Reader, privateKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign: %w", err)
	}

	// Create signature in JWS format (R || S, each 32 bytes for P-256)
	signature := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(signature[32-len(rBytes):32], rBytes)
	copy(signature[64-len(sBytes):64], sBytes)

	signatureB64 := base64.RawURLEncoding.EncodeToString(signature)

	return message + "." + signatureB64, nil
}

// Verify verifies the token signature with the given ECDSA public key.
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
		return fmt.Errorf("failed to decode signature: %w", err)
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

// parseJWT parses a JWT string into a token struct.
func parseJWT(tokenString string) (*jwtToken, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode header: %w", err)
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode payload: %w", err)
	}

	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("failed to unmarshal header: %w", err)
	}

	var payload jwtPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	return &jwtToken{
		Header:  header,
		Payload: payload,
		Raw:     tokenString,
	}, nil
}
