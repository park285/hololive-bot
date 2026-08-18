CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_reply_outbox_discarded_updated
    ON bot_reply_outbox (updated_at ASC, id ASC)
    WHERE status = 'discarded';
