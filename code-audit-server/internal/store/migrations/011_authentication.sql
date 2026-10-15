-- Additive account/session governance. Existing account IDs and credentials stay.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'operator' CHECK(role IN ('admin','operator','viewer'));
ALTER TABLE users ADD COLUMN active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1));
ALTER TABLE users ADD COLUMN auth_version BIGINT NOT NULL DEFAULT 0;
CREATE TABLE auth_sessions (
 id TEXT PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 auth_version BIGINT NOT NULL, token_hash TEXT NOT NULL,
 created_at TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX auth_sessions_user ON auth_sessions(user_id,expires_at);
CREATE TABLE login_limits (
 bucket TEXT PRIMARY KEY, window_start TEXT NOT NULL, attempts INTEGER NOT NULL
);
CREATE INDEX login_limits_window ON login_limits(window_start);
CREATE TABLE operation_audits (
 id TEXT PRIMARY KEY, actor_id BIGINT, actor_name TEXT NOT NULL DEFAULT '',
 action TEXT NOT NULL, resource TEXT NOT NULL, resource_id TEXT NOT NULL DEFAULT '',
 outcome TEXT NOT NULL, occurred_at TEXT NOT NULL, completed_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX operation_audits_time ON operation_audits(occurred_at,id);
