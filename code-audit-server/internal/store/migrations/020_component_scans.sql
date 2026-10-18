CREATE TABLE component_scans (
 task_id BIGINT PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE,
 plan_id BIGINT NOT NULL,
 version INTEGER NOT NULL,
 workers INTEGER NOT NULL CHECK(workers BETWEEN 1 AND 8),
 document_json TEXT NOT NULL,
 FOREIGN KEY(plan_id,version) REFERENCES component_plan_versions(plan_id,version)
);
CREATE TABLE component_scan_attempts (
 task_id BIGINT NOT NULL REFERENCES component_scans(task_id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL,
 request_json TEXT NOT NULL,
 snapshot_json TEXT,
 PRIMARY KEY(task_id,attempt)
);
CREATE TABLE component_scan_receipts (
 task_id BIGINT NOT NULL,
 attempt INTEGER NOT NULL,
 component_index INTEGER NOT NULL,
 document_json TEXT NOT NULL,
 PRIMARY KEY(task_id,attempt,component_index),
 FOREIGN KEY(task_id,attempt) REFERENCES component_scan_attempts(task_id,attempt) ON DELETE CASCADE
);
ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_input_contract_version_check;
ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_input_contract_version_check CHECK(input_contract_version IN (0,1,2,3,4,5,6));
