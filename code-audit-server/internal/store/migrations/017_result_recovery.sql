-- Recovery imports an existing committed result. It never starts a model or
-- changes the identity of an attempt. Keep the superseded failure as evidence.
CREATE TABLE scan_result_recoveries (
 task_id BIGINT NOT NULL REFERENCES scan_tasks(id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL CHECK(attempt > 0),
 commit_sha256 TEXT NOT NULL,
 document_json TEXT NOT NULL,
 previous_completion_json TEXT,
 previous_error_message TEXT NOT NULL,
 PRIMARY KEY(task_id,attempt),
 FOREIGN KEY(task_id,attempt) REFERENCES scan_attempts(task_id,attempt) ON DELETE CASCADE
);
