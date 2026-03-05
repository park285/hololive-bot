-- PR-2A: room 단위 전달 상태 추적 테이블
CREATE TABLE IF NOT EXISTS youtube_notification_delivery (
    id              BIGSERIAL PRIMARY KEY,
    outbox_id       BIGINT NOT NULL REFERENCES youtube_notification_outbox(id) ON DELETE CASCADE,
    room_id         VARCHAR(100) NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    attempt_count   INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at       TIMESTAMPTZ,
    sent_at         TIMESTAMPTZ,
    error           TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ynd_outbox_room
    ON youtube_notification_delivery(outbox_id, room_id);

CREATE INDEX IF NOT EXISTS idx_ynd_pending_next
    ON youtube_notification_delivery(next_attempt_at, created_at)
    WHERE status = 'PENDING';

CREATE INDEX IF NOT EXISTS idx_ynd_sent_cleanup
    ON youtube_notification_delivery(COALESCE(sent_at, created_at))
    WHERE status IN ('SENT', 'FAILED');
