-- Format is fixed per task/attempt; old histories keep their legacy reader.
ALTER TABLE scan_tasks ADD COLUMN observation_contract_version INTEGER NOT NULL DEFAULT 0 CHECK(observation_contract_version IN (0,1));
ALTER TABLE scan_attempts ADD COLUMN observation_contract_version INTEGER NOT NULL DEFAULT 0 CHECK(observation_contract_version IN (0,1));
ALTER TABLE execution_agents ADD COLUMN observation_capabilities_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE scan_tasks ADD COLUMN progress_diagnostic_json TEXT NOT NULL DEFAULT '';
ALTER TABLE scan_attempts ADD COLUMN progress_diagnostic_json TEXT NOT NULL DEFAULT '';
