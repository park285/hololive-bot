CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observations_received
    ON source_observations (received_at, id);
