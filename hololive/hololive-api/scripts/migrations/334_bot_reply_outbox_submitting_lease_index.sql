-- 만료 lease reclaim(reply_outbox_reclaim_candidates.sql)용 부분 index다. 332 이후 lease는 submitting만 가지므로
-- accepted를 담던 idx_bot_reply_outbox_lease_expiry를 대신한다. CONCURRENTLY는 단일 autocommit 문장 migration이어야 한다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_reply_outbox_submitting_lease_expiry
    ON public.bot_reply_outbox (lease_until, id)
    WHERE status = 'submitting';
