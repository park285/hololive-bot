-- 057_add_ycsas_published_at_retry_after.sql
-- pending published_at resolver retry backoff를 DB-visible 상태로 관리한다.

ALTER TABLE youtube_community_shorts_alarm_states
    ADD COLUMN IF NOT EXISTS published_at_retry_after TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_retry_after
    ON youtube_community_shorts_alarm_states (published_at_retry_after ASC, detected_at ASC, post_id ASC)
    WHERE actual_published_at IS NULL
      AND alarm_sent_at IS NULL
      AND authorized_at IS NULL
      AND kind IN ('COMMUNITY_POST', 'NEW_SHORT');
