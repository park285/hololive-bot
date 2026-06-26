-- 066_pg18_db_usage_hot_table_autovacuum.sql
-- 목적: 상태 전환/lock 갱신이 잦은 hot table의 dead tuple 회수와 통계 갱신 지연을 줄인다.
-- 주의: 데이터는 변경하지 않고 table storage parameter만 조정한다.

ALTER TABLE IF EXISTS alarm_dispatch_deliveries SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);

ALTER TABLE IF EXISTS youtube_notification_outbox SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);

ALTER TABLE IF EXISTS youtube_notification_delivery SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);

ALTER TABLE IF EXISTS youtube_content_alarm_tracking SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_vacuum_threshold = 100,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 100
);
