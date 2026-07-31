CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_reply_outbox_terminal_updated
    ON bot_reply_outbox (updated_at ASC, id ASC)
    WHERE status IN ('handoff_completed', 'dead', 'permanent_conflict');
