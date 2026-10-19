-- 봇 handoff reconciler의 확인 예정 행 claim(reply_outbox_handoff_claim.sql)용 부분 index다. 326이 추가한 lease 없는
-- accepted 행만 담는다. CONCURRENTLY는 단일 autocommit 문장 migration이어야 한다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_reply_outbox_handoff_due
    ON public.bot_reply_outbox (handoff_next_check_at, id)
    WHERE status = 'accepted' AND claim_token IS NULL;
