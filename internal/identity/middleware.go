package identity

import (
	"context"
	"github.com/tobilg/neoserver/internal/httputil"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/protocolrequest"
	"github.com/tobilg/neoserver/internal/store"
)

// Validator is the interface for identity validators.
type Validator interface {
	ValidateRequest(ctx context.Context, r *http.Request) (*Identity, error)
}

// MiddlewareConfig configures the identity middleware.
type MiddlewareConfig struct {
	Store         store.Store
	Logger        *slog.Logger
	OIDCConfig    *OIDCConfig              // nil to disable OIDC
	OIDCValidator *RecoveringOIDCValidator // shared with readiness when server-owned
	RequireAuth   bool                     // If true, reject unauthenticated requests

	// StaticAPIKey, if non-empty, enables a server-level static API key validator.
	StaticAPIKey string
	// BasicAuthUsers, if non-empty, enables an HTTP Basic auth validator.
	BasicAuthUsers map[string]string
	// DefaultRole is the global role granted to static-apikey / basic-auth principals.
	DefaultRole string
	// AllowAPIKeyInQuery permits API keys via the ?apikey= query parameter.
	AllowAPIKeyInQuery bool
	Session            *SessionConfig
}

// Middleware creates HTTP middleware that extracts identity from the request.
// It tries validators in order: API key, self-signed JWT, OIDC.
// The first successful validation wins.
func Middleware(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	var validators []Validator
	var sessionValidator *SessionValidator

	// API key validator
	apiKeyValidator := NewAPIKeyValidator(cfg.Store)
	apiKeyValidator.allowQueryParam = cfg.AllowAPIKeyInQuery
	validators = append(validators, apiKeyValidator)

	// Static API key validator (server-configured, optional)
	if cfg.StaticAPIKey != "" {
		staticValidator := NewStaticAPIKeyValidator(cfg.StaticAPIKey, cfg.DefaultRole)
		staticValidator.allowQueryParam = cfg.AllowAPIKeyInQuery
		validators = append(validators, staticValidator)
	}

	// Basic auth validator (server-configured, optional)
	if len(cfg.BasicAuthUsers) > 0 {
		validators = append(validators, NewBasicAuthValidator(cfg.BasicAuthUsers, cfg.DefaultRole))
	}

	// Self-signed JWT validator
	validators = append(validators, NewJWTValidator(cfg.Store))

	// OIDC validator (if configured)
	if cfg.OIDCConfig != nil && cfg.OIDCConfig.IssuerURL != "" {
		if cfg.OIDCConfig.SkipIssuerCheck && cfg.Logger != nil {
			cfg.Logger.Warn("OIDC issuer validation is disabled (Auth.OIDC.SkipIssuerCheck=true); " +
				"any token with a valid signature from the configured provider keys will be accepted regardless of issuer")
		}
		oidcValidator := cfg.OIDCValidator
		if oidcValidator == nil {
			oidcValidator = NewRecoveringOIDCValidator(cfg.Store, *cfg.OIDCConfig)
		}
		validators = append(validators, oidcValidator)
	}
	if cfg.Session != nil {
		if sessions, ok := cfg.Store.(store.BrowserSessionStore); ok {
			sessionConfig := *cfg.Session
			sessionConfig.DefaultRole = cfg.DefaultRole
			sessionValidator = NewSessionValidator(cfg.Store, sessions, sessionConfig, cfg.StaticAPIKey, cfg.BasicAuthUsers, cfg.OIDCConfig)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = protocolrequest.Prepare(r, "")
			// Protect role-dependent responses even when an individual protocol
			// cache is disabled. Public anonymous representations may still cache.
			w.Header().Add("Vary", "Authorization, X-API-Key, Cookie")
			if hasExplicitCredential(r, cfg.AllowAPIKeyInQuery) || r.Header.Get("Cookie") != "" {
				w.Header().Set("Cache-Control", "private, no-store")
			}
			ctx := r.Context()
			var identity *Identity
			var lastErr error

			// Explicit credentials always win over cookies. A rejected explicit
			// credential must never fall back to an existing browser session.
			for _, v := range validators {
				id, err := v.ValidateRequest(ctx, r)
				if err != nil {
					lastErr = err
					// Log auth errors but continue trying other validators
					if cfg.Logger != nil {
						cfg.Logger.Debug("validator returned error", "error", err)
					}
					continue
				}
				if id != nil {
					identity = id
					break
				}
			}
			if identity == nil && lastErr == nil && sessionValidator != nil && !hasExplicitCredential(r, cfg.AllowAPIKeyInQuery) && !isSessionExemptPath(r.URL.Path) {
				id, err := sessionValidator.ValidateRequest(ctx, r)
				if err != nil {
					lastErr = err
				} else {
					identity = id
				}
			}

			// A credential was presented but rejected by a validator (bad/expired
			// API key, unverifiable bearer token). Fail closed with 401 even when
			// RequireAuth is false: presenting an invalid credential is an error,
			// not an anonymous request, and must not silently downgrade to
			// unauthenticated access on otherwise-public endpoints.
			if identity == nil && (lastErr != nil || hasExplicitCredential(r, cfg.AllowAPIKeyInQuery)) {
				if authErr, ok := lastErr.(*AuthError); ok {
					status := authErr.StatusCode
					if status == 0 {
						status = http.StatusUnauthorized
					}
					httputil.HTTPError(w, r, authErr.Message, status)
					return
				}
				httputil.HTTPError(w, r, "credentials were not accepted", http.StatusUnauthorized)
				return
			}

			// If auth is required and we have no identity, reject
			if cfg.RequireAuth && identity == nil {
				httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
				return
			}

			// Add identity to context (may be nil for unauthenticated requests)
			if identity != nil {
				w.Header().Set("Cache-Control", "private, no-store")
				ctx = WithIdentity(ctx, identity)
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func hasExplicitCredential(r *http.Request, allowQuery bool) bool {
	if r.Header.Get("Authorization") != "" || r.Header.Get("X-API-Key") != "" {
		return true
	}
	return allowQuery && r.URL.Query().Get("apikey") != ""
}

// isSessionExemptPath lists the anonymous bootstrap routes that must work with
// a stale browser cookie: signing in again, and the console configuration the
// sign-in page reads to offer password and OIDC sign-in.
func isSessionExemptPath(path string) bool {
	return strings.HasSuffix(path, "/auth/login") || strings.HasSuffix(path, "/api/v1/console/config")
}

// RequireIdentity is middleware that ensures an identity is present in the context.
func RequireIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := FromContext(r.Context())
		if !ok {
			httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole is middleware that ensures the identity has a specific role for a workspace.
func RequireRole(workspaceID, role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := FromContext(r.Context())
			if !ok {
				httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
				return
			}

			if !identity.HasRole(workspaceID, role) {
				httputil.HTTPError(w, r, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireSuperAdmin is middleware that ensures the identity is a super admin.
func RequireSuperAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := FromContext(r.Context())
		if !ok {
			httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
			return
		}

		if !identity.IsSuperAdmin() {
			httputil.HTTPError(w, r, "forbidden: super admin required", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireWorkspaceAccess is middleware that ensures the identity has access to the workspace.
// It checks if the identity has any role for the given workspace or is a super admin.
func RequireWorkspaceAccess(workspaceIDParam string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := FromContext(r.Context())
			if !ok {
				httputil.HTTPError(w, r, "authentication required", http.StatusUnauthorized)
				return
			}

			// Super admins have access to all workspaces
			if identity.IsSuperAdmin() {
				next.ServeHTTP(w, r)
				return
			}

			// Get workspace ID from URL parameter
			workspaceID := r.PathValue(workspaceIDParam)
			if workspaceID == "" {
				// Try chi URL params
				workspaceID = chi.URLParam(r, workspaceIDParam)
			}

			if workspaceID == "" {
				httputil.HTTPError(w, r, "workspace not specified", http.StatusBadRequest)
				return
			}

			// Check if identity has any role for this workspace
			if !identity.HasWorkspaceAccess(workspaceID) {
				httputil.HTTPError(w, r, "forbidden: no access to workspace", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
