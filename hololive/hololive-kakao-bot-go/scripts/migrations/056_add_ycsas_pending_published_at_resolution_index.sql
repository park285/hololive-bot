-- 056_add_ycsas_pending_published_at_resolution_index.sql
-- pending published_at resolver keyset scan을 위한 partial index를 추가한다.

CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_resolution
    ON youtube_community_shorts_alarm_states (detected_at ASC, post_id ASC)
    WHERE actual_published_at IS NULL
      AND alarm_sent_at IS NULL
      AND authorized_at IS NULL
      AND kind IN ('COMMUNITY_POST', 'NEW_SHORT');
