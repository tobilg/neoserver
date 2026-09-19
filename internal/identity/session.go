package identity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/protocolrequest"
	"github.com/tobilg/neoserver/internal/store"
)

const (
	SessionCookieName = "neosrv_session"
	CSRFCookieName    = "neosrv_csrf"
)

type SessionConfig struct {
	IdleTimeout time.Duration
	DefaultRole string
}

type SessionValidator struct {
	catalog     store.Store
	sessions    store.BrowserSessionStore
	config      SessionConfig
	staticHash  string
	basicHashes map[string]string
	groupClaims []string
}

func NewSessionValidator(catalog store.Store, sessions store.BrowserSessionStore, cfg SessionConfig, staticKey string, users map[string]string, oidcConfig *OIDCConfig) *SessionValidator {
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = "super_admin"
	}
	validator := &SessionValidator{catalog: catalog, sessions: sessions, config: cfg, basicHashes: make(map[string]string)}
	if staticKey != "" {
		validator.staticHash = CredentialFingerprint("static", staticKey)
	}
	for username, password := range users {
		validator.basicHashes[username] = CredentialFingerprint(username, password)
	}
	if oidcConfig != nil {
		validator.groupClaims = append([]string(nil), oidcConfig.GroupClaims...)
	}
	return validator
}

func HashSessionToken(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

func CredentialFingerprint(principal, secret string) string {
	return HashSessionToken(principal + "\x00" + secret)
}

func (v *SessionValidator) ValidateRequest(ctx context.Context, request *http.Request) (*Identity, error) {
	cookie, err := request.Cookie(SessionCookieName)
	if err == http.ErrNoCookie {
		return nil, nil
	}
	if err != nil {
		return nil, &AuthError{Message: "Invalid session cookie"}
	}
	if strings.TrimSpace(cookie.Value) == "" {
		return nil, nil
	}
	session, err := v.sessions.GetBrowserSessionByTokenHash(ctx, HashSessionToken(cookie.Value))
	if err != nil {
		return nil, &AuthError{Message: "Invalid session"}
	}
	now := time.Now().UTC()
	if session.RevokedAt != nil || !now.Before(session.ExpiresAt) || !now.Before(session.IdleExpiresAt) {
		return nil, &AuthError{Message: "Session expired"}
	}
	previousToken := session.TokenHash != HashSessionToken(cookie.Value)
	grace := session.PreviousValidUntil != nil && now.Before(*session.PreviousValidUntil)
	if previousToken && !grace {
		return nil, &AuthError{Message: "Session expired"}
	}
	if isUnsafeMethod(request.Method) || protocolrequest.Get(request).Mutating(request.Method) {
		csrf := request.Header.Get("X-CSRF-Token")
		csrfHash := HashSessionToken(csrf)
		validCSRF := subtle.ConstantTimeCompare([]byte(csrfHash), []byte(session.CSRFHash)) == 1
		if grace {
			validCSRF = validCSRF || subtle.ConstantTimeCompare([]byte(csrfHash), []byte(session.PreviousCSRFHash)) == 1
		}
		if csrf == "" || !validCSRF {
			return nil, &AuthError{Message: "CSRF validation failed", StatusCode: http.StatusForbidden}
		}
	}
	roles := cloneRoles(session.Roles)
	switch AuthMethod(session.AuthMethod) {
	case AuthMethodAPIKey:
		lookup, ok := v.catalog.(interface {
			GetAPIKeyByID(context.Context, string) (*store.APIKey, error)
		})
		if !ok {
			return nil, &AuthError{Message: "Session credential lookup unavailable"}
		}
		key, err := lookup.GetAPIKeyByID(ctx, session.CredentialID)
		if err != nil || key == nil || key.Revoked || (key.ExpiresAt != nil && !now.Before(*key.ExpiresAt)) {
			return nil, &AuthError{Message: "Session credential was revoked"}
		}
		roles = make(map[string]string)
		if key.WorkspaceID == nil {
			roles["*"] = key.RoleID
		} else {
			roles[*key.WorkspaceID] = key.RoleID
		}
	case AuthMethodOIDC:
		mappingClaims := extractMappableClaims(session.Claims, v.groupClaims)
		roles, err = v.catalog.ResolveClaimsToRoles(ctx, mappingClaims)
		if err != nil {
			return nil, &AuthError{Message: "Failed to resolve session roles"}
		}
	case AuthMethodJWT:
		key, keyErr := v.catalog.GetActiveSigningKey(ctx)
		if keyErr != nil || (session.CredentialID != "" && key.ID != session.CredentialID) {
			return nil, &AuthError{Message: "Session signing credential was rotated"}
		}
	case AuthMethodBasic, AuthMethod("password"):
		if v.basicHashes[session.Subject] == "" || subtle.ConstantTimeCompare([]byte(v.basicHashes[session.Subject]), []byte(session.CredentialFingerprint)) != 1 {
			return nil, &AuthError{Message: "Session credential changed"}
		}
		roles = map[string]string{"*": v.config.DefaultRole}
	case AuthMethod("static_apikey"):
		if v.staticHash == "" || subtle.ConstantTimeCompare([]byte(v.staticHash), []byte(session.CredentialFingerprint)) != 1 {
			return nil, &AuthError{Message: "Session credential changed"}
		}
		roles = map[string]string{"*": v.config.DefaultRole}
	default:
		return nil, &AuthError{Message: fmt.Sprintf("Unsupported session authentication method %q", session.AuthMethod)}
	}
	// Passive reads and polling do not represent operator activity. Only the
	// explicit, CSRF-protected session refresh endpoint extends idle expiry.
	apiKeyID := ""
	if AuthMethod(session.AuthMethod) == AuthMethodAPIKey {
		apiKeyID = session.CredentialID
	}
	return &Identity{Subject: session.Subject, Email: session.Email, DisplayName: session.DisplayName,
		SessionTokenHash: HashSessionToken(cookie.Value), SessionPreviousToken: previousToken, SessionRotationUntil: session.PreviousValidUntil,
		AuthMethod: AuthMethod(session.AuthMethod), Roles: roles, Claims: session.Claims, APIKeyID: apiKeyID,
		SessionID: session.ID, SessionExpiresAt: session.ExpiresAt, SessionIdleExpiresAt: session.IdleExpiresAt}, nil
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions && method != http.MethodTrace
}

func cloneRoles(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
