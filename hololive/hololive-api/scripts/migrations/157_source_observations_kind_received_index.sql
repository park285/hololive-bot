CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observations_kind_received_id
    ON source_observations (observation_kind, received_at, id);

