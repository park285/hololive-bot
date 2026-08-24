CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_applications_orphaned_kind_applied_id
    ON source_observation_applications (observation_kind, applied_at, id)
    WHERE observation_id IS NULL;
