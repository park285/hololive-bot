-- 071_drop_ycsas_published_at_resolution_artifacts.sql
-- published_at resolver 제거 후 남은 inert column과 partial index를 정리한다.

DROP INDEX IF EXISTS idx_ycsas_pending_published_at_resolution;
DROP INDEX IF EXISTS idx_ycsas_pending_published_at_retry_after;
ALTER TABLE youtube_community_shorts_alarm_states DROP COLUMN IF EXISTS published_at_retry_after;
