-- 052_add_delivery_status_to_youtube_content_alarm_tracking.sql
-- 유튜브 커뮤니티/쇼츠 게시물 단위 단일 발송 상태와 지연 분류 컬럼을 보강

ALTER TABLE youtube_content_alarm_tracking
    ADD COLUMN IF NOT EXISTS delivery_status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    ADD COLUMN IF NOT EXISTS latency_classification_status VARCHAR(40),
    ADD COLUMN IF NOT EXISTS delay_source VARCHAR(40),
    ADD COLUMN IF NOT EXISTS internal_delay_cause VARCHAR(40);

UPDATE youtube_content_alarm_tracking
SET delivery_status = CASE
        WHEN alarm_sent_at IS NULL THEN 'PENDING'
        ELSE 'SENT'
    END
WHERE delivery_status IS NULL
   OR delivery_status NOT IN ('PENDING', 'SENT');

CREATE INDEX IF NOT EXISTS idx_ycat_delivery_status
    ON youtube_content_alarm_tracking(delivery_status, detected_at DESC);

COMMENT ON COLUMN youtube_content_alarm_tracking.delivery_status IS '게시물 기준 canonical 단일 발송 상태: PENDING/SENT';
COMMENT ON COLUMN youtube_content_alarm_tracking.latency_classification_status IS '게시물 지연 판정 상태: insufficient_evidence/within_target/exceeded';
COMMENT ON COLUMN youtube_content_alarm_tracking.delay_source IS '대표 지연 구간: none/external_collection/internal_delivery/mixed';
COMMENT ON COLUMN youtube_content_alarm_tracking.internal_delay_cause IS '대표 내부 지연 원인: none/queue_wait/retry_accumulation/job_failure';
