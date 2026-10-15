-- Existing attempts retain their original format; historical metadata is not guessed.
ALTER TABLE scan_tasks ADD COLUMN result_contract_version INTEGER NOT NULL DEFAULT 0 CHECK(result_contract_version IN (0,1));
ALTER TABLE scan_attempts ADD COLUMN result_contract_version INTEGER NOT NULL DEFAULT 0 CHECK(result_contract_version IN (0,1));
ALTER TABLE execution_agents ADD COLUMN result_capabilities_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE findings ADD COLUMN normalized_json TEXT;
