CREATE TABLE IF NOT EXISTS component_scans (
 task_id INTEGER PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE,
 plan_id INTEGER NOT NULL,
 version INTEGER NOT NULL,
 workers INTEGER NOT NULL CHECK(workers BETWEEN 1 AND 9007199254740991),
 document_json TEXT NOT NULL,
 FOREIGN KEY(plan_id,version) REFERENCES component_plan_versions(plan_id,version)
);
CREATE TABLE IF NOT EXISTS component_scan_attempts (
 task_id INTEGER NOT NULL REFERENCES component_scans(task_id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL,
 request_json TEXT NOT NULL,
 snapshot_json TEXT,
 PRIMARY KEY(task_id,attempt)
);
CREATE TABLE IF NOT EXISTS component_scan_receipts (
 task_id INTEGER NOT NULL,
 attempt INTEGER NOT NULL,
 component_index INTEGER NOT NULL,
 document_json TEXT NOT NULL,
 PRIMARY KEY(task_id,attempt,component_index),
 FOREIGN KEY(task_id,attempt) REFERENCES component_scan_attempts(task_id,attempt) ON DELETE CASCADE
);
