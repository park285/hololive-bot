CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observations_kind_id
    ON source_observations (observation_kind, id);
