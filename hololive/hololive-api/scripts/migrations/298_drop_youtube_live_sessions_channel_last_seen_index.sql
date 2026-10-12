-- 294의 idx_yls_channel이 채널 조건을 처리한다. last_seen_at key가 upsert 갱신마다 바뀌어 HOT 갱신을 막는다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_yls_channel_last_seen;
