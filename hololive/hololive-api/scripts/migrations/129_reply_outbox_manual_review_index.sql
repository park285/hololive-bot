CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_reply_outbox_manual_review_updated
    ON bot_reply_outbox (updated_at ASC, id ASC)
    WHERE status = 'manual_review';
