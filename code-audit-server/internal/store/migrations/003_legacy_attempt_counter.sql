-- Legacy canceled-before-start records may have attempt=0 while their retained
-- history was imported as attempt=1. New claims must always follow history.
UPDATE scan_tasks t SET attempt=a.latest
FROM (SELECT task_id,MAX(attempt) AS latest FROM scan_attempts GROUP BY task_id) a
WHERE t.id=a.task_id AND t.attempt<a.latest;
