-- 051: 커뮤니티/쇼츠 delivery telemetry에 canonical alarm timing 필드 추가
ALTER TABLE youtube_notification_delivery_telemetry
    ADD COLUMN IF NOT EXISTS alarm_sent_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS alarm_latency_millis BIGINT;

WITH timing_match AS (
    SELECT
        t.id AS telemetry_id,
        track.alarm_sent_at AS alarm_sent_at,
        track.alarm_latency_millis AS alarm_latency_millis
    FROM youtube_notification_delivery_telemetry AS t
    LEFT JOIN youtube_notification_outbox AS o
        ON o.id = t.outbox_id
    LEFT JOIN youtube_content_alarm_tracking AS track
        ON track.kind = o.kind
       AND track.content_id = o.content_id
)
UPDATE youtube_notification_delivery_telemetry AS t
SET alarm_sent_at = m.alarm_sent_at,
    alarm_latency_millis = m.alarm_latency_millis
FROM timing_match AS m
WHERE m.telemetry_id = t.id;

COMMENT ON COLUMN youtube_notification_delivery_telemetry.alarm_sent_at IS '게시물 기준 canonical 최초 성공 발송 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_notification_delivery_telemetry.alarm_latency_millis IS '실제 게시 시각부터 canonical 최초 성공 발송까지의 지연 시간(ms)';
