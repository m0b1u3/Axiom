CREATE TABLE IF NOT EXISTS component_plans (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 snapshot_id INTEGER NOT NULL REFERENCES source_snapshots(id),
 source_json TEXT NOT NULL,
 title TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1,
 document_json TEXT NOT NULL,
 origin TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS component_plan_edits (
 plan_id INTEGER NOT NULL REFERENCES component_plans(id),
 revision INTEGER NOT NULL,
 document_json TEXT NOT NULL,
 origin TEXT NOT NULL,
 actor_id INTEGER NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(plan_id,revision)
);
CREATE TABLE IF NOT EXISTS component_plan_versions (
 plan_id INTEGER NOT NULL REFERENCES component_plans(id),
 version INTEGER NOT NULL,
 revision INTEGER NOT NULL,
 document_json TEXT NOT NULL,
 PRIMARY KEY(plan_id,version),
 UNIQUE(plan_id,revision)
);
CREATE TABLE IF NOT EXISTS component_operations (
 task_id INTEGER PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE,
 plan_id INTEGER NOT NULL REFERENCES component_plans(id),
 revision INTEGER NOT NULL,
 kind TEXT NOT NULL,
 document_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS component_operations_plan ON component_operations(plan_id,task_id);
