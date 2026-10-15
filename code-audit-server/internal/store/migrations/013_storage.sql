-- Opt-in cleanup intent and policy. No historical rows or files are removed.
CREATE TABLE storage_settings (
 id INTEGER PRIMARY KEY CHECK(id=1), revision BIGINT NOT NULL,
 min_free_bytes BIGINT NOT NULL CHECK(min_free_bytes BETWEEN 0 AND 9007199254740991),
 orphan_grace_hours INTEGER NOT NULL CHECK(orphan_grace_hours BETWEEN 1 AND 8760)
);
CREATE TABLE storage_cleanup_jobs (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('orphans','scan','snapshot')),
 resource_id BIGINT, state TEXT NOT NULL CHECK(state IN ('preview','running','failed','completed','canceled')),
 document_json TEXT NOT NULL, roots_json TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX storage_cleanup_resource ON storage_cleanup_jobs(kind,resource_id,state);
