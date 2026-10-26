CREATE TABLE IF NOT EXISTS deep_resume_checks (
 id TEXT PRIMARY KEY, task_id BIGINT NOT NULL REFERENCES scan_tasks(id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL, origin_attempt INTEGER NOT NULL, agent_id TEXT NOT NULL,
 configuration_hash TEXT NOT NULL, source_hash TEXT NOT NULL, state TEXT NOT NULL,
 lease_hash TEXT NOT NULL DEFAULT '', lease_until TEXT NOT NULL DEFAULT '',
 inspection_json TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS deep_resume_check_task ON deep_resume_checks(task_id,attempt,created_at);
CREATE TABLE IF NOT EXISTS deep_resume_runs (
 task_id BIGINT NOT NULL REFERENCES scan_tasks(id) ON DELETE CASCADE, attempt INTEGER NOT NULL,
 previous_attempt INTEGER NOT NULL, origin_attempt INTEGER NOT NULL, agent_id TEXT NOT NULL,
 probe_id TEXT NOT NULL REFERENCES deep_resume_checks(id), document_json TEXT NOT NULL,
 status TEXT NOT NULL, PRIMARY KEY(task_id,attempt)
);
