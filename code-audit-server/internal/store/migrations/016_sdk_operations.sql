CREATE TABLE IF NOT EXISTS sdk_operations (
 task_id BIGINT PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE,
 origin_task_id BIGINT REFERENCES scan_tasks(id),
 kind TEXT NOT NULL CHECK(kind IN ('preflight','validate')),
 document_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS sdk_operations_origin ON sdk_operations(origin_task_id,task_id);

ALTER TABLE scan_tasks DROP CONSTRAINT scan_tasks_input_contract_version_check;
ALTER TABLE scan_tasks ADD CONSTRAINT scan_tasks_input_contract_version_check CHECK(input_contract_version IN (0,1,2,3));
