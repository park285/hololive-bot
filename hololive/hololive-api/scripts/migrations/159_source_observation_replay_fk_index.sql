CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_replay_observation_status
    ON source_observation_replay_requests (observation_id, status);
