CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_reply_outbox_due_available
    ON bot_reply_outbox (available_at ASC, id ASC)
    WHERE status IN ('pending', 'retryable_pre_dispatch', 'outcome_unknown');
