CREATE TABLE sdk_identities (
 id BIGSERIAL PRIMARY KEY,
 name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
 agent_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('api_key','chatgpt')),
 generation BIGINT NOT NULL DEFAULT 1,
 state TEXT NOT NULL DEFAULT 'unconfigured',
 authenticated BOOLEAN,
 checked_at TEXT,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 cleanup_pending BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE TABLE sdk_identity_operations (
 id BIGSERIAL PRIMARY KEY,
 identity_id BIGINT NOT NULL REFERENCES sdk_identities(id),
 reference_json TEXT NOT NULL,
 action TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'queued',
 secret_encrypted TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 deadline TEXT NOT NULL,
 lease_until TEXT,
 lease_hash TEXT NOT NULL DEFAULT '',
 sequence BIGINT NOT NULL DEFAULT 0,
 report_hash TEXT NOT NULL DEFAULT '',
 instructions_json TEXT,
 error TEXT
);
CREATE UNIQUE INDEX sdk_identity_one_operation ON sdk_identity_operations(identity_id) WHERE status IN ('queued','running','waiting');
CREATE TABLE task_sdk_identities (
 task_id BIGINT PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE,
 identity_id BIGINT NOT NULL REFERENCES sdk_identities(id),
 reference_json TEXT NOT NULL
);
