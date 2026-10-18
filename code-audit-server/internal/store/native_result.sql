CREATE TABLE IF NOT EXISTS sdk_native_scans (
 task_id BIGINT NOT NULL, attempt INTEGER NOT NULL,
 sdk_scan_id TEXT NOT NULL, source_hash TEXT NOT NULL,
 PRIMARY KEY(task_id,attempt),
 FOREIGN KEY(task_id,attempt) REFERENCES scan_attempts(task_id,attempt) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sdk_native_scans_identity ON sdk_native_scans(sdk_scan_id,source_hash);
