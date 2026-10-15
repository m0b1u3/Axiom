CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    created_by BIGINT,
    created_at TEXT NOT NULL,
    FOREIGN KEY(created_by) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS source_snapshots (
    id BIGSERIAL PRIMARY KEY,
    original_filename TEXT NOT NULL,
    archive_sha256 TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    snapshot_path TEXT NOT NULL,
    file_count INTEGER NOT NULL,
    total_size BIGINT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_snapshots_archive_sha ON source_snapshots(archive_sha256);
CREATE INDEX IF NOT EXISTS idx_snapshots_created_at ON source_snapshots(created_at);

CREATE TABLE IF NOT EXISTS model_configs (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'custom',
    base_url TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL,
    api_key_encrypted TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    compatibility_status TEXT NOT NULL DEFAULT 'experimental',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS scan_tasks (
    id BIGSERIAL PRIMARY KEY,
    scan_no TEXT NOT NULL UNIQUE,
    source_path TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    base_url TEXT,
    scan_mode TEXT NOT NULL DEFAULT 'standard',
    include_paths TEXT,
    scan_focus TEXT,
    timeout_minutes INTEGER NOT NULL DEFAULT 60,
    status TEXT NOT NULL DEFAULT 'pending',
    current_stage TEXT,
    progress_json TEXT,
    agent_id TEXT,
    lease_expires_at TEXT,
    heartbeat_at TEXT,
    attempt INTEGER NOT NULL DEFAULT 0,
    finding_count INTEGER NOT NULL DEFAULT 0,
    error_code TEXT,
    error_message TEXT,
    output_path TEXT,
    source_snapshot_id BIGINT,
    model_config_id BIGINT,
    created_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    FOREIGN KEY(source_snapshot_id) REFERENCES source_snapshots(id)
);

CREATE INDEX IF NOT EXISTS idx_scan_tasks_status ON scan_tasks(status);

CREATE TABLE IF NOT EXISTS findings (
    id BIGSERIAL PRIMARY KEY,
    scan_id BIGINT NOT NULL,
    fingerprint TEXT NOT NULL,
    title TEXT NOT NULL,
    severity TEXT NOT NULL,
    category TEXT,
    cwe TEXT,
    file_path TEXT,
    start_line INTEGER,
    end_line INTEGER,
    description TEXT NOT NULL,
    evidence TEXT,
    remediation TEXT,
    confidence DOUBLE PRECISION,
    status TEXT NOT NULL DEFAULT 'open',
    raw TEXT,
    created_at TEXT NOT NULL,
    UNIQUE(scan_id, fingerprint),
    FOREIGN KEY(scan_id) REFERENCES scan_tasks(id)
);

ALTER TABLE source_snapshots ADD COLUMN IF NOT EXISTS project_id BIGINT REFERENCES projects(id);
ALTER TABLE scan_tasks ADD COLUMN IF NOT EXISTS project_id BIGINT REFERENCES projects(id);
ALTER TABLE model_configs ADD COLUMN IF NOT EXISTS enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1));
ALTER TABLE model_configs ADD COLUMN IF NOT EXISTS remark TEXT NOT NULL DEFAULT '';
ALTER TABLE model_configs ADD COLUMN IF NOT EXISTS website_url TEXT NOT NULL DEFAULT '';
ALTER TABLE model_configs ADD COLUMN IF NOT EXISTS wire_api TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS one_enabled_model ON model_configs(enabled) WHERE enabled=1;
CREATE INDEX IF NOT EXISTS scan_queue ON scan_tasks(status,id);
CREATE INDEX IF NOT EXISTS scan_leases ON scan_tasks(lease_expires_at) WHERE status IN ('claimed','scanning','cancel_requested');
CREATE INDEX IF NOT EXISTS scans_project ON scan_tasks(project_id,id);
CREATE INDEX IF NOT EXISTS finding_severity ON findings(severity,scan_id);
CREATE INDEX IF NOT EXISTS finding_fingerprint ON findings(fingerprint,id);
