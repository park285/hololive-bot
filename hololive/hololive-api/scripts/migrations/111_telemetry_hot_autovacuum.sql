-- 111_telemetry_hot_autovacuum.sql
-- 목적: 소비 UPDATE가 predicate 컬럼(logged_at)을 채워 전부 non-HOT 갱신이 되는
-- youtube_notification_delivery_telemetry의 dead tuple 회수와 통계 갱신 지연을 줄인다(066/084 패턴).
-- 주의: 데이터는 변경하지 않고 table storage parameter만 조정한다.

ALTER TABLE IF EXISTS youtube_notification_delivery_telemetry SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);
