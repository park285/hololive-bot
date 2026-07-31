CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_reply_outbox_room_active
    ON bot_reply_outbox (room_id, id ASC)
    WHERE status IN (
        'pending', 'submitting', 'accepted', 'retryable_pre_dispatch', 'outcome_unknown'
    );
