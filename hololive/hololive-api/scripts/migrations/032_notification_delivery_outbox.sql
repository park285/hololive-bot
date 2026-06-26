CREATE TABLE IF NOT EXISTS notification_delivery_outbox (
    id BIGSERIAL PRIMARY KEY,
    kind VARCHAR(30) NOT NULL,
    period_key VARCHAR(20) NOT NULL,
    room_id VARCHAR(100) NOT NULL,
    content_id VARCHAR(200) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    attempt_count INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    sent_at TIMESTAMPTZ,
    error TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ndo_kind_content ON notification_delivery_outbox(kind, content_id);
CREATE INDEX IF NOT EXISTS idx_ndo_pending_next ON notification_delivery_outbox(next_attempt_at, created_at) WHERE status = 'PENDING';
CREATE INDEX IF NOT EXISTS idx_ndo_sent_cleanup ON notification_delivery_outbox(COALESCE(sent_at, created_at)) WHERE status IN ('SENT', 'FAILED');
