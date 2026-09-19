package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// BrowserSession is the persisted, server-side half of an admin-console
// session. TokenHash and CSRFHash are SHA-256 digests, never raw credentials.
type BrowserSession struct {
	PreviousTokenHash     string            `json:"-"`
	PreviousCSRFHash      string            `json:"-"`
	PreviousValidUntil    *time.Time        `json:"-"`
	ID                    string            `json:"id"`
	TokenHash             string            `json:"-"`
	CSRFHash              string            `json:"-"`
	Subject               string            `json:"subject"`
	Email                 string            `json:"email,omitempty"`
	DisplayName           string            `json:"display_name,omitempty"`
	AuthMethod            string            `json:"auth_method"`
	GlobalRole            string            `json:"global_role,omitempty"`
	Roles                 map[string]string `json:"roles,omitempty"`
	Claims                map[string]any    `json:"-"`
	CredentialID          string            `json:"credential_id,omitempty"`
	CredentialFingerprint string            `json:"-"`
	RemoteAddr            string            `json:"remote_addr,omitempty"`
	UserAgent             string            `json:"user_agent,omitempty"`
	CreatedAt             time.Time         `json:"created_at"`
	LastSeenAt            time.Time         `json:"last_seen_at"`
	IdleExpiresAt         time.Time         `json:"idle_expires_at"`
	ExpiresAt             time.Time         `json:"expires_at"`
	RevokedAt             *time.Time        `json:"revoked_at,omitempty"`
}

type BrowserSessionStore interface {
	CreateBrowserSession(context.Context, BrowserSession) (*BrowserSession, error)
	GetBrowserSessionByTokenHash(context.Context, string) (*BrowserSession, error)
	ListBrowserSessions(context.Context, int) ([]*BrowserSession, error)
	TouchBrowserSession(context.Context, string, time.Time, time.Time) error
	RotateBrowserSession(context.Context, string, string, string, string, time.Time, time.Time) error
	RevokeBrowserSession(context.Context, string, time.Time) error
	DeleteExpiredBrowserSessions(context.Context, time.Time) (int64, error)
}

const browserSessionSelect = `SELECT id,token_hash,csrf_hash,subject,email,display_name,auth_method,global_role,
	roles_json,claims_json,credential_id,credential_fingerprint,remote_addr,user_agent,created_at,last_seen_at,
	idle_expires_at,expires_at,revoked_at,previous_token_hash,previous_csrf_hash,previous_valid_until FROM browser_sessions`

var ErrSessionRotated = errors.New("session was concurrently refreshed")

// SessionRotationGrace only covers requests already in flight. It is never
// extended by accepting an old token, and expiry/revocation still apply.
const SessionRotationGrace = 30 * time.Second

func scanBrowserSession(scanner interface{ Scan(...any) error }) (*BrowserSession, error) {
	var session BrowserSession
	var rolesRaw, claimsRaw any
	err := scanner.Scan(&session.ID, &session.TokenHash, &session.CSRFHash, &session.Subject, &session.Email,
		&session.DisplayName, &session.AuthMethod, &session.GlobalRole, &rolesRaw, &claimsRaw, &session.CredentialID,
		&session.CredentialFingerprint, &session.RemoteAddr, &session.UserAgent, &session.CreatedAt, &session.LastSeenAt,
		&session.IdleExpiresAt, &session.ExpiresAt, &session.RevokedAt, &session.PreviousTokenHash, &session.PreviousCSRFHash, &session.PreviousValidUntil)
	if err != nil {
		return nil, err
	}
	session.Roles = map[string]string{}
	session.Claims = map[string]any{}
	_ = decodeJSONValue(rolesRaw, &session.Roles)
	_ = decodeJSONValue(claimsRaw, &session.Claims)
	return &session, nil
}

