CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alarm_upcoming_candidates_pending
    ON alarm_upcoming_candidates (checked_at, dedupe_key) WHERE outcome = 'pending';
