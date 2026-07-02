-- 084_extend_hot_outbox_autovacuum.sql
-- 목적: 066에서 누락된 hot outbox/state 테이블의 dead tuple 회수와 통계 갱신 지연을 줄인다.
-- 주의: 데이터는 변경하지 않고 table storage parameter만 조정한다.

ALTER TABLE IF EXISTS notification_delivery_outbox SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);

ALTER TABLE IF EXISTS youtube_community_shorts_alarm_states SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);
