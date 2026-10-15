ALTER TABLE scan_tasks ADD COLUMN deep_max_discovery_runs INTEGER NOT NULL DEFAULT 0 CHECK (deep_max_discovery_runs BETWEEN 0 AND 1000);
ALTER TABLE scan_tasks ADD COLUMN deep_stop_after_no_new INTEGER NOT NULL DEFAULT 0 CHECK (deep_stop_after_no_new BETWEEN 0 AND 1000);
