package store

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Role operations

func (s *DuckDBStore) CreateRole(ctx context.Context, input CreateRoleInput) (*Role, error) {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	var retired bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM retired_roles WHERE id=?)", input.ID).Scan(&retired); err != nil {
		return nil, err
	}
	if retired {
		return nil, fmt.Errorf("%w: this role ID is retired; choose a new ID", ErrDuplicateKey)
	}
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO roles (id, name, description, is_system, created_at)
		VALUES (?, ?, ?, false, ?)
	`, input.ID, input.Name, input.Description, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to create role: %w", err)
	}

	return &Role{
		ID:          input.ID,
		Name:        input.Name,
		Description: input.Description,
		IsSystem:    false,
		CreatedAt:   now,
	}, nil
}

func (s *DuckDBStore) GetRole(ctx context.Context, id string) (*Role, error) {
	var role Role
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, is_system, created_at
		FROM roles WHERE id = ?
	`, id).Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem, &role.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get role: %w", err)
	}
	return &role, nil
}

func (s *DuckDBStore) ListRoles(ctx context.Context) ([]*Role, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, is_system, created_at
		FROM roles ORDER BY is_system DESC, name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}
	defer rows.Close()

	var roles []*Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem, &role.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan role: %w", err)
		}
		roles = append(roles, &role)
	}
	return roles, rows.Err()
}

