// Package store provides an encrypted DuckDB backing store for neoserver configuration.
package store

// schemaVersion tracks the current schema version for migrations.
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

// migrationV2SQL contains the migration from schema version 1 to 2.
// Adds workspace settings columns and styles table.
const migrationV2SQL = `
-- Add settings columns to workspaces table
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS wms_settings JSON DEFAULT '{"enabled":false}';
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS wfs_settings JSON DEFAULT '{"enabled":false}';
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS ogcapi_settings JSON DEFAULT '{"enabled":true}';

-- Create styles table
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

-- Update schema version
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (2, current_timestamp);
`

// migrationV3SQL contains the migration from schema version 2 to 3.
// Adds dimensions column to layers table for WMS time/elevation support.
const migrationV3SQL = `
-- Add dimensions column to layers table
ALTER TABLE layers ADD COLUMN IF NOT EXISTS dimensions JSON DEFAULT '[]';

-- Update schema version
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (3, current_timestamp);
`

// migrationV4SQL contains the migration from schema version 3 to 4.
// Adds wfs_stored_queries table for WFS 2.0 stored query persistence.
const migrationV4SQL = `
-- Create WFS stored queries table
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

-- Update schema version
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (4, current_timestamp);
`

// migrationV5SQL contains the migration from schema version 4 to 5.
// Adds SQL View support fields to the layers table.
const migrationV5SQL = `
-- Add SQL View columns to layers table
ALTER TABLE layers ADD COLUMN IF NOT EXISTS is_sql_view BOOLEAN DEFAULT false;
ALTER TABLE layers ADD COLUMN IF NOT EXISTS sql_view_config JSON DEFAULT NULL;

-- Update schema version
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (5, current_timestamp);
`

// migrationV6SQL contains the migration from schema version 5 to 6.
// Adds OGC API Tiles settings column to workspaces table.
const migrationV6SQL = `
-- Add OGC API Tiles settings column to workspaces table
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS ogc_tiles_api_settings JSON DEFAULT '{"enabled":false,"public":false,"versions":["1.0.0"],"settings":{"tile_matrix_sets":["WebMercatorQuad","WorldCRS84Quad"],"vector_tiles":{"enabled":true,"formats":["application/vnd.mapbox-vector-tile"]},"map_tiles":{"enabled":true,"formats":["image/png","image/jpeg","image/webp"]},"cache_enabled":true}}';

-- Update schema version
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (6, current_timestamp);
`

// migrationV7SQL persists per-service cache overrides.
const migrationV7SQL = `
ALTER TABLE services ADD COLUMN IF NOT EXISTS cache_settings JSON DEFAULT NULL;
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (7, current_timestamp);
`

// migrationV8SQL adds per-layer read-access controls.
// Existing layers default to public=false with an empty allowed_roles list,
// which preserves prior behavior: any principal with workspace access can read them.
const migrationV8SQL = `
ALTER TABLE layers ADD COLUMN IF NOT EXISTS public BOOLEAN DEFAULT false;
ALTER TABLE layers ADD COLUMN IF NOT EXISTS allowed_roles JSON DEFAULT '[]';
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (8, current_timestamp);
`

// migrationV9SQL adds WCS settings and independently managed coverages.
const migrationV9SQL = `
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS wcs_settings JSON DEFAULT '{"enabled":false,"public":false}';
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
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(service_id, public_id),
	UNIQUE(service_id, source_coverage),
	UNIQUE(workspace_id, public_id)
);
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (9, current_timestamp);
`

// migrationV10SQL adds common portrayal metadata to feature layers and
// coverages. Empty style bindings preserve the existing built-in defaults.
const migrationV10SQL = `
ALTER TABLE layers ADD COLUMN IF NOT EXISTS default_style VARCHAR DEFAULT '';
ALTER TABLE layers ADD COLUMN IF NOT EXISTS styles JSON DEFAULT '[]';
ALTER TABLE coverages ADD COLUMN IF NOT EXISTS default_style VARCHAR DEFAULT '';
ALTER TABLE coverages ADD COLUMN IF NOT EXISTS styles JSON DEFAULT '[]';
ALTER TABLE coverages ADD COLUMN IF NOT EXISTS resampling VARCHAR DEFAULT 'bilinear';
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (10, current_timestamp);
`

