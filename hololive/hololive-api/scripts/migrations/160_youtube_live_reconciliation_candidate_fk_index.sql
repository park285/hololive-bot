CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_live_reconciliation_end_candidate
    ON youtube_live_reconciliation_heads (end_candidate_observation_id)
    WHERE end_candidate_observation_id IS NOT NULL;

