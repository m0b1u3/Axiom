CREATE TABLE git_snapshots (
 snapshot_id BIGINT PRIMARY KEY REFERENCES source_snapshots(id) ON DELETE CASCADE,
 uploaded_archive_sha256 TEXT NOT NULL,
 target_json TEXT NOT NULL
);
CREATE TABLE task_git_targets (
 task_id BIGINT PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE,
 target_json TEXT NOT NULL
);
ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_input_contract_version_check;
ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_input_contract_version_check CHECK(input_contract_version IN (0,1,2,3,4));
