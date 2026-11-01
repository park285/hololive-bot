-- 채널 override를 키·채널·변형마다 하나로 제한한다. CONCURRENTLY는 BEGIN 블록 밖 단일 문장이다(CONVENTIONS).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ux_notification_templates_channel_format
    ON notification_templates (template_key, channel_id, format_variant) WHERE channel_id IS NOT NULL;
