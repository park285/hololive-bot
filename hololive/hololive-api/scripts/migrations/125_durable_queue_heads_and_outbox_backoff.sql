ALTER TABLE bot_reply_outbox
    ADD COLUMN IF NOT EXISTS available_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE TABLE IF NOT EXISTS bot_webhook_heads (
    ordering_key TEXT PRIMARY KEY,
    message_id TEXT NOT NULL UNIQUE REFERENCES bot_webhook_inbox(message_id) ON DELETE CASCADE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_bot_webhook_heads_ordering_key_size
        CHECK (length(ordering_key) > 0 AND length(ordering_key) <= 512)
);

INSERT INTO bot_webhook_heads (ordering_key, message_id)
SELECT DISTINCT ON (ordering_key) ordering_key, message_id
FROM bot_webhook_inbox
WHERE status IN ('pending', 'processing', 'retry')
ORDER BY ordering_key, id
ON CONFLICT (ordering_key) DO NOTHING;

COMMENT ON TABLE bot_webhook_heads IS 'Transactionally maintained claimable head for each webhook ordering key.';
