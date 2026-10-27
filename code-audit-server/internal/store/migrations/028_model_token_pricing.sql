ALTER TABLE model_configs ADD COLUMN pricing_json TEXT NOT NULL DEFAULT '';
ALTER TABLE model_configs ADD COLUMN pricing_revision BIGINT NOT NULL DEFAULT 0 CHECK(pricing_revision>=0);
ALTER TABLE task_cost_attempts DROP CONSTRAINT task_cost_attempts_source_check;
ALTER TABLE task_cost_attempts ADD CONSTRAINT task_cost_attempts_source_check
 CHECK(source IN ('none','sdk_progress','sdk_result','manual_estimate','configured_pricing'));
CREATE TABLE task_token_costs (
 task_id BIGINT NOT NULL REFERENCES scan_tasks(id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL CHECK(attempt>0),
 pricing_snapshot_json TEXT NOT NULL,
 usage_json TEXT NOT NULL DEFAULT '',
 estimate_json TEXT NOT NULL,
 progress_revision BIGINT NOT NULL DEFAULT 0 CHECK(progress_revision>=0),
 PRIMARY KEY(task_id,attempt)
);
