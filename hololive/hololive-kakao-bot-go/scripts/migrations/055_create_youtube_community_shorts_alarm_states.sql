-- 055_create_youtube_community_shorts_alarm_states.sql
-- canonical post identifier 기준으로 community/shorts 게시물별 단일 알람 발송 상태를 보존한다.

CREATE TABLE IF NOT EXISTS youtube_community_shorts_alarm_states (
    kind                VARCHAR(20) NOT NULL,
    post_id             VARCHAR(50) NOT NULL,
    content_id          VARCHAR(50) NOT NULL,
    channel_id          VARCHAR(50) NOT NULL,
    actual_published_at TIMESTAMPTZ,
    detected_at         TIMESTAMPTZ NOT NULL,
    authorized_at       TIMESTAMPTZ,
    alarm_sent_at       TIMESTAMPTZ,
    delivery_status     VARCHAR(20) NOT NULL DEFAULT 'DETECTED',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (kind, post_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ycsas_kind_content
    ON youtube_community_shorts_alarm_states(kind, content_id);
CREATE INDEX IF NOT EXISTS idx_ycsas_detected_at
    ON youtube_community_shorts_alarm_states(detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_ycsas_channel_detected
    ON youtube_community_shorts_alarm_states(channel_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_ycsas_authorized_at
    ON youtube_community_shorts_alarm_states(authorized_at DESC)
    WHERE authorized_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ycsas_alarm_sent_at
    ON youtube_community_shorts_alarm_states(alarm_sent_at DESC)
    WHERE alarm_sent_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ycsas_delivery_status
    ON youtube_community_shorts_alarm_states(delivery_status, detected_at DESC);

WITH outbox_authorization AS (
    SELECT
        o.kind,
        o.content_id,
        MIN(o.created_at) AS authorized_at
    FROM youtube_notification_outbox AS o
    WHERE o.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
    GROUP BY o.kind, o.content_id
)
INSERT INTO youtube_community_shorts_alarm_states (
    kind,
    post_id,
    content_id,
    channel_id,
    actual_published_at,
    detected_at,
    authorized_at,
    alarm_sent_at,
    delivery_status,
    created_at,
    updated_at
)
SELECT
    t.kind,
    t.canonical_content_id AS post_id,
    t.content_id,
    t.channel_id,
    t.actual_published_at,
    t.detected_at,
    oa.authorized_at,
    t.alarm_sent_at,
    CASE
        WHEN t.alarm_sent_at IS NOT NULL THEN 'SENT'
        WHEN oa.authorized_at IS NOT NULL THEN 'ENQUEUED'
        ELSE 'DETECTED'
    END AS delivery_status,
    COALESCE(t.created_at, NOW()) AS created_at,
    COALESCE(t.updated_at, NOW()) AS updated_at
FROM youtube_content_alarm_tracking AS t
LEFT JOIN outbox_authorization AS oa
  ON oa.kind = t.kind
 AND oa.content_id = t.content_id
WHERE t.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
ON CONFLICT (kind, post_id) DO UPDATE
SET content_id = EXCLUDED.content_id,
    channel_id = EXCLUDED.channel_id,
    actual_published_at = COALESCE(EXCLUDED.actual_published_at, youtube_community_shorts_alarm_states.actual_published_at),
    detected_at = LEAST(youtube_community_shorts_alarm_states.detected_at, EXCLUDED.detected_at),
    authorized_at = CASE
        WHEN youtube_community_shorts_alarm_states.authorized_at IS NULL THEN EXCLUDED.authorized_at
        WHEN EXCLUDED.authorized_at IS NULL THEN youtube_community_shorts_alarm_states.authorized_at
        ELSE LEAST(youtube_community_shorts_alarm_states.authorized_at, EXCLUDED.authorized_at)
    END,
    alarm_sent_at = CASE
        WHEN youtube_community_shorts_alarm_states.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
        WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_community_shorts_alarm_states.alarm_sent_at
        ELSE LEAST(youtube_community_shorts_alarm_states.alarm_sent_at, EXCLUDED.alarm_sent_at)
    END,
    delivery_status = CASE
        WHEN CASE
            WHEN youtube_community_shorts_alarm_states.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
            WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_community_shorts_alarm_states.alarm_sent_at
            ELSE LEAST(youtube_community_shorts_alarm_states.alarm_sent_at, EXCLUDED.alarm_sent_at)
        END IS NOT NULL THEN 'SENT'
        WHEN CASE
            WHEN youtube_community_shorts_alarm_states.authorized_at IS NULL THEN EXCLUDED.authorized_at
            WHEN EXCLUDED.authorized_at IS NULL THEN youtube_community_shorts_alarm_states.authorized_at
            ELSE LEAST(youtube_community_shorts_alarm_states.authorized_at, EXCLUDED.authorized_at)
        END IS NOT NULL THEN 'ENQUEUED'
        ELSE 'DETECTED'
    END,
    updated_at = GREATEST(youtube_community_shorts_alarm_states.updated_at, EXCLUDED.updated_at);

COMMENT ON TABLE youtube_community_shorts_alarm_states IS '유튜브 community/shorts 게시물별 단일 알람 발송 상태';
COMMENT ON COLUMN youtube_community_shorts_alarm_states.post_id IS '게시물당 정확히 1개의 상태 레코드만 허용하는 canonical post identifier';
COMMENT ON COLUMN youtube_community_shorts_alarm_states.content_id IS '현재 outbox/tracking과 조인되는 게시물 content identity';
COMMENT ON COLUMN youtube_community_shorts_alarm_states.authorized_at IS '게시물이 실제 알람 전송 경로로 진입한 최초 시각';
COMMENT ON COLUMN youtube_community_shorts_alarm_states.alarm_sent_at IS '게시물의 최초 성공 발송 시각';
COMMENT ON COLUMN youtube_community_shorts_alarm_states.delivery_status IS 'DETECTED, ENQUEUED, SENT 중 하나의 게시물 상태';
