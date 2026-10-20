-- notification delivery handoff reconciler의 확인 예정 행 claim(handoff_claim_due.sql)용 부분 index다. 330이 추가한
-- 확인 대기 SENT 행만 담는다. CONCURRENTLY는 단일 autocommit 문장 migration이어야 한다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ndo_handoff_due
    ON public.notification_delivery_outbox (handoff_next_check_at, id)
    WHERE handoff_next_check_at IS NOT NULL;