func (s *DuckDBStore) DeleteRole(ctx context.Context, id string) error {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Check if it's a system role
	var isSystem bool
	err = tx.QueryRowContext(ctx, "SELECT is_system FROM roles WHERE id = ?", id).Scan(&isSystem)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to check role: %w", err)
	}
	if isSystem {
		return errors.New("cannot delete system role")
	}

	dependencies, err := roleDependencies(ctx, tx, id)
	if err != nil {
		return err
	}
	for kind, count := range dependencies {
		if count > 0 {
			return fmt.Errorf("%w: role is referenced by %s (%d); remove assignments before deletion", ErrResourceNotEmpty, kind, count)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM casbin_rules WHERE v0 = ?", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO retired_roles (id) VALUES (?) ON CONFLICT DO NOTHING", id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM roles WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete role: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

type roleQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func roleDependencies(ctx context.Context, db roleQuerier, id string) (map[string]int, error) {
	encoded, _ := json.Marshal(id)
	result := make(map[string]int)
	checks := []struct {
		name, query string
		arg         any
	}{
		{"api_keys", "SELECT count(*) FROM api_keys WHERE role_id=? AND NOT revoked AND (expires_at IS NULL OR expires_at > (current_timestamp AT TIME ZONE 'UTC'))", id},
		{"claim_mappings", "SELECT count(*) FROM claim_role_mappings WHERE role_id=?", id},
		{"sessions", "SELECT count(*) FROM browser_sessions WHERE revoked_at IS NULL AND expires_at > (current_timestamp AT TIME ZONE 'UTC') AND idle_expires_at > (current_timestamp AT TIME ZONE 'UTC') AND EXISTS (SELECT 1 FROM json_each(roles_json) WHERE json_extract_string(value, '$') = ?)", id},
	}
	for _, table := range []string{"layers", "coverages", "layer_groups"} {
		checks = append(checks, struct {
			name, query string
			arg         any
		}{table, "SELECT count(*) FROM " + table + " WHERE json_contains(allowed_roles, CAST(? AS JSON))", string(encoded)})
	}
	for _, check := range checks {
		var count int
		if err := db.QueryRowContext(ctx, check.query, check.arg).Scan(&count); err != nil {
			return nil, err
		}
		result[check.name] = count
	}
	return result, nil
}

func (s *DuckDBStore) RoleDependencies(ctx context.Context, id string) (map[string]int, error) {
	return roleDependencies(ctx, s.db, id)
}

// API Key operations

func (s *DuckDBStore) CreateAPIKey(ctx context.Context, input CreateAPIKeyInput) (*CreateAPIKeyOutput, error) {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	if err := s.checkRoleAssignments(ctx, []string{input.RoleID}); err != nil {
		return nil, err
	}
	// Generate a random API key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	key := "nsk_" + hex.EncodeToString(keyBytes) // nsk = neoserver key
	keyPrefix := key[:12]

	// Hash the key for storage
	hash := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(hash[:])

	id := uuid.New().String()
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, key_hash, key_prefix, owner_name, owner_email, workspace_id, role_id, name, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, keyHash, keyPrefix, input.OwnerName, input.OwnerEmail, input.WorkspaceID, input.RoleID, input.Name, input.ExpiresAt, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return &CreateAPIKeyOutput{
		APIKey: APIKey{
			ID:          id,
			KeyHash:     keyHash,
			KeyPrefix:   keyPrefix,
			OwnerName:   input.OwnerName,
			OwnerEmail:  input.OwnerEmail,
			WorkspaceID: input.WorkspaceID,
			RoleID:      input.RoleID,
			Name:        input.Name,
			ExpiresAt:   input.ExpiresAt,
			Revoked:     false,
			CreatedAt:   now,
		},
		Key: key,
	}, nil
}

func (s *DuckDBStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	return s.getAPIKey(ctx, "key_hash", keyHash)
}

// GetAPIKeyByID uses the primary key, including current revocation and expiry.
func (s *DuckDBStore) GetAPIKeyByID(ctx context.Context, id string) (*APIKey, error) {
	return s.getAPIKey(ctx, "id", id)
}

func (s *DuckDBStore) getAPIKey(ctx context.Context, column, value string) (*APIKey, error) {
	var apiKey APIKey
	var workspaceID sql.NullString
	var expiresAt sql.NullTime

	err := s.db.QueryRowContext(ctx, `
		SELECT id, key_hash, key_prefix, owner_name, owner_email, workspace_id, role_id, name, expires_at, revoked, created_at
		FROM api_keys WHERE `+column+` = ? AND revoked = false AND role_id IN (SELECT id FROM roles)
	`, value).Scan(&apiKey.ID, &apiKey.KeyHash, &apiKey.KeyPrefix, &apiKey.OwnerName, &apiKey.OwnerEmail,
		&workspaceID, &apiKey.RoleID, &apiKey.Name, &expiresAt, &apiKey.Revoked, &apiKey.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	if workspaceID.Valid {
		apiKey.WorkspaceID = &workspaceID.String
	}
	if expiresAt.Valid {
		apiKey.ExpiresAt = &expiresAt.Time
	}

	// Check expiration
	if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
		return nil, ErrInvalidCredentials
	}

	return &apiKey, nil
}

func (s *DuckDBStore) ListAPIKeys(ctx context.Context, workspaceID *string) ([]*APIKey, error) {
	var rows *sql.Rows
	var err error

	if workspaceID != nil {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, key_hash, key_prefix, owner_name, owner_email, workspace_id, role_id, name, expires_at, revoked, created_at
			FROM api_keys WHERE workspace_id = ? ORDER BY created_at DESC
		`, *workspaceID)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, key_hash, key_prefix, owner_name, owner_email, workspace_id, role_id, name, expires_at, revoked, created_at
			FROM api_keys ORDER BY created_at DESC
		`)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list API keys: %w", err)
	}
	defer rows.Close()

	var apiKeys []*APIKey
	for rows.Next() {
		var apiKey APIKey
		var wsID sql.NullString
		var expiresAt sql.NullTime
		if err := rows.Scan(&apiKey.ID, &apiKey.KeyHash, &apiKey.KeyPrefix, &apiKey.OwnerName, &apiKey.OwnerEmail,
			&wsID, &apiKey.RoleID, &apiKey.Name, &expiresAt, &apiKey.Revoked, &apiKey.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan API key: %w", err)
		}
		if wsID.Valid {
			apiKey.WorkspaceID = &wsID.String
		}
		if expiresAt.Valid {
			apiKey.ExpiresAt = &expiresAt.Time
		}
		apiKeys = append(apiKeys, &apiKey)
	}
	return apiKeys, rows.Err()
}

func (s *DuckDBStore) RevokeAPIKey(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE api_keys SET revoked = true WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to revoke API key: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAPIKey permanently removes a revoked API key. Active keys are
// refused so that deletion is always a deliberate second step after revoking.
func (s *DuckDBStore) DeleteAPIKey(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}
	defer tx.Rollback()

	var revoked bool
	switch err := tx.QueryRowContext(ctx, "SELECT revoked FROM api_keys WHERE id = ?", id).Scan(&revoked); {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("failed to delete API key: %w", err)
	case !revoked:
		return ErrAPIKeyNotRevoked
	}
	// Sessions of a revoked key are already rejected; remove them with the key.
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM browser_sessions WHERE auth_method = 'apikey' AND credential_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete API key sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM api_keys WHERE id = ?", id); err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}
	return nil
}

// Claim mapping operations

func (s *DuckDBStore) CreateClaimMapping(ctx context.Context, input CreateClaimMappingInput) (*ClaimRoleMapping, error) {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	if err := s.checkRoleAssignments(ctx, []string{input.RoleID}); err != nil {
		return nil, err
	}
	id := uuid.New().String()
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO claim_role_mappings (id, workspace_id, claim_name, claim_value, role_id, priority, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, input.WorkspaceID, input.ClaimName, input.ClaimValue, input.RoleID, input.Priority, now)
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate key") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to create claim mapping: %w", err)
	}

	return &ClaimRoleMapping{
		ID:          id,
		WorkspaceID: input.WorkspaceID,
		ClaimName:   input.ClaimName,
		ClaimValue:  input.ClaimValue,
		RoleID:      input.RoleID,
		Priority:    input.Priority,
		CreatedAt:   now,
	}, nil
}

