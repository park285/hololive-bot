-- 048: 커뮤니티/쇼츠 발송 telemetry에 시도 시작/완료 시각 추가
ALTER TABLE youtube_notification_delivery_telemetry
    ADD COLUMN IF NOT EXISTS attempt_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS attempt_finished_at TIMESTAMPTZ;

UPDATE youtube_notification_delivery_telemetry
SET attempt_finished_at = COALESCE(attempt_finished_at, event_at)
WHERE attempt_finished_at IS NULL;

COMMENT ON COLUMN youtube_notification_delivery_telemetry.attempt_started_at IS 'room 단위 발송 시도가 실제로 시작된 시각 (delivery lock claim 시각)';
COMMENT ON COLUMN youtube_notification_delivery_telemetry.attempt_finished_at IS 'room 단위 발송 시도가 완료된 시각 (성공/실패 기록 시각)';
