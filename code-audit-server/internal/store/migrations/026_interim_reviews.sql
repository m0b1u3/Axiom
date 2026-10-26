CREATE TABLE interim_review_groups (
 id BIGSERIAL PRIMARY KEY, source_hash TEXT NOT NULL, group_key TEXT NOT NULL,
 project_id BIGINT NOT NULL DEFAULT 0, created_at TEXT NOT NULL,
 UNIQUE(source_hash,group_key,project_id)
);
CREATE TABLE interim_review_sources (
 id BIGSERIAL PRIMARY KEY, group_id BIGINT NOT NULL REFERENCES interim_review_groups(id),
 origin_task_id BIGINT NOT NULL REFERENCES scan_tasks(id), origin_attempt INTEGER NOT NULL,
 input_json TEXT NOT NULL, input_sha256 TEXT NOT NULL, created_at TEXT NOT NULL,
 UNIQUE(group_id,input_sha256), FOREIGN KEY(origin_task_id,origin_attempt) REFERENCES scan_attempts(task_id,attempt)
);
CREATE TABLE interim_reviews (
 task_id BIGINT PRIMARY KEY REFERENCES scan_tasks(id), group_id BIGINT NOT NULL REFERENCES interim_review_groups(id),
 source_id BIGINT NOT NULL REFERENCES interim_review_sources(id), created_at TEXT NOT NULL
);
CREATE INDEX interim_review_group_tasks ON interim_reviews(group_id,task_id);
CREATE INDEX interim_review_origins ON interim_review_sources(origin_task_id,origin_attempt,group_id);
CREATE TABLE interim_review_requests (
 request_id TEXT PRIMARY KEY, input_sha256 TEXT NOT NULL,
 group_id BIGINT NOT NULL REFERENCES interim_review_groups(id), task_id BIGINT NOT NULL REFERENCES interim_reviews(task_id)
);
CREATE TABLE interim_review_archives (
 group_id BIGINT PRIMARY KEY REFERENCES interim_review_groups(id),
 review_task_id BIGINT NOT NULL UNIQUE REFERENCES interim_reviews(task_id), review_attempt INTEGER NOT NULL,
 finding_id BIGINT NOT NULL UNIQUE REFERENCES findings(id), document_json TEXT NOT NULL,
 archived_at TEXT NOT NULL, archived_by BIGINT NOT NULL REFERENCES users(id),
 FOREIGN KEY(review_task_id,review_attempt) REFERENCES scan_attempts(task_id,attempt)
);
