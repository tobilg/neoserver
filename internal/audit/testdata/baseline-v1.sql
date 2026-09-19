-- Frozen 0.1.0 schema baseline. Do not update for later migrations.
CREATE TABLE IF NOT EXISTS audit_events (
	id VARCHAR PRIMARY KEY, occurred_at TIMESTAMP NOT NULL, request_id VARCHAR, principal VARCHAR,
	auth_method VARCHAR, workspace VARCHAR, protocol VARCHAR, operation VARCHAR, action VARCHAR,
	method VARCHAR, path VARCHAR, status INTEGER, duration_ms BIGINT, security_event BOOLEAN,
	credential_id VARCHAR DEFAULT ''
); CREATE INDEX IF NOT EXISTS audit_events_time ON audit_events(occurred_at);
CREATE INDEX IF NOT EXISTS audit_events_workspace ON audit_events(workspace,occurred_at);
CREATE TABLE audit_schema (version INTEGER PRIMARY KEY, applied_at TIMESTAMP DEFAULT current_timestamp);
INSERT INTO audit_schema(version) VALUES (1);
