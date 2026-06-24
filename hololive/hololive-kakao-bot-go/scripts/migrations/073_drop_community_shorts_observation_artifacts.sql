-- 073_drop_community_shorts_observation_artifacts.sql
-- observation-window enrichment 전면 폐기에 따라 delivery-telemetry observation 컬럼/인덱스와 windows 테이블을 정리한다.

DROP INDEX IF EXISTS idx_ydt_observation_window_event;
DROP INDEX IF EXISTS idx_ydt_observation_status_event;

ALTER TABLE youtube_notification_delivery_telemetry
    DROP COLUMN IF EXISTS observation_status,
    DROP COLUMN IF EXISTS observation_runtime_name,
    DROP COLUMN IF EXISTS observation_bigbang_cutover_at,
    DROP COLUMN IF EXISTS observation_started_at,
    DROP COLUMN IF EXISTS observation_ended_at;

DROP TABLE IF EXISTS youtube_community_shorts_observation_windows;
