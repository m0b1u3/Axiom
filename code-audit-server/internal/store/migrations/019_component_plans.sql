CREATE TABLE component_plans (
 id BIGSERIAL PRIMARY KEY,
 snapshot_id BIGINT NOT NULL REFERENCES source_snapshots(id),
 source_json TEXT NOT NULL,
 title TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1,
 document_json TEXT NOT NULL,
 origin TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE component_plan_edits (
 plan_id BIGINT NOT NULL REFERENCES component_plans(id),
 revision INTEGER NOT NULL,
 document_json TEXT NOT NULL,
 origin TEXT NOT NULL,
 actor_id BIGINT NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(plan_id,revision)
);
CREATE TABLE component_plan_versions (
 plan_id BIGINT NOT NULL REFERENCES component_plans(id),
 version INTEGER NOT NULL,
 revision INTEGER NOT NULL,
 document_json TEXT NOT NULL,
 PRIMARY KEY(plan_id,version),
 UNIQUE(plan_id,revision)
);
CREATE TABLE component_operations (
 task_id BIGINT PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE,
 plan_id BIGINT NOT NULL REFERENCES component_plans(id),
 revision INTEGER NOT NULL,
 kind TEXT NOT NULL,
 document_json TEXT NOT NULL
);
CREATE INDEX component_operations_plan ON component_operations(plan_id,task_id);
ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_input_contract_version_check;
ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_input_contract_version_check CHECK(input_contract_version IN (0,1,2,3,4,5));
