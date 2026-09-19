package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tobilg/neoserver/internal/store"
)

// RecoveringOIDCValidator retries discovery on readiness probes and bearer
// requests. It owns no background goroutine; idle/shutdown servers cannot leak
// retries. Single-flight initialization and backoff bound provider load.
type RecoveringOIDCValidator struct {
	store       store.Store
	config      OIDCConfig
	gate        chan struct{}
	mu          sync.Mutex
	validator   *OIDCValidator
	nextAttempt time.Time
	retryDelay  time.Duration
}

func NewRecoveringOIDCValidator(s store.Store, cfg OIDCConfig) *RecoveringOIDCValidator {
	return &RecoveringOIDCValidator{store: s, config: cfg, gate: make(chan struct{}, 1), retryDelay: time.Second}
}

func (v *RecoveringOIDCValidator) get(ctx context.Context) (*OIDCValidator, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case v.gate <- struct{}{}:
		defer func() { <-v.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.validator != nil {
		return v.validator, nil
	}
	if time.Now().Before(v.nextAttempt) {
		return nil, errors.New("OIDC provider discovery is unavailable; retry pending")
	}
	validator, err := NewOIDCValidator(ctx, v.store, v.config)
	if err != nil {
		v.nextAttempt = time.Now().Add(v.retryDelay)
		v.retryDelay = min(30*time.Second, v.retryDelay*2)
		return nil, errors.New("OIDC provider discovery is unavailable; retry pending")
	}
	v.validator = validator
	return validator, nil
}

func (v *RecoveringOIDCValidator) Health(ctx context.Context) error { _, err := v.get(ctx); return err }

func (v *RecoveringOIDCValidator) ValidateRequest(ctx context.Context, r *http.Request) (*Identity, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || strings.HasPrefix(strings.TrimPrefix(auth, "Bearer "), "nsk_") {
		return nil, nil
	}
	parts := strings.Split(strings.TrimPrefix(auth, "Bearer "), ".")
	if len(parts) == 3 {
		if payload, err := decodeJWTPayload(parts[1]); err == nil && payload["iss"] == "neoserver" {
			return nil, nil
		}
	}
	validator, err := v.get(ctx)
	if err != nil {
		return nil, &AuthError{Message: "OIDC authentication temporarily unavailable", StatusCode: http.StatusServiceUnavailable}
	}
	return validator.ValidateRequest(ctx, r)
}
