package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/dbschema"
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

// catalogMigrations contains only upgrades after the released 0.1.0 baseline.
var catalogMigrations []dbschema.Migration

func (s *DuckDBStore) runMigrations(currentVersion int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := dbschema.Apply(tx, "catalog", "schema_info", catalogBaselineVersion, schemaVersion, currentVersion, catalogMigrations,
		"open it once with neoserver 0.1.0 to migrate it, or initialise a new catalog"); err != nil {
		return err
	}
	return tx.Commit()
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
