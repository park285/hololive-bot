CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observations_subject_time
    ON source_observations (observation_kind, subject_key, scheduled_for DESC, id DESC);
