CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alarm_upcoming_candidates_terminal
    ON alarm_upcoming_candidates (terminal_at) WHERE terminal_at IS NOT NULL;
