-- Widen only numeric domains. Rows, attempts, immutable inputs and reports stay
-- intact. SDK 0.1.25 uses JavaScript safe integers; no platform worker ceiling.
ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_workers_check;
ALTER TABLE scan_tasks ALTER COLUMN workers TYPE BIGINT;
ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_workers_check CHECK(workers BETWEEN 1 AND 9007199254740991);
ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_deep_max_discovery_runs_check;
ALTER TABLE scan_tasks ALTER COLUMN deep_max_discovery_runs TYPE BIGINT;
ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_deep_max_discovery_runs_check CHECK(deep_max_discovery_runs BETWEEN 0 AND 9007199254740991);
ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_deep_stop_after_no_new_check;
ALTER TABLE scan_tasks ALTER COLUMN deep_stop_after_no_new TYPE BIGINT;
ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_deep_stop_after_no_new_check CHECK(deep_stop_after_no_new BETWEEN 0 AND 9007199254740991);
ALTER TABLE component_scans DROP CONSTRAINT component_scans_workers_check;
ALTER TABLE component_scans ALTER COLUMN workers TYPE BIGINT;
ALTER TABLE component_scans ADD CONSTRAINT component_scans_workers_check CHECK(workers BETWEEN 1 AND 9007199254740991);
-- Legacy presence is diagnostic metadata; it must not retain a hidden 64 ceiling.
ALTER TABLE execution_agents DROP CONSTRAINT execution_agents_slots_check;
ALTER TABLE execution_agents ALTER COLUMN slots TYPE BIGINT;
ALTER TABLE execution_agents ADD CONSTRAINT execution_agents_slots_check CHECK(slots BETWEEN 0 AND 9007199254740991);
