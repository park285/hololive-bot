-- 085_add_ycat_kind_content_index.sql
-- bulk mark-sent 경로가 youtube_content_alarm_tracking.content_id arm을 조인할 때
-- 070 이후 제거된 (kind, content_id) 선두 접근 경로를 복원한다.
-- apply-all.sh는 psql -f(파일 단위 autocommit, single-transaction 미사용)로 적용하므로 CONCURRENTLY를 쓸 수 있다.
-- 빌드 실패 잔재(invalid index)는 apply-all.sh가 ledger 기록 전에 감지·DROP하고 실패 처리하므로,
-- 재실행하면 이 파일이 다시 적용되어 인덱스가 재빌드된다(IF NOT EXISTS는 이름만 비교).

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ycat_kind_content
    ON youtube_content_alarm_tracking (kind, content_id);
