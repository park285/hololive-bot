-- 045_create_youtube_content_alarm_tracking.sql
-- 유튜브 커뮤니티/쇼츠 알람 시각 추적 저장소 생성

CREATE TABLE IF NOT EXISTS youtube_content_alarm_tracking (
    kind                VARCHAR(20) NOT NULL,
    content_id          VARCHAR(50) NOT NULL,
    channel_id          VARCHAR(50) NOT NULL,
    actual_published_at TIMESTAMPTZ,
    detected_at         TIMESTAMPTZ NOT NULL,
    alarm_sent_at       TIMESTAMPTZ,
    alarm_latency_millis BIGINT,
    alarm_latency_exceeded BOOLEAN,
    delivery_status     VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    latency_classification_status VARCHAR(40),
    delay_source        VARCHAR(40),
    internal_delay_cause VARCHAR(40),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (kind, content_id)
);

CREATE INDEX IF NOT EXISTS idx_ycat_detected_at ON youtube_content_alarm_tracking(detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_ycat_channel_detected ON youtube_content_alarm_tracking(channel_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_ycat_alarm_sent_at ON youtube_content_alarm_tracking(alarm_sent_at) WHERE alarm_sent_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ycat_delivery_status ON youtube_content_alarm_tracking(delivery_status, detected_at DESC);

COMMENT ON TABLE youtube_content_alarm_tracking IS '유튜브 커뮤니티/쇼츠 알람의 실제 게시, 감지, 게시물 기준 단일 발송 상태 추적';
COMMENT ON COLUMN youtube_content_alarm_tracking.actual_published_at IS '유튜브 실제 게시 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_content_alarm_tracking.detected_at IS '내부 시스템이 해당 게시물을 처음 감지한 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_content_alarm_tracking.alarm_sent_at IS '해당 게시물 알람이 최초 성공 발송된 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_content_alarm_tracking.alarm_latency_millis IS '실제 게시 시각부터 최초 성공 발송까지의 지연 시간(ms)';
COMMENT ON COLUMN youtube_content_alarm_tracking.alarm_latency_exceeded IS '실제 게시 시각 기준 지연이 2분을 초과했는지 여부';
COMMENT ON COLUMN youtube_content_alarm_tracking.delivery_status IS '게시물 기준 canonical 단일 발송 상태: PENDING/SENT';
COMMENT ON COLUMN youtube_content_alarm_tracking.latency_classification_status IS '게시물 지연 판정 상태: insufficient_evidence/within_target/exceeded';
COMMENT ON COLUMN youtube_content_alarm_tracking.delay_source IS '대표 지연 구간: none/external_collection/internal_delivery/mixed';
COMMENT ON COLUMN youtube_content_alarm_tracking.internal_delay_cause IS '대표 내부 지연 원인: none/queue_wait/retry_accumulation/job_failure';
