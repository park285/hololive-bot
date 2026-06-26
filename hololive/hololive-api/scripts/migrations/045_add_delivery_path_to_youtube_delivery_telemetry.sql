-- 045: 커뮤니티/쇼츠 발송 경로 추적 필드 추가
ALTER TABLE youtube_notification_delivery_telemetry
    ADD COLUMN IF NOT EXISTS delivery_path VARCHAR(100);

UPDATE youtube_notification_delivery_telemetry
SET delivery_path = 'youtube_outbox_dispatcher'
WHERE COALESCE(delivery_path, '') = '';

ALTER TABLE youtube_notification_delivery_telemetry
    ALTER COLUMN delivery_path SET DEFAULT 'youtube_outbox_dispatcher';

ALTER TABLE youtube_notification_delivery_telemetry
    ALTER COLUMN delivery_path SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ydt_channel_path_event
    ON youtube_notification_delivery_telemetry(channel_id, delivery_path, event_at);
