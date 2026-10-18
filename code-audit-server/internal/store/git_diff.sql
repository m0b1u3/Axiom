CREATE TABLE IF NOT EXISTS git_snapshots (snapshot_id BIGINT PRIMARY KEY REFERENCES source_snapshots(id) ON DELETE CASCADE, uploaded_archive_sha256 TEXT NOT NULL,target_json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS task_git_targets (task_id BIGINT PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE,target_json TEXT NOT NULL);
