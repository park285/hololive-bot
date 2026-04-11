-- 046_add_youtube_content_alarm_latency_result.sql
-- 유튜브 커뮤니티/쇼츠 알람의 게시물별 지연 수치와 2분 초과 판정 저장

ALTER TABLE youtube_content_alarm_tracking
    ADD COLUMN IF NOT EXISTS alarm_latency_millis BIGINT,
    ADD COLUMN IF NOT EXISTS alarm_latency_exceeded BOOLEAN;

UPDATE youtube_content_alarm_tracking
SET alarm_latency_millis = CASE
        WHEN actual_published_at IS NULL OR alarm_sent_at IS NULL THEN NULL
        ELSE CAST(ROUND(EXTRACT(EPOCH FROM (alarm_sent_at - actual_published_at)) * 1000) AS BIGINT)
    END,
    alarm_latency_exceeded = CASE
        WHEN actual_published_at IS NULL OR alarm_sent_at IS NULL THEN NULL
        WHEN CAST(ROUND(EXTRACT(EPOCH FROM (alarm_sent_at - actual_published_at)) * 1000) AS BIGINT) > 120000 THEN TRUE
        ELSE FALSE
    END
WHERE alarm_latency_millis IS NULL OR alarm_latency_exceeded IS NULL;

COMMENT ON COLUMN youtube_content_alarm_tracking.alarm_latency_millis IS '실제 게시 시각부터 최초 성공 발송까지의 지연 시간(ms)';
COMMENT ON COLUMN youtube_content_alarm_tracking.alarm_latency_exceeded IS '실제 게시 시각 기준 지연이 2분을 초과했는지 여부';
