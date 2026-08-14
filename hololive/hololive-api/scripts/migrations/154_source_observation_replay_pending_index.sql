CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_replay_pending
    ON source_observation_replay_requests (requested_at, id)
    WHERE status = 'PENDING';
