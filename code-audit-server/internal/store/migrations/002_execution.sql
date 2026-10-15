CREATE TABLE scan_attempts (
 task_id BIGINT NOT NULL REFERENCES scan_tasks(id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL CHECK(attempt>0),
 agent_id TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('claimed','scanning','completed','partial','failed','canceled','timeout','abandoned')),
 lease_expires_at TEXT,
 started_at TEXT NOT NULL,
 finished_at TEXT,
 output_path TEXT NOT NULL DEFAULT '',
 progress_json TEXT NOT NULL DEFAULT '',
 error_code TEXT NOT NULL DEFAULT '',
 error_message TEXT NOT NULL DEFAULT '',
 result_json TEXT,
 completion_hash TEXT,
 PRIMARY KEY(task_id,attempt)
);
CREATE INDEX attempts_status ON scan_attempts(status,lease_expires_at);
ALTER TABLE scan_tasks ADD COLUMN automatic_retries INTEGER NOT NULL DEFAULT 0;
-- Preserve the latest available legacy run. Earlier overwritten runs cannot be reconstructed.
INSERT INTO scan_attempts(task_id,attempt,agent_id,status,lease_expires_at,started_at,finished_at,output_path,progress_json,error_code,error_message)
SELECT id,GREATEST(attempt,1),COALESCE(agent_id,'legacy'),CASE WHEN status='cancel_requested' THEN 'scanning' ELSE status END,lease_expires_at,COALESCE(started_at,created_at),finished_at,COALESCE(output_path,''),COALESCE(progress_json,''),COALESCE(error_code,''),COALESCE(error_message,'') FROM scan_tasks WHERE status<>'pending';
