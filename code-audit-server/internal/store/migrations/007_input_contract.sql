-- Existing tasks retain legacy input and an unset budget. No guessed backfill.
ALTER TABLE scan_tasks ADD COLUMN max_cost_usd DOUBLE PRECISION CHECK(max_cost_usd > 0 AND max_cost_usd <= 9007199254740991);
ALTER TABLE scan_tasks ADD COLUMN workers INTEGER CHECK(workers BETWEEN 1 AND 64);
ALTER TABLE scan_tasks ADD COLUMN input_contract_version INTEGER NOT NULL DEFAULT 0 CHECK(input_contract_version IN (0,1));
ALTER TABLE scan_tasks ADD COLUMN source_content_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE scan_tasks ADD COLUMN model_snapshot_json TEXT;
ALTER TABLE scan_tasks ADD COLUMN policy_snapshot_json TEXT;
ALTER TABLE scan_tasks ADD COLUMN credential_snapshot_encrypted TEXT;
ALTER TABLE execution_agents ADD COLUMN input_capabilities_json TEXT NOT NULL DEFAULT '{}';
