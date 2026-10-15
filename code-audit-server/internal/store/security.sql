CREATE TABLE IF NOT EXISTS auth_sessions (
 id TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 auth_version INTEGER NOT NULL, token_hash TEXT NOT NULL,
 created_at TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS auth_sessions_user ON auth_sessions(user_id,expires_at);
CREATE TABLE IF NOT EXISTS login_limits (
 bucket TEXT PRIMARY KEY, window_start TEXT NOT NULL, attempts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS login_limits_window ON login_limits(window_start);
CREATE TABLE IF NOT EXISTS operation_audits (
 id TEXT PRIMARY KEY, actor_id INTEGER, actor_name TEXT NOT NULL DEFAULT '',
 action TEXT NOT NULL, resource TEXT NOT NULL, resource_id TEXT NOT NULL DEFAULT '',
 outcome TEXT NOT NULL, occurred_at TEXT NOT NULL, completed_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS operation_audits_time ON operation_audits(occurred_at,id);
