package mgmt

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/store"
)

type loginRequest struct {
	Method    string `json:"method,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Token     string `json:"token,omitempty"`
	OIDCToken string `json:"oidc_token,omitempty"`
	IDToken   string `json:"id_token,omitempty"`
}

var serverStartedAt = time.Now().UTC()

type loginAttempt struct {
	failures     int
	blockedUntil time.Time
	updatedAt    time.Time
}

type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{attempts: make(map[string]loginAttempt)}
}

func (limiter *loginRateLimiter) allowed(key string, now time.Time) time.Duration {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.prune(now)
	attempt := limiter.attempts[key]
	if attempt.blockedUntil.After(now) {
		return attempt.blockedUntil.Sub(now)
	}
	return 0
}

func (limiter *loginRateLimiter) fail(key string, now time.Time) time.Duration {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.prune(now)
	attempt := limiter.attempts[key]
	attempt.failures++
	attempt.updatedAt = now
	if attempt.failures >= 5 {
		shift := attempt.failures - 5
		if shift > 5 {
			shift = 5
		}
		delay := 30 * time.Second * time.Duration(1<<shift)
		if delay > 15*time.Minute {
			delay = 15 * time.Minute
		}
		attempt.blockedUntil = now.Add(delay)
	}
	limiter.attempts[key] = attempt
	if attempt.blockedUntil.After(now) {
		return attempt.blockedUntil.Sub(now)
	}
	return 0
}

func (limiter *loginRateLimiter) success(key string) {
	limiter.mu.Lock()
	delete(limiter.attempts, key)
	limiter.mu.Unlock()
}

func (limiter *loginRateLimiter) prune(now time.Time) {
	if len(limiter.attempts) < 1024 {
		return
	}
	for key, attempt := range limiter.attempts {
		if !attempt.blockedUntil.After(now) && now.Sub(attempt.updatedAt) > time.Hour {
			delete(limiter.attempts, key)
		}
	}
}

func (h *handler) consoleConfig(w http.ResponseWriter, r *http.Request) {
	passwordLogin := h.cfg.Auth.Enabled && h.cfg.Auth.Method == "basic" && len(h.cfg.Auth.Users) > 0
	oidcEnabled := h.cfg.Auth.OIDC.BrowserLoginEnabled && h.cfg.Auth.OIDC.IssuerURL != "" && h.cfg.Auth.OIDC.ClientID != ""
	basePath := strings.TrimSuffix(h.cfg.Server.BasePath, "/")
	publicOrigin := strings.TrimSuffix(h.cfg.Server.UrlBase, "/")
	if publicOrigin == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
			scheme = forwarded
		}
		publicOrigin = scheme + "://" + r.Host
	}
	redirectURI := publicOrigin + basePath + "/admin/auth/callback"
	scopes := browserScopes(h.cfg.Auth.OIDC.BrowserScopes, h.cfg.Auth.OIDC.RequiredScopes)
	writeJSON(w, http.StatusOK, map[string]any{
		"version": conf.App.Version, "commit": conf.App.Commit, "title": h.cfg.Metadata.Title, "base_path": basePath, "url_base": h.cfg.Server.UrlBase,
		"server_started_at": serverStartedAt,
		"auth": map[string]any{"enabled": h.cfg.Auth.Enabled, "method": h.cfg.Auth.Method, "password_login": passwordLogin,
			"token_login": true, "oidc": map[string]any{"enabled": oidcEnabled, "issuer": h.cfg.Auth.OIDC.IssuerURL,
				"client_id": h.cfg.Auth.OIDC.ClientID, "scopes": scopes, "redirect_uri": redirectURI,
				"group_claims": h.cfg.Auth.OIDC.GroupClaims}},
		"services": map[string]bool{"ogcapi": true, "wms": h.cfg.WMS.Enabled, "wfs": h.cfg.WFS.Enabled,
			"wcs": h.cfg.WCS.Enabled, "wmts": h.cfg.WMTS.Enabled, "tiles": h.cfg.Tiles.Enabled},
		"features": map[string]bool{"imports": h.cfg.Importer.Enabled, "audit": h.cfg.Audit.Enabled,
			"persistent_tile_cache": h.cfg.PersistentCache.Enabled, "mosaic": h.cfg.MosaicCatalog.Enabled,
			"pprof": h.cfg.Observability.Pprof.Enabled},
		"basemap_url": h.cfg.Website.BasemapURL, "allowed_paths": h.cfg.Datasource.AllowedPaths,
		"limits": map[string]any{"upload_bytes": h.cfg.Importer.MaxUploadBytes, "source_bytes": h.cfg.Importer.MaxSourceBytes,
			"layers": h.cfg.Importer.MaxLayers, "features": h.cfg.Importer.MaxFeatures, "page_size": h.cfg.Paging.LimitMax},
	})
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid login request")
		return
	}
	request.normalize()
	switch request.Method {
	case "password":
		if !h.cfg.Auth.Enabled || h.cfg.Auth.Method != "basic" || len(h.cfg.Auth.Users) == 0 {
			writeError(w, http.StatusForbidden, "This sign-in method is disabled", "login_disabled")
			return
		}
	case "oidc":
		if !h.cfg.Auth.OIDC.BrowserLoginEnabled || h.cfg.Auth.OIDC.IssuerURL == "" || h.cfg.Auth.OIDC.ClientID == "" {
			writeError(w, http.StatusForbidden, "This sign-in method is disabled", "login_disabled")
			return
		}
	case "token":
	default:
		writeError(w, http.StatusBadRequest, "Bad Request", "method must be password, token, or oidc")
		return
	}
	principal := request.Username
	if principal == "" {
		principal = request.APIKey
	}
	if principal == "" {
		principal = request.Token + request.OIDCToken + request.IDToken
	}
	rateKeys := []string{"ip:" + clientAddress(r), "principal:" + identity.HashSessionToken(principal)}
	now := time.Now().UTC()
	wait := time.Duration(0)
	for _, key := range rateKeys {
		if delay := h.loginRate.allowed(key, now); delay > wait {
			wait = delay
		}
	}
	if wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "Too Many Requests", "login temporarily rate limited")
		return
	}
	principalIdentity, credentialKind, fingerprint, err := h.authenticateLogin(r.Context(), request)
	if err != nil || principalIdentity == nil {
		wait := time.Duration(0)
		for _, key := range rateKeys {
			if delay := h.loginRate.fail(key, now); delay > wait {
				wait = delay
			}
		}
		if wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		}
		writeError(w, http.StatusUnauthorized, "Authentication failed", "invalid credentials")
		return
	}
	for _, key := range rateKeys {
		h.loginRate.success(key)
	}
	sessions, ok := h.store.(store.BrowserSessionStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Sessions unavailable", "browser session storage is not configured")
		return
	}
	token, csrf, err := newSessionSecrets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to create session")
		return
	}
	expiresAt := now.Add(time.Duration(h.cfg.Auth.Session.TTLSec) * time.Second)
	if claimExpiry := claimExpiration(principalIdentity.Claims); !claimExpiry.IsZero() && claimExpiry.Before(expiresAt) {
		expiresAt = claimExpiry
	}
	credentialID := principalIdentity.APIKeyID
	if credentialKind == string(identity.AuthMethodJWT) {
		if key, keyErr := h.store.GetActiveSigningKey(r.Context()); keyErr == nil {
			credentialID = key.ID
		}
	}
	idleExpiry := now.Add(time.Duration(h.cfg.Auth.Session.IdleTimeoutSec) * time.Second)
	if idleExpiry.After(expiresAt) {
		idleExpiry = expiresAt
	}
	session, err := sessions.CreateBrowserSession(r.Context(), store.BrowserSession{
		TokenHash: identity.HashSessionToken(token), CSRFHash: identity.HashSessionToken(csrf), Subject: principalIdentity.Subject,
		Email: principalIdentity.Email, DisplayName: principalIdentity.DisplayName, AuthMethod: credentialKind,
		GlobalRole: principalIdentity.Roles["*"], Roles: principalIdentity.Roles, Claims: principalIdentity.Claims,
		CredentialID: credentialID, CredentialFingerprint: fingerprint, RemoteAddr: clientAddress(r), UserAgent: r.UserAgent(),
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: idleExpiry, ExpiresAt: expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to persist session")
		return
	}
	h.setSessionCookies(w, r, token, csrf, expiresAt)
	identity.RecordAuditPrincipal(r.Context(), principalIdentity)
	principalIdentity.SessionID, principalIdentity.SessionExpiresAt, principalIdentity.SessionIdleExpiresAt = session.ID, expiresAt, idleExpiry
	writeJSON(w, http.StatusCreated, h.meResponse(r.Context(), principalIdentity))
}

func (request *loginRequest) normalize() {
	if request.Method == "" {
		switch {
		case request.Username != "" || request.Password != "":
			request.Method = "password"
		case request.OIDCToken != "":
			request.Method, request.IDToken = "oidc", request.OIDCToken
		default:
			request.Method = "token"
			if request.Token == "" {
				request.Token = request.APIKey
			}
		}
	}
}

func (h *handler) authenticateLogin(ctx context.Context, request loginRequest) (*identity.Identity, string, string, error) {
	if request.Method == "password" {
		fake, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://session.local", nil)
		fake.SetBasicAuth(request.Username, request.Password)
		result, err := identity.NewBasicAuthValidator(h.cfg.Auth.Users, h.cfg.Auth.DefaultRole).ValidateRequest(ctx, fake)
		return result, "password", identity.CredentialFingerprint(request.Username, request.Password), err
	}
	if request.Method == "token" {
		if strings.HasPrefix(request.Token, "nsk_") {
			result, err := identity.NewAPIKeyValidator(h.store).Validate(ctx, request.Token)
			return result, string(identity.AuthMethodAPIKey), "", err
		}
		if h.cfg.Auth.Enabled && h.cfg.Auth.Method == "apikey" && h.cfg.Auth.ApiKey != "" {
			fake, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://session.local", nil)
			fake.Header.Set("X-API-Key", request.Token)
			if result, err := identity.NewStaticAPIKeyValidator(h.cfg.Auth.ApiKey, h.cfg.Auth.DefaultRole).ValidateRequest(ctx, fake); err == nil && result != nil {
				return result, "static_apikey", identity.CredentialFingerprint("static", request.Token), nil
			}
		}
		if request.Token == "" {
			return nil, "", "", errors.New("credential is required")
		}
		result, err := identity.NewJWTValidator(h.store).Validate(ctx, request.Token)
		return result, string(identity.AuthMethodJWT), "", err
	}
	token := request.IDToken
	if token == "" {
		return nil, "", "", errors.New("id_token is required")
	}
	validator, err := identity.NewOIDCValidator(ctx, h.store, identity.OIDCConfig{IssuerURL: h.cfg.Auth.OIDC.IssuerURL,
		ClientID: h.cfg.Auth.OIDC.ClientID, RequiredScopes: h.cfg.Auth.OIDC.RequiredScopes,
		SkipIssuerCheck: h.cfg.Auth.OIDC.SkipIssuerCheck, GroupClaims: h.cfg.Auth.OIDC.GroupClaims})
	if err != nil {
		return nil, "", "", err
	}
	result, err := validator.ValidateIDToken(ctx, token)
	return result, string(identity.AuthMethodOIDC), "", err
}

func (h *handler) authMe(w http.ResponseWriter, r *http.Request) {
	principal, _ := identity.FromContext(r.Context())
	writeJSON(w, http.StatusOK, h.meResponse(r.Context(), principal))
}

func (h *handler) meResponse(ctx context.Context, principal *identity.Identity) map[string]any {
	workspaceValues := make([]map[string]any, 0)
	workspaces, _ := h.store.ListWorkspaces(ctx)
	globalAdmin := principal.Roles["*"] == "admin"
	for _, workspace := range workspaces {
		role := principal.GetWorkspaceRole(workspace.ID)
		if role == "" {
			continue
		}
		workspaceValues = append(workspaceValues, map[string]any{
			"id": workspace.ID, "name": workspace.Name, "role": role, "console_access": principal.IsSuperAdmin() || role == "admin",
		})
	}
	consoleAccess := principal.IsSuperAdmin() || globalAdmin
	for _, role := range principal.Roles {
		if role == "admin" {
			consoleAccess = true
		}
	}
	var presentedClaims any
	if principal.AuthMethod == identity.AuthMethodOIDC {
		claimValues := make(map[string]any)
		for name, value := range principal.Claims {
			switch name {
			case "iss", "sub", "email", "name", "exp":
			default:
				claimValues[name] = value
			}
		}
		presentedClaims = map[string]any{
			"iss": principal.Claims["iss"], "sub": principal.Claims["sub"], "email": principal.Claims["email"], "claims": claimValues,
		}
	}
	return map[string]any{
		"authenticated": true, "principal": principal.Subject,
		"session":    map[string]any{"expires_at": nullableTime(principal.SessionExpiresAt), "idle_expires_at": nullableTime(principal.SessionIdleExpiresAt)},
		"session_id": nullableValue(principal.SessionID), "session_expires_at": nullableTime(principal.SessionExpiresAt),
		"session_idle_expires_at": nullableTime(principal.SessionIdleExpiresAt),
		"subject":                 principal.Subject, "display_name": principal.DisplayName, "email": principal.Email,
		"auth_method": principal.AuthMethod, "super_admin": principal.IsSuperAdmin(), "global_role": principal.Roles["*"],
		"console_access": consoleAccess, "workspaces": workspaceValues, "presented_claims": presentedClaims,
		"api_key_id": nullableValue(principal.APIKeyID),
		"capabilities": map[string]bool{"manage_workspaces": principal.IsSuperAdmin(), "manage_sessions": principal.IsSuperAdmin(),
			"manage_roles": principal.IsSuperAdmin(), "manage_global_roles": principal.IsSuperAdmin(),
			"manage_tile_matrix_sets": principal.IsSuperAdmin(), "read_audit": principal.IsSuperAdmin(),
			"manage_global_cache": principal.IsSuperAdmin(), "read_catalog_integrity": principal.IsSuperAdmin(),
			"manage_workspace": consoleAccess},
	}
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	principal, _ := identity.FromContext(r.Context())
	if principal.SessionID != "" {
		if sessions, ok := h.store.(store.BrowserSessionStore); ok {
			if err := sessions.RevokeBrowserSession(r.Context(), principal.SessionID, time.Now().UTC()); err != nil {
				if h.logger != nil {
					h.logger.Error("browser session revocation failed", "error", err)
				}
				// Keep cookies so the browser can retry durable revocation.
				writeError(w, http.StatusServiceUnavailable, "Sign-out failed", "session could not be revoked; retry signing out")
				return
			}
		} else {
			writeError(w, http.StatusServiceUnavailable, "Sign-out failed", "browser session storage is unavailable; retry signing out")
			return
		}
	}
	h.clearSessionCookies(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) refreshSession(w http.ResponseWriter, r *http.Request) {
	principal, _ := identity.FromContext(r.Context())
	if principal.SessionID == "" {
		writeError(w, http.StatusBadRequest, "Session required", "header credentials cannot be refreshed")
		return
	}
	if principal.SessionPreviousToken {
		writeError(w, http.StatusConflict, "Session refreshed", "another request refreshed this session; use the current browser cookies")
		return
	}
	// Coalesce refreshes inside the fixed overlap window. Do not rotate again
	// before in-flight requests from the preceding generation have completed.
	if principal.SessionRotationUntil != nil && time.Now().Before(*principal.SessionRotationUntil) {
		writeJSON(w, http.StatusOK, h.meResponse(r.Context(), principal))
		return
	}
	sessions, ok := h.store.(store.BrowserSessionStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Sessions unavailable", "browser session storage is not configured")
		return
	}
	token, csrf, err := newSessionSecrets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to rotate session")
		return
	}
	now := time.Now().UTC()
	idleExpiry := now.Add(time.Duration(h.cfg.Auth.Session.IdleTimeoutSec) * time.Second)
	if idleExpiry.After(principal.SessionExpiresAt) {
		idleExpiry = principal.SessionExpiresAt
	}
	if err = sessions.RotateBrowserSession(r.Context(), principal.SessionID, principal.SessionTokenHash, identity.HashSessionToken(token), identity.HashSessionToken(csrf), now, idleExpiry); err != nil {
		if errors.Is(err, store.ErrSessionRotated) {
			writeError(w, http.StatusConflict, "Session refreshed", "session changed during refresh; reload the current session")
		} else {
			if h.logger != nil {
				h.logger.Error("browser session rotation failed", "error", err)
			}
			writeError(w, http.StatusServiceUnavailable, "Session refresh failed", "session could not be extended; retry before it expires")
		}
		return
	}
	h.setSessionCookies(w, r, token, csrf, principal.SessionExpiresAt)
	principal.SessionIdleExpiresAt = idleExpiry
	writeJSON(w, http.StatusOK, h.meResponse(r.Context(), principal))
}

func browserScopes(configured, required []string) []string {
	result := make([]string, 0, len(configured)+len(required)+1)
	seen := make(map[string]struct{})
	for _, scope := range append(append([]string{"openid"}, configured...), required...) {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, exists := seen[scope]; exists {
			continue
		}
		seen[scope] = struct{}{}
		result = append(result, scope)
	}
	return result
}

func (h *handler) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, ok := h.store.(store.BrowserSessionStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Sessions unavailable", "browser session storage is not configured")
		return
	}
	values, err := sessions.ListBrowserSessions(r.Context(), 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list sessions")
		return
	}
	items := make([]sessionListItem, 0, len(values))
	for _, value := range values {
		items = append(items, newSessionListItem(value))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": items})
}

// sessionListItem adds what an administrator needs to grant access: whether
// the sign-in reached the console, and which identity-provider claims it sent.
type sessionListItem struct {
	*store.BrowserSession
	ConsoleAccess   bool           `json:"console_access"`
	PresentedClaims map[string]any `json:"presented_claims,omitempty"`
}

func newSessionListItem(session *store.BrowserSession) sessionListItem {
	item := sessionListItem{BrowserSession: session, ConsoleAccess: session.GlobalRole == "super_admin" || session.GlobalRole == "admin"}
	for _, role := range session.Roles {
		if role == "admin" || role == "super_admin" {
			item.ConsoleAccess = true
		}
	}
	if session.AuthMethod == string(identity.AuthMethodOIDC) {
		claims := make(map[string]any)
		for name, value := range session.Claims {
			switch name {
			case "iss", "sub", "email", "name", "exp":
			default:
				claims[name] = value
			}
		}
		item.PresentedClaims = map[string]any{"iss": session.Claims["iss"], "sub": session.Claims["sub"], "email": session.Claims["email"], "claims": claims}
	}
	return item
}

func (h *handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	sessions, ok := h.store.(store.BrowserSessionStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Sessions unavailable", "browser session storage is not configured")
		return
	}
	if err := sessions.RevokeBrowserSession(r.Context(), chi.URLParam(r, "session"), time.Now().UTC()); err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "session not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func newSessionSecrets() (string, string, error) {
	makeSecret := func() (string, error) {
		value := make([]byte, 32)
		if _, err := rand.Read(value); err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(value), nil
	}
	token, err := makeSecret()
	if err != nil {
		return "", "", err
	}
	csrf, err := makeSecret()
	return token, csrf, err
}

func (h *handler) setSessionCookies(w http.ResponseWriter, r *http.Request, token, csrf string, expires time.Time) {
	path := strings.TrimSuffix(h.cfg.Server.BasePath, "/") + "/"
	secure := h.cfg.Auth.RequireHTTPS || r.TLS != nil
	http.SetCookie(w, &http.Cookie{Name: identity.SessionCookieName, Value: token, Path: path, HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge(expires)})
	http.SetCookie(w, &http.Cookie{Name: identity.CSRFCookieName, Value: csrf, Path: path, Secure: secure,
		SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge(expires)})
}

func (h *handler) clearSessionCookies(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(h.cfg.Server.BasePath, "/") + "/"
	secure := h.cfg.Auth.RequireHTTPS || r.TLS != nil
	for _, name := range []string{identity.SessionCookieName, identity.CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{Name: name, Path: path, Secure: secure, HttpOnly: name == identity.SessionCookieName,
			SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	}
}

func maxAge(expires time.Time) int {
	seconds := int(time.Until(expires).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

func claimExpiration(claims map[string]interface{}) time.Time {
	if claims == nil {
		return time.Time{}
	}
	switch value := claims["exp"].(type) {
	case int64:
		return time.Unix(value, 0).UTC()
	case int:
		return time.Unix(int64(value), 0).UTC()
	case float64:
		return time.Unix(int64(value), 0).UTC()
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return time.Unix(parsed, 0).UTC()
		}
	}
	return time.Time{}
}

func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func nullableValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
