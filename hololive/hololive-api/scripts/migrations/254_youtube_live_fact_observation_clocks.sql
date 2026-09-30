-- last_seen_at에는 예정 시각도 저장되므로 실제 최신 사실의 증거로 사용하지 않는다.
-- 기존 행의 관측 시각은 추정 backfill하지 않고, 각 owner writer가 새 사실과 함께 기록한다.
ALTER TABLE public.youtube_live_sessions
    ADD COLUMN IF NOT EXISTS status_observed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS schedule_observed_at TIMESTAMPTZ;
