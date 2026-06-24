-- 072_drop_community_shorts_observation_tables.sql
-- observation-window 리포트 페이드아웃 후 orphaned baselines 테이블을 정리한다. windows 테이블은 deferred delivery-telemetry 보강이 아직 읽으므로 후속 정리로 남긴다.

DROP TABLE IF EXISTS youtube_community_shorts_observation_post_baselines;
