-- 051_add_closed_at_to_youtube_community_shorts_observation_windows.sql
-- 관찰 구간 종료 시점에 닫힘 메타데이터를 추가하고 종료된 창을 즉시 닫힌 상태로 backfill

ALTER TABLE youtube_community_shorts_observation_windows
    ADD COLUMN IF NOT EXISTS closed_at TIMESTAMPTZ;

UPDATE youtube_community_shorts_observation_windows
SET closed_at = observation_ended_at
WHERE closed_at IS NULL
  AND observation_ended_at <= NOW();

CREATE INDEX IF NOT EXISTS idx_ycsow_closed_at
    ON youtube_community_shorts_observation_windows(closed_at DESC);

COMMENT ON COLUMN youtube_community_shorts_observation_windows.closed_at IS '관찰 구간 종료 시점에 닫힌 기준 시각 (UTC canonical, observation_ended_at와 동일)';
