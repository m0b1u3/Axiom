-- Legacy "supported" did not record whether it came from ping or an SDK run.
-- Restore unknown compatibility once; preserve configuration, keys and history.
UPDATE model_configs SET compatibility_status = 'experimental'
WHERE compatibility_status = 'supported';
