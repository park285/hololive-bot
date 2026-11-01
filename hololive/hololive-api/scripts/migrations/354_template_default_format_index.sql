-- 전역 기본 본문을 키·변형마다 하나로 제한한다. CONCURRENTLY는 BEGIN 블록 밖 단일 문장이다(CONVENTIONS).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ux_notification_templates_default_format
    ON notification_templates (template_key, format_variant) WHERE channel_id IS NULL;