// migrationV11SQL adds WMTS settings and the original persistent-cache job
// tables. V12 removes those tables after moving their ownership out of the
// catalog. Existing protocol behavior remains disabled until explicitly enabled.
const migrationV11SQL = `
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS wmts_settings JSON DEFAULT '{"enabled":false,"public":false,"feature_info_enabled":true}';
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS tile_revision BIGINT DEFAULT 1;
ALTER TABLE layers ADD COLUMN IF NOT EXISTS native_extent JSON DEFAULT NULL;
ALTER TABLE layers ADD COLUMN IF NOT EXISTS tile_cache_quota_bytes BIGINT DEFAULT 0;
ALTER TABLE layers ADD COLUMN IF NOT EXISTS tile_cache_generation BIGINT DEFAULT 1;
ALTER TABLE coverages ADD COLUMN IF NOT EXISTS native_extent JSON DEFAULT NULL;
ALTER TABLE coverages ADD COLUMN IF NOT EXISTS tile_cache_quota_bytes BIGINT DEFAULT 0;
ALTER TABLE coverages ADD COLUMN IF NOT EXISTS tile_cache_generation BIGINT DEFAULT 1;

CREATE TABLE IF NOT EXISTS tile_cache_jobs (
    id VARCHAR PRIMARY KEY,
    workspace_id VARCHAR NOT NULL,
    request_json JSON NOT NULL,
    status VARCHAR NOT NULL,
    total_tiles BIGINT DEFAULT 0,
    processed_tiles BIGINT DEFAULT 0,
    succeeded_tiles BIGINT DEFAULT 0,
    skipped_tiles BIGINT DEFAULT 0,
    failed_tiles BIGINT DEFAULT 0,
    bytes_written BIGINT DEFAULT 0,
    bytes_deleted BIGINT DEFAULT 0,
    cancel_requested BOOLEAN DEFAULT false,
    error_message VARCHAR DEFAULT '',
    created_by VARCHAR DEFAULT '',
    created_at TIMESTAMP DEFAULT current_timestamp,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    updated_at TIMESTAMP DEFAULT current_timestamp
);

CREATE TABLE IF NOT EXISTS tile_cache_job_chunks (
    id VARCHAR PRIMARY KEY,
    job_id VARCHAR NOT NULL,
    zoom INTEGER NOT NULL,
    min_col INTEGER NOT NULL,
    max_col INTEGER NOT NULL,
    min_row INTEGER NOT NULL,
    max_row INTEGER NOT NULL,
    next_offset BIGINT DEFAULT 0,
    status VARCHAR NOT NULL DEFAULT 'queued',
    attempts INTEGER DEFAULT 0,
    last_error VARCHAR DEFAULT '',
    updated_at TIMESTAMP DEFAULT current_timestamp
);

INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (11, current_timestamp);
`

// migrationV12SQL moves durable cache jobs into the standalone tile-cache
// database. Cache state is intentionally not retained in the catalog.
const migrationV12SQL = `
DROP TABLE IF EXISTS tile_cache_job_chunks;
DROP TABLE IF EXISTS tile_cache_jobs;
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (12, current_timestamp);
`

const migrationV13SQL = `
ALTER TABLE coverages ADD COLUMN IF NOT EXISTS dimensions JSON DEFAULT '[]';
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
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp,
    UNIQUE(workspace_id, public_id)
);
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (13, current_timestamp);
`

const migrationV14SQL = `
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
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (14, current_timestamp);
`

const migrationV15SQL = `
ALTER TABLE coverages ADD COLUMN IF NOT EXISTS wcs20_coverage_subtype VARCHAR DEFAULT 'RectifiedGridCoverage';
UPDATE coverages SET wcs20_coverage_subtype = 'RectifiedGridCoverage'
WHERE wcs20_coverage_subtype IS NULL OR trim(wcs20_coverage_subtype) = '';
ALTER TABLE coverages ALTER COLUMN wcs20_coverage_subtype SET DEFAULT 'RectifiedGridCoverage';
ALTER TABLE coverages ALTER COLUMN wcs20_coverage_subtype SET NOT NULL;
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (15, current_timestamp);
`

