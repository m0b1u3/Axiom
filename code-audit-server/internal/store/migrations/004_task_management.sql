ALTER TABLE scan_tasks ADD COLUMN queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
UPDATE scan_tasks SET queued_at=created_at::timestamptz;
CREATE INDEX scan_queue_order ON scan_tasks(queued_at,id) WHERE status='pending';
ALTER TABLE scan_attempts ADD COLUMN queued_at TIMESTAMPTZ;
UPDATE scan_attempts a SET queued_at=t.created_at::timestamptz FROM scan_tasks t WHERE t.id=a.task_id AND a.attempt=1;
CREATE TABLE execution_settings (
 id INTEGER PRIMARY KEY CHECK(id=1),
 global_limit INTEGER NOT NULL CHECK(global_limit BETWEEN 1 AND 64),
 model_limit INTEGER NOT NULL CHECK(model_limit BETWEEN 1 AND 64),
 project_limit INTEGER NOT NULL CHECK(project_limit BETWEEN 1 AND 64),
 revision INTEGER NOT NULL DEFAULT 1,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_by TEXT NOT NULL DEFAULT 'environment'
);
CREATE TABLE execution_setting_changes (
 revision INTEGER PRIMARY KEY, global_limit INTEGER NOT NULL, model_limit INTEGER NOT NULL,
 project_limit INTEGER NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_by TEXT NOT NULL
);
CREATE TABLE execution_agents (
 agent_id TEXT PRIMARY KEY, slots INTEGER NOT NULL CHECK(slots BETWEEN 1 AND 64),
 last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Preserve the latest legacy result before a retry can clear the current findings.
-- Older overwritten results cannot be reconstructed and remain explicitly unavailable.
UPDATE scan_attempts a SET result_json=jsonb_build_object('status',a.status,'errorCode',a.error_code,'errorMessage',a.error_message,'findings',COALESCE((
 SELECT jsonb_agg(jsonb_build_object('id',f.id,'scanId',f.scan_id,'fingerprint',f.fingerprint,'title',f.title,'severity',f.severity,'category',f.category,'cwe',f.cwe::jsonb,'filePath',f.file_path,'startLine',f.start_line,'endLine',f.end_line,'description',f.description,'evidence',f.evidence,'remediation',f.remediation,'confidence',f.confidence,'status',f.status,'raw',NULLIF(f.raw,'')::jsonb)) FROM findings f WHERE f.scan_id=a.task_id
),'[]'::jsonb))::text FROM scan_tasks t
WHERE a.task_id=t.id AND a.attempt=t.attempt AND a.result_json IS NULL AND t.status IN ('completed','partial','failed','canceled','timeout');
