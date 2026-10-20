-- alarm-dispatch handoff reconciler의 확인 예정 단위 claim(handoff_claim_due.sql)용 부분 index다. 329가 추가한 확인
-- 대기 단위만 담는다. CONCURRENTLY는 단일 autocommit 문장 migration이어야 한다. 329와 같이 search_path 기준 이름을
-- 써서 schema 격리 integration 시험도 이 파일을 그대로 재생한다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alarm_dispatch_send_units_handoff_due
    ON alarm_dispatch_send_units (handoff_next_check_at, id)
    WHERE handoff_next_check_at IS NOT NULL;