const migrationV16SQL = `
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
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (16, current_timestamp);
`

const migrationV17SQL = `
CREATE TABLE IF NOT EXISTS wfs_locks (
    lock_id VARCHAR PRIMARY KEY,
    workspace_id VARCHAR NOT NULL,
    owner_id VARCHAR NOT NULL DEFAULT '',
    feature_ids JSON NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS wfs_locks_workspace ON wfs_locks(workspace_id);
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
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (17, current_timestamp);
`

const migrationV18SQL = `
ALTER TABLE layers ADD COLUMN IF NOT EXISTS tile_cache_parameters JSON DEFAULT NULL;
ALTER TABLE coverages ADD COLUMN IF NOT EXISTS tile_cache_parameters JSON DEFAULT NULL;
ALTER TABLE layer_groups ADD COLUMN IF NOT EXISTS tile_cache_parameters JSON DEFAULT NULL;
CREATE TABLE IF NOT EXISTS import_jobs (
    id VARCHAR PRIMARY KEY,
    workspace_id VARCHAR NOT NULL,
    name VARCHAR NOT NULL,
    source_kind VARCHAR NOT NULL,
    source_locator VARCHAR DEFAULT '',
    source_path VARCHAR NOT NULL,
    source_filename VARCHAR DEFAULT '',
    status VARCHAR NOT NULL,
    phase VARCHAR NOT NULL,
    plan_json JSON,
    discovery_json JSON,
    service_id VARCHAR,
    managed_path VARCHAR,
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
CREATE TABLE IF NOT EXISTS managed_assets (
    import_id VARCHAR PRIMARY KEY,
    workspace_id VARCHAR NOT NULL,
    service_id VARCHAR UNIQUE,
    path VARCHAR NOT NULL UNIQUE,
    encryption_key VARCHAR NOT NULL,
    created_at TIMESTAMP DEFAULT current_timestamp
);
CREATE TABLE IF NOT EXISTS tile_matrix_sets (
    id VARCHAR PRIMARY KEY,
    definition_json JSON NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    digest VARCHAR NOT NULL,
    created_at TIMESTAMP DEFAULT current_timestamp,
    updated_at TIMESTAMP DEFAULT current_timestamp
);
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (18, current_timestamp);
`

const migrationV19SQL = `
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS rollback_operation_id VARCHAR;
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
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (19, current_timestamp);
`

const migrationV24SQL = `
DROP INDEX IF EXISTS import_jobs_workspace;
DROP INDEX IF EXISTS import_jobs_status;
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS source_relative_path VARCHAR DEFAULT '';
CREATE INDEX IF NOT EXISTS import_jobs_workspace ON import_jobs(workspace_id, created_at);
CREATE INDEX IF NOT EXISTS import_jobs_status ON import_jobs(status, updated_at);
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (24, current_timestamp);
`

const migrationV25SQL = `
ALTER TABLE browser_sessions ADD COLUMN IF NOT EXISTS previous_token_hash VARCHAR DEFAULT '';
ALTER TABLE browser_sessions ADD COLUMN IF NOT EXISTS previous_csrf_hash VARCHAR DEFAULT '';
ALTER TABLE browser_sessions ADD COLUMN IF NOT EXISTS previous_valid_until TIMESTAMP;
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (25, current_timestamp);
`

const migrationV20SQL = `
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
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (20, current_timestamp);
`

const migrationV22SQL = `
CREATE TABLE IF NOT EXISTS retired_roles (id VARCHAR PRIMARY KEY, retired_at TIMESTAMP DEFAULT current_timestamp);
INSERT INTO retired_roles (id)
SELECT DISTINCT v0 FROM casbin_rules WHERE v0 IS NOT NULL AND v0 NOT IN (SELECT id FROM roles)
ON CONFLICT DO NOTHING;
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (22, current_timestamp);
`

const migrationV21SQL = `
CREATE TABLE IF NOT EXISTS browser_sessions (
    id VARCHAR PRIMARY KEY,
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
INSERT OR REPLACE INTO schema_info (version, applied_at) VALUES (21, current_timestamp);
`
