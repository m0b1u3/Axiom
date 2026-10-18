CREATE TABLE IF NOT EXISTS knowledge_documents (
 id TEXT PRIMARY KEY, title TEXT NOT NULL, version INTEGER NOT NULL,
 archived INTEGER NOT NULL DEFAULT 0 CHECK(archived IN (0,1)), created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS knowledge_versions (
 id INTEGER PRIMARY KEY AUTOINCREMENT, document_id TEXT NOT NULL REFERENCES knowledge_documents(id),
 version INTEGER NOT NULL, title TEXT NOT NULL, content TEXT NOT NULL, sha256 TEXT NOT NULL, created_at TEXT NOT NULL,
 UNIQUE(document_id,version)
);
CREATE TABLE IF NOT EXISTS task_scan_inputs (
 task_id INTEGER PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE, document_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS task_parent_states (
 task_id INTEGER PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE, parent_task_id BIGINT NOT NULL,parent_attempt INTEGER NOT NULL,document_json TEXT NOT NULL
);
