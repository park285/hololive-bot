-- 047: YouTube 커뮤니티/쇼츠 발송 텔레메트리에 canonical post_id 적재
ALTER TABLE youtube_notification_delivery_telemetry
    ADD COLUMN IF NOT EXISTS post_id VARCHAR(50);

UPDATE youtube_notification_delivery_telemetry AS t
SET post_id = COALESCE(
    NULLIF(BTRIM(o.payload ->> 'canonical_post_id'), ''),
    NULLIF(BTRIM(o.payload ->> 'post_id'), ''),
    NULLIF(BTRIM(o.payload ->> 'video_id'), ''),
    NULLIF(BTRIM(o.content_id), ''),
    NULLIF(BTRIM(t.content_id), '')
)
FROM youtube_notification_outbox AS o
WHERE t.outbox_id = o.id
  AND COALESCE(BTRIM(t.post_id), '') = '';

UPDATE youtube_notification_delivery_telemetry
SET post_id = NULLIF(BTRIM(content_id), '')
WHERE COALESCE(BTRIM(post_id), '') = '';

ALTER TABLE youtube_notification_delivery_telemetry
    ALTER COLUMN post_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ydt_post_event
    ON youtube_notification_delivery_telemetry(post_id, event_at);
