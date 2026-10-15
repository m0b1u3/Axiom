-- Task-level budgeting is opt-in metadata. Existing tasks are not backfilled.
CREATE TABLE task_budgets (
 task_id BIGINT PRIMARY KEY REFERENCES scan_tasks(id) ON DELETE CASCADE,
 total_micros BIGINT CHECK(total_micros BETWEEN 1 AND 9007199254740991),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>=1)
);
CREATE TABLE task_cost_attempts (
 task_id BIGINT NOT NULL REFERENCES task_budgets(task_id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL CHECK(attempt>0),
 granted_micros BIGINT CHECK(granted_micros BETWEEN 0 AND 9007199254740991), booked_micros BIGINT CHECK(booked_micros BETWEEN 0 AND 9007199254740991), observed_micros BIGINT CHECK(observed_micros BETWEEN 0 AND 9007199254740991), final_micros BIGINT CHECK(final_micros BETWEEN 0 AND 9007199254740991),
 state TEXT NOT NULL CHECK(state IN ('active','complete','unknown')),
 source TEXT NOT NULL DEFAULT 'none' CHECK(source IN ('none','sdk_progress','sdk_result','manual_estimate')), progress_revision BIGINT NOT NULL DEFAULT 0 CHECK(progress_revision>=0),
 reason TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL,
 note TEXT NOT NULL DEFAULT '', updated_by TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(task_id,attempt)
);
