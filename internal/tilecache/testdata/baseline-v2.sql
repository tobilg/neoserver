-- Frozen 0.1.0 schema baseline. Do not update for later migrations.

CREATE TABLE IF NOT EXISTS cache_schema (
 version INTEGER PRIMARY KEY,
 applied_at TIMESTAMP DEFAULT current_timestamp
);
CREATE TABLE IF NOT EXISTS tile_entries (
 cache_key TEXT PRIMARY KEY,
 object_key TEXT NOT NULL UNIQUE,
 workspace_id TEXT NOT NULL,
	workspace_revision BIGINT NOT NULL,
 resource_id TEXT NOT NULL,
 resource_kind TEXT NOT NULL,
 generation BIGINT NOT NULL,
 tile_type TEXT NOT NULL,
 matrix_set TEXT NOT NULL,
 zoom INTEGER NOT NULL,
 tile_col INTEGER NOT NULL,
 tile_row INTEGER NOT NULL,
 style_digest TEXT NOT NULL,
 style_name TEXT NOT NULL DEFAULT '',
 format TEXT NOT NULL,
 size_bytes BIGINT NOT NULL DEFAULT 0,
 etag TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL,
 created_at BIGINT NOT NULL,
 last_accessed_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS tile_entries_global_lru ON tile_entries(state, last_accessed_at);
CREATE INDEX IF NOT EXISTS tile_entries_workspace_lru ON tile_entries(workspace_id, state, last_accessed_at);
CREATE INDEX IF NOT EXISTS tile_entries_resource_lru ON tile_entries(workspace_id, resource_id, state, last_accessed_at);
CREATE INDEX IF NOT EXISTS tile_entries_selector ON tile_entries(workspace_id, resource_id, tile_type, matrix_set, zoom, format);
CREATE TABLE IF NOT EXISTS cache_usage (
 scope_type TEXT NOT NULL,
 scope_id TEXT NOT NULL,
 size_bytes BIGINT NOT NULL DEFAULT 0,
 entry_count BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(scope_type, scope_id)
);
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
CREATE INDEX IF NOT EXISTS tile_cache_jobs_workspace_created ON tile_cache_jobs(workspace_id, created_at);
CREATE INDEX IF NOT EXISTS tile_cache_jobs_status_created ON tile_cache_jobs(status, created_at);
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
INSERT INTO cache_schema(version) VALUES (2);
