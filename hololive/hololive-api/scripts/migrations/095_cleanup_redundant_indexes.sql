-- 095_cleanup_redundant_indexes.sql
-- PG18 사용 패턴 리뷰 후 hot-path 인덱스를 정리한다.
-- apply-all.sh는 파일을 statement 단위 autocommit으로 실행하므로 CONCURRENTLY를 쓸 수 있다.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ydt_outbox
    ON youtube_notification_delivery_telemetry (outbox_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_milestones_unnotified_achieved_at
    ON youtube_milestones (achieved_at DESC)
    WHERE notified = false;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_changes_unnotified_detected_at
    ON youtube_stats_changes (detected_at DESC)
    WHERE notified = false;

DROP INDEX CONCURRENTLY IF EXISTS idx_members_org;
DROP INDEX CONCURRENTLY IF EXISTS idx_milestones_lookup;
DROP INDEX CONCURRENTLY IF EXISTS idx_youtube_stats_history_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_ycss_channel_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_yno_status_next_attempt;
DROP INDEX CONCURRENTLY IF EXISTS idx_milestones_unnotified;
DROP INDEX CONCURRENTLY IF EXISTS idx_changes_unnotified;
