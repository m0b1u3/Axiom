ALTER TABLE model_configs ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT '';
ALTER TABLE scan_tasks ADD COLUMN sdk_parameters_json TEXT NOT NULL DEFAULT '';
