-- 334의 submitting 전용 index가 만료 lease reclaim을 맡는다. accepted까지 담던 이전 lease index를 지운다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_bot_reply_outbox_lease_expiry;