func (s *DuckDBStore) GetClaimMapping(ctx context.Context, id string) (*ClaimRoleMapping, error) {
	var mapping ClaimRoleMapping
	err := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, claim_name, claim_value, role_id, priority, created_at
		FROM claim_role_mappings WHERE id = ?
	`, id).Scan(&mapping.ID, &mapping.WorkspaceID, &mapping.ClaimName, &mapping.ClaimValue, &mapping.RoleID, &mapping.Priority, &mapping.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get claim mapping: %w", err)
	}
	return &mapping, nil
}

func (s *DuckDBStore) ListClaimMappings(ctx context.Context, workspaceID string) ([]*ClaimRoleMapping, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, claim_name, claim_value, role_id, priority, created_at
		FROM claim_role_mappings WHERE workspace_id = ? ORDER BY priority DESC, claim_name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list claim mappings: %w", err)
	}
	defer rows.Close()

	var mappings []*ClaimRoleMapping
	for rows.Next() {
		var mapping ClaimRoleMapping
		if err := rows.Scan(&mapping.ID, &mapping.WorkspaceID, &mapping.ClaimName, &mapping.ClaimValue, &mapping.RoleID, &mapping.Priority, &mapping.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan claim mapping: %w", err)
		}
		mappings = append(mappings, &mapping)
	}
	return mappings, rows.Err()
}

func (s *DuckDBStore) DeleteClaimMapping(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM claim_role_mappings WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete claim mapping: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) ResolveClaimsToRoles(ctx context.Context, claims map[string][]string) (map[string]string, error) {
	roles := make(map[string]string)
	names := make([]string, 0, len(claims))
	for name := range claims {
		names = append(names, name)
	}
	sort.Strings(names)
	clauses := make([]string, 0, len(names))
	args := make([]interface{}, 0)
	for _, name := range names {
		values := append([]string(nil), claims[name]...)
		sort.Strings(values)
		if len(values) == 0 {
			continue
		}
		placeholders := make([]string, len(values))
		clauses = append(clauses, fmt.Sprintf("(claim_name=? AND claim_value IN (%s))", strings.Join(placeholdersWithQuestionMarks(placeholders), ",")))
		args = append(args, name)
		for _, value := range values {
			args = append(args, value)
		}
	}
	if len(clauses) == 0 {
		return roles, nil
	}
	query := fmt.Sprintf(`SELECT workspace_id,role_id FROM claim_role_mappings WHERE role_id IN (SELECT id FROM roles) AND (%s)
		ORDER BY workspace_id,priority DESC,
		CASE role_id WHEN 'super_admin' THEN 4 WHEN 'admin' THEN 3 WHEN 'editor' THEN 2 WHEN 'viewer' THEN 1 ELSE 0 END DESC,
		role_id`, strings.Join(clauses, " OR "))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve claims: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var workspaceID, roleID string
		if err = rows.Scan(&workspaceID, &roleID); err != nil {
			return nil, fmt.Errorf("failed to scan claim mapping: %w", err)
		}
		if _, exists := roles[workspaceID]; !exists {
			roles[workspaceID] = roleID
		}
	}
	return roles, rows.Err()
}

func placeholdersWithQuestionMarks(placeholders []string) []string {
	for index := range placeholders {
		placeholders[index] = "?"
	}
	return placeholders
}

// Signing key operations

func (s *DuckDBStore) GetActiveSigningKey(ctx context.Context) (*SigningKey, error) {
	var key SigningKey
	err := s.db.QueryRowContext(ctx, `
		SELECT id, private_key, public_key, algorithm, created_at, is_active
		FROM signing_keys WHERE is_active = true
		ORDER BY created_at DESC LIMIT 1
	`).Scan(&key.ID, &key.PrivateKey, &key.PublicKey, &key.Algorithm, &key.CreatedAt, &key.IsActive)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get signing key: %w", err)
	}
	return &key, nil
}

func (s *DuckDBStore) RotateSigningKey(ctx context.Context) (*SigningKey, error) {
	// Generate new ECDSA P-256 key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	privateBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	publicBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	// Deactivate existing keys
	_, err = s.db.ExecContext(ctx, "UPDATE signing_keys SET is_active = false")
	if err != nil {
		return nil, fmt.Errorf("failed to deactivate existing keys: %w", err)
	}

	// Insert new key
	id := uuid.New().String()
	now := time.Now().UTC()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO signing_keys (id, private_key, public_key, algorithm, created_at, is_active)
		VALUES (?, ?, ?, 'ES256', ?, true)
	`, id, privateBytes, publicBytes, now)
	if err != nil {
		return nil, fmt.Errorf("failed to insert signing key: %w", err)
	}

	return &SigningKey{
		ID:         id,
		PrivateKey: privateBytes,
		PublicKey:  publicBytes,
		Algorithm:  "ES256",
		CreatedAt:  now,
		IsActive:   true,
	}, nil
}

