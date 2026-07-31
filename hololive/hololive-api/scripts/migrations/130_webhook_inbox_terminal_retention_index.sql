CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_webhook_inbox_terminal_updated
    ON bot_webhook_inbox (updated_at ASC, id ASC)
    WHERE status IN ('dead', 'succeeded');
