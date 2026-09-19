// Package store provides an encrypted DuckDB backing store for neoserver configuration.
package store

// schemaVersion is the catalog schema this binary creates and opens. It is
// also the baseline: a catalog at an older version is refused rather than
// upgraded. A future schema change bumps this and appends a step to
// catalogMigrations in store_init.go.
const catalogBaselineVersion = 25
const schemaVersion = 25

// schemaSQL contains the DDL for creating all backing store tables.
const schemaSQL = dataRevisionSchema + `
-- Retired role IDs must never restore grants held by old credentials.
CREATE TABLE IF NOT EXISTS retired_roles (id VARCHAR PRIMARY KEY, retired_at TIMESTAMP DEFAULT current_timestamp);
-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_info (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT current_timestamp
);

-- Internal signing key for self-signed JWTs
CREATE TABLE IF NOT EXISTS signing_keys (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    private_key BLOB NOT NULL,
    public_key BLOB NOT NULL,
    algorithm VARCHAR NOT NULL DEFAULT 'ES256',
    created_at TIMESTAMP DEFAULT current_timestamp,
    is_active BOOLEAN DEFAULT true
);

-- Workspaces
CREATE TABLE IF NOT EXISTS workspaces (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    name VARCHAR NOT NULL UNIQUE,
    description VARCHAR,
    wms_settings JSON DEFAULT '{"enabled":false}',
    wfs_settings JSON DEFAULT '{"enabled":false}',
    ogcapi_settings JSON DEFAULT '{"enabled":true}',
	ogc_tiles_api_settings JSON DEFAULT '{"enabled":false,"public":false,"versions":["1.0.0"],"settings":{"tile_matrix_sets":["WebMercatorQuad","WorldCRS84Quad"],"vector_tiles":{"enabled":true,"formats":["application/vnd.mapbox-vector-tile"]},"map_tiles":{"enabled":true,"formats":["image/png","image/jpeg","image/webp"]},"cache_enabled":true}}',
	wcs_settings JSON DEFAULT '{"enabled":false,"public":false}',
	wmts_settings JSON DEFAULT '{"enabled":false,"public":false,"feature_info_enabled":true}',
	tile_revision BIGINT DEFAULT 1,
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp
);

-- Services (data sources)
CREATE TABLE IF NOT EXISTS services (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    workspace_id VARCHAR NOT NULL,
    name VARCHAR NOT NULL,
    type VARCHAR NOT NULL,
    connection_info JSON,
	cache_settings JSON DEFAULT NULL,
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(workspace_id, name)
);

-- Layers
CREATE TABLE IF NOT EXISTS layers (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    service_id VARCHAR NOT NULL,
    source_layer VARCHAR NOT NULL,
    public_id VARCHAR NOT NULL,
    title VARCHAR,
    description VARCHAR,
    enabled BOOLEAN DEFAULT true,
    crs_default INTEGER DEFAULT 4326,
    dimensions JSON DEFAULT '[]',
    is_sql_view BOOLEAN DEFAULT false,
    sql_view_config JSON DEFAULT NULL,
    public BOOLEAN DEFAULT false,
    allowed_roles JSON DEFAULT '[]',
	default_style VARCHAR DEFAULT '',
	styles JSON DEFAULT '[]',
	native_extent JSON DEFAULT NULL,
	tile_cache_quota_bytes BIGINT DEFAULT 0,
	tile_cache_generation BIGINT DEFAULT 1,
	tile_cache_parameters JSON DEFAULT NULL,
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(service_id, public_id)
);

-- WCS coverages
CREATE TABLE IF NOT EXISTS coverages (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
	workspace_id VARCHAR NOT NULL,
    service_id VARCHAR NOT NULL,
    source_coverage VARCHAR NOT NULL,
    public_id VARCHAR NOT NULL,
    title VARCHAR,
    description VARCHAR,
    enabled BOOLEAN DEFAULT true,
    public BOOLEAN DEFAULT false,
    allowed_roles JSON DEFAULT '[]',
	range_fields JSON DEFAULT '[]',
	dimensions JSON DEFAULT '[]',
	default_style VARCHAR DEFAULT '',
	styles JSON DEFAULT '[]',
	resampling VARCHAR DEFAULT 'bilinear',
	wcs20_coverage_subtype VARCHAR NOT NULL DEFAULT 'RectifiedGridCoverage',
	native_extent JSON DEFAULT NULL,
	tile_cache_quota_bytes BIGINT DEFAULT 0,
	tile_cache_generation BIGINT DEFAULT 1,
	tile_cache_parameters JSON DEFAULT NULL,
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(service_id, public_id),
	UNIQUE(service_id, source_coverage),
	UNIQUE(workspace_id, public_id)
);

-- Styles (SLD styles for WMS rendering)
CREATE TABLE IF NOT EXISTS styles (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    workspace_id VARCHAR NOT NULL,
    name VARCHAR NOT NULL,
    title VARCHAR,
    description VARCHAR,
    sld_body TEXT NOT NULL,
    format VARCHAR DEFAULT 'sld_1.1.0',
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(workspace_id, name)
);

-- Metadata for workspace-scoped graphic assets. Payloads are stored beneath
-- WMS.StyleAssetPath, keeping binary content out of the encrypted catalog.
CREATE TABLE IF NOT EXISTS style_assets (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    workspace_id VARCHAR NOT NULL,
    name VARCHAR NOT NULL,
    content_type VARCHAR NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 VARCHAR NOT NULL,
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(workspace_id, name)
);

-- Persisted WMS/WMTS/OGC map-tile layer groups. Members reference public
-- resource or group identifiers so nesting remains workspace-local.
CREATE TABLE IF NOT EXISTS layer_groups (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    workspace_id VARCHAR NOT NULL,
    public_id VARCHAR NOT NULL,
    title VARCHAR,
    description VARCHAR,
    enabled BOOLEAN DEFAULT true,
    public BOOLEAN DEFAULT false,
    allowed_roles JSON DEFAULT '[]',
    members JSON DEFAULT '[]',
    default_style VARCHAR DEFAULT '',
    styles JSON DEFAULT '[]',
    native_extent JSON DEFAULT NULL,
    tile_cache_quota_bytes BIGINT DEFAULT 0,
    tile_cache_generation BIGINT DEFAULT 1,
	tile_cache_parameters JSON DEFAULT NULL,
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(workspace_id, public_id)
);

-- Roles (built-in + custom)
CREATE TABLE IF NOT EXISTS roles (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL UNIQUE,
    description VARCHAR,
    is_system BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT current_timestamp
);

-- API Keys (standalone, not tied to users table)
CREATE TABLE IF NOT EXISTS api_keys (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    key_hash VARCHAR NOT NULL UNIQUE,
    key_prefix VARCHAR NOT NULL,
    owner_name VARCHAR NOT NULL,
    owner_email VARCHAR,
    workspace_id VARCHAR,
    role_id VARCHAR NOT NULL,
    name VARCHAR NOT NULL,
    expires_at TIMESTAMP,
    revoked BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT current_timestamp
);

-- OIDC/JWT Role Mappings (claims → workspace roles)
CREATE TABLE IF NOT EXISTS claim_role_mappings (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    workspace_id VARCHAR NOT NULL,
    claim_name VARCHAR NOT NULL,
    claim_value VARCHAR NOT NULL,
    role_id VARCHAR NOT NULL,
    priority INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(workspace_id, claim_name, claim_value)
);

-- WFS Stored Queries
CREATE TABLE IF NOT EXISTS wfs_stored_queries (
    id VARCHAR PRIMARY KEY DEFAULT gen_random_uuid()::VARCHAR,
    workspace_id VARCHAR NOT NULL,
    query_id VARCHAR NOT NULL,
    title VARCHAR,
    abstract VARCHAR,
    parameters JSON DEFAULT '[]',
    query_expression TEXT NOT NULL,
    language VARCHAR,
    return_types JSON DEFAULT '[]',
    created_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(workspace_id, query_id)
);

-- Casbin policy rules
CREATE TABLE IF NOT EXISTS casbin_rules (
    id INTEGER PRIMARY KEY,
    ptype VARCHAR NOT NULL,
    v0 VARCHAR,
    v1 VARCHAR,
    v2 VARCHAR,
    v3 VARCHAR,
    v4 VARCHAR,
    v5 VARCHAR
);

-- Durable catalog deletion operations. These rows deliberately have no
-- foreign key to their target: completed operations remain useful after the
-- target catalog row has been removed, and failed operations act as
-- tombstones during crash recovery.
CREATE TABLE IF NOT EXISTS catalog_deletions (
    id VARCHAR PRIMARY KEY,
    scope_kind VARCHAR NOT NULL,
    workspace_id VARCHAR NOT NULL,
    target_id VARCHAR NOT NULL,
    target_name VARCHAR NOT NULL,
    active_target VARCHAR UNIQUE,
    status VARCHAR NOT NULL,
    phase VARCHAR NOT NULL,
    plan_json JSON NOT NULL,
    last_error VARCHAR DEFAULT '',
    attempt_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp,
    completed_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS catalog_deletions_target ON catalog_deletions(scope_kind, target_id, status);
CREATE INDEX IF NOT EXISTS catalog_deletions_status ON catalog_deletions(status, updated_at);

-- Durable managed-vector imports. Source payloads and staged databases live on
-- disk; only lifecycle metadata and per-import encryption keys are cataloged.
CREATE TABLE IF NOT EXISTS import_jobs (
    id VARCHAR PRIMARY KEY,
    workspace_id VARCHAR NOT NULL,
    name VARCHAR NOT NULL,
    source_kind VARCHAR NOT NULL,
    source_locator VARCHAR DEFAULT '',
    source_path VARCHAR NOT NULL,
    source_relative_path VARCHAR DEFAULT '',
    source_filename VARCHAR DEFAULT '',
    status VARCHAR NOT NULL,
    phase VARCHAR NOT NULL,
    plan_json JSON,
    discovery_json JSON,
    service_id VARCHAR,
    managed_path VARCHAR,
    rollback_operation_id VARCHAR,
    processed_bytes BIGINT DEFAULT 0,
    processed_features BIGINT DEFAULT 0,
    processed_layers INTEGER DEFAULT 0,
    total_layers INTEGER DEFAULT 0,
    retry_count INTEGER DEFAULT 0,
    cancel_requested BOOLEAN DEFAULT false,
    error_message VARCHAR DEFAULT '',
    created_by VARCHAR DEFAULT '',
    created_at TIMESTAMP DEFAULT current_timestamp,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    updated_at TIMESTAMP DEFAULT current_timestamp
);
CREATE INDEX IF NOT EXISTS import_jobs_workspace ON import_jobs(workspace_id, created_at);
CREATE INDEX IF NOT EXISTS import_jobs_status ON import_jobs(status, updated_at);

-- Immutable lifecycle events make long-running imports inspectable after the
-- mutable job row has advanced to a later phase.
CREATE TABLE IF NOT EXISTS import_job_events (
    id VARCHAR PRIMARY KEY,
    import_id VARCHAR NOT NULL,
    workspace_id VARCHAR NOT NULL,
    status VARCHAR NOT NULL,
    phase VARCHAR NOT NULL,
    warnings JSON DEFAULT '[]',
    error_message VARCHAR DEFAULT '',
    processed_bytes BIGINT DEFAULT 0,
    processed_features BIGINT DEFAULT 0,
    processed_layers INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT current_timestamp
);
CREATE INDEX IF NOT EXISTS import_job_events_import ON import_job_events(import_id, created_at, id);

-- Browser console sessions. Only hashes are persisted; raw session and CSRF
-- tokens exist in the browser cookies and are never recoverable from storage.
CREATE TABLE IF NOT EXISTS browser_sessions (
    id VARCHAR PRIMARY KEY,
    previous_token_hash VARCHAR DEFAULT '',
    previous_csrf_hash VARCHAR DEFAULT '',
    previous_valid_until TIMESTAMP,
    token_hash VARCHAR NOT NULL UNIQUE,
    csrf_hash VARCHAR NOT NULL,
    subject VARCHAR NOT NULL,
    email VARCHAR DEFAULT '',
    display_name VARCHAR DEFAULT '',
    auth_method VARCHAR NOT NULL,
    global_role VARCHAR DEFAULT '',
    roles_json JSON DEFAULT '{}',
    claims_json JSON DEFAULT '{}',
    credential_id VARCHAR DEFAULT '',
    credential_fingerprint VARCHAR DEFAULT '',
    remote_addr VARCHAR DEFAULT '',
    user_agent VARCHAR DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP NOT NULL,
    idle_expires_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS browser_sessions_subject ON browser_sessions(subject, created_at);
CREATE INDEX IF NOT EXISTS browser_sessions_expiry ON browser_sessions(expires_at, idle_expires_at);

CREATE TABLE IF NOT EXISTS managed_assets (
    import_id VARCHAR PRIMARY KEY,
    workspace_id VARCHAR NOT NULL,
    service_id VARCHAR UNIQUE,
    path VARCHAR NOT NULL UNIQUE,
    encryption_key VARCHAR NOT NULL,
    created_at TIMESTAMP DEFAULT current_timestamp
);

-- Audit events that could not be delivered to the standalone audit database.
-- Payloads contain the same bounded/redacted fields as audit_events and remain
-- encrypted by the main catalog while awaiting idempotent delivery.
CREATE TABLE IF NOT EXISTS audit_outbox (
    id VARCHAR PRIMARY KEY,
    occurred_at TIMESTAMP NOT NULL,
    payload JSON NOT NULL,
    attempt_count INTEGER DEFAULT 0,
    last_error VARCHAR DEFAULT '',
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp
);
CREATE INDEX IF NOT EXISTS audit_outbox_order ON audit_outbox(occurred_at, id);

-- Custom OGC Tile Matrix Set definitions. Built-ins remain immutable code
-- definitions and are merged with these rows by the runtime registry.
CREATE TABLE IF NOT EXISTS tile_matrix_sets (
    id VARCHAR PRIMARY KEY,
    definition_json JSON NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    digest VARCHAR NOT NULL,
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp
);

-- WFS feature locks. Locks are short-lived (bounded by WFS.MaxLockExpirySec)
-- but must survive a server restart so held lockIds stay valid; expired rows
-- are pruned by the WFS runtime cleanup ticker.
CREATE TABLE IF NOT EXISTS wfs_locks (
    lock_id VARCHAR PRIMARY KEY,
    workspace_id VARCHAR NOT NULL,
    owner_id VARCHAR NOT NULL DEFAULT '',
    feature_ids JSON NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS wfs_locks_workspace ON wfs_locks(workspace_id);

-- WFS feature version metadata recorded by transactions. Only metadata is
-- kept (no feature content), so version navigation is not served from this
-- table yet; capabilities declare feature versioning FALSE until content
-- history exists.
CREATE TABLE IF NOT EXISTS wfs_feature_versions (
    workspace_id VARCHAR NOT NULL,
    layer_id VARCHAR NOT NULL,
    feature_id VARCHAR NOT NULL,
    version INTEGER NOT NULL,
    previous_rid VARCHAR DEFAULT '',
    state VARCHAR NOT NULL,
    modified_by VARCHAR DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (workspace_id, layer_id, feature_id, version)
);

-- Create sequence for casbin_rules id
CREATE SEQUENCE IF NOT EXISTS casbin_rules_id_seq;

`

// defaultRolesSQL inserts the built-in system roles.
const defaultRolesSQL = `
INSERT OR IGNORE INTO roles (id, name, description, is_system) VALUES
    ('super_admin', 'super_admin', 'Global admin - manage all workspaces', true),
    ('admin', 'admin', 'Workspace admin - manage the assigned workspace, including the console', true),
    ('editor', 'editor', 'Modify layers and styles via the API and WFS transactions (no console access)', true),
    ('viewer', 'viewer', 'Read-only access to published services and the API (no console access)', true);
`

// defaultCasbinPoliciesSQL inserts default Casbin policies for system roles.
const defaultCasbinPoliciesSQL = `
INSERT OR IGNORE INTO casbin_rules (id, ptype, v0, v1, v2, v3) VALUES
    (nextval('casbin_rules_id_seq'), 'p', 'super_admin', '*', '*', '*'),
    (nextval('casbin_rules_id_seq'), 'p', 'admin', '', '*', '*'),
    (nextval('casbin_rules_id_seq'), 'p', 'editor', '', '*', 'write'),
    (nextval('casbin_rules_id_seq'), 'p', 'viewer', '', '*', 'read');
`
