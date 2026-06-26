-- 044: 커뮤니티/쇼츠 발송 감사 로그 영속 버퍼
CREATE TABLE IF NOT EXISTS youtube_notification_delivery_telemetry (
    id              BIGSERIAL PRIMARY KEY,
    delivery_id     BIGINT NOT NULL,
    attempt_ordinal INT NOT NULL,
    outbox_id       BIGINT NOT NULL REFERENCES youtube_notification_outbox(id) ON DELETE CASCADE,
    channel_id      VARCHAR(50) NOT NULL,
    content_id      VARCHAR(50) NOT NULL,
    room_id         VARCHAR(100) NOT NULL,
    alarm_type      VARCHAR(20) NOT NULL,
    dedupe_key      VARCHAR(200) NOT NULL,
    delivery_mode   VARCHAR(20) NOT NULL,
    send_result     VARCHAR(20) NOT NULL,
    failure_reason  VARCHAR(100),
    event_at        TIMESTAMPTZ NOT NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at       TIMESTAMPTZ,
    logged_at       TIMESTAMPTZ,
    error           TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ydt_delivery_attempt
    ON youtube_notification_delivery_telemetry(delivery_id, attempt_ordinal);

CREATE INDEX IF NOT EXISTS idx_ydt_pending_next
    ON youtube_notification_delivery_telemetry(next_attempt_at, event_at)
    WHERE logged_at IS NULL;
