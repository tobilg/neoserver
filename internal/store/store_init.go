package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// escapeSQLLiteral escapes single quotes for safe interpolation inside a DuckDB
// single-quoted string literal (used for ATTACH path / ENCRYPTION_KEY, which cannot
// be passed as bound parameters).
func escapeSQLLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// Open opens an existing encrypted DuckDB store.
func Open(cfg Config) (*DuckDBStore, error) {
	// Open an in-memory database first, then attach the encrypted file
	db, catalogAttached, err := newCatalogConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// If encryption key is provided, attach the encrypted database
	// DuckDB v1.4+ has built-in encryption support via ATTACH with ENCRYPTION_KEY
	if cfg.EncryptionKey != "" {
		// Load httpfs for OpenSSL support (better write performance)
		_, _ = db.Exec("INSTALL httpfs; LOAD httpfs;")

		// Attach the encrypted database
		_, err = db.Exec(fmt.Sprintf(`
			ATTACH '%s' AS store (ENCRYPTION_KEY '%s');
			USE store;
		`, escapeSQLLiteral(cfg.Path), escapeSQLLiteral(cfg.EncryptionKey)))
		if err != nil {
			db.Close()
			return nil, WrapAttachError(fmt.Errorf("failed to attach encrypted database: %w", err), cfg.Path)
		}
	} else {
		// No encryption - just attach normally
		_, err = db.Exec(fmt.Sprintf(`ATTACH '%s' AS store; USE store;`, escapeSQLLiteral(cfg.Path)))
		if err != nil {
			db.Close()
			return nil, WrapAttachError(fmt.Errorf("failed to attach database: %w", err), cfg.Path)
		}
	}

	catalogAttached()
	// Verify the store is initialized
	var version int
	err = db.QueryRow("SELECT version FROM schema_info ORDER BY version DESC LIMIT 1").Scan(&version)
	if err != nil {
		db.Close()
		return nil, ErrNotInitialized
	}

	store := &DuckDBStore{
		db:            db,
		encryptionKey: cfg.EncryptionKey,
	}

	// Run migrations if needed
	if err := store.runMigrations(version); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// runMigrations runs any pending schema migrations.
func (s *DuckDBStore) runMigrations(currentVersion int) error {
	if currentVersion > schemaVersion {
		return fmt.Errorf("catalog schema version %d is newer than this binary supports (%d); use a compatible newer binary or restore a pre-upgrade backup, do not downgrade this catalog", currentVersion, schemaVersion)
	}
	if currentVersion == schemaVersion {
		return nil // No migrations needed
	}

	// Run migrations in order
	if currentVersion < 2 {
		if _, err := s.db.Exec(migrationV2SQL); err != nil {
			return fmt.Errorf("migration V2 failed: %w", err)
		}
	}
	if currentVersion < 3 {
		if _, err := s.db.Exec(migrationV3SQL); err != nil {
			return fmt.Errorf("migration V3 failed: %w", err)
		}
	}
	if currentVersion < 4 {
		if _, err := s.db.Exec(migrationV4SQL); err != nil {
			return fmt.Errorf("migration V4 failed: %w", err)
		}
	}
	if currentVersion < 5 {
		if _, err := s.db.Exec(migrationV5SQL); err != nil {
			return fmt.Errorf("migration V5 failed: %w", err)
		}
	}
	if currentVersion < 6 {
		if _, err := s.db.Exec(migrationV6SQL); err != nil {
			return fmt.Errorf("migration V6 failed: %w", err)
		}
	}
	if currentVersion < 7 {
		if _, err := s.db.Exec(migrationV7SQL); err != nil {
			return fmt.Errorf("migration V7 failed: %w", err)
		}
	}
	if currentVersion < 8 {
		if _, err := s.db.Exec(migrationV8SQL); err != nil {
			return fmt.Errorf("migration V8 failed: %w", err)
		}
	}
	if currentVersion < 9 {
		if _, err := s.db.Exec(migrationV9SQL); err != nil {
			return fmt.Errorf("migration V9 failed: %w", err)
		}
	}
	if currentVersion < 10 {
		if _, err := s.db.Exec(migrationV10SQL); err != nil {
			return fmt.Errorf("migration V10 failed: %w", err)
		}
	}
	if currentVersion < 11 {
		if _, err := s.db.Exec(migrationV11SQL); err != nil {
			return fmt.Errorf("migration V11 failed: %w", err)
		}
	}
	if currentVersion < 12 {
		if _, err := s.db.Exec(migrationV12SQL); err != nil {
			return fmt.Errorf("migration V12 failed: %w", err)
		}
	}
	if currentVersion < 13 {
		if _, err := s.db.Exec(migrationV13SQL); err != nil {
			return fmt.Errorf("migration V13 failed: %w", err)
		}
	}
	if currentVersion < 14 {
		if _, err := s.db.Exec(migrationV14SQL); err != nil {
			return fmt.Errorf("migration V14 failed: %w", err)
		}
	}
	if currentVersion < 15 {
		if _, err := s.db.Exec(migrationV15SQL); err != nil {
			return fmt.Errorf("migration V15 failed: %w", err)
		}
	}
	if currentVersion < 16 {
		if _, err := s.db.Exec(migrationV16SQL); err != nil {
			return fmt.Errorf("migration V16 failed: %w", err)
		}
	}
	if currentVersion < 17 {
		if _, err := s.db.Exec(migrationV17SQL); err != nil {
			return fmt.Errorf("migration V17 failed: %w", err)
		}
	}
	if currentVersion < 18 {
		if _, err := s.db.Exec(migrationV18SQL); err != nil {
			return fmt.Errorf("migration V18 failed: %w", err)
		}
	}
	if currentVersion < 19 {
		if _, err := s.db.Exec(migrationV19SQL); err != nil {
			return fmt.Errorf("migration V19 failed: %w", err)
		}
	}
	if currentVersion < 20 {
		if _, err := s.db.Exec(migrationV20SQL); err != nil {
			return fmt.Errorf("migration V20 failed: %w", err)
		}
	}
	if currentVersion < 21 {
		if _, err := s.db.Exec(migrationV21SQL); err != nil {
			return fmt.Errorf("migration V21 failed: %w", err)
		}
	}
	if currentVersion < 22 {
		if _, err := s.db.Exec(migrationV22SQL); err != nil {
			return fmt.Errorf("migration v22: %w", err)
		}
	}
	if currentVersion < 23 {
		if _, err := s.db.Exec(dataRevisionSchema + `INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (23, current_timestamp);`); err != nil {
			return fmt.Errorf("migration v23: %w", err)
		}
	}
	if currentVersion < 24 {
		if _, err := s.db.Exec(migrationV24SQL); err != nil {
			return fmt.Errorf("migration v24: %w", err)
		}
	}
	if currentVersion < 25 {
		if _, err := s.db.Exec(migrationV25SQL); err != nil {
			return fmt.Errorf("migration v25: %w", err)
		}
	}

	return nil
}

// Init creates a new encrypted DuckDB store and returns a bootstrap JWT.
func Init(cfg Config) (*DuckDBStore, string, error) {
	// Open an in-memory database first, then attach/create the encrypted file
	db, catalogAttached, err := newCatalogConnection()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create database: %w", err)
	}

	// If encryption key is provided, create encrypted database
	// DuckDB v1.4+ has built-in encryption support via ATTACH with ENCRYPTION_KEY
	if cfg.EncryptionKey != "" {
		// Load httpfs for OpenSSL support (better write performance)
		_, _ = db.Exec("INSTALL httpfs; LOAD httpfs;")

		// Create and attach the encrypted database
		_, err = db.Exec(fmt.Sprintf(`
			ATTACH '%s' AS store (ENCRYPTION_KEY '%s');
			USE store;
		`, escapeSQLLiteral(cfg.Path), escapeSQLLiteral(cfg.EncryptionKey)))
		if err != nil {
			db.Close()
			return nil, "", fmt.Errorf("failed to create encrypted database: %w", err)
		}
	} else {
		// No encryption - just attach normally
		_, err = db.Exec(fmt.Sprintf(`ATTACH '%s' AS store; USE store;`, escapeSQLLiteral(cfg.Path)))
		if err != nil {
			db.Close()
			return nil, "", fmt.Errorf("failed to create database: %w", err)
		}
	}

	// Run schema creation
	catalogAttached()
	_, err = db.Exec(schemaSQL)
	if err != nil {
		db.Close()
		return nil, "", fmt.Errorf("failed to create schema: %w", err)
	}

	// Insert schema version
	_, err = db.Exec("INSERT INTO schema_info (version) VALUES (?)", schemaVersion)
	if err != nil {
		db.Close()
		return nil, "", fmt.Errorf("failed to insert schema version: %w", err)
	}

	// Insert default roles
	_, err = db.Exec(defaultRolesSQL)
	if err != nil {
		db.Close()
		return nil, "", fmt.Errorf("failed to insert default roles: %w", err)
	}

	// Insert default Casbin policies
	_, err = db.Exec(defaultCasbinPoliciesSQL)
	if err != nil {
		db.Close()
		return nil, "", fmt.Errorf("failed to insert default policies: %w", err)
	}

	store := &DuckDBStore{
		db:            db,
		encryptionKey: cfg.EncryptionKey,
	}

	// Generate initial signing key
	signingKey, err := store.RotateSigningKey(context.Background())
	if err != nil {
		db.Close()
		return nil, "", fmt.Errorf("failed to generate signing key: %w", err)
	}

	// Generate bootstrap JWT
	bootstrapToken, err := store.createBootstrapToken(signingKey)
	if err != nil {
		db.Close()
		return nil, "", fmt.Errorf("failed to create bootstrap token: %w", err)
	}

	return store, bootstrapToken, nil
}

// Close closes the database connection.
func (s *DuckDBStore) Close() error {
	return s.db.Close()
}

// createBootstrapToken creates a self-signed JWT for initial setup.
func (s *DuckDBStore) createBootstrapToken(signingKey *SigningKey) (string, error) {
	return s.CreateToken(signingKey, "bootstrap", "super_admin", 24*time.Hour)
}