func (s *DuckDBStore) CreateBrowserSession(ctx context.Context, session BrowserSession) (*BrowserSession, error) {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	assignedRoles := make([]string, 0, len(session.Roles))
	for _, role := range session.Roles {
		assignedRoles = append(assignedRoles, role)
	}
	if err := s.checkRoleAssignments(ctx, assignedRoles); err != nil {
		return nil, err
	}
	if session.ID == "" {
		session.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.LastSeenAt.IsZero() {
		session.LastSeenAt = session.CreatedAt
	}
	// DuckDB TIMESTAMP columns hold microseconds. Truncate before writing so
	// the returned session equals what a later read returns; on platforms whose
	// clock has nanosecond resolution the two would otherwise disagree.
	session.CreatedAt = session.CreatedAt.Truncate(time.Microsecond)
	session.LastSeenAt = session.LastSeenAt.Truncate(time.Microsecond)
	session.IdleExpiresAt = session.IdleExpiresAt.Truncate(time.Microsecond)
	session.ExpiresAt = session.ExpiresAt.Truncate(time.Microsecond)
	if session.RevokedAt != nil {
		revoked := session.RevokedAt.Truncate(time.Microsecond)
		session.RevokedAt = &revoked
	}
	roles, _ := json.Marshal(session.Roles)
	claims, _ := json.Marshal(session.Claims)
	_, err := s.db.ExecContext(ctx, `INSERT INTO browser_sessions
		(id,token_hash,csrf_hash,subject,email,display_name,auth_method,global_role,roles_json,claims_json,
		credential_id,credential_fingerprint,remote_addr,user_agent,created_at,last_seen_at,idle_expires_at,expires_at,revoked_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, session.ID, session.TokenHash, session.CSRFHash, session.Subject,
		session.Email, session.DisplayName, session.AuthMethod, session.GlobalRole, string(roles), string(claims), session.CredentialID,
		session.CredentialFingerprint, session.RemoteAddr, session.UserAgent, session.CreatedAt, session.LastSeenAt,
		session.IdleExpiresAt, session.ExpiresAt, session.RevokedAt)
	if err != nil {
		return nil, fmt.Errorf("create browser session: %w", err)
	}
	return &session, nil
}

func (s *DuckDBStore) GetBrowserSessionByTokenHash(ctx context.Context, tokenHash string) (*BrowserSession, error) {
	session, err := scanBrowserSession(s.db.QueryRowContext(ctx, browserSessionSelect+` WHERE token_hash=? OR (previous_token_hash=? AND previous_valid_until>?)`, tokenHash, tokenHash, time.Now().UTC()))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get browser session: %w", err)
	}
	return session, nil
}

func (s *DuckDBStore) ListBrowserSessions(ctx context.Context, limit int) ([]*BrowserSession, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, browserSessionSelect+` ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list browser sessions: %w", err)
	}
	defer rows.Close()
	result := make([]*BrowserSession, 0)
	for rows.Next() {
		session, scanErr := scanBrowserSession(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, session)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) TouchBrowserSession(ctx context.Context, id string, seenAt, idleExpiresAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE browser_sessions SET last_seen_at=?,idle_expires_at=?
		WHERE id=? AND revoked_at IS NULL`, seenAt, idleExpiresAt, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) RotateBrowserSession(ctx context.Context, id, expectedHash, tokenHash, csrfHash string, seenAt, idleExpiresAt time.Time) error {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE browser_sessions SET previous_token_hash=token_hash,previous_csrf_hash=csrf_hash,previous_valid_until=?,token_hash=?,csrf_hash=?,last_seen_at=?,idle_expires_at=LEAST(?,expires_at)
		WHERE id=? AND token_hash=? AND revoked_at IS NULL AND expires_at>? AND idle_expires_at>?
		AND (previous_valid_until IS NULL OR previous_valid_until<=?)`, seenAt.Add(SessionRotationGrace), tokenHash, csrfHash, seenAt, idleExpiresAt, id, expectedHash, seenAt, seenAt, seenAt)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrSessionRotated
	}
	return nil
}

func (s *DuckDBStore) RevokeBrowserSession(ctx context.Context, id string, revokedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE browser_sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, revokedAt, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) DeleteExpiredBrowserSessions(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM browser_sessions WHERE expires_at<=? OR idle_expires_at<=? OR revoked_at IS NOT NULL`, now, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
