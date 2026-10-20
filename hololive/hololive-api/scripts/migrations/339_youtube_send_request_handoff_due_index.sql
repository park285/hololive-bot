-- youtube handoff reconciler의 확인 예정 요청 claim(handoff_claim_due.sql)용 부분 index다. 336이 추가한 확인 대기
-- 요청만 담는다. CONCURRENTLY는 단일 autocommit 문장 migration이어야 한다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_send_request_handoff_due
    ON public.youtube_notification_send_request (handoff_next_check_at, base_id)
    WHERE handoff_next_check_at IS NOT NULL;