// Casbin adapter operations

func (s *DuckDBStore) LoadCasbinPolicies(ctx context.Context) ([]*CasbinRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, ptype, v0, v1, v2, v3, v4, v5
		FROM casbin_rules WHERE v0 IN (SELECT id FROM roles)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to load casbin policies: %w", err)
	}
	defer rows.Close()

	var rules []*CasbinRule
	for rows.Next() {
		var rule CasbinRule
		var v0, v1, v2, v3, v4, v5 sql.NullString
		if err := rows.Scan(&rule.ID, &rule.PType, &v0, &v1, &v2, &v3, &v4, &v5); err != nil {
			return nil, fmt.Errorf("failed to scan casbin rule: %w", err)
		}
		rule.V0 = v0.String
		rule.V1 = v1.String
		rule.V2 = v2.String
		rule.V3 = v3.String
		rule.V4 = v4.String
		rule.V5 = v5.String
		rules = append(rules, &rule)
	}
	return rules, rows.Err()
}

func (s *DuckDBStore) SaveCasbinPolicy(ctx context.Context, rule *CasbinRule) error {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	if err := s.checkRoleAssignments(ctx, []string{rule.V0}); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO casbin_rules (id, ptype, v0, v1, v2, v3, v4, v5)
		VALUES (nextval('casbin_rules_id_seq'), ?, ?, ?, ?, ?, ?, ?)
	`, rule.PType, rule.V0, rule.V1, rule.V2, rule.V3, rule.V4, rule.V5)
	if err != nil {
		return fmt.Errorf("failed to save casbin policy: %w", err)
	}
	return nil
}

func (s *DuckDBStore) RemoveCasbinPolicy(ctx context.Context, rule *CasbinRule) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM casbin_rules
		WHERE ptype = ? AND v0 = ? AND v1 = ? AND v2 = ? AND v3 = ? AND v4 = ? AND v5 = ?
	`, rule.PType, rule.V0, rule.V1, rule.V2, rule.V3, rule.V4, rule.V5)
	if err != nil {
		return fmt.Errorf("failed to remove casbin policy: %w", err)
	}
	return nil
}

// ReplaceCasbinPolicies atomically replaces the authorization policy set.
// Casbin SavePolicy uses this during policy changes so a restart observes the
// exact same service, operation, workspace, and layer grants.
func (s *DuckDBStore) ReplaceCasbinPolicies(ctx context.Context, rules []*CasbinRule) error {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM casbin_rules`); err != nil {
		return err
	}
	for _, rule := range rules {
		var exists bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM roles WHERE id=?)", rule.V0).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("cannot save policy for missing role %q", rule.V0)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO casbin_rules(id,ptype,v0,v1,v2,v3,v4,v5)
			VALUES(nextval('casbin_rules_id_seq'),?,?,?,?,?,?,?)`, rule.PType, rule.V0, rule.V1, rule.V2, rule.V3, rule.V4, rule.V5); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateToken creates a self-signed JWT with the given parameters.
func (s *DuckDBStore) CreateToken(signingKey *SigningKey, subject, role string, duration time.Duration) (string, error) {
	privateKey, err := x509.ParseECPrivateKey(signingKey.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	now := time.Now()
	token := &jwtToken{
		Header: jwtHeader{
			Alg: "ES256",
			Typ: "JWT",
		},
		Payload: jwtPayload{
			Iss:  "neoserver",
			Sub:  subject,
			Role: role,
			Iat:  now.Unix(),
			Exp:  now.Add(duration).Unix(),
		},
	}

	return token.Sign(privateKey)
}

// ValidateToken validates a self-signed JWT and returns the claims.
func (s *DuckDBStore) ValidateToken(ctx context.Context, tokenString string) (*jwtPayload, error) {
	signingKey, err := s.GetActiveSigningKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get signing key: %w", err)
	}

	publicKey, err := x509.ParsePKIXPublicKey(signingKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	ecdsaKey, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("invalid public key type")
	}

	token, err := parseJWT(tokenString)
	if err != nil {
		return nil, err
	}

	if err := token.Verify(ecdsaKey); err != nil {
		return nil, err
	}

	// Check expiration
	if time.Now().Unix() > token.Payload.Exp {
		return nil, errors.New("token expired")
	}

	return &token.Payload, nil
}
